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
	// InfrastructureProviderKind is the kind of infrastructure provider.
	InfrastructureProviderKind = "InfrastructureProvider"
	// InfrastructureProviderListKind is the kind of infrastructure provider list.
	InfrastructureProviderListKind = "InfrastructureProviderList"
)

// InfrastructureProviderWrapper wrapper for InfrastructureProvider.
type InfrastructureProviderWrapper struct {
	*operatorv1.InfrastructureProvider
}

// GetConditions returns provider conditions.
func (i *InfrastructureProviderWrapper) GetConditions() clusterv1.Conditions {
	return i.Status.Conditions
}

// SetConditions sets conditions for the provider.
func (i *InfrastructureProviderWrapper) SetConditions(conditions clusterv1.Conditions) {
	i.Status.Conditions = conditions
}

// GetSpec returns spec for provider.
func (i *InfrastructureProviderWrapper) GetSpec() operatorv1.ProviderSpec {
	return i.Spec.ProviderSpec
}

// SetSpec sets spec for provider.
func (i *InfrastructureProviderWrapper) SetSpec(in operatorv1.ProviderSpec) {
	i.Spec.ProviderSpec = in
}

// GetStatus gets the status for provider.
func (i *InfrastructureProviderWrapper) GetStatus() operatorv1.ProviderStatus {
	return i.Status.ProviderStatus
}

// SetStatus sets the status for provider.
func (i *InfrastructureProviderWrapper) SetStatus(in operatorv1.ProviderStatus) {
	i.Status.ProviderStatus = in
}

// GetObject get provider object.
func (i *InfrastructureProviderWrapper) GetObject() client.Object {
	return i.InfrastructureProvider
}

// InfrastructureProviderListWrapper is a wrapper for InfrastructureProviderList.
type InfrastructureProviderListWrapper struct {
	*operatorv1.InfrastructureProviderList
}

// GetItems returns items from provider list.
func (i *InfrastructureProviderListWrapper) GetItems() []GenericProvider {
	providers := []GenericProvider{}
	for j := range i.Items {
		p := &i.Items[j]
		providers = append(providers, &InfrastructureProviderWrapper{p})
	}

	return providers
}

// GetObject get list object.
func (i *InfrastructureProviderListWrapper) GetObject() client.ObjectList {
	return i.InfrastructureProviderList
}
