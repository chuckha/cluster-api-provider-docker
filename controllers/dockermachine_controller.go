/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"time"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	infrastructurev1alpha1 "sigs.k8s.io/cluster-api-provider-docker/api/v1alpha1"
	"sigs.k8s.io/cluster-api-provider-docker/kind/actions"
	capiv1alpha2 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha2"
	"sigs.k8s.io/cluster-api/pkg/util"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
	kindcluster "sigs.k8s.io/kind/pkg/cluster"
	"sigs.k8s.io/kind/pkg/cluster/constants"
	"sigs.k8s.io/kind/pkg/cluster/nodes"
)

const (
	containerRunningStatus         = "running"
	clusterAPIControlPlaneSetLabel = "controlplane"
)

// DockerMachineReconciler reconciles a DockerMachine object
type DockerMachineReconciler struct {
	client.Client
	Log logr.Logger
}

var (
	machineKind = capiv1alpha2.SchemeGroupVersion.WithKind("Machine").String()
)

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=dockermachines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=dockermachines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;machines,verbs=get;list;watch

// Reconcile handles DockerMachine events
func (r *DockerMachineReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("dockermachine", req.NamespacedName)

	dockerMachine := &infrastructurev1alpha1.DockerMachine{}
	if err := r.Client.Get(ctx, req.NamespacedName, dockerMachine); err != nil {
		log.Error(err, "failed to get dockerMachine")
		return ctrl.Result{}, err
	}

	machine := &capiv1alpha2.Machine{}
	// Find the owner reference
	var machineRef *metav1.OwnerReference
	for _, ref := range dockerMachine.OwnerReferences {
		if fmt.Sprintf("%s, Kind=%s", ref.APIVersion, ref.Kind) == machineKind {
			machineRef = &ref
			break
		}
	}
	if machineRef == nil {
		log.Info("did not find matching machine reference, requeuing")
		return ctrl.Result{Requeue: true}, nil
	}

	machineKey := client.ObjectKey{
		Namespace: dockerMachine.Namespace,
		Name:      machineRef.Name,
	}
	if err := r.Client.Get(ctx, machineKey, machine); err != nil {
		log.Error(err, "failed to fetch machine", "machinekey", machineKey)
		return ctrl.Result{}, err
	}

	if machine.Labels[capiv1alpha2.MachineClusterLabelName] == "" {
		return ctrl.Result{}, errors.New("machine has no associated cluster")
	}

	if machine.Spec.Bootstrap.Data == nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Get the cluster
	cluster := &capiv1alpha2.Cluster{}
	clusterKey := client.ObjectKey{
		Namespace: req.Namespace,
		Name:      machine.Labels[capiv1alpha2.MachineClusterLabelName],
	}
	if err := r.Get(ctx, clusterKey, cluster); err != nil {
		log.Error(err, "failed to get cluster")
		return ctrl.Result{}, errors.WithStack(err)
	}

	// lookup to see if the cluster has been created
	clusterExists, err := kindcluster.IsKnown(cluster.Name)
	if err != nil {
		log.Error(err, "failed to determine existence of cluster")
		return ctrl.Result{}, errors.WithStack(err)
	}
	// create the cluster if it doesn't exist
	if !clusterExists {
		if err := r.createCluster(cluster); err != nil {
			log.Error(err, "failed to create cluster")
			return ctrl.Result{}, errors.WithStack(err)
		}
	}

	// now create the machine
	_ = machine.DeepCopy()
	log.Info("Creating a machine for cluster", "cluster-name", cluster.Name)

	controlPlanes, err := actions.ListControlPlanes(cluster.Name)
	if err != nil {
		log.Error(err, "Error listing control planes")
		return ctrl.Result{}, err
	}
	log.Info("Is there a cluster?", "cluster-exists", clusterExists)
	setValue := getRole(machine)
	log.Info("This node has a role", "role", setValue)
	if setValue == clusterAPIControlPlaneSetLabel {
		if len(controlPlanes) > 0 {
			log.Info("Adding a control plane")
			controlPlaneNode, err := actions.AddControlPlane(cluster.Name, machine.GetName(), *machine.Spec.Version)
			if err != nil {
				log.Error(err, "Error adding control plane")
				return ctrl.Result{}, err
			}
			providerID := actions.ProviderID(controlPlaneNode.Name())
			machine.Spec.ProviderID = &providerID
			return ctrl.Result{}, nil
		}

		log.Info("Creating a brand new cluster")
		elb, err := getExternalLoadBalancerNode(cluster.Name, log)
		if err != nil {
			log.Error(err, "Error getting external load balancer node")
			return ctrl.Result{}, err
		}
		lbipv4, _, err := elb.IP()
		if err != nil {
			log.Error(err, "Error getting node IP address")
			return ctrl.Result{}, err
		}
		controlPlaneNode, err := actions.CreateControlPlane(cluster.Name, machine.GetName(), lbipv4, *machine.Spec.Version, nil)
		if err != nil {
			log.Error(err, "Error creating control plane")
			return ctrl.Result{}, err
		}
		// set the machine's providerID
		providerID := actions.ProviderID(controlPlaneNode.Name())
		machine.Spec.ProviderID = &providerID

		s, err := kubeconfigToSecret(cluster.Name, cluster.Namespace)
		if err != nil {
			log.Error(err, "Error converting kubeconfig to a secret")
			return ctrl.Result{}, err
		}
		// Save the secret to the management cluster
		if err := r.Client.Create(ctx, s); err != nil {
			log.Error(err, "Error saving secret to management cluster")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// If there are no control plane then we should hold off on joining workers
	if len(controlPlanes) == 0 {
		log.Info("Sending machine back since there is no cluster to join", "machine", machine.Name)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	log.Info("Creating a new worker node")
	worker, err := actions.AddWorker(cluster.Name, machine.GetName(), *machine.Spec.Version)
	if err != nil {
		log.Error(err, "Error creating new worker node")
		return ctrl.Result{}, err
	}
	providerID := actions.ProviderID(worker.Name())
	machine.Spec.ProviderID = &providerID
	return ctrl.Result{}, nil
}

func (r *DockerMachineReconciler) createCluster(cluster *capiv1alpha2.Cluster) error {
	r.Log.Info("Reconciling cluster", "cluster-namespace", cluster.Namespace, "cluster-name", cluster.Name)
	elb, err := getExternalLoadBalancerNode(cluster.Name, r.Log)
	if err != nil {
		r.Log.Error(err, "Error getting external load balancer node")
		return err
	}
	if elb != nil {
		r.Log.Info("External Load Balancer already exists. Nothing to do for this cluster.")
		return nil
	}
	r.Log.Info("Cluster has been created! Setting up some infrastructure", "cluster-name", cluster.Name)
	_, err = actions.SetUpLoadBalancer(cluster.Name)
	return err
}

func getExternalLoadBalancerNode(clusterName string, log logr.Logger) (*nodes.Node, error) {
	log.Info("Getting external load balancer node for cluster", "cluster-name", clusterName)
	elb, err := nodes.List(
		fmt.Sprintf("label=%s=%s", constants.NodeRoleKey, constants.ExternalLoadBalancerNodeRoleValue),
		fmt.Sprintf("label=%s=%s", constants.ClusterLabelKey, clusterName),
		fmt.Sprintf("status=%s", containerRunningStatus),
	)
	if err != nil {
		return nil, err
	}
	if len(elb) == 0 {
		return nil, nil
	}
	if len(elb) > 1 {
		return nil, errors.New("too many external load balancers")
	}
	log.Info("External loadbalancer node for cluster", "cluster-name", clusterName, "elb", elb[0])
	return &elb[0], nil
}

// SetupWithManager will add watches for this controller
func (r *DockerMachineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructurev1alpha1.DockerMachine{}).
		Watches(
			&source.Kind{Type: &infrastructurev1alpha1.DockerMachine{}},
			&handler.EnqueueRequestsFromMapFunc{
				ToRequests: util.MachineToInfrastructureMapFunc(capiv1alpha2.SchemeGroupVersion.WithKind("Machine")),
			},
		).
		Complete(r)
}

func getRole(machine *capiv1alpha2.Machine) string {
	// Figure out what kind of node we're making
	labels := machine.GetLabels()
	setValue, ok := labels["set"]
	if !ok {
		setValue = constants.WorkerNodeRoleValue
	}
	return setValue
}

func kubeconfigToSecret(clusterName, namespace string) (*v1.Secret, error) {
	// open kubeconfig file
	data, err := ioutil.ReadFile(actions.KubeConfigPath(clusterName))
	if err != nil {
		return nil, errors.WithStack(err)
	}

	allNodes, err := nodes.List(fmt.Sprintf("label=%s=%s", constants.ClusterLabelKey, clusterName))
	if err != nil {
		return nil, errors.WithStack(err)
	}

	// TODO: Clean this up at some point
	// The Management cluster, running the NodeRef controller, needs to talk to the child clusters.
	// The management cluster and child cluster must communicate over DockerIP address/ports.
	// The load balancer listens on <docker_ip>:6443 and exposes a port on the host at some random open port.
	// Any traffic directed to the nginx container will get round-robined to a control plane node in the cluster.
	// Since the NodeRef controller is running inside a container, it must reference the child cluster load balancer
	// host by using the Docker IP address and port 6443, but us, running on the docker host, must use the localhost
	// and random port the LB is exposing to our system.
	// Right now the secret that contains the kubeconfig will work only for the node ref controller. In order for *us*
	// to interact with the child clusters via kubeconfig we must take the secret uploaded,
	// rewrite the kube-apiserver-address to be 127.0.0.1:<randomly-assigned-by-docker-port>.
	// It's not perfect but it works to at least play with cluster-api v0.1.4
	lbip, _, err := actions.GetLoadBalancerHostAndPort(allNodes)
	lines := bytes.Split(data, []byte("\n"))
	for i, line := range lines {
		if bytes.Contains(line, []byte("https://")) {
			lines[i] = []byte(fmt.Sprintf("    server: https://%s:%d", lbip, 6443))
		}
	}

	// write it to a secret
	return &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			// TODO pull in the kubeconfig secret function from cluster api
			Name:      fmt.Sprintf("%s-kubeconfig", clusterName),
			Namespace: namespace,
		},
		Data: map[string][]byte{
			// TODO pull in constant from cluster api
			"value": bytes.Join(lines, []byte("\n")),
		},
	}, nil
}
