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

import clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"

// TODO: NOTE: The API details will be incrementally added and will eventually
// conform to the CAPI Provider CAEP.

// ProviderSpec is the desired state of the Provider.
type ProviderSpec struct{}

// ProviderStatus defines the observed state of the Provider.
type ProviderStatus struct {
	// Conditions define the current service state of the provider.
	// +optional
	Conditions clusterv1.Conditions `json:"conditions,omitempty"`
}
