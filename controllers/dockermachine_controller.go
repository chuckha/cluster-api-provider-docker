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
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
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
	containerRunningStatus = "running"
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
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines;clusters,verbs=get;list;watch

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

	log.Info("an informational log", "dm", dockerMachine)

	log.Info("a new log for a new demo")

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
