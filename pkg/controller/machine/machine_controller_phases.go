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

package machine

import (
	"context"
	"path"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/cluster-api/pkg/apis/cluster/common"
	"sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha2"
	capierrors "sigs.k8s.io/cluster-api/pkg/controller/error"
	"sigs.k8s.io/cluster-api/pkg/util"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *ReconcileMachine) reconcile(ctx context.Context, m *v1alpha2.Machine) error {
	bootstrapErr := r.reconcileBootstrap(ctx, m)
	infrastructureErr := r.reconcileInfrastructure(ctx, m)

	// Set the phase to "pending" if nil.
	if m.Status.Phase == nil {
		m.Status.SetTypedPhase(v1alpha2.MachinePhasePending)
	}

	// Set the phase to "provisioning" if bootstrap is ready and the infrastructure isn't.
	if (m.Status.BootstrapReady != nil && *m.Status.BootstrapReady) &&
		(m.Status.InfrastructureReady == nil || !*m.Status.InfrastructureReady) {
		m.Status.SetTypedPhase(v1alpha2.MachinePhaseProvisioning)
	}

	// Set the phase to "provisioned" if the infrastructure is ready.
	if m.Status.InfrastructureReady != nil && *m.Status.InfrastructureReady {
		m.Status.SetTypedPhase(v1alpha2.MachinePhaseProvisioned)
	}

	// Set the phase to "running" if there is a NodeRef field.
	if m.Status.NodeRef != nil &&
		(m.Status.InfrastructureReady != nil && *m.Status.InfrastructureReady) {
		m.Status.SetTypedPhase(v1alpha2.MachinePhaseRunning)
	}

	// Set the phase to "failed" if any of Status.ErrorReason or Status.ErrorMessage is not-nil.
	if m.Status.ErrorReason != nil || m.Status.ErrorMessage != nil {
		m.Status.SetTypedPhase(v1alpha2.MachinePhaseFailed)
	}

	// Set the phase to "deleting" if the deletion timestamp is set.
	if !m.DeletionTimestamp.IsZero() {
		m.Status.SetTypedPhase(v1alpha2.MachinePhaseDeleting)
	}

	// Determine the return error, giving precedence to non-nil errors and non-requeueAfter.
	var err error
	if bootstrapErr != nil {
		err = bootstrapErr
	}
	if infrastructureErr != nil && (err == nil || capierrors.IsRequeueAfter(err)) {
		err = infrastructureErr
	}
	return err
}

// reconcileExternal handles generic unstructured objects referenced by a Machine.
func (r *ReconcileMachine) reconcileExternal(ctx context.Context, m *v1alpha2.Machine, ref *corev1.ObjectReference) (*unstructured.Unstructured, error) {
	// TODO(vincepri): Handle watching dynamic external objects.

	obj, err := r.getExternal(ctx, ref, m.Namespace)
	if err != nil {
		if apierrors.IsNotFound(err) && !m.DeletionTimestamp.IsZero() {
			return nil, nil
		} else if apierrors.IsNotFound(err) {
			return nil, errors.Wrapf(&capierrors.RequeueAfterError{RequeueAfter: 30 * time.Second},
				"could not find %s %q for Machine %q in namespace %q, requeuing",
				path.Join(ref.APIVersion, ref.Kind), ref.Name, m.Name, m.Namespace)
		}
		return nil, err
	}

	objPatch := client.MergeFrom(obj.DeepCopy())

	// Delete the external object if the Machine is being deleted.
	if !m.DeletionTimestamp.IsZero() {
		if err := r.Delete(ctx, obj); err != nil {
			return nil, errors.Wrapf(err,
				"failed to delete %s %q for Machine %q in namespace %q",
				path.Join(ref.APIVersion, ref.Kind), ref.Name, m.Name, m.Namespace)
		}
		return obj, nil
	}

	// Set external object OwnerReference to the Machine.
	machineOwnerRef := metav1.OwnerReference{
		APIVersion: m.APIVersion,
		Kind:       m.Kind,
		Name:       m.Name,
		UID:        m.UID,
	}

	if !util.HasOwnerRef(obj.GetOwnerReferences(), machineOwnerRef) {
		obj.SetOwnerReferences(util.EnsureOwnerRef(obj.GetOwnerReferences(), machineOwnerRef))
		if err := r.Patch(ctx, obj, objPatch); err != nil {
			return nil, errors.Wrapf(err,
				"failed to set OwnerReference on %s %q for Machine %q in namespace %q",
				path.Join(ref.APIVersion, ref.Kind), ref.Name, m.Name, m.Namespace)
		}
	}

	// Set error reason and message, if any.
	errorReason, errorMessage, err := r.getExternalErrors(obj)
	if err != nil {
		return nil, err
	}
	if errorReason != "" {
		machineStatusError := common.MachineStatusError(errorReason)
		m.Status.ErrorReason = &machineStatusError
	}
	if errorMessage != "" {
		m.Status.ErrorMessage = pointer.StringPtr(errorMessage)
	}

	return obj, nil
}

// reconcileBootstrap reconciles the Spec.Bootstrap.ConfigRef object on a Machine.
func (r *ReconcileMachine) reconcileBootstrap(ctx context.Context, m *v1alpha2.Machine) error {
	// TODO(vincepri): Move this validation in kubebuilder / webhook.
	if m.Spec.Bootstrap.ConfigRef == nil && m.Spec.Bootstrap.Data == nil {
		return errors.Errorf(
			"Expected at least one of `Bootstrap.ConfigRef` or `Bootstrap.Data` to be populated for Machine %q in namespace %q",
			m.Name, m.Namespace,
		)
	}

	if m.Spec.Bootstrap.Data != nil {
		m.Status.BootstrapReady = pointer.BoolPtr(true)
		return nil
	}

	// Call generic external reconciler.
	bootstrapConfig, err := r.reconcileExternal(ctx, m, m.Spec.Bootstrap.ConfigRef)
	if bootstrapConfig == nil && err == nil {
		m.Status.BootstrapReady = pointer.BoolPtr(false)
		return nil
	} else if err != nil {
		return err
	}

	// If the bootstrap config is being deleted, return early.
	if !bootstrapConfig.GetDeletionTimestamp().IsZero() {
		return nil
	}

	// Determine if the bootstrap provider is ready.
	ready, err := r.isExternalReady(bootstrapConfig)
	if err != nil {
		return err
	} else if !ready {
		klog.V(3).Infof("Bootstrap provider for Machine %q in namespace %q is not ready, requeuing", m.Name, m.Namespace)
		return &capierrors.RequeueAfterError{RequeueAfter: 30 * time.Second}
	}

	// Get and set data from the bootstrap provider.
	data, _, err := unstructured.NestedString(bootstrapConfig.Object, "status", "bootstrapData")
	if err != nil {
		return errors.Wrapf(err, "failed to retrieve data from bootstrap provider for Machine %q in namespace %q", m.Name, m.Namespace)
	} else if data == "" {
		return errors.Errorf("retrieved empty data from bootstrap provider for Machine %q in namespace %q", m.Name, m.Namespace)
	}

	m.Spec.Bootstrap.Data = pointer.StringPtr(data)
	m.Status.BootstrapReady = pointer.BoolPtr(true)
	return nil
}

// reconcileInfrastructure reconciles the Spec.InfrastructureRef object on a Machine.
func (r *ReconcileMachine) reconcileInfrastructure(ctx context.Context, m *v1alpha2.Machine) error {
	// Call generic external reconciler.
	infraConfig, err := r.reconcileExternal(ctx, m, &m.Spec.InfrastructureRef)
	if infraConfig == nil && err == nil {
		return nil
	} else if err != nil {
		return err
	}

	if (m.Status.InfrastructureReady != nil && *m.Status.InfrastructureReady) ||
		!infraConfig.GetDeletionTimestamp().IsZero() {
		return nil
	}

	// Determine if the infrastructure provider is ready
	ready, err := r.isExternalReady(infraConfig)
	if err != nil {
		return err
	} else if !ready {
		klog.V(3).Infof("Infrastructure provider for Machine %q in namespace %q is not ready, requeuing", m.Name, m.Namespace)
		return &capierrors.RequeueAfterError{RequeueAfter: 30 * time.Second}
	}

	m.Status.InfrastructureReady = pointer.BoolPtr(true)
	return nil
}

// isExternalReady returns true if the Status.Ready field on an external object is true.
func (r *ReconcileMachine) isExternalReady(obj *unstructured.Unstructured) (bool, error) {
	ready, found, err := unstructured.NestedBool(obj.Object, "status", "ready")
	if err != nil {
		return false, errors.Wrapf(err, "failed to determine %s %q readiness",
			path.Join(obj.GetAPIVersion(), obj.GetKind()), obj.GetName())
	}
	return ready && found, nil
}

// getExternalErrors return the ErrorReason and ErrorMessage fields from the external object status.
func (r *ReconcileMachine) getExternalErrors(obj *unstructured.Unstructured) (string, string, error) {
	errorReason, _, err := unstructured.NestedString(obj.Object, "status", "errorReason")
	if err != nil {
		return "", "", errors.Wrapf(err, "failed to determine errorReason on %s %q",
			path.Join(obj.GetAPIVersion(), obj.GetKind()), obj.GetName())
	}
	errorMessage, _, err := unstructured.NestedString(obj.Object, "status", "errorMessage")
	if err != nil {
		return "", "", errors.Wrapf(err, "failed to determine errorMessage on %s %q",
			path.Join(obj.GetAPIVersion(), obj.GetKind()), obj.GetName())
	}
	return errorReason, errorMessage, nil
}

// getExternal takes an ObjectReference and namespace and returns an Unstructured object.
func (r *ReconcileMachine) getExternal(ctx context.Context, ref *corev1.ObjectReference, namespace string) (*unstructured.Unstructured, error) {
	obj := new(unstructured.Unstructured)
	obj.SetAPIVersion(ref.APIVersion)
	obj.SetKind(ref.Kind)
	obj.SetName(ref.Name)
	key := client.ObjectKey{Name: obj.GetName(), Namespace: namespace}
	if err := r.Get(ctx, key, obj); err != nil {
		return nil, err
	}
	return obj, nil
}
