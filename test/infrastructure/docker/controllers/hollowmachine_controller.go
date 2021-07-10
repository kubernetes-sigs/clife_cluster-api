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
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	"sigs.k8s.io/cluster-api/controllers/remote"
	infrav1 "sigs.k8s.io/cluster-api/test/infrastructure/docker/api/v1alpha4"
	"sigs.k8s.io/cluster-api/test/infrastructure/docker/kubemark"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/cluster-api/util/predicates"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// HollowMachineReconciler reconciles a HollowMachine object.
type HollowMachineReconciler struct {
	client.Client
	Tracker *remote.ClusterCacheTracker
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=hollowmachines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=hollowmachines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;machines,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets;,verbs=get;list;watch

// Reconcile handles HollowMachine events.
func (r *HollowMachineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, rerr error) {
	log := ctrl.LoggerFrom(ctx)

	// Fetch the HollowMachine instance.
	hollowMachine := &infrav1.HollowMachine{}
	if err := r.Client.Get(ctx, req.NamespacedName, hollowMachine); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Fetch the kubemark.
	machine, err := util.GetOwnerMachine(ctx, r.Client, hollowMachine.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, err
	}
	if machine == nil {
		log.V(2).Info("Waiting for Machine Controller to set OwnerRef on the HollowMachine")
		return ctrl.Result{}, nil
	}

	log = log.WithValues("machine", machine.Name)

	if util.IsControlPlaneMachine(machine) {
		return ctrl.Result{}, errors.New("HollowMachine can't be used for control plane machines")
	}

	// Fetch the Cluster.
	cluster, err := util.GetClusterFromMetadata(ctx, r.Client, machine.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, errors.Wrapf(err, "HollowMachine owner kubemark is missing cluster label or cluster does not exist")
	}
	if cluster == nil {
		log.Info(fmt.Sprintf("Please associate this machine with a cluster using the label %s: <name of cluster>", clusterv1.ClusterLabelName))
		return ctrl.Result{}, nil
	}

	log = log.WithValues("cluster", cluster.Name)

	// Return early if the object or Cluster is paused.
	if annotations.IsPaused(cluster, hollowMachine) {
		log.Info("Reconciliation is paused for this object")
		return ctrl.Result{}, nil
	}

	// Initialize the patch helper
	patchHelper, err := patch.NewHelper(hollowMachine, r)
	if err != nil {
		return ctrl.Result{}, err
	}
	// Always attempt to Patch the HollowMachine object and status after each reconciliation.
	defer func() {
		if err := patchHelper.Patch(ctx, hollowMachine); err != nil {
			log.Error(err, "failed to patch HollowMachine")
			if rerr == nil {
				rerr = err
			}
		}
	}()

	// Add finalizer first if not exist to avoid the race condition between init and delete
	if !controllerutil.ContainsFinalizer(hollowMachine, infrav1.HollowMachineFinalizer) {
		controllerutil.AddFinalizer(hollowMachine, infrav1.HollowMachineFinalizer)
		return ctrl.Result{}, nil
	}

	// Check if the infrastructure is ready, otherwise return and wait for the cluster object to be updated
	if !cluster.Status.InfrastructureReady {
		log.Info("Waiting for Cluster Controller infrastructure to be ready")
		return ctrl.Result{}, nil
	}

	// Handle deleted machines
	if !hollowMachine.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, cluster, machine, hollowMachine)
	}

	// Handle non-deleted machines
	return r.reconcileNormal(ctx, cluster, machine, hollowMachine)
}

func (r *HollowMachineReconciler) reconcileNormal(ctx context.Context, cluster *clusterv1.Cluster, machine *clusterv1.Machine, hollowMachine *infrav1.HollowMachine) (res ctrl.Result, retErr error) {
	log := ctrl.LoggerFrom(ctx)

	// Make sure bootstrap data is available and populated.
	if machine.Spec.Bootstrap.DataSecretName == nil {
		log.V(2).Info("Waiting for the Bootstrap provider controller to set bootstrap data")
		return ctrl.Result{}, nil
	}

	// Reconcile the pod running hollow kubelet.
	externalCluster, err := r.getExternalCluster(ctx, cluster, hollowMachine)
	if err != nil {
		return ctrl.Result{}, err
	}
	if err := kubemark.ReconcilePod(ctx, r.Client, r.Tracker, externalCluster, cluster, machine, hollowMachine); err != nil {
		return ctrl.Result{}, err
	}

	// Set ProviderID so the Cluster API kubemark controller can pull it
	// NOTE: There is no cloud controller for hollow nodes, however kubemark generates one with a predictable format
	kubemarkProviderID := fmt.Sprintf("kubemark://%s", kubemark.HollowNodeName(hollowMachine))
	hollowMachine.Spec.ProviderID = &kubemarkProviderID
	hollowMachine.Status.Ready = true

	return ctrl.Result{}, nil
}

func (r *HollowMachineReconciler) reconcileDelete(ctx context.Context, cluster *clusterv1.Cluster, machine *clusterv1.Machine, hollowMachine *infrav1.HollowMachine) (ctrl.Result, error) {
	// Reconcile the pod running hollow kubelet.
	externalCluster, err := r.getExternalCluster(ctx, cluster, hollowMachine)
	if err != nil {
		return ctrl.Result{}, err
	}
	if err := kubemark.DeletePod(ctx, r.Client, r.Tracker, externalCluster, cluster, machine, hollowMachine); err != nil {
		return ctrl.Result{}, err
	}

	// kubemark is deleted so remove the finalizer.
	controllerutil.RemoveFinalizer(hollowMachine, infrav1.HollowMachineFinalizer)
	return ctrl.Result{}, nil
}

func (r *HollowMachineReconciler) getExternalCluster(ctx context.Context, cluster *clusterv1.Cluster, hollowMachine *infrav1.HollowMachine) (*clusterv1.Cluster, error) {
	if hollowMachine.Spec.ExternalCluster != nil {
		externalCluster := &clusterv1.Cluster{}
		key := client.ObjectKey{
			Namespace: hollowMachine.Namespace,
			Name:      *hollowMachine.Spec.ExternalCluster,
		}

		if err := r.Client.Get(ctx, key, externalCluster); err != nil {
			return nil, errors.Wrapf(err, "failed to get external cluster with name %s", key.Name)
		}
		return externalCluster, nil
	}
	return cluster, nil
}

// SetupWithManager will add watches for this controller.
func (r *HollowMachineReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, options controller.Options) error {
	clusterToHollowMachines, err := util.ClusterToObjectsMapper(mgr.GetClient(), &infrav1.HollowMachineList{}, mgr.GetScheme())
	if err != nil {
		return err
	}

	c, err := ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.HollowMachine{}).
		WithOptions(options).
		WithEventFilter(predicates.ResourceNotPaused(ctrl.LoggerFrom(ctx))).
		Watches(
			&source.Kind{Type: &clusterv1.Machine{}},
			handler.EnqueueRequestsFromMapFunc(util.MachineToInfrastructureMapFunc(infrav1.GroupVersion.WithKind("HollowMachine"))),
		).
		Build(r)
	if err != nil {
		return err
	}
	return c.Watch(
		&source.Kind{Type: &clusterv1.Cluster{}},
		handler.EnqueueRequestsFromMapFunc(clusterToHollowMachines),
		predicates.ClusterUnpausedAndInfrastructureReady(ctrl.LoggerFrom(ctx)),
	)
}
