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
	"context"
	"reflect"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/cluster-api/api/v1alpha4"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	"sigs.k8s.io/cluster-api/cmd/clusterctl/client/repository"
	operatorv1 "sigs.k8s.io/cluster-api/exp/operator/api/v1alpha1"
	"sigs.k8s.io/cluster-api/exp/operator/controllers/genericprovider"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestReconcilerPreflightConditions(t *testing.T) {
	g := NewWithT(t)

	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "provider-test"}}

	g.Expect(testEnv.Create(ctx, namespace)).To(Succeed())

	defer func() {
		g.Expect(testEnv.Delete(ctx, namespace)).To(Succeed())
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
						Name:      "cluster-api",
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
						Name:      "kubeadm",
						Namespace: namespace.Name,
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gs := NewWithT(t)

			if tc.provider.GetName() != "cluster-api" {
				core := &operatorv1.CoreProvider{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster-api",
						Namespace: namespace.Name,
					},
					Status: operatorv1.CoreProviderStatus{
						ProviderStatus: operatorv1.ProviderStatus{
							Conditions: []clusterv1.Condition{
								{
									Type:               v1alpha4.ReadyCondition,
									Status:             corev1.ConditionTrue,
									LastTransitionTime: metav1.Now(),
								},
							},
						},
					},
				}
				gs.Expect(testEnv.Create(ctx, core)).To(Succeed())
				gs.Expect(testEnv.Status().Update(ctx, core)).To(Succeed())
			}
			gs.Expect(testEnv.Create(ctx, tc.provider.GetObject())).To(Succeed())

			g.Eventually(func() bool {
				if err := testEnv.Get(ctx, client.ObjectKeyFromObject(tc.provider.GetObject()), tc.provider.GetObject()); err != nil {
					return false
				}

				for _, cond := range tc.provider.GetStatus().Conditions {
					if cond.Type == operatorv1.PreflightCheckCondition && cond.Status == corev1.ConditionTrue {
						return true
					}
				}

				return false
			}, timeout).Should(BeEquivalentTo(true))

			gs.Expect(testEnv.Delete(ctx, tc.provider.GetObject())).To(Succeed())
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

			genericProvider, err := r.NewGenericProvider()
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

			genericProviderList, err := r.NewGenericProviderList()
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

func setupScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		panic(err)
	}
	if err := operatorv1.AddToScheme(scheme); err != nil {
		panic(err)
	}
	return scheme
}

func TestConfigmapRepository(t *testing.T) {
	provider := &genericprovider.InfrastructureProviderWrapper{
		InfrastructureProvider: &operatorv1.InfrastructureProvider{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "aws",
				Namespace: "ns1",
			},
			TypeMeta: metav1.TypeMeta{
				Kind:       "InfrastructureProvider",
				APIVersion: "operator.cluster.x-k8s.io/v1alpha1",
			},
			Spec: operatorv1.InfrastructureProviderSpec{
				ProviderSpec: operatorv1.ProviderSpec{
					FetchConfig: &operatorv1.FetchConfiguration{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"provider-components": "aws"},
						},
					},
				},
			},
		},
	}
	metadata := `
apiVersion: clusterctl.cluster.x-k8s.io/v1alpha3
releaseSeries:
  - major: 0
	minor: 4
	contract: v1alpha4
  - major: 0
	minor: 3
	contract: v1alpha3`

	components := `
	apiVersion: v1
kind: Namespace
metadata:
  labels:
    cluster.x-k8s.io/provider: cluster-api
    control-plane: controller-manager
  name: capi-system
---
apiVersion: v1
kind: Namespace
metadata:
  labels:
    cluster.x-k8s.io/provider: cluster-api
    control-plane: controller-manager
  name: capi-webhook-system
---`
	tests := []struct {
		name               string
		configMaps         []corev1.ConfigMap
		want               repository.Repository
		wantErr            string
		wantDefaultVersion string
	}{
		{
			name:    "missing configmaps",
			wantErr: "no ConfigMaps found with selector &LabelSelector{MatchLabels:map[string]string{provider-components: aws,},MatchExpressions:[]LabelSelectorRequirement{},}",
		},
		{
			name: "configmap with missing metadata",
			configMaps: []corev1.ConfigMap{
				{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ConfigMap",
						APIVersion: "v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "v1.2.3",
						Namespace: "ns1",
						Labels:    map[string]string{"provider-components": "aws"},
					},
					Data: map[string]string{"components": components},
				},
			},
			wantErr: "ConfigMap ns1/v1.2.3 has no metadata",
		},
		{
			name: "configmap with missing components",
			configMaps: []corev1.ConfigMap{
				{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ConfigMap",
						APIVersion: "v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "v1.2.3",
						Namespace: "ns1",
						Labels:    map[string]string{"provider-components": "aws"},
					},
					Data: map[string]string{
						"metadata": metadata,
					},
				},
			},
			wantErr: "ConfigMap ns1/v1.2.3 has no components",
		},
		{
			name: "one correct configmap",
			configMaps: []corev1.ConfigMap{
				{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ConfigMap",
						APIVersion: "v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "v1.2.3",
						Namespace: "ns1",
						Labels:    map[string]string{"provider-components": "aws"},
					},
					Data: map[string]string{
						"metadata":   metadata,
						"components": components,
					},
				},
			},
			wantDefaultVersion: "v1.2.3",
		},
		{
			name: "three correct configmaps",
			configMaps: []corev1.ConfigMap{
				{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ConfigMap",
						APIVersion: "v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "v1.2.3",
						Namespace: "ns1",
						Labels:    map[string]string{"provider-components": "aws"},
					},
					Data: map[string]string{
						"metadata":   metadata,
						"components": components,
					},
				},
				{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ConfigMap",
						APIVersion: "v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "v1.2.7",
						Namespace: "ns1",
						Labels:    map[string]string{"provider-components": "aws"},
					},
					Data: map[string]string{
						"metadata":   metadata,
						"components": components,
					},
				},
				{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ConfigMap",
						APIVersion: "v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "v1.2.4",
						Namespace: "ns1",
						Labels:    map[string]string{"provider-components": "aws"},
					},
					Data: map[string]string{
						"metadata":   metadata,
						"components": components,
					},
				},
			},
			wantDefaultVersion: "v1.2.7",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			fakeclient := fake.NewClientBuilder().WithScheme(setupScheme()).WithObjects(provider.GetObject()).Build()
			r := &GenericProviderReconciler{
				Provider: provider,
				Client:   fakeclient,
			}

			for i := range tt.configMaps {
				g.Expect(fakeclient.Create(ctx, &tt.configMaps[i])).To(Succeed())
			}

			got, err := r.configmapRepository(context.TODO(), provider)
			if len(tt.wantErr) > 0 {
				g.Expect(err).Should(MatchError(tt.wantErr))
				return
			}
			g.Expect(err).To(Succeed())
			g.Expect(got.GetFile(got.DefaultVersion(), got.ComponentsPath())).To(Equal([]byte(components)))
			g.Expect(got.GetFile(got.DefaultVersion(), "metadata.yaml")).To(Equal([]byte(metadata)))
			g.Expect(got.DefaultVersion()).To(Equal(tt.wantDefaultVersion))
		})
	}
}
