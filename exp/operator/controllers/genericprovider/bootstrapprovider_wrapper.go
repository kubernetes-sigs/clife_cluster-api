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

package genericprovider

import (
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	operatorv1 "sigs.k8s.io/cluster-api/exp/operator/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// BootstrapProviderKind is the kind of the bootstrap provider.
	BootstrapProviderKind = "BootstrapProvider"
	// BootstrapProviderListKind is the kind of the bootstrap provider list.
	BootstrapProviderListKind = "BootstrapProviderList"
)

// BootstrapProviderWrapper wrapper for BootstrapProvider.
type BootstrapProviderWrapper struct {
	*operatorv1.BootstrapProvider
}

// GetConditions returns provider conditions.
func (b *BootstrapProviderWrapper) GetConditions() clusterv1.Conditions {
	return b.Status.Conditions
}

// SetConditions sets conditions for the provider.
func (b *BootstrapProviderWrapper) SetConditions(conditions clusterv1.Conditions) {
	b.Status.Conditions = conditions
}

// GetSpec returns spec for provider.
func (b *BootstrapProviderWrapper) GetSpec() operatorv1.ProviderSpec {
	return b.Spec.ProviderSpec
}

// SetSpec sets spec for provider.
func (b *BootstrapProviderWrapper) SetSpec(in operatorv1.ProviderSpec) {
	b.Spec.ProviderSpec = in
}

// GetStatus returns the status for provider.
func (b *BootstrapProviderWrapper) GetStatus() operatorv1.ProviderStatus {
	return b.Status.ProviderStatus
}

// SetStatus sets the status for provider.
func (b *BootstrapProviderWrapper) SetStatus(in operatorv1.ProviderStatus) {
	b.Status.ProviderStatus = in
}

// GetObject get provider object.
func (b *BootstrapProviderWrapper) GetObject() client.Object {
	return b.BootstrapProvider
}

// BootstrapProviderListWrapper is a wrapper for the BootstrapProviderList.
type BootstrapProviderListWrapper struct {
	*operatorv1.BootstrapProviderList
}

// GetItems returns items from provider list.
func (b *BootstrapProviderListWrapper) GetItems() []GenericProvider {
	providers := []GenericProvider{}
	for i := range b.Items {
		p := &b.Items[i]
		providers = append(providers, &BootstrapProviderWrapper{p})
	}

	return providers
}

// GetObject get list object.
func (b *BootstrapProviderListWrapper) GetObject() client.ObjectList {
	return b.BootstrapProviderList
}
