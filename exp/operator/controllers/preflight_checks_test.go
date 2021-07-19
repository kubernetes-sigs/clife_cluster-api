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
	"fmt"
	"testing"

	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/cluster-api/api/v1alpha4"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	operatorv1 "sigs.k8s.io/cluster-api/exp/operator/api/v1alpha1"
	"sigs.k8s.io/cluster-api/exp/operator/controllers/genericprovider"

	"sigs.k8s.io/cluster-api/test/helpers"
)

func TestPreflightChecks(t *testing.T) {
	namespaceName1 := "provider-test-ns-1"
	namespaceName2 := "provider-test-ns-2"

	testCases := []struct {
		name              string
		providers         []genericprovider.GenericProvider
		providerList      genericprovider.GenericProviderList
		expectedCondition clusterv1.Condition
	}{
		{
			name: "only one core provider exists, preflight check passed",
			providers: []genericprovider.GenericProvider{
				&genericprovider.CoreProviderWrapper{
					CoreProvider: &operatorv1.CoreProvider{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "cluster-api",
							Namespace: namespaceName1,
						},
						TypeMeta: metav1.TypeMeta{
							Kind:       "CoreProvider",
							APIVersion: "operator.cluster.x-k8s.io/v1alpha1",
						},
					},
				},
			},
			expectedCondition: clusterv1.Condition{
				Type:   operatorv1.PreflightCheckCondition,
				Status: corev1.ConditionTrue,
			},
			providerList: &genericprovider.CoreProviderListWrapper{
				CoreProviderList: &operatorv1.CoreProviderList{},
			},
		},
		{
			name: "two core providers were created, preflight check failed",
			providers: []genericprovider.GenericProvider{
				&genericprovider.CoreProviderWrapper{
					CoreProvider: &operatorv1.CoreProvider{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "cluster-api",
							Namespace: namespaceName1,
						},
						TypeMeta: metav1.TypeMeta{
							Kind:       "CoreProvider",
							APIVersion: "operator.cluster.x-k8s.io/v1alpha1",
						},
					},
				},
				&genericprovider.CoreProviderWrapper{
					CoreProvider: &operatorv1.CoreProvider{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "core-3",
							Namespace: namespaceName1,
						},
						TypeMeta: metav1.TypeMeta{
							Kind:       "CoreProvider",
							APIVersion: "operator.cluster.x-k8s.io/v1alpha1",
						},
					},
				},
			},
			expectedCondition: clusterv1.Condition{
				Type:     operatorv1.PreflightCheckCondition,
				Reason:   operatorv1.MoreThanOneProviderInstanceExistsReason,
				Severity: clusterv1.ConditionSeverityWarning,
				Message:  moreThanOneCoreProviderInstanceExistsMessage,
				Status:   corev1.ConditionFalse,
			},
			providerList: &genericprovider.CoreProviderListWrapper{
				CoreProviderList: &operatorv1.CoreProviderList{},
			},
		},
		{
			name: "two core providers in two different namespaces were created, preflight check failed",
			providers: []genericprovider.GenericProvider{
				&genericprovider.CoreProviderWrapper{
					CoreProvider: &operatorv1.CoreProvider{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "cluster-api",
							Namespace: namespaceName1,
						},
						TypeMeta: metav1.TypeMeta{
							Kind:       "CoreProvider",
							APIVersion: "operator.cluster.x-k8s.io/v1alpha1",
						},
					},
				},
				&genericprovider.CoreProviderWrapper{
					CoreProvider: &operatorv1.CoreProvider{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "cluster-api",
							Namespace: namespaceName2,
						},
						TypeMeta: metav1.TypeMeta{
							Kind:       "CoreProvider",
							APIVersion: "operator.cluster.x-k8s.io/v1alpha1",
						},
					},
				},
			},
			expectedCondition: clusterv1.Condition{
				Type:     operatorv1.PreflightCheckCondition,
				Reason:   operatorv1.MoreThanOneProviderInstanceExistsReason,
				Severity: clusterv1.ConditionSeverityWarning,
				Message:  moreThanOneCoreProviderInstanceExistsMessage,
				Status:   corev1.ConditionFalse,
			},
			providerList: &genericprovider.CoreProviderListWrapper{
				CoreProviderList: &operatorv1.CoreProviderList{},
			},
		},
		{
			name: "only one infra provider exists, preflight check passed",
			providers: []genericprovider.GenericProvider{
				&genericprovider.InfrastructureProviderWrapper{
					InfrastructureProvider: &operatorv1.InfrastructureProvider{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "aws",
							Namespace: namespaceName1,
						},
						TypeMeta: metav1.TypeMeta{
							Kind:       "InfrastructureProvider",
							APIVersion: "operator.cluster.x-k8s.io/v1alpha1",
						},
					},
				},
				&genericprovider.CoreProviderWrapper{
					CoreProvider: &operatorv1.CoreProvider{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "cluster-api",
							Namespace: namespaceName2,
						},
						TypeMeta: metav1.TypeMeta{
							Kind:       "CoreProvider",
							APIVersion: "operator.cluster.x-k8s.io/v1alpha4",
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
					},
				},
			},
			expectedCondition: clusterv1.Condition{
				Type:   operatorv1.PreflightCheckCondition,
				Status: corev1.ConditionTrue,
			},
			providerList: &genericprovider.InfrastructureProviderListWrapper{
				InfrastructureProviderList: &operatorv1.InfrastructureProviderList{},
			},
		},
		{
			name: "two different infra providers exist in same namespaces, preflight check passed",
			providers: []genericprovider.GenericProvider{
				&genericprovider.InfrastructureProviderWrapper{
					InfrastructureProvider: &operatorv1.InfrastructureProvider{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "metal3",
							Namespace: namespaceName1,
						},
						TypeMeta: metav1.TypeMeta{
							Kind:       "InfrastructureProvider",
							APIVersion: "operator.cluster.x-k8s.io/v1alpha1",
						},
					},
				},
				&genericprovider.InfrastructureProviderWrapper{
					InfrastructureProvider: &operatorv1.InfrastructureProvider{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "aws",
							Namespace: namespaceName1,
						},
						TypeMeta: metav1.TypeMeta{
							Kind:       "InfrastructureProvider",
							APIVersion: "operator.cluster.x-k8s.io/v1alpha1",
						},
					},
				},
				&genericprovider.CoreProviderWrapper{
					CoreProvider: &operatorv1.CoreProvider{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "cluster-api",
							Namespace: namespaceName2,
						},
						TypeMeta: metav1.TypeMeta{
							Kind:       "CoreProvider",
							APIVersion: "operator.cluster.x-k8s.io/v1alpha4",
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
					},
				},
			},
			expectedCondition: clusterv1.Condition{
				Type:   operatorv1.PreflightCheckCondition,
				Status: corev1.ConditionTrue,
			},
			providerList: &genericprovider.InfrastructureProviderListWrapper{
				InfrastructureProviderList: &operatorv1.InfrastructureProviderList{},
			},
		},
		{
			name: "two different infra providers exist in different namespaces, preflight check passed",
			providers: []genericprovider.GenericProvider{
				&genericprovider.InfrastructureProviderWrapper{
					InfrastructureProvider: &operatorv1.InfrastructureProvider{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "metal3",
							Namespace: namespaceName1,
						},
						TypeMeta: metav1.TypeMeta{
							Kind:       "InfrastructureProvider",
							APIVersion: "operator.cluster.x-k8s.io/v1alpha1",
						},
					},
				},
				&genericprovider.InfrastructureProviderWrapper{
					InfrastructureProvider: &operatorv1.InfrastructureProvider{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "aws",
							Namespace: namespaceName2,
						},
						TypeMeta: metav1.TypeMeta{
							Kind:       "InfrastructureProvider",
							APIVersion: "operator.cluster.x-k8s.io/v1alpha1",
						},
					},
				},
				&genericprovider.CoreProviderWrapper{
					CoreProvider: &operatorv1.CoreProvider{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "cluster-api",
							Namespace: namespaceName2,
						},
						TypeMeta: metav1.TypeMeta{
							Kind:       "CoreProvider",
							APIVersion: "operator.cluster.x-k8s.io/v1alpha4",
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
					},
				},
			},
			expectedCondition: clusterv1.Condition{
				Type:   operatorv1.PreflightCheckCondition,
				Status: corev1.ConditionTrue,
			},
			providerList: &genericprovider.InfrastructureProviderListWrapper{
				InfrastructureProviderList: &operatorv1.InfrastructureProviderList{},
			},
		},
		{
			name: "two similar infra provider exist in different namespaces, preflight check failed",
			providers: []genericprovider.GenericProvider{
				&genericprovider.InfrastructureProviderWrapper{
					InfrastructureProvider: &operatorv1.InfrastructureProvider{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "aws",
							Namespace: namespaceName1,
						},
						TypeMeta: metav1.TypeMeta{
							Kind:       "InfrastructureProvider",
							APIVersion: "operator.cluster.x-k8s.io/v1alpha1",
						},
					},
				},
				&genericprovider.InfrastructureProviderWrapper{
					InfrastructureProvider: &operatorv1.InfrastructureProvider{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "aws",
							Namespace: namespaceName2,
						},
						TypeMeta: metav1.TypeMeta{
							Kind:       "InfrastructureProvider",
							APIVersion: "operator.cluster.x-k8s.io/v1alpha1",
						},
					},
				},
			},
			expectedCondition: clusterv1.Condition{
				Type:     operatorv1.PreflightCheckCondition,
				Reason:   operatorv1.MoreThanOneProviderInstanceExistsReason,
				Severity: clusterv1.ConditionSeverityWarning,
				Message:  fmt.Sprintf(moreThanOneProviderInstanceExistsMessage, "aws", namespaceName2),
				Status:   corev1.ConditionFalse,
			},
			providerList: &genericprovider.InfrastructureProviderListWrapper{
				InfrastructureProviderList: &operatorv1.InfrastructureProviderList{},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gs := NewWithT(t)

			fakeclient := helpers.NewFakeClientWithScheme(
				scheme.Scheme,
			)

			for _, c := range tc.providers {
				gs.Expect(fakeclient.Create(ctx, c.GetObject())).To(Succeed())
			}

			_, err := preflightChecks(context.Background(), fakeclient, tc.providers[0], tc.providerList)
			gs.Expect(err).ToNot(HaveOccurred())

			// Check if proper condition is returned
			gs.Expect(len(tc.providers[0].GetStatus().Conditions)).To(Equal(1))
			gs.Expect(tc.providers[0].GetStatus().Conditions[0].Type).To(Equal(tc.expectedCondition.Type))
			gs.Expect(tc.providers[0].GetStatus().Conditions[0].Status).To(Equal(tc.expectedCondition.Status))
			gs.Expect(tc.providers[0].GetStatus().Conditions[0].Message).To(Equal(tc.expectedCondition.Message))
			gs.Expect(tc.providers[0].GetStatus().Conditions[0].Severity).To(Equal(tc.expectedCondition.Severity))
		})
	}
}
