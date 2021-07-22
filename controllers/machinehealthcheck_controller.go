/*
Copyright 2020 The Kubernetes Authors.

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
	"sort"
	"strconv"
	"strings"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	"sigs.k8s.io/cluster-api/controllers/external"
	"sigs.k8s.io/cluster-api/controllers/noderefutil"
	"sigs.k8s.io/cluster-api/controllers/remote"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/cluster-api/util/predicates"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	// Event types.

	// EventRemediationRestricted is emitted in case when machine remediation
	// is restricted by remediation circuit shorting logic.
	EventRemediationRestricted string = "RemediationRestricted"
)

// +kubebuilder:rbac:groups=core,resources=events,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines;machines/status,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machinehealthchecks;machinehealthchecks/status;machinehealthchecks/finalizers,verbs=get;list;watch;update;patch

// MachineHealthCheckReconciler reconciles a MachineHealthCheck object.
type MachineHealthCheckReconciler struct {
	Client           client.Client
	Tracker          *remote.ClusterCacheTracker
	WatchFilterValue string

	controller controller.Controller
	recorder   record.EventRecorder
}

func (r *MachineHealthCheckReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, options controller.Options) error {
	controller, err := ctrl.NewControllerManagedBy(mgr).
		For(&clusterv1.MachineHealthCheck{}).
		Watches(
			&source.Kind{Type: &clusterv1.Machine{}},
			handler.EnqueueRequestsFromMapFunc(r.machineToMachineHealthCheck),
		).
		WithOptions(options).
		WithEventFilter(predicates.ResourceNotPausedAndHasFilterLabel(ctrl.LoggerFrom(ctx), r.WatchFilterValue)).
		Build(r)
	if err != nil {
		return errors.Wrap(err, "failed setting up with a controller manager")
	}
	err = controller.Watch(
		&source.Kind{Type: &clusterv1.Cluster{}},
		handler.EnqueueRequestsFromMapFunc(r.clusterToMachineHealthCheck),
		// TODO: should this wait for Cluster.Status.InfrastructureReady similar to Infra Machine resources?
		predicates.ClusterUnpaused(ctrl.LoggerFrom(ctx)),
	)
	if err != nil {
		return errors.Wrap(err, "failed to add Watch for Clusters to controller manager")
	}

	r.controller = controller
	r.recorder = mgr.GetEventRecorderFor("machinehealthcheck-controller")
	return nil
}

func (r *MachineHealthCheckReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Reconciling")

	// Fetch the MachineHealthCheck instance
	m := &clusterv1.MachineHealthCheck{}
	if err := r.Client.Get(ctx, req.NamespacedName, m); err != nil {
		if apierrors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return ctrl.Result{}, nil
		}

		// Error reading the object - requeue the request.
		log.Error(err, "Failed to fetch MachineHealthCheck")
		return ctrl.Result{}, err
	}

	log = log.WithValues("cluster", m.Spec.ClusterName)
	ctx = ctrl.LoggerInto(ctx, log)

	cluster, err := util.GetClusterByName(ctx, r.Client, m.Namespace, m.Spec.ClusterName)
	if err != nil {
		log.Error(err, "Failed to fetch Cluster for MachineHealthCheck")
		return ctrl.Result{}, err
	}

	// Return early if the object or Cluster is paused.
	if annotations.IsPaused(cluster, m) {
		log.Info("Reconciliation is paused for this object")
		return ctrl.Result{}, nil
	}

	// Initialize the patch helper
	patchHelper, err := patch.NewHelper(m, r.Client)
	if err != nil {
		log.Error(err, "Failed to build patch helper")
		return ctrl.Result{}, err
	}

	defer func() {
		// Always attempt to patch the object and status after each reconciliation.
		// Patch ObservedGeneration only if the reconciliation completed successfully
		patchOpts := []patch.Option{}
		if reterr == nil {
			patchOpts = append(patchOpts, patch.WithStatusObservedGeneration{})
		}
		if err := patchHelper.Patch(ctx, m, patchOpts...); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	// Reconcile labels.
	if m.Labels == nil {
		m.Labels = make(map[string]string)
	}
	m.Labels[clusterv1.ClusterLabelName] = m.Spec.ClusterName

	result, err := r.reconcile(ctx, log, cluster, m)
	if err != nil {
		log.Error(err, "Failed to reconcile MachineHealthCheck")
		r.recorder.Eventf(m, corev1.EventTypeWarning, "ReconcileError", "%v", err)

		// Requeue immediately if any errors occurred
		return ctrl.Result{}, err
	}

	return result, nil
}

func (r *MachineHealthCheckReconciler) reconcile(ctx context.Context, logger logr.Logger, cluster *clusterv1.Cluster, m *clusterv1.MachineHealthCheck) (ctrl.Result, error) {
	// Ensure the MachineHealthCheck is owned by the Cluster it belongs to
	m.OwnerReferences = util.EnsureOwnerRef(m.OwnerReferences, metav1.OwnerReference{
		APIVersion: clusterv1.GroupVersion.String(),
		Kind:       "Cluster",
		Name:       cluster.Name,
		UID:        cluster.UID,
	})

	// Get the remote cluster cache to use as a client.Reader.
	remoteClient, err := r.Tracker.GetClient(ctx, util.ObjectKey(cluster))
	if err != nil {
		logger.Error(err, "error creating remote cluster cache")
		return ctrl.Result{}, err
	}

	if err := r.watchClusterNodes(ctx, logger, cluster); err != nil {
		logger.Error(err, "error watching nodes on target cluster")
		return ctrl.Result{}, err
	}

	// fetch all targets
	logger.V(3).Info("Finding targets")
	targets, err := r.getTargetsFromMHC(ctx, logger, remoteClient, cluster, m)
	if err != nil {
		logger.Error(err, "Failed to fetch targets from MachineHealthCheck")
		return ctrl.Result{}, err
	}
	totalTargets := len(targets)
	m.Status.ExpectedMachines = int32(totalTargets)
	m.Status.Targets = make([]string, totalTargets)
	for i, t := range targets {
		m.Status.Targets[i] = t.Machine.Name
	}
	// do sort to avoid keep changing m.Status as the returned machines are not in order
	sort.Strings(m.Status.Targets)

	nodeStartupTimeout := m.Spec.NodeStartupTimeout // nolint:ifshort
	if nodeStartupTimeout == nil {
		nodeStartupTimeout = &clusterv1.DefaultNodeStartupTimeout
	}

	// health check all targets and reconcile mhc status
	healthy, unhealthy, nextCheckTimes := r.healthCheckTargets(targets, logger, *nodeStartupTimeout)
	m.Status.CurrentHealthy = int32(len(healthy))

	var unhealthyLimitKey, unhealthyLimitValue interface{}

	// check MHC current health against MaxUnhealthy
	remediationAllowed, remediationCount, err := isAllowedRemediation(m)
	if err != nil {
		return ctrl.Result{}, errors.Wrapf(err, "error checking if remediation is allowed")
	}

	if !remediationAllowed {
		var message string

		if m.Spec.UnhealthyRange == nil {
			unhealthyLimitKey = "max unhealthy"
			unhealthyLimitValue = m.Spec.MaxUnhealthy
			message = fmt.Sprintf("Remediation is not allowed, the number of not started or unhealthy machines exceeds maxUnhealthy (total: %v, unhealthy: %v, maxUnhealthy: %v)",
				totalTargets,
				len(unhealthy),
				m.Spec.MaxUnhealthy)
		} else {
			unhealthyLimitKey = "unhealthy range"
			unhealthyLimitValue = *m.Spec.UnhealthyRange
			message = fmt.Sprintf("Remediation is not allowed, the number of not started or unhealthy machines does not fall within the range (total: %v, unhealthy: %v, unhealthyRange: %v)",
				totalTargets,
				len(unhealthy),
				*m.Spec.UnhealthyRange)
		}

		logger.V(3).Info(
			"Short-circuiting remediation",
			"total target", totalTargets,
			unhealthyLimitKey, unhealthyLimitValue,
			"unhealthy targets", len(unhealthy),
		)

		// Remediation not allowed, the number of not started or unhealthy machines either exceeds maxUnhealthy (or) not within unhealthyRange
		m.Status.RemediationsAllowed = 0
		conditions.Set(m, &clusterv1.Condition{
			Type:     clusterv1.RemediationAllowedCondition,
			Status:   corev1.ConditionFalse,
			Severity: clusterv1.ConditionSeverityWarning,
			Reason:   clusterv1.TooManyUnhealthyReason,
			Message:  message,
		})

		r.recorder.Eventf(
			m,
			corev1.EventTypeWarning,
			EventRemediationRestricted,
			message,
		)
		errList := []error{}
		for _, t := range append(healthy, unhealthy...) {
			if err := t.patchHelper.Patch(ctx, t.Machine); err != nil {
				errList = append(errList, errors.Wrapf(err, "failed to patch machine status for machine: %s/%s", t.Machine.Namespace, t.Machine.Name))
				continue
			}
		}
		if len(errList) > 0 {
			return ctrl.Result{}, kerrors.NewAggregate(errList)
		}
		return reconcile.Result{Requeue: true}, nil
	}

	logger.V(3).Info(
		"Remediations are allowed",
		"total target", totalTargets,
		unhealthyLimitKey, unhealthyLimitValue,
		"unhealthy targets", len(unhealthy),
	)

	// Remediation is allowed so unhealthyMachineCount is within unhealthyRange (or) maxUnhealthy - unhealthyMachineCount >= 0
	m.Status.RemediationsAllowed = remediationCount
	conditions.MarkTrue(m, clusterv1.RemediationAllowedCondition)

	errList := r.patchUnhealthyTargets(ctx, logger, unhealthy, cluster, m)
	errList = append(errList, r.patchHealthyTargets(ctx, logger, healthy, m)...)

	// handle update errors
	if len(errList) > 0 {
		logger.V(3).Info("Error(s) marking machine, requeueing")
		return reconcile.Result{}, kerrors.NewAggregate(errList)
	}

	if minNextCheck := minDuration(nextCheckTimes); minNextCheck > 0 {
		logger.V(3).Info("Some targets might go unhealthy. Ensuring a requeue happens", "requeueIn", minNextCheck.Truncate(time.Second).String())
		return ctrl.Result{RequeueAfter: minNextCheck}, nil
	}

	logger.V(3).Info("No more targets meet unhealthy criteria")

	return ctrl.Result{}, nil
}

// patchHealthyTargets patches healthy machines with MachineHealthCheckSucceededCondition.
func (r *MachineHealthCheckReconciler) patchHealthyTargets(ctx context.Context, logger logr.Logger, healthy []healthCheckTarget, m *clusterv1.MachineHealthCheck) []error {
	errList := []error{}
	for _, t := range healthy {
		if m.Spec.RemediationTemplate != nil {
			// Get remediation request object
			obj, err := r.getExternalRemediationRequest(ctx, m, t.Machine.Name)
			if err != nil {
				if !apierrors.IsNotFound(errors.Cause(err)) {
					wrappedErr := errors.Wrapf(err, "failed to fetch remediation request for machine %q in namespace %q within cluster %q", t.Machine.Name, t.Machine.Namespace, t.Machine.ClusterName)
					errList = append(errList, wrappedErr)
				}
				continue
			}
			// Check that obj has no DeletionTimestamp to avoid hot loop
			if obj.GetDeletionTimestamp() == nil {
				// Issue a delete for remediation request.
				if err := r.Client.Delete(ctx, obj); err != nil && !apierrors.IsNotFound(err) {
					logger.Error(err, "failed to delete %v %q for Machine %q", obj.GroupVersionKind(), obj.GetName(), t.Machine.Name)
				}
			}
		}

		if err := t.patchHelper.Patch(ctx, t.Machine); err != nil {
			logger.Error(err, "failed to patch healthy machine status for machine", "machine", t.Machine.GetName())
			errList = append(errList, errors.Wrapf(err, "failed to patch healthy machine status for machine: %s/%s", t.Machine.Namespace, t.Machine.Name))
		}
	}
	return errList
}

// patchUnhealthyTargets patches machines with MachineOwnerRemediatedCondition for remediation.
func (r *MachineHealthCheckReconciler) patchUnhealthyTargets(ctx context.Context, logger logr.Logger, unhealthy []healthCheckTarget, cluster *clusterv1.Cluster, m *clusterv1.MachineHealthCheck) []error {
	// mark for remediation
	errList := []error{}
	for _, t := range unhealthy {
		condition := conditions.Get(t.Machine, clusterv1.MachineHealthCheckSuccededCondition)

		if annotations.IsPaused(cluster, t.Machine) {
			logger.Info("Machine has failed health check, but machine is paused so skipping remediation", "target", t.string(), "reason", condition.Reason, "message", condition.Message)
		} else {
			if m.Spec.RemediationTemplate != nil {
				// If external remediation request already exists,
				// return early
				if r.externalRemediationRequestExists(ctx, m, t.Machine.Name) {
					return errList
				}

				cloneOwnerRef := &metav1.OwnerReference{
					APIVersion: clusterv1.GroupVersion.String(),
					Kind:       "Machine",
					Name:       t.Machine.Name,
					UID:        t.Machine.UID,
				}

				from, err := external.Get(ctx, r.Client, m.Spec.RemediationTemplate, t.Machine.Namespace)
				if err != nil {
					conditions.MarkFalse(m, clusterv1.ExternalRemediationTemplateAvailable, clusterv1.ExternalRemediationTemplateNotFound, clusterv1.ConditionSeverityError, err.Error())
					errList = append(errList, errors.Wrapf(err, "error retrieving remediation template %v %q for machine %q in namespace %q within cluster %q", m.Spec.RemediationTemplate.GroupVersionKind(), m.Spec.RemediationTemplate.Name, t.Machine.Name, t.Machine.Namespace, m.Spec.ClusterName))
					return errList
				}

				generateTemplateInput := &external.GenerateTemplateInput{
					Template:    from,
					TemplateRef: m.Spec.RemediationTemplate,
					Namespace:   t.Machine.Namespace,
					ClusterName: t.Machine.ClusterName,
					OwnerRef:    cloneOwnerRef,
				}
				to, err := external.GenerateTemplate(generateTemplateInput)
				if err != nil {
					errList = append(errList, errors.Wrapf(err, "failed to create template for remediation request %v %q for machine %q in namespace %q within cluster %q", m.Spec.RemediationTemplate.GroupVersionKind(), m.Spec.RemediationTemplate.Name, t.Machine.Name, t.Machine.Namespace, m.Spec.ClusterName))
					return errList
				}

				// Set the Remediation Request to match the Machine name, the name is used to
				// guarantee uniqueness between runs. A Machine should only ever have a single
				// remediation object of a specific GVK created.
				//
				// NOTE: This doesn't guarantee uniqueness across different MHC objects watching
				// the same Machine, users are in charge of setting health checks and remediation properly.
				to.SetName(t.Machine.Name)

				logger.Info("Target has failed health check, creating an external remediation request", "remediation request name", to.GetName(), "target", t.string(), "reason", condition.Reason, "message", condition.Message)
				// Create the external clone.
				if err := r.Client.Create(ctx, to); err != nil {
					conditions.MarkFalse(m, clusterv1.ExternalRemediationRequestAvailable, clusterv1.ExternalRemediationRequestCreationFailed, clusterv1.ConditionSeverityError, err.Error())
					errList = append(errList, errors.Wrapf(err, "error creating remediation request for machine %q in namespace %q within cluster %q", t.Machine.Name, t.Machine.Namespace, t.Machine.ClusterName))
					return errList
				}
			} else {
				logger.Info("Target has failed health check, marking for remediation", "target", t.string(), "reason", condition.Reason, "message", condition.Message)
				// NOTE: MHC is responsible for creating MachineOwnerRemediatedCondition if missing or to trigger another remediation if the previous one is completed;
				// instead, if a remediation is in already progress, the remediation owner is responsible for completing the process and MHC should not overwrite the condition.
				if !conditions.Has(t.Machine, clusterv1.MachineOwnerRemediatedCondition) || conditions.IsTrue(t.Machine, clusterv1.MachineOwnerRemediatedCondition) {
					conditions.MarkFalse(t.Machine, clusterv1.MachineOwnerRemediatedCondition, clusterv1.WaitingForRemediationReason, clusterv1.ConditionSeverityWarning, "")
				}
			}
		}

		if err := t.patchHelper.Patch(ctx, t.Machine); err != nil {
			errList = append(errList, errors.Wrapf(err, "failed to patch unhealthy machine status for machine: %s/%s", t.Machine.Namespace, t.Machine.Name))
			continue
		}
		r.recorder.Eventf(
			t.Machine,
			corev1.EventTypeNormal,
			EventMachineMarkedUnhealthy,
			"Machine %v has been marked as unhealthy",
			t.string(),
		)
	}
	return errList
}

// clusterToMachineHealthCheck maps events from Cluster objects to
// MachineHealthCheck objects that belong to the Cluster.
func (r *MachineHealthCheckReconciler) clusterToMachineHealthCheck(o client.Object) []reconcile.Request {
	c, ok := o.(*clusterv1.Cluster)
	if !ok {
		panic(fmt.Sprintf("Expected a Cluster, got %T", o))
	}

	mhcList := &clusterv1.MachineHealthCheckList{}
	if err := r.Client.List(
		context.TODO(),
		mhcList,
		client.InNamespace(c.Namespace),
		client.MatchingLabels{clusterv1.ClusterLabelName: c.Name},
	); err != nil {
		return nil
	}

	// This list should only contain MachineHealthChecks which belong to the given Cluster
	requests := []reconcile.Request{}
	for _, mhc := range mhcList.Items {
		key := types.NamespacedName{Namespace: mhc.Namespace, Name: mhc.Name}
		requests = append(requests, reconcile.Request{NamespacedName: key})
	}
	return requests
}

// machineToMachineHealthCheck maps events from Machine objects to
// MachineHealthCheck objects that monitor the given machine.
func (r *MachineHealthCheckReconciler) machineToMachineHealthCheck(o client.Object) []reconcile.Request {
	m, ok := o.(*clusterv1.Machine)
	if !ok {
		panic(fmt.Sprintf("Expected a Machine, got %T", o))
	}

	mhcList := &clusterv1.MachineHealthCheckList{}
	if err := r.Client.List(
		context.TODO(),
		mhcList,
		client.InNamespace(m.Namespace),
		client.MatchingLabels{clusterv1.ClusterLabelName: m.Spec.ClusterName},
	); err != nil {
		return nil
	}

	var requests []reconcile.Request
	for k := range mhcList.Items {
		mhc := &mhcList.Items[k]
		if hasMatchingLabels(mhc.Spec.Selector, m.Labels) {
			key := util.ObjectKey(mhc)
			requests = append(requests, reconcile.Request{NamespacedName: key})
		}
	}
	return requests
}

func (r *MachineHealthCheckReconciler) nodeToMachineHealthCheck(o client.Object) []reconcile.Request {
	node, ok := o.(*corev1.Node)
	if !ok {
		panic(fmt.Sprintf("Expected a corev1.Node, got %T", o))
	}

	machine, err := noderefutil.GetMachineFromNode(context.TODO(), r.Client, node.Name)
	if machine == nil || err != nil {
		return nil
	}

	return r.machineToMachineHealthCheck(machine)
}

func (r *MachineHealthCheckReconciler) watchClusterNodes(ctx context.Context, logger logr.Logger, cluster *clusterv1.Cluster) error {
	// If there is no tracker, don't watch remote nodes
	if r.Tracker == nil {
		return nil
	}

	return r.Tracker.Watch(ctx, remote.WatchInput{
		Name:         "machinehealthcheck-watchClusterNodes",
		Cluster:      util.ObjectKey(cluster),
		Watcher:      r.controller,
		Kind:         &corev1.Node{},
		EventHandler: handler.EnqueueRequestsFromMapFunc(r.nodeToMachineHealthCheck),
		Predicates:   []predicate.Predicate{predicates.NodeConditionUpdated(logger)},
	})
}

// isAllowedRemediation checks the value of the MaxUnhealthy field to determine
// returns whether remediation should be allowed or not, the remediation count, and error if any.
func isAllowedRemediation(mhc *clusterv1.MachineHealthCheck) (bool, int32, error) {
	var remediationAllowed bool
	var remediationCount int32
	if mhc.Spec.UnhealthyRange != nil {
		min, max, err := getUnhealthyRange(mhc)
		if err != nil {
			return false, 0, err
		}
		unhealthyMachineCount := unhealthyMachineCount(mhc)
		remediationAllowed = unhealthyMachineCount >= min && unhealthyMachineCount <= max
		remediationCount = int32(max - unhealthyMachineCount)
		return remediationAllowed, remediationCount, nil
	}

	maxUnhealthy, err := getMaxUnhealthy(mhc)
	if err != nil {
		return false, 0, err
	}

	// Remediation is not allowed if unhealthy is above maxUnhealthy
	unhealthyMachineCount := unhealthyMachineCount(mhc)
	remediationAllowed = unhealthyMachineCount <= maxUnhealthy
	remediationCount = int32(maxUnhealthy - unhealthyMachineCount)
	return remediationAllowed, remediationCount, nil
}

// getUnhealthyRange parses an integer range and returns the min and max values
// Eg. [2-5] will return (2,5,nil).
func getUnhealthyRange(mhc *clusterv1.MachineHealthCheck) (int, int, error) {
	// remove '[' and ']'
	unhealthyRange := (*(mhc.Spec.UnhealthyRange))[1 : len(*mhc.Spec.UnhealthyRange)-1]

	parts := strings.Split(unhealthyRange, "-")

	min, err := strconv.ParseUint(parts[0], 10, 32)
	if err != nil {
		return 0, 0, err
	}

	max, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		return 0, 0, err
	}

	if max < min {
		return 0, 0, errors.Errorf("max value %d cannot be less than min value %d for unhealthyRange", max, min)
	}

	return int(min), int(max), nil
}

func getMaxUnhealthy(mhc *clusterv1.MachineHealthCheck) (int, error) {
	if mhc.Spec.MaxUnhealthy == nil {
		return 0, errors.New("spec.maxUnhealthy must be set")
	}
	maxUnhealthy, err := intstr.GetScaledValueFromIntOrPercent(mhc.Spec.MaxUnhealthy, int(mhc.Status.ExpectedMachines), false)
	if err != nil {
		return 0, err
	}
	return maxUnhealthy, nil
}

// unhealthyMachineCount calculates the number of presently unhealthy or missing machines
// ie the delta between the expected number of machines and the current number deemed healthy.
func unhealthyMachineCount(mhc *clusterv1.MachineHealthCheck) int {
	return int(mhc.Status.ExpectedMachines - mhc.Status.CurrentHealthy)
}

// getExternalRemediationRequest gets reference to External Remediation Request, unstructured object.
func (r *MachineHealthCheckReconciler) getExternalRemediationRequest(ctx context.Context, m *clusterv1.MachineHealthCheck, machineName string) (*unstructured.Unstructured, error) {
	remediationRef := &corev1.ObjectReference{
		APIVersion: m.Spec.RemediationTemplate.APIVersion,
		Kind:       strings.TrimSuffix(m.Spec.RemediationTemplate.Kind, external.TemplateSuffix),
		Name:       machineName,
	}
	remediationReq, err := external.Get(ctx, r.Client, remediationRef, m.Namespace)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to retrieve external remediation request object")
	}
	return remediationReq, nil
}

// externalRemediationRequestExists checks if the External Remediation Request is created
// for the machine.
func (r *MachineHealthCheckReconciler) externalRemediationRequestExists(ctx context.Context, m *clusterv1.MachineHealthCheck, machineName string) bool {
	remediationReq, err := r.getExternalRemediationRequest(ctx, m, machineName)
	if err != nil {
		return false
	}
	return remediationReq != nil
}
