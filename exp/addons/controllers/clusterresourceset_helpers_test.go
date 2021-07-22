/*
Copyright 2020 The Kubernetes Authors.

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
	"testing"
	"time"

	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	addonsv1 "sigs.k8s.io/cluster-api/exp/addons/api/v1alpha4"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	notDefaultNamespace = "not-default"
)

func TestGetorCreateClusterResourceSetBinding(t *testing.T) {
	testClusterWithBinding := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster-with-binding",
			Namespace: metav1.NamespaceDefault,
		},
	}

	testClusterResourceSetBinding := &addonsv1.ClusterResourceSetBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testClusterWithBinding.Namespace,
			Name:      testClusterWithBinding.Name,
		},
		Spec: addonsv1.ClusterResourceSetBindingSpec{
			Bindings: []*addonsv1.ResourceSetBinding{
				{
					ClusterResourceSetName: "test-clusterResourceSet",
					Resources: []addonsv1.ResourceBinding{
						{
							ResourceRef: addonsv1.ResourceRef{
								Name: "mySecret",
								Kind: "Secret",
							},
							Applied:         true,
							Hash:            "xyz",
							LastAppliedTime: &metav1.Time{Time: time.Now().UTC()},
						},
					},
				},
			},
		},
	}

	testClusterNoBinding := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster-no-binding",
			Namespace: metav1.NamespaceDefault,
		},
	}

	c := fake.NewClientBuilder().
		WithObjects(testClusterResourceSetBinding).
		Build()
	r := &ClusterResourceSetReconciler{
		Client: c,
	}

	tests := []struct {
		name                     string
		cluster                  *clusterv1.Cluster
		numOfClusterResourceSets int
	}{
		{
			name:                     "should return existing ClusterResourceSetBinding ",
			cluster:                  testClusterWithBinding,
			numOfClusterResourceSets: 1,
		},
		{
			name:                     "should return a new ClusterResourceSetBinding",
			cluster:                  testClusterNoBinding,
			numOfClusterResourceSets: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gs := NewWithT(t)

			clusterResourceSetBinding, err := r.getOrCreateClusterResourceSetBinding(context.TODO(), tt.cluster, &addonsv1.ClusterResourceSet{})
			gs.Expect(err).NotTo(HaveOccurred())

			gs.Expect(len(clusterResourceSetBinding.Spec.Bindings)).To(Equal(tt.numOfClusterResourceSets))
		})
	}
}

func TestGetSecretFromNamespacedName(t *testing.T) {
	existingSecretName := types.NamespacedName{Name: "my-secret", Namespace: metav1.NamespaceDefault}
	existingSecret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{Kind: "Secret", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      existingSecretName.Name,
			Namespace: existingSecretName.Namespace,
		},
	}

	tests := []struct {
		name       string
		secretName types.NamespacedName
		want       *corev1.Secret
		wantErr    bool
	}{
		{
			name:       "should return secret when secret exists",
			secretName: types.NamespacedName{Name: "my-secret", Namespace: metav1.NamespaceDefault},
			want:       existingSecret,
			wantErr:    false,
		},
		{
			name:       "should return error when secret does not exist",
			secretName: types.NamespacedName{Name: "my-secret", Namespace: notDefaultNamespace},
			want:       nil,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gs := NewWithT(t)

			c := fake.NewClientBuilder().
				WithObjects(existingSecret).
				Build()

			got, err := getSecret(context.TODO(), c, tt.secretName)

			if tt.wantErr {
				gs.Expect(err).To(HaveOccurred())
				return
			}
			gs.Expect(err).NotTo(HaveOccurred())

			gs.Expect(*got).To(Equal(*tt.want))
		})
	}
}

func TestGetConfigMapFromNamespacedName(t *testing.T) {
	g := NewWithT(t)

	scheme := runtime.NewScheme()
	g.Expect(corev1.AddToScheme(scheme)).To(Succeed())

	existingConfigMapName := types.NamespacedName{Name: "my-configmap", Namespace: metav1.NamespaceDefault}
	existingConfigMap := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      existingConfigMapName.Name,
			Namespace: existingConfigMapName.Namespace,
		},
	}

	tests := []struct {
		name          string
		configMapName types.NamespacedName
		want          *corev1.ConfigMap
		wantErr       bool
	}{
		{
			name:          "should return configmap when configmap exists",
			configMapName: types.NamespacedName{Name: "my-configmap", Namespace: metav1.NamespaceDefault},
			want:          existingConfigMap,
			wantErr:       false,
		},
		{
			name:          "should return error when configmap does not exist",
			configMapName: types.NamespacedName{Name: "my-configmap", Namespace: notDefaultNamespace},
			want:          nil,
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gs := NewWithT(t)

			c := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(existingConfigMap).
				Build()

			got, err := getConfigMap(context.TODO(), c, tt.configMapName)

			if tt.wantErr {
				gs.Expect(err).To(HaveOccurred())
				return
			}
			gs.Expect(err).NotTo(HaveOccurred())

			gs.Expect(*got).To(Equal(*tt.want))
		})
	}
}
