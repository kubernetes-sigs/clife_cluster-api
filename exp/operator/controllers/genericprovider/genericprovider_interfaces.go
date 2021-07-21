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

// Package genericprovider provider generic interface and wrapper for different providers.
package genericprovider

import (
	operatorv1 "sigs.k8s.io/cluster-api/exp/operator/api/v1alpha1"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GenericProvider is the interface that represents all providers.
type GenericProvider interface {
	client.Object
	conditions.Setter
	GetSpec() operatorv1.ProviderSpec
	SetSpec(in operatorv1.ProviderSpec)
	GetStatus() operatorv1.ProviderStatus
	SetStatus(in operatorv1.ProviderStatus)
	GetObject() client.Object
}

// GenericProviderList is the interface that represents all provider lists.
type GenericProviderList interface { //nolint
	client.ObjectList
	GetObject() client.ObjectList
	GetItems() []GenericProvider
}
