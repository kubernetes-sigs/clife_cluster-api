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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"

	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	"sigs.k8s.io/cluster-api/controllers/remote"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func TestReconcileInterruptibleNodeLabel(t *testing.T) {
	g := NewWithT(t)

	ns, err := env.CreateNamespace(ctx, "test-interruptible-node-label")
	g.Expect(err).ToNot(HaveOccurred())

	infraMachine := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "InfrastructureMachine",
			"apiVersion": "infrastructure.cluster.x-k8s.io/v1alpha4",
			"metadata": map[string]interface{}{
				"name":      "infra-config1",
				"namespace": ns.Name,
			},
			"status": map[string]interface{}{
				"interruptible": true,
			},
		},
	}

	cluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster-1",
			Namespace: ns.Name,
		},
	}

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-1",
		},
	}

	machine := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machine-test",
			Namespace: ns.Name,
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: cluster.Name,
			InfrastructureRef: corev1.ObjectReference{
				APIVersion: "infrastructure.cluster.x-k8s.io/v1alpha4",
				Kind:       "InfrastructureMachine",
				Name:       "infra-config1",
				Namespace:  ns.Name,
			},
			Bootstrap: clusterv1.Bootstrap{
				ConfigRef: &corev1.ObjectReference{
					APIVersion: "bootstrap.cluster.x-k8s.io/v1alpha4",
					Kind:       "BootstrapMachine",
					Name:       "bootstrap-config1",
				},
			},
		},
		Status: clusterv1.MachineStatus{
			NodeRef: &corev1.ObjectReference{
				Name: "node-1",
			},
		},
	}

	g.Expect(env.Create(ctx, cluster)).To(Succeed())
	g.Expect(env.Create(ctx, node)).To(Succeed())
	g.Expect(env.Create(ctx, infraMachine)).To(Succeed())
	g.Expect(env.Create(ctx, machine)).To(Succeed())

	// Patch infra machine status
	patchHelper, err := patch.NewHelper(infraMachine, env)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(unstructured.SetNestedField(infraMachine.Object, true, "status", "interruptible")).To(Succeed())
	g.Expect(patchHelper.Patch(ctx, infraMachine, patch.WithStatusObservedGeneration{})).To(Succeed())

	defer func(do ...client.Object) {
		g.Expect(env.Cleanup(ctx, do...)).To(Succeed())
	}(cluster, ns, node, infraMachine, machine)

	r := &MachineReconciler{
		Client:   env.Client,
		Tracker:  remote.NewTestClusterCacheTracker(log.NullLogger{}, env.Client, scheme.Scheme, client.ObjectKey{Name: cluster.Name, Namespace: cluster.Namespace}),
		recorder: record.NewFakeRecorder(32),
	}

	_, err = r.reconcileInterruptibleNodeLabel(context.Background(), cluster, machine)
	g.Expect(err).ToNot(HaveOccurred())

	// Check if node gets interruptible label
	g.Eventually(func() bool {
		updatedNode := &corev1.Node{}
		err := env.Get(ctx, client.ObjectKey{Name: node.Name}, updatedNode)
		if err != nil {
			return false
		}

		if updatedNode.Labels == nil {
			return false
		}

		_, ok := updatedNode.Labels[clusterv1.InterruptibleLabel]

		return ok
	}, 10*time.Second).Should(BeTrue())
}
