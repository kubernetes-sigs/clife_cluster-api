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
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/cluster-api/feature"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

func (in *ClusterClass) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(in).
		Complete()
}

// +kubebuilder:webhook:verbs=create;update,path=/validate-cluster-x-k8s-io-v1alpha4-clusterclass,mutating=false,failurePolicy=fail,matchPolicy=Equivalent,groups=cluster.x-k8s.io,resources=clusterclasses,versions=v1alpha4,name=validation.clusterclass.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1;v1beta1
// +kubebuilder:webhook:verbs=create;update,path=/mutate-cluster-x-k8s-io-v1alpha4-clusterclass,mutating=true,failurePolicy=fail,matchPolicy=Equivalent,groups=cluster.x-k8s.io,resources=clusterclasses,versions=v1alpha4,name=default.clusterclass.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1;v1beta1

var _ webhook.Validator = &ClusterClass{}
var _ webhook.Defaulter = &ClusterClass{}

// Default satisfies the defaulting webhook interface.
func (in *ClusterClass) Default() {
	// Default all namespaces in the references to the object namespace.
	if in.Spec.Infrastructure.Ref != nil && len(in.Spec.Infrastructure.Ref.Namespace) == 0 {
		in.Spec.Infrastructure.Ref.Namespace = in.Namespace
	}
	if in.Spec.ControlPlane.Ref != nil && len(in.Spec.ControlPlane.Ref.Namespace) == 0 {
		in.Spec.ControlPlane.Ref.Namespace = in.Namespace
	}
	for i := range in.Spec.Workers.MachineDeployments {
		if in.Spec.Workers.MachineDeployments[i].Template.Bootstrap.Ref != nil &&
			len(in.Spec.Workers.MachineDeployments[i].Template.Bootstrap.Ref.Namespace) == 0 {
			in.Spec.Workers.MachineDeployments[i].Template.Bootstrap.Ref.Namespace = in.Namespace
		}
		if in.Spec.Workers.MachineDeployments[i].Template.Infrastructure.Ref != nil &&
			len(in.Spec.Workers.MachineDeployments[i].Template.Infrastructure.Ref.Namespace) == 0 {
			in.Spec.Workers.MachineDeployments[i].Template.Infrastructure.Ref.Namespace = in.Namespace
		}
	}
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type.
func (in *ClusterClass) ValidateCreate() error {
	return in.validate(nil)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type.
func (in *ClusterClass) ValidateUpdate(old runtime.Object) error {
	oldClusterClass, ok := old.(*ClusterClass)
	if !ok {
		return apierrors.NewBadRequest(fmt.Sprintf("expected a ClusterClass but got a %T", old))
	}
	return in.validate(oldClusterClass)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type.
func (in *ClusterClass) ValidateDelete() error {
	return nil
}

func (in *ClusterClass) validate(old *ClusterClass) error {
	// NOTE: ClusterClass and managed topologies are behind ClusterTopology feature gate flag; the web hook
	// must prevent creating new objects in case the feature flag is disabled.
	if !feature.Gates.Enabled(feature.ClusterTopology) {
		return field.Forbidden(
			field.NewPath("spec"),
			"can be set only if the ClusterTopology feature flag is enabled",
		)
	}

	var allErrs field.ErrorList

	// ensure all the references are within the same namespace
	allErrs = append(allErrs, in.validateRefNamespace()...)

	// Ensure all references are templates
	allErrs = append(allErrs, in.validateRefTemplates()...)

	// Ensure that all the refs have valid apiVersions
	allErrs = append(allErrs, in.validateRefAPIVersions()...)

	// Ensure all machine deployments are unique
	// for update:
	//   also ensure that the old machine deployments still exist and new deployments are unique
	allErrs = append(allErrs, in.validateMachineDeployments(old)...)

	// Ensure sure no additional changes were applied.
	allErrs = append(allErrs, in.validateAdditionalChanges(old)...)

	if len(allErrs) > 0 {
		return apierrors.NewInvalid(GroupVersion.WithKind("ClusterClass").GroupKind(), in.Name, allErrs)
	}
	return nil
}

func (in *ClusterClass) validateRefNamespace() field.ErrorList {
	var allErrs field.ErrorList

	if in.Spec.Infrastructure.Ref.Namespace != in.Namespace {
		allErrs = append(
			allErrs,
			field.Invalid(
				field.NewPath("spec", "infrastructure", "ref", "namespace"),
				in.Spec.Infrastructure.Ref.Namespace,
				"must match metadata.namespace",
			),
		)
	}
	if in.Spec.ControlPlane.Ref.Namespace != in.Namespace {
		allErrs = append(
			allErrs,
			field.Invalid(
				field.NewPath("spec", "controlPlane", "ref", "namespace"),
				in.Spec.ControlPlane.Ref.Namespace,
				"must match metadata.namespace",
			),
		)
	}
	for _, class := range in.Spec.Workers.MachineDeployments {
		if class.Template.Bootstrap.Ref.Namespace != in.Namespace {
			allErrs = append(allErrs,
				field.Invalid(
					field.NewPath("spec", "workers", "machineDeployments", "template", "bootstrap", "ref", "namespace"),
					class.Template.Bootstrap.Ref.Namespace,
					"must match metadata.namespace",
				),
			)
		}
		if class.Template.Infrastructure.Ref.Namespace != in.Namespace {
			allErrs = append(allErrs,
				field.Invalid(
					field.NewPath("spec", "workers", "machineDeployments", "template", "infrastructure", "ref", "namespace"),
					class.Template.Infrastructure.Ref.Namespace,
					"must match metadata.namespace",
				),
			)
		}
	}
	return allErrs
}

func (in ClusterClass) validateRefTemplates() field.ErrorList {
	var allErrs field.ErrorList

	if !strings.HasSuffix(strings.ToLower(in.Spec.Infrastructure.Ref.Kind), "template") {
		allErrs = append(allErrs,
			field.Invalid(
				field.NewPath("spec", "infrastructure", "ref", "kind"),
				in.Spec.Infrastructure.Ref.Kind,
				"must be a template",
			),
		)
	}
	if !strings.HasSuffix(strings.ToLower(in.Spec.ControlPlane.Ref.Kind), "template") {
		allErrs = append(allErrs,
			field.Invalid(
				field.NewPath("spec", "controlplane", "ref", "kind"),
				in.Spec.ControlPlane.Ref.Kind,
				"must be a template",
			),
		)
	}
	for _, class := range in.Spec.Workers.MachineDeployments {
		if !strings.HasSuffix(strings.ToLower(class.Template.Bootstrap.Ref.Kind), "template") {
			allErrs = append(allErrs,
				field.Invalid(
					field.NewPath("spec", "workers", "machineDeployments", "template", "bootstrap", "ref", "kind"),
					class.Template.Bootstrap.Ref.Kind,
					"must be a template",
				),
			)
		}
		if !strings.HasSuffix(strings.ToLower(class.Template.Infrastructure.Ref.Kind), "template") {
			allErrs = append(allErrs,
				field.Invalid(
					field.NewPath("spec", "workers", "machineDeployments", "template", "infrastructure", "ref", "kind"),
					class.Template.Infrastructure.Ref.Kind,
					"must be a template",
				),
			)
		}
	}
	return allErrs
}

func (in ClusterClass) validateRefAPIVersions() field.ErrorList {
	var allErrs field.ErrorList

	if in.Spec.Infrastructure.Ref.APIVersion != "" {
		if _, err := schema.ParseGroupVersion(in.Spec.Infrastructure.Ref.APIVersion); err != nil {
			allErrs = append(allErrs,
				field.Invalid(
					field.NewPath("spec", "infrastructure", "ref", "apiVersion"),
					in.Spec.Infrastructure.Ref.APIVersion,
					fmt.Sprintf("must be a valid apiVerison. found error: %v", err),
				),
			)
		}
	}
	if in.Spec.ControlPlane.Ref.APIVersion != "" {
		if _, err := schema.ParseGroupVersion(in.Spec.ControlPlane.Ref.APIVersion); err != nil {
			allErrs = append(allErrs,
				field.Invalid(
					field.NewPath("spec", "controlplane", "ref", "apiVersion"),
					in.Spec.ControlPlane.Ref.APIVersion,
					fmt.Sprintf("must be a valid apiVerison. found error: %v", err),
				),
			)
		}
	}
	for _, class := range in.Spec.Workers.MachineDeployments {
		if class.Template.Bootstrap.Ref.APIVersion != "" {
			if _, err := schema.ParseGroupVersion(class.Template.Bootstrap.Ref.APIVersion); err != nil {
				allErrs = append(allErrs,
					field.Invalid(
						field.NewPath("spec", "workers", "machineDeployments", "template", "bootstrap", "ref", "apiVersion"),
						class.Template.Bootstrap.Ref.APIVersion,
						fmt.Sprintf("must be a valid apiVerison. found error: %v", err),
					),
				)
			}
		}
		if class.Template.Infrastructure.Ref.APIVersion != "" {
			if _, err := schema.ParseGroupVersion(class.Template.Infrastructure.Ref.APIVersion); err != nil {
				allErrs = append(allErrs,
					field.Invalid(
						field.NewPath("spec", "workers", "machineDeployments", "template", "infrastructure", "ref", "apiVersion"),
						class.Template.Infrastructure.Ref.APIVersion,
						fmt.Sprintf("must be a valid apiVerison. found error: %v", err),
					),
				)
			}
		}
	}
	return allErrs
}

func (in ClusterClass) validateMachineDeployments(old *ClusterClass) field.ErrorList {
	var allErrs field.ErrorList

	// Ensure MachineDeployment class are unique.
	classNames := sets.String{}
	for _, class := range in.Spec.Workers.MachineDeployments {
		if classNames.Has(class.Class) {
			allErrs = append(allErrs,
				field.Invalid(
					field.NewPath("spec", "workers", "machineDeployments"),
					class,
					fmt.Sprintf("MachineDeployment class should be unique. MachineDeployment with class %q is defined more than once.", class.Class),
				),
			)
		}
		classNames.Insert(class.Class)
	}

	// incase of crate we are done
	if old == nil {
		return allErrs
	}

	// incase of update:

	// Makes sure all the old MachineDeployment classes are still there (only MachineDeployment class addition are allowed).
	oldClassNames := sets.String{}
	for _, oldClass := range old.Spec.Workers.MachineDeployments {
		if !classNames.Has(oldClass.Class) {
			allErrs = append(allErrs,
				field.Invalid(
					field.NewPath("spec", "workers", "machineDeployments"),
					in.Spec.Workers.MachineDeployments,
					fmt.Sprintf("The %q MachineDeployment class can't be removed.", oldClass.Class),
				),
			)
		}
		oldClassNames.Insert(oldClass.Class)
	}

	return allErrs
}

func (in ClusterClass) validateAdditionalChanges(old *ClusterClass) field.ErrorList {
	var allErrs field.ErrorList

	if old == nil {
		return nil
	}

	if !reflect.DeepEqual(in.Spec.Infrastructure, old.Spec.Infrastructure) {
		allErrs = append(allErrs,
			field.Invalid(
				field.NewPath("spec", "infrastructure"),
				in.Spec.Infrastructure,
				"cannot be changed.",
			),
		)
	}

	if !reflect.DeepEqual(in.Spec.ControlPlane, old.Spec.ControlPlane) {
		allErrs = append(allErrs,
			field.Invalid(
				field.NewPath("spec", "controlPlane"),
				in.Spec.Infrastructure,
				"cannot be changed.",
			),
		)
	}

	for _, class := range in.Spec.Workers.MachineDeployments {
		for _, oldClass := range old.Spec.Workers.MachineDeployments {
			if class.Class == oldClass.Class && !reflect.DeepEqual(class, oldClass) {
				allErrs = append(allErrs,
					field.Invalid(
						field.NewPath("spec", "workers", "machineDeployments"),
						class,
						"cannot be changed.",
					),
				)
			}
		}
	}

	return allErrs
}
