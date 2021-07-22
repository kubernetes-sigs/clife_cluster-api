/*
Copyright 2019 The Kubernetes Authors.

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

package util

import (
	"testing"

	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	bootstrapv1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1alpha4"
	expv1 "sigs.k8s.io/cluster-api/exp/api/v1alpha4"
	"sigs.k8s.io/cluster-api/feature"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGetConfigOwner(t *testing.T) {
	t.Run("should get the owner when present (Machine)", func(t *testing.T) {
		g := NewWithT(t)
		myMachine := &clusterv1.Machine{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-machine",
				Namespace: metav1.NamespaceDefault,
				Labels: map[string]string{
					clusterv1.MachineControlPlaneLabelName: "",
				},
			},
			Spec: clusterv1.MachineSpec{
				ClusterName: "my-cluster",
				Bootstrap: clusterv1.Bootstrap{
					DataSecretName: pointer.StringPtr("my-data-secret"),
				},
				Version: pointer.StringPtr("v1.19.6"),
			},
			Status: clusterv1.MachineStatus{
				InfrastructureReady: true,
			},
		}

		c := fake.NewClientBuilder().WithObjects(myMachine).Build()
		obj := &bootstrapv1.KubeadmConfig{
			ObjectMeta: metav1.ObjectMeta{
				OwnerReferences: []metav1.OwnerReference{
					{
						Kind:       "Machine",
						APIVersion: clusterv1.GroupVersion.String(),
						Name:       "my-machine",
					},
				},
				Namespace: metav1.NamespaceDefault,
				Name:      "my-resource-owned-by-machine",
			},
		}
		configOwner, err := GetConfigOwner(ctx, c, obj)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(configOwner).ToNot(BeNil())
		g.Expect(configOwner.ClusterName()).To(BeEquivalentTo("my-cluster"))
		g.Expect(configOwner.IsInfrastructureReady()).To(BeTrue())
		g.Expect(configOwner.IsControlPlaneMachine()).To(BeTrue())
		g.Expect(configOwner.IsMachinePool()).To(BeFalse())
		g.Expect(configOwner.KubernetesVersion()).To(Equal("v1.19.6"))
		g.Expect(*configOwner.DataSecretName()).To(BeEquivalentTo("my-data-secret"))
	})

	t.Run("should get the owner when present (MachinePool)", func(t *testing.T) {
		_ = feature.MutableGates.Set("MachinePool=true")

		g := NewWithT(t)
		myPool := &expv1.MachinePool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-machine-pool",
				Namespace: metav1.NamespaceDefault,
				Labels: map[string]string{
					clusterv1.MachineControlPlaneLabelName: "",
				},
			},
			Spec: expv1.MachinePoolSpec{
				ClusterName: "my-cluster",
				Template: clusterv1.MachineTemplateSpec{
					Spec: clusterv1.MachineSpec{
						Version: pointer.StringPtr("v1.19.6"),
					},
				},
			},
			Status: expv1.MachinePoolStatus{
				InfrastructureReady: true,
			},
		}

		c := fake.NewClientBuilder().WithObjects(myPool).Build()
		obj := &bootstrapv1.KubeadmConfig{
			ObjectMeta: metav1.ObjectMeta{
				OwnerReferences: []metav1.OwnerReference{
					{
						Kind:       "MachinePool",
						APIVersion: expv1.GroupVersion.String(),
						Name:       "my-machine-pool",
					},
				},
				Namespace: metav1.NamespaceDefault,
				Name:      "my-resource-owned-by-machine-pool",
			},
		}
		configOwner, err := GetConfigOwner(ctx, c, obj)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(configOwner).ToNot(BeNil())
		g.Expect(configOwner.ClusterName()).To(BeEquivalentTo("my-cluster"))
		g.Expect(configOwner.IsInfrastructureReady()).To(BeTrue())
		g.Expect(configOwner.IsControlPlaneMachine()).To(BeFalse())
		g.Expect(configOwner.IsMachinePool()).To(BeTrue())
		g.Expect(configOwner.KubernetesVersion()).To(Equal("v1.19.6"))
		g.Expect(configOwner.DataSecretName()).To(BeNil())
	})

	t.Run("return an error when not found", func(t *testing.T) {
		g := NewWithT(t)
		c := fake.NewClientBuilder().Build()
		obj := &bootstrapv1.KubeadmConfig{
			ObjectMeta: metav1.ObjectMeta{
				OwnerReferences: []metav1.OwnerReference{
					{
						Kind:       "Machine",
						APIVersion: clusterv1.GroupVersion.String(),
						Name:       "my-machine",
					},
				},
				Namespace: metav1.NamespaceDefault,
				Name:      "my-resource-owned-by-machine",
			},
		}
		_, err := GetConfigOwner(ctx, c, obj)
		g.Expect(err).To(HaveOccurred())
	})

	t.Run("return nothing when there is no owner", func(t *testing.T) {
		g := NewWithT(t)
		c := fake.NewClientBuilder().Build()
		obj := &bootstrapv1.KubeadmConfig{
			ObjectMeta: metav1.ObjectMeta{
				OwnerReferences: []metav1.OwnerReference{},
				Namespace:       metav1.NamespaceDefault,
				Name:            "my-resource-owned-by-machine",
			},
		}
		configOwner, err := GetConfigOwner(ctx, c, obj)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(configOwner).To(BeNil())
	})
}
