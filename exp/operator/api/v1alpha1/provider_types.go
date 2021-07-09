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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
)

// TODO: NOTE: The API details will be incrementally added and will eventually
// conform to the CAPI Provider CAEP.

// ProProviderSpec is the desired state of the Provider.
type ProviderSpec struct {
	// Version indicates the provider version.
	// +optional
	Version *string `json:"version,omitempty"`

	// Deployment defines the properties that can be enabled on the deployment for the provider.
	// +optional
	Deployment *DeploymentSpec `json:"deployment,omitempty"`

	// SecretName is the name of the Secret providing the configuration
	// variables for the current provider instance, like e.g. credentials.
	// Such configurations will be used when creating or upgrading provider components.
	// The contents of the secret will be treated as immutable. If changes need
	// to be made, a new object can be created and the name should be updated.
	// The contents should be in the form of key:value. This secret must be in
	// the same namespace as the provider.
	// +optional
	SecretName *string `json:"secretName,omitempty"`

	// FetchConfig determines how the operator will fetch the components and metadata for the provider.
	// If nil, the operator will try to fetch components according to default
	// embedded fetch configuration for the given kind and `ObjectMeta.Name`.
	// For example, the infrastructure name `aws` will fetch artifacts from
	// https://github.com/kubernetes-sigs/cluster-api-provider-aws/releases.
	// +optional
	FetchConfig *FetchConfiguration `json:"fetchConfig,omitempty"`
}

// DeploymentSpec defines the properties that can be enabled on the Deployment for the provider.
type DeploymentSpec struct {
	// Number of desired pods. This is a pointer to distinguish between explicit zero and not specified. Defaults to 1.
	// +optional
	Replicas *int `json:"replicas,omitempty"`

	// NodeSelector is a selector which must be true for the pod to fit on a node.
	// Selector which must match a node's labels for the pod to be scheduled on that node.
	// More info: https://kubernetes.io/docs/concepts/configuration/assign-pod-node/
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// If specified, the pod's tolerations.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// If specified, the pod's scheduling constraints
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// List of containers specified in the Deployment
	// +optional
	Containers []ContainerSpec `json:"containers"`
}

// ContainerSpec defines the properties available to override for each
// container in a provider deployment such as Image and Args to the container’s
// entrypoint.
type ContainerSpec struct {
	// Name of the container. Cannot be updated.
	Name string `json:"name"`

	// Container Image Name
	// +optional
	Image *ImageMeta `json:"image,omitempty"`

	// Args represents extra provider specific flags that are not encoded as fields in this API.
	// Explicit controller manager properties defined in the `Provider.ManagerSpec`
	// will have higher precedence than those defined in `ContainerSpec.Args`.
	// For example, `ManagerSpec.SyncPeriod` will be used instead of the
	// container arg `--sync-period` if both are defined.
	// The same holds for `ManagerSpec.FeatureGates` and `--feature-gates`.
	// +optional
	Args map[string]string `json:"args,omitempty"`

	// List of environment variables to set in the container.
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// Compute resources required by this container.
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
}

// ImageMeta allows to customize the image used.
type ImageMeta struct {
	// Repository sets the container registry to pull images from.
	// +optional
	Repository *string `json:"repository,omitempty"`

	// Name allows to specify a name for the image.
	// +optional
	Name *string `json:"name,omitempty"`

	// Tag allows to specify a tag for the image.
	// +optional
	Tag *string `json:"tag,omitempty"`
}

// FetchConfiguration determines the way to fetch the components and metadata for the provider.
type FetchConfiguration struct {
	// URL to be used for fetching the provider’s components and metadata from a remote Github repository.
	// For example, https://github.com/{owner}/{repository}/releases
	// The version of the release will be `ProviderSpec.Version` if defined
	// otherwise the `latest` version will be computed and used.
	// +optional
	URL *string `json:"url,omitempty"`

	// Selector to be used for fetching provider’s components and metadata from
	// ConfigMaps stored inside the cluster. Each ConfigMap is expected to contain
	// components and metadata for a specific version only.
	// +optional
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
}

// ProProviderStatus defines the observed state of the Provider.
type ProviderStatus struct {
	// Conditions define the current service state of the provider.
	// +optional
	Conditions clusterv1.Conditions `json:"conditions,omitempty"`
}
