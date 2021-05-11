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

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	intstrutil "k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/integer"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	"sigs.k8s.io/cluster-api/controllers/mdutil"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/cluster-api/util/predicates"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var (
	// machineDeploymentKind contains the schema.GroupVersionKind for the MachineDeployment type.
	machineDeploymentKind = clusterv1.GroupVersion.WithKind("MachineDeployment")
)

// +kubebuilder:rbac:groups=core,resources=events,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io;bootstrap.cluster.x-k8s.io,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machinedeployments;machinedeployments/status,verbs=get;list;watch;create;update;patch;delete

// MachineDeploymentReconciler reconciles a MachineDeployment object.
type MachineDeploymentReconciler struct {
	Client           client.Client
	WatchFilterValue string

	recorder   record.EventRecorder
	restConfig *rest.Config
}

func (r *MachineDeploymentReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, options controller.Options) error {
	clusterToMachineDeployments, err := util.ClusterToObjectsMapper(mgr.GetClient(), &clusterv1.MachineDeploymentList{}, mgr.GetScheme())
	if err != nil {
		return err
	}

	c, err := ctrl.NewControllerManagedBy(mgr).
		For(&clusterv1.MachineDeployment{}).
		Owns(&clusterv1.MachineSet{}).
		Watches(
			&source.Kind{Type: &clusterv1.MachineSet{}},
			handler.EnqueueRequestsFromMapFunc(r.MachineSetToDeployments),
		).
		WithOptions(options).
		WithEventFilter(predicates.ResourceNotPausedAndHasFilterLabel(ctrl.LoggerFrom(ctx), r.WatchFilterValue)).
		Build(r)
	if err != nil {
		return errors.Wrap(err, "failed setting up with a controller manager")
	}

	err = c.Watch(
		&source.Kind{Type: &clusterv1.Cluster{}},
		handler.EnqueueRequestsFromMapFunc(clusterToMachineDeployments),
		// TODO: should this wait for Cluster.Status.InfrastructureReady similar to Infra Machine resources?
		predicates.ClusterUnpaused(ctrl.LoggerFrom(ctx)),
	)
	if err != nil {
		return errors.Wrap(err, "failed to add Watch for Clusters to controller manager")
	}

	r.recorder = mgr.GetEventRecorderFor("machinedeployment-controller")
	r.restConfig = mgr.GetConfig()
	return nil
}

func (r *MachineDeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := ctrl.LoggerFrom(ctx)

	// Fetch the MachineDeployment instance.
	deployment := &clusterv1.MachineDeployment{}
	if err := r.Client.Get(ctx, req.NamespacedName, deployment); err != nil {
		if apierrors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	cluster, err := util.GetClusterByName(ctx, r.Client, deployment.Namespace, deployment.Spec.ClusterName)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Return early if the object or Cluster is paused.
	if annotations.IsPaused(cluster, deployment) {
		log.Info("Reconciliation is paused for this object")
		return ctrl.Result{}, nil
	}

	// Initialize the patch helper
	patchHelper, err := patch.NewHelper(deployment, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	defer func() {
		// Always attempt to patch the object and status after each reconciliation.
		if err := patchHelper.Patch(ctx, deployment); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	// Ignore deleted MachineDeployments, this can happen when foregroundDeletion
	// is enabled
	if !deployment.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	result, err := r.reconcile(ctx, cluster, deployment)
	if err != nil {
		log.Error(err, "Failed to reconcile MachineDeployment")
		r.recorder.Eventf(deployment, corev1.EventTypeWarning, "ReconcileError", "%v", err)
	}
	return result, err
}

func (r *MachineDeploymentReconciler) reconcile(ctx context.Context, cluster *clusterv1.Cluster, d *clusterv1.MachineDeployment) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	log.V(4).Info("Reconcile MachineDeployment")

	// Reconcile and retrieve the Cluster object.
	if d.Labels == nil {
		d.Labels = make(map[string]string)
	}
	if d.Spec.Selector.MatchLabels == nil {
		d.Spec.Selector.MatchLabels = make(map[string]string)
	}
	if d.Spec.Template.Labels == nil {
		d.Spec.Template.Labels = make(map[string]string)
	}

	d.Labels[clusterv1.ClusterLabelName] = d.Spec.ClusterName

	if r.shouldAdopt(d) {
		d.OwnerReferences = util.EnsureOwnerRef(d.OwnerReferences, metav1.OwnerReference{
			APIVersion: clusterv1.GroupVersion.String(),
			Kind:       "Cluster",
			Name:       cluster.Name,
			UID:        cluster.UID,
		})
		return ctrl.Result{}, nil
	}

	// Make sure to reconcile the external infrastructure reference.
	if err := reconcileExternalTemplateReference(ctx, r.Client, r.restConfig, cluster, &d.Spec.Template.Spec.InfrastructureRef); err != nil {
		return ctrl.Result{}, err
	}
	// Make sure to reconcile the external bootstrap reference, if any.
	if d.Spec.Template.Spec.Bootstrap.ConfigRef != nil {
		if err := reconcileExternalTemplateReference(ctx, r.Client, r.restConfig, cluster, d.Spec.Template.Spec.Bootstrap.ConfigRef); err != nil {
			return ctrl.Result{}, err
		}
	}

	msList, err := r.getMachineSetsForDeployment(ctx, d)
	if err != nil {
		return ctrl.Result{}, err
	}

	if d.Spec.Paused {
		return ctrl.Result{}, r.sync(ctx, d, msList)
	}

	if d.Spec.Strategy == nil {
		return ctrl.Result{}, errors.Errorf("missing MachineDeployment strategy")
	}

	if d.Spec.Strategy.Type == clusterv1.RollingUpdateMachineDeploymentStrategyType {
		if d.Spec.Strategy.RollingUpdate == nil {
			return ctrl.Result{}, errors.Errorf("missing MachineDeployment settings for strategy type: %s", d.Spec.Strategy.Type)
		}

		// If we are not in a rolling update operation, rollout after if we can.
		newMachineSet, oldMachineSets, err := r.getAllMachineSetsAndSyncRevision(ctx, d, msList, true)
		if err != nil {
			return ctrl.Result{}, err
		}
		allMachineSets := append(oldMachineSets, newMachineSet)
		oldMachinesCount := mdutil.GetReplicaCountForMachineSets(oldMachineSets)
		// If there's no oldMachinesCount we assume we are not in a rolling update operation.
		if oldMachinesCount == 0 {
			if err := r.rolloutAfter(ctx, d, newMachineSet); err != nil {
				return ctrl.Result{}, err
			}

			// Update status and requeue.
			return ctrl.Result{}, r.syncDeploymentStatus(allMachineSets, newMachineSet, d)
		}

		if err := r.rolloutRolling(ctx, d, msList); err != nil {
			return ctrl.Result{}, err
		}
	}

	if d.Spec.Strategy.Type == clusterv1.OnDeleteMachineDeploymentStrategyType {
		return ctrl.Result{}, r.rolloutOnDelete(ctx, d, msList)
	}

	return ctrl.Result{}, errors.Errorf("unexpected deployment strategy type: %s", d.Spec.Strategy.Type)
}

func (r *MachineDeploymentReconciler) rolloutAfter(ctx context.Context, machineDeployment *clusterv1.MachineDeployment, newMachineSet *clusterv1.MachineSet) error {
	var machinesToRollout []*clusterv1.Machine
	machines, err := r.getMachinesForMachineSet(ctx, newMachineSet)
	if err != nil {
		return err
	}

	// Find Machines needing to rollout after.
	for key := range machines {
		if machines[key].DeletionTimestamp.IsZero() &&
			machines[key].CreationTimestamp.Before(machineDeployment.Spec.RolloutAfter) {
			machines[key].Annotations[clusterv1.DeleteMachineAnnotation] = "True"
			// TODO: Patch machines
			machinesToRollout = append(machinesToRollout, machines[key])
		}
	}

	// Scale up if we can
	maxSurge, err := intstrutil.GetScaledValueFromIntOrPercent(machineDeployment.Spec.Strategy.RollingUpdate.MaxSurge, int(*(machineDeployment.Spec.Replicas)), true)
	if err != nil {
		return err
	}
	maxTotalMachines := *(machineDeployment.Spec.Replicas) + int32(maxSurge)
	if *newMachineSet.Spec.Replicas < maxTotalMachines {
		scaleUpCount := integer.Int32Min(maxTotalMachines-*newMachineSet.Spec.Replicas, int32(len(machinesToRollout)))
		if err := r.scaleMachineSet(ctx, newMachineSet, scaleUpCount, machineDeployment); err != nil {
			return err
		}
	}

	// Scale down if we can
	maxUnavailable := mdutil.MaxUnavailable(*machineDeployment)
	minAvailable := *(machineDeployment.Spec.Replicas) - maxUnavailable
	newMachineSetUnavailableMachineCount := *(newMachineSet.Spec.Replicas) - newMachineSet.Status.AvailableReplicas
	maxScaledDown := *newMachineSet.Spec.Replicas - minAvailable - newMachineSetUnavailableMachineCount
	scaleDownCount := integer.Int32Min(maxScaledDown, int32(len(machinesToRollout)))
	if maxScaledDown > 0 {
		return r.scaleMachineSet(ctx, newMachineSet, scaleDownCount, machineDeployment)
	}

	return nil
}

func (r *MachineDeploymentReconciler) getMachinesForMachineSet(ctx context.Context, machineSet *clusterv1.MachineSet) ([]*clusterv1.Machine, error) {
	selectorMap, err := metav1.LabelSelectorAsMap(&machineSet.Spec.Selector)
	if err != nil {
		return nil, fmt.Errorf("failed to convert MachineSet %v label selector to a map: %w",
			client.ObjectKeyFromObject(machineSet), err)
	}

	// Get all Machines linked to this MachineSet.
	allMachines := &clusterv1.MachineList{}
	if err := r.Client.List(ctx,
		allMachines,
		client.InNamespace(machineSet.Namespace),
		client.MatchingLabels(selectorMap),
	); err != nil {
		return nil, fmt.Errorf("failed to list machines for MachineSet %v: %w",
			client.ObjectKeyFromObject(machineSet), err)
	}

	// Filter out machines not owned by this machineSet
	filteredMachines := make([]*clusterv1.Machine, 0, len(allMachines.Items))
	for key := range allMachines.Items {
		machine := &allMachines.Items[key]
		if shouldExcludeMachine(machineSet, machine) {
			continue
		}
		filteredMachines = append(filteredMachines, machine)
	}

	return filteredMachines, nil
}

// getMachineSetsForDeployment returns a list of MachineSets associated with a MachineDeployment.
func (r *MachineDeploymentReconciler) getMachineSetsForDeployment(ctx context.Context, d *clusterv1.MachineDeployment) ([]*clusterv1.MachineSet, error) {
	log := ctrl.LoggerFrom(ctx)

	// List all MachineSets to find those we own but that no longer match our selector.
	machineSets := &clusterv1.MachineSetList{}
	if err := r.Client.List(ctx, machineSets, client.InNamespace(d.Namespace)); err != nil {
		return nil, err
	}

	filtered := make([]*clusterv1.MachineSet, 0, len(machineSets.Items))
	for idx := range machineSets.Items {
		ms := &machineSets.Items[idx]

		selector, err := metav1.LabelSelectorAsSelector(&d.Spec.Selector)
		if err != nil {
			log.Error(err, "Skipping MachineSet, failed to get label selector from spec selector", "machineset", ms.Name)
			continue
		}

		// If a MachineDeployment with a nil or empty selector creeps in, it should match nothing, not everything.
		if selector.Empty() {
			log.Info("Skipping MachineSet as the selector is empty", "machineset", ms.Name)
			continue
		}

		// Skip this MachineSet unless either selector matches or it has a controller ref pointing to this MachineDeployment
		if !selector.Matches(labels.Set(ms.Labels)) && !metav1.IsControlledBy(ms, d) {
			log.V(4).Info("Skipping MachineSet, label mismatch", "machineset", ms.Name)
			continue
		}

		// Attempt to adopt machine if it meets previous conditions and it has no controller references.
		if metav1.GetControllerOf(ms) == nil {
			if err := r.adoptOrphan(ctx, d, ms); err != nil {
				r.recorder.Eventf(d, corev1.EventTypeWarning, "FailedAdopt", "Failed to adopt MachineSet %q: %v", ms.Name, err)
				log.Error(err, "Failed to adopt MachineSet into MachineDeployment", "machineset", ms.Name)
				continue
			}
			r.recorder.Eventf(d, corev1.EventTypeNormal, "SuccessfulAdopt", "Adopted MachineSet %q", ms.Name)
		}

		if !metav1.IsControlledBy(ms, d) {
			continue
		}

		filtered = append(filtered, ms)
	}

	return filtered, nil
}

// adoptOrphan sets the MachineDeployment as a controller OwnerReference to the MachineSet.
func (r *MachineDeploymentReconciler) adoptOrphan(ctx context.Context, deployment *clusterv1.MachineDeployment, machineSet *clusterv1.MachineSet) error {
	patch := client.MergeFrom(machineSet.DeepCopy())
	newRef := *metav1.NewControllerRef(deployment, machineDeploymentKind)
	machineSet.OwnerReferences = append(machineSet.OwnerReferences, newRef)
	return r.Client.Patch(ctx, machineSet, patch)
}

// getMachineDeploymentsForMachineSet returns a list of MachineDeployments that could potentially match a MachineSet.
func (r *MachineDeploymentReconciler) getMachineDeploymentsForMachineSet(ctx context.Context, ms *clusterv1.MachineSet) []*clusterv1.MachineDeployment {
	log := ctrl.LoggerFrom(ctx)

	if len(ms.Labels) == 0 {
		log.V(2).Info("No MachineDeployments found for MachineSet because it has no labels", "machineset", ms.Name)
		return nil
	}

	dList := &clusterv1.MachineDeploymentList{}
	if err := r.Client.List(ctx, dList, client.InNamespace(ms.Namespace)); err != nil {
		log.Error(err, "Failed to list MachineDeployments")
		return nil
	}

	deployments := make([]*clusterv1.MachineDeployment, 0, len(dList.Items))
	for idx, d := range dList.Items {
		selector, err := metav1.LabelSelectorAsSelector(&d.Spec.Selector)
		if err != nil {
			continue
		}

		// If a deployment with a nil or empty selector creeps in, it should match nothing, not everything.
		if selector.Empty() || !selector.Matches(labels.Set(ms.Labels)) {
			continue
		}

		deployments = append(deployments, &dList.Items[idx])
	}

	return deployments
}

// MachineSetToDeployments is a handler.ToRequestsFunc to be used to enqueue requests for reconciliation
// for MachineDeployments that might adopt an orphaned MachineSet.
func (r *MachineDeploymentReconciler) MachineSetToDeployments(o client.Object) []ctrl.Request {
	result := []ctrl.Request{}

	ms, ok := o.(*clusterv1.MachineSet)
	if !ok {
		panic(fmt.Sprintf("Expected a MachineSet but got a %T", o))
	}

	// Check if the controller reference is already set and
	// return an empty result when one is found.
	for _, ref := range ms.ObjectMeta.OwnerReferences {
		if ref.Controller != nil && *ref.Controller {
			return result
		}
	}

	mds := r.getMachineDeploymentsForMachineSet(context.TODO(), ms)
	if len(mds) == 0 {
		return nil
	}

	for _, md := range mds {
		name := client.ObjectKey{Namespace: md.Namespace, Name: md.Name}
		result = append(result, ctrl.Request{NamespacedName: name})
	}

	return result
}

func (r *MachineDeploymentReconciler) shouldAdopt(md *clusterv1.MachineDeployment) bool {
	return !util.HasOwner(md.OwnerReferences, clusterv1.GroupVersion.String(), []string{"Cluster"})
}
