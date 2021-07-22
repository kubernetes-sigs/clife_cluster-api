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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

func (c *DockerCluster) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(c).
		Complete()
}

// +kubebuilder:webhook:verbs=create;update,path=/mutate-infrastructure-cluster-x-k8s-io-v1alpha4-dockercluster,mutating=true,failurePolicy=fail,matchPolicy=Equivalent,groups=infrastructure.cluster.x-k8s.io,resources=dockerclusters,versions=v1alpha4,name=default.dockercluster.infrastructure.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1;v1beta1

var _ webhook.Defaulter = &DockerCluster{}

// Default implements webhook.Defaulter so a webhook will be registered for the type.
func (c *DockerCluster) Default() {
	defaultDockerClusterSpec(&c.Spec)
}

// +kubebuilder:webhook:verbs=create;update,path=/validate-infrastructure-cluster-x-k8s-io-v1alpha4-dockercluster,mutating=false,failurePolicy=fail,matchPolicy=Equivalent,groups=infrastructure.cluster.x-k8s.io,resources=dockerclusters,versions=v1alpha4,name=validation.dockercluster.infrastructure.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1;v1beta1

var _ webhook.Validator = &DockerCluster{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type.
func (c *DockerCluster) ValidateCreate() error {
	allErrs := validateDockerClusterSpec(c.Spec)
	if len(allErrs) > 0 {
		return apierrors.NewInvalid(GroupVersion.WithKind("DockerCluster").GroupKind(), c.Name, allErrs)
	}
	return nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type.
func (c *DockerCluster) ValidateUpdate(old runtime.Object) error {
	return nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type.
func (c *DockerCluster) ValidateDelete() error {
	return nil
}

func defaultDockerClusterSpec(s *DockerClusterSpec) {}

func validateDockerClusterSpec(s DockerClusterSpec) field.ErrorList {
	return nil
}
