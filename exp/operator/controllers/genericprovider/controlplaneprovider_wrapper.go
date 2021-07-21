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
	// ControlPlaneProviderKind is the kind of the control plane provider.
	ControlPlaneProviderKind = "ControlPlaneProvider"
	// ControlPlaneProviderListKind is the kind of the control plane provider list.
	ControlPlaneProviderListKind = "ControlPlaneProviderList"
)

// ControlPlaneProviderWrapper wrapper for ControlPlaneProvider.
type ControlPlaneProviderWrapper struct {
	*operatorv1.ControlPlaneProvider
}

// GetConditions returns provider conditions.
func (c *ControlPlaneProviderWrapper) GetConditions() clusterv1.Conditions {
	return c.Status.Conditions
}

// SetConditions sets conditions for the provider.
func (c *ControlPlaneProviderWrapper) SetConditions(conditions clusterv1.Conditions) {
	c.Status.Conditions = conditions
}

// GetSpec returns spec for provider.
func (c *ControlPlaneProviderWrapper) GetSpec() operatorv1.ProviderSpec {
	return c.Spec.ProviderSpec
}

// SetSpec sets spec for provider.
func (c *ControlPlaneProviderWrapper) SetSpec(in operatorv1.ProviderSpec) {
	c.Spec.ProviderSpec = in
}

// GetStatus gets the status for provider.
func (c *ControlPlaneProviderWrapper) GetStatus() operatorv1.ProviderStatus {
	return c.Status.ProviderStatus
}

// SetStatus sets the status for provider.
func (c *ControlPlaneProviderWrapper) SetStatus(in operatorv1.ProviderStatus) {
	c.Status.ProviderStatus = in
}

// GetObject get provider object.
func (c *ControlPlaneProviderWrapper) GetObject() client.Object {
	return c.ControlPlaneProvider
}

// ControlPlaneProviderListWrapper is a wrapper for the ControlPlaneProviderList.
type ControlPlaneProviderListWrapper struct {
	*operatorv1.ControlPlaneProviderList
}

// GetItems returns items from provider list.
func (c *ControlPlaneProviderListWrapper) GetItems() []GenericProvider {
	providers := []GenericProvider{}
	for i := range c.Items {
		p := &c.Items[i]
		providers = append(providers, &ControlPlaneProviderWrapper{p})
	}

	return providers
}

// GetObject get list object.
func (c *ControlPlaneProviderListWrapper) GetObject() client.ObjectList {
	return c.ControlPlaneProviderList
}
