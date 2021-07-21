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

package controllers

import (
	"reflect"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	operatorv1 "sigs.k8s.io/cluster-api/exp/operator/api/v1alpha1"
	"sigs.k8s.io/cluster-api/exp/operator/controllers/genericprovider"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestReconcilerPreflightConditions(t *testing.T) {
	g := NewWithT(t)

	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "provider-test"}}

	g.Expect(env.Create(ctx, namespace)).To(Succeed())

	defer func() {
		g.Expect(env.Delete(ctx, namespace)).To(Succeed())
	}()

	testCases := []struct {
		name     string
		provider genericprovider.GenericProvider
	}{
		{
			name: "preflight conditions for CoreProvider",
			provider: &genericprovider.CoreProviderWrapper{
				CoreProvider: &operatorv1.CoreProvider{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "core",
						Namespace: namespace.Name,
					},
				},
			},
		},
		{
			name: "preflight conditions for ControlPlaneProvider",
			provider: &genericprovider.ControlPlaneProviderWrapper{
				ControlPlaneProvider: &operatorv1.ControlPlaneProvider{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "controlplane",
						Namespace: namespace.Name,
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gs := NewWithT(t)

			gs.Expect(env.Create(ctx, tc.provider.GetObject())).To(Succeed())

			g.Eventually(func() bool {
				if err := env.Get(ctx, client.ObjectKeyFromObject(tc.provider.GetObject()), tc.provider.GetObject()); err != nil {
					return false
				}

				conditions := tc.provider.GetStatus().Conditions

				if len(conditions) == 0 {
					return false
				}

				if conditions[0].Type != operatorv1.PreflightCheckCondition {
					return false
				}

				if conditions[0].Status != corev1.ConditionTrue {
					return false
				}

				return true
			}, timeout).Should(BeEquivalentTo(true))

			gs.Expect(env.Delete(ctx, tc.provider.GetObject())).To(Succeed())
		})
	}
}

func TestNewGenericProvider(t *testing.T) {
	testCases := []struct {
		name         string
		provider     client.Object
		expectError  bool
		expectedType genericprovider.GenericProvider
	}{
		{
			name:         "create new generic provider from core provider",
			provider:     &operatorv1.CoreProvider{},
			expectedType: &genericprovider.CoreProviderWrapper{},
		},
		{
			name:         "create new generic provider from bootstrap provider",
			provider:     &operatorv1.BootstrapProvider{},
			expectedType: &genericprovider.BootstrapProviderWrapper{},
		},
		{
			name:         "create new generic provider from control plane provider",
			provider:     &operatorv1.ControlPlaneProvider{},
			expectedType: &genericprovider.ControlPlaneProviderWrapper{},
		},
		{
			name:         "create new generic provider from infrastructure plane provider",
			provider:     &operatorv1.InfrastructureProvider{},
			expectedType: &genericprovider.InfrastructureProviderWrapper{},
		},
		{
			name:        "fail to create new generic provider",
			provider:    &corev1.Pod{},
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			r := GenericProviderReconciler{
				Provider: tc.provider,
			}

			genericProvider, err := r.newGenericProvider()
			if tc.expectError {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(reflect.TypeOf(genericProvider)).To(Equal(reflect.TypeOf(tc.expectedType)))
				g.Expect(genericProvider.GetObject()).ToNot(BeIdenticalTo(tc.provider))
			}
		})
	}
}

func TestNewGenericProviderList(t *testing.T) {
	testCases := []struct {
		name         string
		providerList client.ObjectList
		expectError  bool
		expectedType genericprovider.GenericProviderList
	}{
		{
			name:         "create new generic provider from core provider",
			providerList: &operatorv1.CoreProviderList{},
			expectedType: &genericprovider.CoreProviderListWrapper{},
		},
		{
			name:         "create new generic provider from bootstrap provider",
			providerList: &operatorv1.BootstrapProviderList{},
			expectedType: &genericprovider.BootstrapProviderListWrapper{},
		},
		{
			name:         "create new generic provider from control plane provider",
			providerList: &operatorv1.ControlPlaneProviderList{},
			expectedType: &genericprovider.ControlPlaneProviderListWrapper{},
		},
		{
			name:         "create new generic provider from infrastructure plane provider",
			providerList: &operatorv1.InfrastructureProviderList{},
			expectedType: &genericprovider.InfrastructureProviderListWrapper{},
		},
		{
			name:         "fail to create new generic provider",
			providerList: &corev1.PodList{},
			expectError:  true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			r := GenericProviderReconciler{
				ProviderList: tc.providerList,
			}

			genericProviderList, err := r.newGenericProviderList()
			if tc.expectError {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(reflect.TypeOf(genericProviderList)).To(Equal(reflect.TypeOf(tc.expectedType)))
				g.Expect(genericProviderList.GetObject()).ToNot(BeIdenticalTo(tc.providerList))
			}
		})
	}
}
