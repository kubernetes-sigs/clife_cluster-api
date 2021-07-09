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

package v1alpha4

import (
	"fmt"
	"reflect"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

const kubeadmControlPlaneTemplateImmutableMsg = "KubeadmControlPlaneTemplate spec.template.spec field is immutable. Please create new resource instead."

func (r *KubeadmControlPlaneTemplate) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:verbs=create;update,path=/mutate-controlplane-cluster-x-k8s-io-v1alpha4-kubeadmcontrolplanetemplate,mutating=true,failurePolicy=fail,groups=controlplane.cluster.x-k8s.io,resources=kubeadmcontrolplanetemplates,versions=v1alpha4,name=default.kubeadmcontrolplanetemplate.controlplane.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1;v1beta1

var _ webhook.Defaulter = &KubeadmControlPlaneTemplate{}

// Default implements webhook.Defaulter so a webhook will be registered for the type.
func (r *KubeadmControlPlaneTemplate) Default() {
	defaultKubeadmControlPlaneSpec(&r.Spec.Template.Spec, r.Namespace)
}

// +kubebuilder:webhook:verbs=create;update,path=/validate-controlplane-cluster-x-k8s-io-v1alpha4-kubeadmcontrolplanetemplate,mutating=false,failurePolicy=fail,groups=controlplane.cluster.x-k8s.io,resources=kubeadmcontrolplanetemplates,versions=v1alpha4,name=validation.kubeadmcontrolplanetemplate.controlplane.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1;v1beta1

var _ webhook.Validator = &KubeadmControlPlaneTemplate{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type.
func (r *KubeadmControlPlaneTemplate) ValidateCreate() error {
	spec := r.Spec.Template.Spec
	allErrs := validateKubeadmControlPlaneSpec(spec, r.Namespace, field.NewPath("spec", "template", "spec"))
	allErrs = append(allErrs, validateEtcd(&spec, nil)...)
	if len(allErrs) > 0 {
		return apierrors.NewInvalid(GroupVersion.WithKind("KubeadmControlPlaneTemplate").GroupKind(), r.Name, allErrs)
	}
	return nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type.
func (r *KubeadmControlPlaneTemplate) ValidateUpdate(oldRaw runtime.Object) error {
	var allErrs field.ErrorList
	old, ok := oldRaw.(*KubeadmControlPlaneTemplate)
	if !ok {
		return apierrors.NewBadRequest(fmt.Sprintf("expected a KubeadmControlPlaneTemplate but got a %T", oldRaw))
	}

	if !reflect.DeepEqual(r.Spec.Template.Spec, old.Spec.Template.Spec) {
		allErrs = append(allErrs,
			field.Invalid(field.NewPath("spec", "template", "spec"), r, kubeadmControlPlaneTemplateImmutableMsg),
		)
	}

	if len(allErrs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(GroupVersion.WithKind("KubeadmControlPlaneTemplate").GroupKind(), r.Name, allErrs)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type.
func (r *KubeadmControlPlaneTemplate) ValidateDelete() error {
	return nil
}
