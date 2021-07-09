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
	ControlPlaneProviderKind     = "ControlPlaneProvider"
	ControlPlaneProviderListKind = "ControlPlaneProviderList"
)

type ControlPlaneProviderWrapper struct {
	*operatorv1.ControlPlaneProvider
}

func (c *ControlPlaneProviderWrapper) GetConditions() clusterv1.Conditions {
	return c.Status.Conditions
}

func (c *ControlPlaneProviderWrapper) SetConditions(conditions clusterv1.Conditions) {
	c.Status.Conditions = conditions
}

func (c *ControlPlaneProviderWrapper) GetSpec() operatorv1.ProviderSpec {
	return c.Spec.ProviderSpec
}

func (c *ControlPlaneProviderWrapper) SetSpec(in operatorv1.ProviderSpec) {
	c.Spec.ProviderSpec = in
}

func (c *ControlPlaneProviderWrapper) GetStatus() operatorv1.ProviderStatus {
	return c.Status.ProviderStatus
}

func (c *ControlPlaneProviderWrapper) SetStatus(in operatorv1.ProviderStatus) {
	c.Status.ProviderStatus = in
}

func (c *ControlPlaneProviderWrapper) GetObject() client.Object {
	return c.ControlPlaneProvider
}

type ControlPlaneProviderListWrapper struct {
	*operatorv1.ControlPlaneProviderList
}

func (c *ControlPlaneProviderListWrapper) GetItems() []GenericProvider {
	providers := []GenericProvider{}
	for pi := range c.Items {
		providers = append(providers, &ControlPlaneProviderWrapper{&c.Items[pi]})
	}

	return providers
}

func (c *ControlPlaneProviderListWrapper) GetObject() client.ObjectList {
	return c.ControlPlaneProviderList
}
