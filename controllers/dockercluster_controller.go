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
	"github.com/chuckha/cluster-api-lib/reconciler"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	infrav1 "sigs.k8s.io/cluster-api-provider-docker/api/v1alpha2"
	"sigs.k8s.io/cluster-api-provider-docker/docker"
	"sigs.k8s.io/cluster-api-provider-docker/third_party/forked/loadbalancer"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha2"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=dockerclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=dockerclusters/status,verbs=get;update;patch

type SetupWithMangerer interface {
	SetupWithManager(ctrl.Manager, controller.Options) error
}

type ReconcilerSetup interface {
	reconcile.Reconciler
	SetupWithMangerer
}

// VirtualClusterReconciler reconciles a VirtualCluster object
type DockerClusterReconciler struct {
	client.Client
	Log logr.Logger

	elbHelper *docker.LoadBalancer
	patchHelper *patch.Helper
}

// +kubebuilder:rbac:groups=infra.cluster.x-k8s.io,resources=virtualclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infra.cluster.x-k8s.io,resources=virtualclusters/status,verbs=get;update;patch

func NewDockerClusterReconciler(client client.Client, log logr.Logger) ReconcilerSetup {
	return reconciler.NewInfrastructureReconciler(&DockerClusterReconciler{
		Client: client,
		Log:    log,
	})
}

func (dcr *DockerClusterReconciler) GetLog() logr.Logger {
	return dcr.Log
}
func (dcr *DockerClusterReconciler) GetClient() client.Client {
	return dcr.Client
}
func (dcr *DockerClusterReconciler) GetCluster() reconciler.Cluster {
	return &infrav1.DockerCluster{}
}
func (dcr *DockerClusterReconciler) GetClusterFinalizer() string {
	return infrav1.ClusterFinalizer
}

func (dcr *DockerClusterReconciler) Setup(infracluster reconciler.Cluster, cluster *clusterv1.Cluster) error {
	ph, err := patch.NewHelper(cluster, dcr.Client)
	if err != nil {
		return err
	}
	dcr.patchHelper = ph

	// Create a helper for managing a docker container hosting the loadbalancer.
	externalLoadBalancer, err := docker.NewLoadBalancer(infracluster.GetName(), dcr.Log)
	if err != nil {
		return err
	}
	dcr.elbHelper = externalLoadBalancer
	return nil
}

func (dcr *DockerClusterReconciler) Delete(cluster reconciler.Cluster) error {
	// Delete the docker container hosting the load balancer
	if err := dcr.elbHelper.Delete(); err != nil {
		return err
	}

	return nil
}

func (dcr *DockerClusterReconciler) Create(cluster reconciler.Cluster) error {
	// Create the docker container hosting the load balancer
	if err := dcr.elbHelper.Create(); err != nil {
		return err
	}

	// Set APIEndpoints with the load balancer IP so the Cluster API Cluster Controller can pull it
	lbip4, err := dcr.elbHelper.IP()
	if err != nil {
		return err
	}

	// The alternative to this is to expose some kind of ClusterAPIStatusObject interface that allows
	// setting exactly the fields on the status that ClusterAPI requires to function.
	dc, ok := cluster.(*infrav1.DockerCluster)
	if !ok {
		return errors.Wrapf(errors.New("failed to create docker cluster"), "expected a DockerCluster but got %T", dc)
	}

	// Set the endpoint
	dc.Status.APIEndpoints = []infrav1.APIEndpoint{
		{
			Host: lbip4,
			Port: loadbalancer.ControlPlanePort,
		},
	}

	// Mark the dockerCluster ready
	dc.Status.Ready = true

	return nil
}

func (dcr *DockerClusterReconciler) GetWatches() map[source.Source]handler.EventHandler {
	return map[source.Source]handler.EventHandler{
		&source.Kind{Type: &clusterv1.Cluster{}}: &handler.EnqueueRequestsFromMapFunc{
			ToRequests: util.ClusterToInfrastructureMapFunc(infrav1.GroupVersion.WithKind("DockerCluster")),
		},
	}
}


