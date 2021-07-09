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
	InfrastructureProviderKind     = "InfrastructureProvider"
	InfrastructureProviderListKind = "InfrastructureProviderList"
)

type InfrastructureProviderWrapper struct {
	*operatorv1.InfrastructureProvider
}

func (i *InfrastructureProviderWrapper) GetConditions() clusterv1.Conditions {
	return i.Status.Conditions
}

func (i *InfrastructureProviderWrapper) SetConditions(conditions clusterv1.Conditions) {
	i.Status.Conditions = conditions
}

func (i *InfrastructureProviderWrapper) GetSpec() operatorv1.ProviderSpec {
	return i.Spec.ProviderSpec
}

func (i *InfrastructureProviderWrapper) SetSpec(in operatorv1.ProviderSpec) {
	i.Spec.ProviderSpec = in
}

func (i *InfrastructureProviderWrapper) GetStatus() operatorv1.ProviderStatus {
	return i.Status.ProviderStatus
}

func (i *InfrastructureProviderWrapper) SetStatus(in operatorv1.ProviderStatus) {
	i.Status.ProviderStatus = in
}

func (i *InfrastructureProviderWrapper) GetObject() client.Object {
	return i.InfrastructureProvider
}

type InfrastructureProviderListWrapper struct {
	*operatorv1.InfrastructureProviderList
}

func (i *InfrastructureProviderListWrapper) GetItems() []GenericProvider {
	providers := []GenericProvider{}
	for pi := range i.Items {
		providers = append(providers, &InfrastructureProviderWrapper{&i.Items[pi]})
	}

	return providers
}

func (i *InfrastructureProviderListWrapper) GetObject() client.ObjectList {
	return i.InfrastructureProviderList
}
