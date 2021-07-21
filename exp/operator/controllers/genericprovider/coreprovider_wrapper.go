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
	// CoreProviderKind is the kind for core provider.
	CoreProviderKind = "CoreProvider"
	// CoreProviderListKind is the kind for core provider list.
	CoreProviderListKind = "CoreProviderList"
)

// CoreProviderWrapper wrapper for CoreProvider.
type CoreProviderWrapper struct {
	*operatorv1.CoreProvider
}

// GetConditions returns provider conditions.
func (c *CoreProviderWrapper) GetConditions() clusterv1.Conditions {
	return c.Status.Conditions
}

// SetConditions sets conditions for the provider.
func (c *CoreProviderWrapper) SetConditions(conditions clusterv1.Conditions) {
	c.Status.Conditions = conditions
}

// GetSpec returns spec for provider.
func (c *CoreProviderWrapper) GetSpec() operatorv1.ProviderSpec {
	return c.Spec.ProviderSpec
}

// SetSpec sets spec for provider.
func (c *CoreProviderWrapper) SetSpec(in operatorv1.ProviderSpec) {
	c.Spec.ProviderSpec = in
}

// GetStatus gets the status for provider.
func (c *CoreProviderWrapper) GetStatus() operatorv1.ProviderStatus {
	return c.Status.ProviderStatus
}

// SetStatus sets the status for provider.
func (c *CoreProviderWrapper) SetStatus(in operatorv1.ProviderStatus) {
	c.Status.ProviderStatus = in
}

// GetObject get provider object.
func (c *CoreProviderWrapper) GetObject() client.Object {
	return c.CoreProvider
}

// CoreProviderListWrapper is a wrapper for CoreProviderList.
type CoreProviderListWrapper struct {
	*operatorv1.CoreProviderList
}

// GetItems returns items from provider list.
func (c *CoreProviderListWrapper) GetItems() []GenericProvider {
	providers := []GenericProvider{}
	for i := range c.Items {
		p := &c.Items[i]
		providers = append(providers, &CoreProviderWrapper{p})
	}

	return providers
}

// GetObject get list object.
func (c *CoreProviderListWrapper) GetObject() client.ObjectList {
	return c.CoreProviderList
}
