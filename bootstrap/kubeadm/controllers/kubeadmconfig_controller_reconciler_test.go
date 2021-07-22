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

package controllers

import (
	"testing"

	. "github.com/onsi/gomega"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	bootstrapv1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1alpha4"
)

func TestKubeadmConfigReconciler(t *testing.T) {
	t.Run("Reconcile a KubeadmConfig", func(t *testing.T) {
		t.Run("should wait until infrastructure is ready", func(t *testing.T) {
			g := NewWithT(t)

			ns, err := env.CreateNamespace(ctx, "test-kubeadm-config-reconciler")
			g.Expect(err).To(BeNil())

			cluster := newCluster("cluster1", ns.Name)
			g.Expect(env.Create(ctx, cluster)).To(Succeed())

			machine := newMachine(cluster, "my-machine", ns.Name)
			g.Expect(env.Create(ctx, machine)).To(Succeed())

			config := newKubeadmConfig(machine, "my-machine-config", ns.Name)
			g.Expect(env.Create(ctx, config)).To(Succeed())
			defer func(do ...client.Object) {
				g.Expect(env.Cleanup(ctx, do...)).To(Succeed())
			}(cluster, machine, config, ns)

			reconciler := KubeadmConfigReconciler{
				Client: env,
			}
			t.Log("Calling reconcile should requeue")
			result, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Namespace: ns.Name,
					Name:      "my-machine-config",
				},
			})
			g.Expect(err).To(Succeed())
			g.Expect(result.Requeue).To(BeFalse())
		})
	})
}

// getKubeadmConfig returns a KubeadmConfig object from the cluster.
func getKubeadmConfig(c client.Client, name, namespace string) (*bootstrapv1.KubeadmConfig, error) {
	controlplaneConfigKey := client.ObjectKey{
		Namespace: namespace,
		Name:      name,
	}
	config := &bootstrapv1.KubeadmConfig{}
	err := c.Get(ctx, controlplaneConfigKey, config)
	return config, err
}
