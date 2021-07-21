/*
Copyright 2021 The Kubernetes Authors.

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

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	"sigs.k8s.io/cluster-api/controllers/external"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/predicates"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io;bootstrap.cluster.x-k8s.io;controlplane.cluster.x-k8s.io,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusterclasses;machinedeployments,verbs=get;list;watch
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch

// ClusterTopologyReconciler reconciles a managed topology for a Cluster object.
type ClusterTopologyReconciler struct {
	Client           client.Client
	WatchFilterValue string

	// UnstructuredCachingClient provides a client that forces caching of unstructured objects,
	// thus allowing to optimize reads for templates or provider specific objects in a managed topology.
	UnstructuredCachingClient client.Client

	restConfig      *rest.Config
	externalTracker external.ObjectTracker
}

func (r *ClusterTopologyReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, options controller.Options) error {
	c, err := ctrl.NewControllerManagedBy(mgr).
		For(&clusterv1.Cluster{}).
		Watches(
			&source.Kind{Type: &clusterv1.ClusterClass{}},
			handler.EnqueueRequestsFromMapFunc(r.clusterClassToCluster),
		).
		Watches(
			&source.Kind{Type: &clusterv1.MachineDeployment{}},
			handler.EnqueueRequestsFromMapFunc(r.machineDeploymentToCluster),
		).
		WithOptions(options).
		WithEventFilter(predicates.ResourceNotPausedAndHasFilterLabel(ctrl.LoggerFrom(ctx), r.WatchFilterValue)).
		Build(r)

	if err != nil {
		return errors.Wrap(err, "failed setting up with a controller manager")
	}

	r.restConfig = mgr.GetConfig()
	r.externalTracker = external.ObjectTracker{
		Controller: c,
	}
	return nil
}

func (r *ClusterTopologyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := ctrl.LoggerFrom(ctx)

	// Fetch the Cluster instance.
	cluster := &clusterv1.Cluster{}
	if err := r.Client.Get(ctx, req.NamespacedName, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	// Return early, if the Cluster does not use a managed topology.
	// NOTE: This should be removed as soon as we start to support Clusters moving from managed <-> unmanaged.
	if cluster.Spec.Topology == nil {
		return ctrl.Result{}, nil
	}

	// Return early if the Cluster is paused.
	if annotations.IsPaused(cluster, cluster) {
		log.Info("Reconciliation is paused for this object")
		return ctrl.Result{}, nil
	}

	// TODO: Add patching as soon as we define how to report managed topology state into conditions

	// In case the object is deleted, the managed topology stops to reconcile;
	// (the other controllers will take care of deletion).
	if !cluster.ObjectMeta.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	// Handle normal reconciliation loop.
	return r.reconcile(ctx, cluster)
}

// reconcile handles cluster reconciliation.
func (r *ClusterTopologyReconciler) reconcile(ctx context.Context, cluster *clusterv1.Cluster) (ctrl.Result, error) {
	// Gets the ClusterClass and the referenced templates.
	class, err := r.getClass(ctx, cluster)
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "error reading the ClusterClass")
	}

	// Gets the current state of the Cluster.
	currentState, err := r.getCurrentState(ctx, cluster)
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "error reading current state of the Cluster topology")
	}

	// Computes the desired state of the Cluster
	desiredState, err := r.computeDesiredState(ctx, class, currentState)
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "error computing the desired state of the Cluster topology")
	}

	// Reconciles current and desired state of the Cluster
	if err := r.reconcileState(ctx, currentState, desiredState); err != nil {
		return ctrl.Result{}, errors.Wrap(err, "error reconciling the Cluster topology")
	}

	return ctrl.Result{}, nil
}

// clusterClassToCluster is a handler.ToRequestsFunc to be used to enqueue requests for reconciliation
// for Cluster to update when its own ClusterClass gets updated.
func (r *ClusterTopologyReconciler) clusterClassToCluster(o client.Object) []ctrl.Request {
	_, ok := o.(*clusterv1.ClusterClass)
	if !ok {
		panic(fmt.Sprintf("Expected a ClusterClass but got a %T", o))
	}

	// TODO: use the custom cache index for Cluster.Topology.Class to retry all the Cluster using a given ClusterClass

	return nil
}

// machineDeploymentToCluster is a handler.ToRequestsFunc to be used to enqueue requests for reconciliation
// for Cluster to update when one of its own MachineDeployments gets updated.
func (r *ClusterTopologyReconciler) machineDeploymentToCluster(o client.Object) []ctrl.Request {
	md, ok := o.(*clusterv1.MachineDeployment)
	if !ok {
		panic(fmt.Sprintf("Expected a MachineDeployment but got a %T", o))
	}
	if md.Spec.ClusterName == "" {
		return nil
	}

	return []ctrl.Request{{
		NamespacedName: types.NamespacedName{
			Namespace: md.Namespace,
			Name:      md.Spec.ClusterName,
		},
	}}
}
