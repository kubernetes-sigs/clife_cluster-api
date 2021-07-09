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
	"testing"

	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	operatorv1 "sigs.k8s.io/cluster-api/exp/operator/api/v1alpha1"
)

func TestCustomizeDeployment(t *testing.T) {
	memTestQuantity, _ := resource.ParseQuantity("16Gi")
	corednsDepl := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "coredns",
			Namespace: metav1.NamespaceSystem,
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name: "coredns",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "coredns",
						Image: "k8s.gcr.io/coredns:1.6.2",
						Env: []corev1.EnvVar{
							{
								Name:  "test1",
								Value: "value1",
							},
						},
						Args: []string{"--webhook-port=2345"},
					}},
				},
			},
		},
	}
	tests := []struct {
		name  string
		dSpec *operatorv1.DeploymentSpec
		dIn   *appsv1.Deployment
		dOut  *appsv1.Deployment
	}{
		{
			name:  "empty",
			dSpec: &operatorv1.DeploymentSpec{},
			dIn:   corednsDepl,
			dOut:  corednsDepl,
		},
		{
			name: "all options",
			dSpec: &operatorv1.DeploymentSpec{
				NodeSelector: map[string]string{"a": "b"},
				Tolerations: []corev1.Toleration{
					{
						Key:    "node-role.kubernetes.io/master",
						Effect: corev1.TaintEffectNoSchedule,
					},
				},
				Affinity: &corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{},
								},
							},
						},
					},
				},
				Containers: []operatorv1.ContainerSpec{
					{
						Name: "coredns",
						Image: &operatorv1.ImageMeta{
							Name:       pointer.StringPtr("mydns"),
							Repository: pointer.StringPtr("quay.io/dev"),
							Tag:        pointer.StringPtr("v3.4.2"),
						},
						Env: []corev1.EnvVar{
							{
								Name:  "test1",
								Value: "value2",
							},
							{
								Name:  "new1",
								Value: "value22",
							},
						},
						Args: map[string]string{
							"--webhook-port": "3456",
							"--log_dir":      "/var/log",
						},
						Resources: &corev1.ResourceRequirements{
							Requests: corev1.ResourceList{corev1.ResourceMemory: memTestQuantity},
						},
					},
				},
			},
			dIn: corednsDepl,
			dOut: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "coredns",
					Namespace: metav1.NamespaceSystem,
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Name: "coredns",
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "coredns",
									Image: "quay.io/dev/mydns:v3.4.2",
									Env: []corev1.EnvVar{
										{
											Name:  "test1",
											Value: "value2",
										},
										{
											Name:  "new1",
											Value: "value22",
										},
									},
									Args: []string{"--webhook-port=3456", "--log_dir=/var/log"},
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{corev1.ResourceMemory: memTestQuantity},
									},
								},
							},
							NodeSelector: map[string]string{"a": "b"},
							Tolerations: []corev1.Toleration{
								{
									Key:    "node-role.kubernetes.io/master",
									Effect: corev1.TaintEffectNoSchedule,
								},
							},
							Affinity: &corev1.Affinity{
								NodeAffinity: &corev1.NodeAffinity{
									RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
										NodeSelectorTerms: []corev1.NodeSelectorTerm{
											{
												MatchExpressions: []corev1.NodeSelectorRequirement{},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			customizeDeployment(tt.dSpec, tt.dIn)
			g.Expect(tt.dIn.Spec).To(Equal(tt.dOut.Spec))
		})
	}
}
