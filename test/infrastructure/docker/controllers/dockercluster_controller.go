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

// Package controllers implements controller functionality.
package controllers

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	infrav1 "sigs.k8s.io/cluster-api/test/infrastructure/docker/api/v1alpha4"
	"sigs.k8s.io/cluster-api/test/infrastructure/docker/docker"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/cluster-api/util/predicates"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// DockerClusterReconciler reconciles a DockerCluster object.
type DockerClusterReconciler struct {
	client.Client
	Log logr.Logger
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=dockerclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=dockerclusters/status;dockerclusters/finalizers,verbs=get;update;patch

// Reconcile reads that state of the cluster for a DockerCluster object and makes changes based on the state read
// and what is in the DockerCluster.Spec.
func (r *DockerClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, rerr error) {
	log := ctrl.LoggerFrom(ctx)

	// Fetch the DockerCluster instance
	dockerCluster := &infrav1.DockerCluster{}
	if err := r.Client.Get(ctx, req.NamespacedName, dockerCluster); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Fetch the Cluster.
	cluster, err := util.GetOwnerCluster(ctx, r.Client, dockerCluster.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, err
	}
	if cluster == nil {
		log.Info("Waiting for Cluster Controller to set OwnerRef on DockerCluster")
		return ctrl.Result{}, nil
	}

	log = log.WithValues("cluster", cluster.Name)

	// Create a helper for managing a docker container hosting the loadbalancer.
	externalLoadBalancer, err := docker.NewLoadBalancer(cluster, dockerCluster)
	if err != nil {
		return ctrl.Result{}, errors.Wrapf(err, "failed to create helper for managing the externalLoadBalancer")
	}

	// Initialize the patch helper
	patchHelper, err := patch.NewHelper(dockerCluster, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}
	// Always attempt to Patch the DockerCluster object and status after each reconciliation.
	defer func() {
		if err := patchDockerCluster(ctx, patchHelper, dockerCluster); err != nil {
			log.Error(err, "failed to patch DockerCluster")
			if rerr == nil {
				rerr = err
			}
		}
	}()

	// Support FailureDomains
	// In cloud providers this would likely look up which failure domains are supported and set the status appropriately.
	// In the case of Docker, failure domains don't mean much so we simply copy the Spec into the Status.
	dockerCluster.Status.FailureDomains = dockerCluster.Spec.FailureDomains

	// Add finalizer first if not exist to avoid the race condition between init and delete
	if !controllerutil.ContainsFinalizer(dockerCluster, infrav1.ClusterFinalizer) {
		controllerutil.AddFinalizer(dockerCluster, infrav1.ClusterFinalizer)
		return ctrl.Result{}, nil
	}

	// Handle deleted clusters
	if !dockerCluster.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, dockerCluster, externalLoadBalancer)
	}

	// Handle non-deleted clusters
	return r.reconcileNormal(ctx, dockerCluster, externalLoadBalancer)
}

func patchDockerCluster(ctx context.Context, patchHelper *patch.Helper, dockerCluster *infrav1.DockerCluster) error {
	// Always update the readyCondition by summarizing the state of other conditions.
	// A step counter is added to represent progress during the provisioning process (instead we are hiding it during the deletion process).
	conditions.SetSummary(dockerCluster,
		conditions.WithConditions(
			infrav1.LoadBalancerAvailableCondition,
		),
		conditions.WithStepCounterIf(dockerCluster.ObjectMeta.DeletionTimestamp.IsZero()),
	)

	// Patch the object, ignoring conflicts on the conditions owned by this controller.
	return patchHelper.Patch(
		ctx,
		dockerCluster,
		patch.WithOwnedConditions{Conditions: []clusterv1.ConditionType{
			clusterv1.ReadyCondition,
			infrav1.LoadBalancerAvailableCondition,
		}},
	)
}

func (r *DockerClusterReconciler) reconcileNormal(ctx context.Context, dockerCluster *infrav1.DockerCluster, externalLoadBalancer *docker.LoadBalancer) (ctrl.Result, error) {
	// Create the docker container hosting the load balancer.
	if err := externalLoadBalancer.Create(ctx); err != nil {
		conditions.MarkFalse(dockerCluster, infrav1.LoadBalancerAvailableCondition, infrav1.LoadBalancerProvisioningFailedReason, clusterv1.ConditionSeverityWarning, err.Error())
		return ctrl.Result{}, errors.Wrap(err, "failed to create load balancer")
	}

	// Set APIEndpoints with the load balancer IP so the Cluster API Cluster Controller can pull it
	lbIP, err := externalLoadBalancer.IP(ctx)
	if err != nil {
		conditions.MarkFalse(dockerCluster, infrav1.LoadBalancerAvailableCondition, infrav1.LoadBalancerProvisioningFailedReason, clusterv1.ConditionSeverityWarning, err.Error())
		return ctrl.Result{}, errors.Wrap(err, "failed to get ip for the load balancer")
	}

	dockerCluster.Spec.ControlPlaneEndpoint = infrav1.APIEndpoint{
		Host: lbIP,
		Port: 6443,
	}

	// Mark the dockerCluster ready
	dockerCluster.Status.Ready = true
	conditions.MarkTrue(dockerCluster, infrav1.LoadBalancerAvailableCondition)

	return ctrl.Result{}, nil
}

func (r *DockerClusterReconciler) reconcileDelete(ctx context.Context, dockerCluster *infrav1.DockerCluster, externalLoadBalancer *docker.LoadBalancer) (ctrl.Result, error) {
	// Set the LoadBalancerAvailableCondition reporting delete is started, and issue a patch in order to make
	// this visible to the users.
	// NB. The operation in docker is fast, so there is the chance the user will not notice the status change;
	// nevertheless we are issuing a patch so we can test a pattern that will be used by other providers as well
	patchHelper, err := patch.NewHelper(dockerCluster, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}
	conditions.MarkFalse(dockerCluster, infrav1.LoadBalancerAvailableCondition, clusterv1.DeletingReason, clusterv1.ConditionSeverityInfo, "")
	if err := patchDockerCluster(ctx, patchHelper, dockerCluster); err != nil {
		return ctrl.Result{}, errors.Wrap(err, "failed to patch DockerCluster")
	}

	// Delete the docker container hosting the load balancer
	if err := externalLoadBalancer.Delete(ctx); err != nil {
		return ctrl.Result{}, errors.Wrap(err, "failed to delete load balancer")
	}

	// Cluster is deleted so remove the finalizer.
	controllerutil.RemoveFinalizer(dockerCluster, infrav1.ClusterFinalizer)

	return ctrl.Result{}, nil
}

// SetupWithManager will add watches for this controller.
func (r *DockerClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	c, err := ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.DockerCluster{}).
		WithEventFilter(predicates.ResourceNotPaused(r.Log)).
		Build(r)
	if err != nil {
		return err
	}
	return c.Watch(
		&source.Kind{Type: &clusterv1.Cluster{}},
		handler.EnqueueRequestsFromMapFunc(util.ClusterToInfrastructureMapFunc(infrav1.GroupVersion.WithKind("DockerCluster"))),
		predicates.ClusterUnpaused(r.Log),
	)
}
