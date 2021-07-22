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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// IdentityReference is a reference to an infrastructure
// provider identity to be used to provision cluster resources.
type IdentityReference struct {
	// Kind of the identity. Must be supported by the infrastructure
	// provider and may be either cluster or namespace-scoped.
	// +kubebuilder:validation:MinLength=1
	Kind string `json:"kind"`

	// Name of the infrastructure identity to be used.
	// Must be either a cluster-scoped resource, or namespaced-scoped
	// resource the same namespace as the resource(s) being provisioned.
	Name string `json:"name"`
}

type InfraClusterResourceIdentitySpec struct {
	// IdentityReference is a reference to an infrastructure
	// provider identity to be used to provision cluster resources.
	IdentityRef *IdentityReference `json:"identityRef,omitempty"`
}

type InfraClusterScopedIdentityCommonSpec struct {
	// AllowedNamespaces is used to identify which namespaces are allowed to use the identity from.
	// Namespaces can be selected either using an array of namespaces or with label selector.
	// A namespace should be either in the NamespaceList or match with Selector to use the identity.
	//
	// +optional
	AllowedNamespaces InfraClusterScopedIdentityAllowedNamespaces `json:"allowedNamespaces"`
}

type InfraClusterScopedIdentitySecretSpec struct {
	// SecretRef is the name of the secret in the controller
	// namespace containing private information for this
	// identity.
	SecretRef string `json:"secretRef"`
}

type InfraNamespaceScopedIdentitySecretSpec struct {
	// SecretRef is the name of the secret in the same namespace
	// containing private information for this identity.
	// identity.
	SecretRef string `json:"secretRef"`
}

type InfraClusterScopedIdentityAllowedNamespaces struct {
	// EmptySelectorMatch defines what occurs when no selectors are provided.
	// Valid values are MatchNone (match no namespaces) and MatchAll (match all namespaces).
	// The default value is MatchNone
	// +kubebuilder:default=MatchNone
	// +kubebuilder:validation:Enum=MatchNone;MatchAll
	EmptySelectorMatch string `json:"emptySelectorMatch"`

	// Selector is a label query over a set of resources. The result of matchLabels and
	// matchExpressions are ANDed. An empty label selector matches all objects. A null
	// label selector matches no objects.
	//
	// This field is mutually exclusive with list.
	// +optional
	Selector metav1.LabelSelector `json:"selector"`

	// DEPRECATED.
	// An nil or empty list indicates that resources cannot use the identity from any namespace.
	//
	// This field should not be used where Cluster API is deployed to Kubernetes
	// v1.21 or above, where the selector should be used instead.
	// This field is mutually exclusive with selector.
	//
	// +optional
	// +nullable
	List []string `json:"list"`
}
