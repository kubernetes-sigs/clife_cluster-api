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

package v1alpha4

import (
	"fmt"
	"reflect"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	runtime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

const dockerMachineTemplateImmutableMsg = "DockerMachineTemplate spec.template.spec field is immutable. Please create a new resource instead."

func (m *DockerMachineTemplate) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(m).
		Complete()
}

// +kubebuilder:webhook:verbs=create;update,path=/validate-infrastructure-cluster-x-k8s-io-v1alpha4-dockermachinetemplate,mutating=false,failurePolicy=fail,matchPolicy=Equivalent,groups=infrastructure.cluster.x-k8s.io,resources=dockermachinetemplates,versions=v1alpha4,name=validation.dockermachinetemplate.infrastructure.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1;v1beta1

var _ webhook.Validator = &DockerMachineTemplate{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type.
func (m *DockerMachineTemplate) ValidateCreate() error {
	return nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type.
func (m *DockerMachineTemplate) ValidateUpdate(oldRaw runtime.Object) error {
	var allErrs field.ErrorList
	old, ok := oldRaw.(*DockerMachineTemplate)
	if !ok {
		return apierrors.NewBadRequest(fmt.Sprintf("expected a DockerMachineTemplate but got a %T", oldRaw))
	}
	if !reflect.DeepEqual(m.Spec.Template.Spec, old.Spec.Template.Spec) {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "template", "spec"), m, dockerMachineTemplateImmutableMsg))
	}
	if len(allErrs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(GroupVersion.WithKind("DockerMachineTemplate").GroupKind(), m.Name, allErrs)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type.
func (m *DockerMachineTemplate) ValidateDelete() error {
	return nil
}
