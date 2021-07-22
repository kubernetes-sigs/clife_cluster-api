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
	"fmt"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"github.com/pkg/errors"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	addonsv1 "sigs.k8s.io/cluster-api/exp/addons/api/v1alpha4"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	timeout = time.Second * 15
)

func TestClusterResourceSetReconciler(t *testing.T) {
	var (
		clusterResourceSetName string
		testCluster            *clusterv1.Cluster
		clusterName            string
		labels                 map[string]string
		configmapName          = "test-configmap"
		secretName             = "test-secret"
		namespacePrefix        = "test-cluster-resource-set"
	)

	setup := func(t *testing.T, g *WithT) *corev1.Namespace {
		clusterResourceSetName = fmt.Sprintf("clusterresourceset-%s", util.RandomString(6))
		labels = map[string]string{clusterResourceSetName: "bar"}

		ns, err := env.CreateNamespace(ctx, namespacePrefix)
		g.Expect(err).ToNot(HaveOccurred())

		clusterName = fmt.Sprintf("cluster-%s", util.RandomString(6))
		testCluster = &clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: ns.Name}}

		t.Log("Creating the Cluster")
		g.Expect(env.Create(ctx, testCluster)).To(Succeed())
		t.Log("Creating the remote Cluster kubeconfig")
		g.Expect(env.CreateKubeconfigSecret(ctx, testCluster)).To(Succeed())
		testConfigmap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configmapName,
				Namespace: ns.Name,
			},
			Data: map[string]string{
				"cm": `metadata:
 name: resource-configmap
 namespace: default
kind: ConfigMap
apiVersion: v1`,
			},
		}
		testSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: ns.Name,
			},
			Type: "addons.cluster.x-k8s.io/resource-set",
			StringData: map[string]string{
				"cm": `metadata:
kind: ConfigMap
apiVersion: v1
metadata:
 name: resource-configmap
 namespace: default`,
			},
		}
		t.Log("Creating a Secret and a ConfigMap with ConfigMap in their data field")
		g.Expect(env.Create(ctx, testConfigmap)).To(Succeed())
		g.Expect(env.Create(ctx, testSecret)).To(Succeed())

		return ns
	}

	teardown := func(t *testing.T, g *WithT, ns *corev1.Namespace) {
		t.Log("Deleting the Kubeconfigsecret")
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName + "-kubeconfig",
				Namespace: ns.Name,
			},
		}
		g.Expect(env.Delete(ctx, secret)).To(Succeed())

		clusterResourceSetInstance := &addonsv1.ClusterResourceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterResourceSetName,
				Namespace: ns.Name,
			},
		}

		err := env.Get(ctx, client.ObjectKey{Namespace: clusterResourceSetInstance.Namespace, Name: clusterResourceSetInstance.Name}, clusterResourceSetInstance)
		if err == nil {
			g.Expect(env.Delete(ctx, clusterResourceSetInstance)).To(Succeed())
		}

		g.Eventually(func() bool {
			crsKey := client.ObjectKey{
				Namespace: clusterResourceSetInstance.Namespace,
				Name:      clusterResourceSetInstance.Name,
			}
			crs := &addonsv1.ClusterResourceSet{}
			err := env.Get(ctx, crsKey, crs)
			return err != nil
		}, timeout).Should(BeTrue())

		g.Expect(env.Delete(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
			Name:      configmapName,
			Namespace: ns.Name,
		}})).To(Succeed())
		g.Expect(env.Delete(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: ns.Name,
		}})).To(Succeed())
		g.Expect(env.Delete(ctx, ns)).To(Succeed())
	}

	t.Run("Should reconcile a ClusterResourceSet with multiple resources when a cluster with matching label exists", func(t *testing.T) {
		g := NewWithT(t)
		ns := setup(t, g)
		defer teardown(t, g, ns)

		t.Log("Updating the cluster with labels")
		testCluster.SetLabels(labels)
		g.Expect(env.Update(ctx, testCluster)).To(Succeed())

		t.Log("Creating a ClusterResourceSet instance that has same labels as selector")
		clusterResourceSetInstance := &addonsv1.ClusterResourceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterResourceSetName,
				Namespace: ns.Name,
			},
			Spec: addonsv1.ClusterResourceSetSpec{
				ClusterSelector: metav1.LabelSelector{
					MatchLabels: labels,
				},
				Resources: []addonsv1.ResourceRef{{Name: configmapName, Kind: "ConfigMap"}, {Name: secretName, Kind: "Secret"}},
			},
		}
		// Create the ClusterResourceSet.
		g.Expect(env.Create(ctx, clusterResourceSetInstance)).To(Succeed())

		t.Log("Verifying ClusterResourceSetBinding is created with cluster owner reference")
		g.Eventually(func() bool {
			binding := &addonsv1.ClusterResourceSetBinding{}
			clusterResourceSetBindingKey := client.ObjectKey{
				Namespace: testCluster.Namespace,
				Name:      testCluster.Name,
			}
			err := env.Get(ctx, clusterResourceSetBindingKey, binding)
			if err != nil {
				return false
			}

			if len(binding.Spec.Bindings) != 1 {
				return false
			}
			if len(binding.Spec.Bindings[0].Resources) != 2 {
				return false
			}

			if binding.Spec.Bindings[0].Resources[0].Applied != true || binding.Spec.Bindings[0].Resources[1].Applied != true {
				return false
			}

			return util.HasOwnerRef(binding.GetOwnerReferences(), metav1.OwnerReference{
				APIVersion: clusterv1.GroupVersion.String(),
				Kind:       "Cluster",
				Name:       testCluster.Name,
				UID:        testCluster.UID,
			})
		}, timeout).Should(BeTrue())
		t.Log("Deleting the Cluster")
		g.Expect(env.Delete(ctx, testCluster)).To(Succeed())
	})

	t.Run("Should reconcile a cluster when its labels are changed to match a ClusterResourceSet's selector", func(t *testing.T) {
		g := NewWithT(t)
		ns := setup(t, g)
		defer teardown(t, g, ns)

		clusterResourceSetInstance := &addonsv1.ClusterResourceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterResourceSetName,
				Namespace: ns.Name,
			},
			Spec: addonsv1.ClusterResourceSetSpec{
				ClusterSelector: metav1.LabelSelector{
					MatchLabels: labels,
				},
			},
		}
		// Create the ClusterResourceSet.
		g.Expect(env.Create(ctx, clusterResourceSetInstance)).To(Succeed())

		testCluster.SetLabels(labels)
		g.Expect(env.Update(ctx, testCluster)).To(Succeed())

		t.Log("Verifying ClusterResourceSetBinding is created with cluster owner reference")
		g.Eventually(func() bool {
			binding := &addonsv1.ClusterResourceSetBinding{}
			clusterResourceSetBindingKey := client.ObjectKey{
				Namespace: testCluster.Namespace,
				Name:      testCluster.Name,
			}
			err := env.Get(ctx, clusterResourceSetBindingKey, binding)
			if err != nil {
				return false
			}

			return util.HasOwnerRef(binding.GetOwnerReferences(), metav1.OwnerReference{
				APIVersion: clusterv1.GroupVersion.String(),
				Kind:       "Cluster",
				Name:       testCluster.Name,
				UID:        testCluster.UID,
			})
		}, timeout).Should(BeTrue())

		// Wait until ClusterResourceSetBinding is created for the Cluster
		clusterResourceSetBindingKey := client.ObjectKey{
			Namespace: testCluster.Namespace,
			Name:      testCluster.Name,
		}
		g.Eventually(func() bool {
			binding := &addonsv1.ClusterResourceSetBinding{}

			err := env.Get(ctx, clusterResourceSetBindingKey, binding)
			return err == nil
		}, timeout).Should(BeTrue())

		t.Log("Verifying ClusterResourceSetBinding is deleted when its cluster owner reference is deleted")
		g.Expect(env.Delete(ctx, testCluster)).To(Succeed())

		g.Eventually(func() bool {
			binding := &addonsv1.ClusterResourceSetBinding{}
			err := env.Get(ctx, clusterResourceSetBindingKey, binding)
			return apierrors.IsNotFound(err)
		}, timeout).Should(BeTrue())
	})

	t.Run("Should reconcile a ClusterResourceSet when a ConfigMap resource is created that is part of ClusterResourceSet resources", func(t *testing.T) {
		g := NewWithT(t)
		ns := setup(t, g)
		defer teardown(t, g, ns)

		newCMName := fmt.Sprintf("test-configmap-%s", util.RandomString(6))

		crsInstance := &addonsv1.ClusterResourceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterResourceSetName,
				Namespace: ns.Name,
			},
			Spec: addonsv1.ClusterResourceSetSpec{
				ClusterSelector: metav1.LabelSelector{
					MatchLabels: labels,
				},
				Resources: []addonsv1.ResourceRef{{Name: newCMName, Kind: "ConfigMap"}},
			},
		}
		// Create the ClusterResourceSet.
		g.Expect(env.Create(ctx, crsInstance)).To(Succeed())

		testCluster.SetLabels(labels)
		g.Expect(env.Update(ctx, testCluster)).To(Succeed())

		t.Log("Verifying ClusterResourceSetBinding is created with cluster owner reference")
		// Wait until ClusterResourceSetBinding is created for the Cluster
		clusterResourceSetBindingKey := client.ObjectKey{
			Namespace: testCluster.Namespace,
			Name:      testCluster.Name,
		}
		g.Eventually(func() bool {
			binding := &addonsv1.ClusterResourceSetBinding{}

			err := env.Get(ctx, clusterResourceSetBindingKey, binding)
			return err == nil
		}, timeout).Should(BeTrue())

		// Initially ConfigMap is missing, so no resources in the binding.
		g.Eventually(func() bool {
			binding := &addonsv1.ClusterResourceSetBinding{}

			err := env.Get(ctx, clusterResourceSetBindingKey, binding)
			if err == nil {
				if len(binding.Spec.Bindings) > 0 && len(binding.Spec.Bindings[0].Resources) == 0 {
					return true
				}
			}
			return false
		}, timeout).Should(BeTrue())

		// Must sleep here to make sure resource is created after the previous reconcile.
		// If the resource is created in between, predicates are not used as intended in this test.
		time.Sleep(2 * time.Second)

		newConfigmap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      newCMName,
				Namespace: ns.Name,
			},
			Data: map[string]string{},
		}
		g.Expect(env.Create(ctx, newConfigmap)).To(Succeed())
		defer func() {
			g.Expect(env.Delete(ctx, newConfigmap)).To(Succeed())
		}()

		cmKey := client.ObjectKey{
			Namespace: ns.Name,
			Name:      newCMName,
		}
		g.Eventually(func() bool {
			m := &corev1.ConfigMap{}
			err := env.Get(ctx, cmKey, m)
			return err == nil
		}, timeout).Should(BeTrue())

		// When the ConfigMap resource is created, CRS should get reconciled immediately.
		g.Eventually(func() error {
			binding := &addonsv1.ClusterResourceSetBinding{}
			if err := env.Get(ctx, clusterResourceSetBindingKey, binding); err != nil {
				return err
			}
			if len(binding.Spec.Bindings[0].Resources) > 0 && binding.Spec.Bindings[0].Resources[0].Name == newCMName {
				return nil
			}
			return errors.Errorf("ClusterResourceSet binding does not have any resources matching %q: %v", newCMName, binding.Spec.Bindings)
		}, timeout).Should(Succeed())

		t.Log("Verifying ClusterResourceSetBinding is deleted when its cluster owner reference is deleted")
		g.Expect(env.Delete(ctx, testCluster)).To(Succeed())

		g.Eventually(func() bool {
			binding := &addonsv1.ClusterResourceSetBinding{}
			err := env.Get(ctx, clusterResourceSetBindingKey, binding)
			return apierrors.IsNotFound(err)
		}, timeout).Should(BeTrue())
	})

	t.Run("Should reconcile a ClusterResourceSet when a Secret resource is created that is part of ClusterResourceSet resources", func(t *testing.T) {
		g := NewWithT(t)
		ns := setup(t, g)
		defer teardown(t, g, ns)

		newSecretName := fmt.Sprintf("test-secret-%s", util.RandomString(6))

		crsInstance := &addonsv1.ClusterResourceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterResourceSetName,
				Namespace: ns.Name,
			},
			Spec: addonsv1.ClusterResourceSetSpec{
				ClusterSelector: metav1.LabelSelector{
					MatchLabels: labels,
				},
				Resources: []addonsv1.ResourceRef{{Name: newSecretName, Kind: "Secret"}},
			},
		}
		// Create the ClusterResourceSet.
		g.Expect(env.Create(ctx, crsInstance)).To(Succeed())

		testCluster.SetLabels(labels)
		g.Expect(env.Update(ctx, testCluster)).To(Succeed())

		// Must sleep here to make sure resource is created after the previous reconcile.
		// If the resource is created in between, predicates are not used as intended in this test.
		time.Sleep(2 * time.Second)

		t.Log("Verifying ClusterResourceSetBinding is created with cluster owner reference")
		// Wait until ClusterResourceSetBinding is created for the Cluster
		clusterResourceSetBindingKey := client.ObjectKey{
			Namespace: testCluster.Namespace,
			Name:      testCluster.Name,
		}
		g.Eventually(func() bool {
			binding := &addonsv1.ClusterResourceSetBinding{}

			err := env.Get(ctx, clusterResourceSetBindingKey, binding)
			return err == nil
		}, timeout).Should(BeTrue())

		// Initially Secret is missing, so no resources in the binding.
		g.Eventually(func() bool {
			binding := &addonsv1.ClusterResourceSetBinding{}

			err := env.Get(ctx, clusterResourceSetBindingKey, binding)
			if err == nil {
				if len(binding.Spec.Bindings) > 0 && len(binding.Spec.Bindings[0].Resources) == 0 {
					return true
				}
			}
			return false
		}, timeout).Should(BeTrue())

		newSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      newSecretName,
				Namespace: ns.Name,
			},
			Type: addonsv1.ClusterResourceSetSecretType,
			Data: map[string][]byte{},
		}
		g.Expect(env.Create(ctx, newSecret)).To(Succeed())
		defer func() {
			g.Expect(env.Delete(ctx, newSecret)).To(Succeed())
		}()

		cmKey := client.ObjectKey{
			Namespace: ns.Name,
			Name:      newSecretName,
		}
		g.Eventually(func() bool {
			m := &corev1.Secret{}
			err := env.Get(ctx, cmKey, m)
			return err == nil
		}, timeout).Should(BeTrue())

		// When the Secret resource is created, CRS should get reconciled immediately.
		g.Eventually(func() error {
			binding := &addonsv1.ClusterResourceSetBinding{}
			if err := env.Get(ctx, clusterResourceSetBindingKey, binding); err != nil {
				return err
			}
			if len(binding.Spec.Bindings[0].Resources) > 0 && binding.Spec.Bindings[0].Resources[0].Name == newSecretName {
				return nil
			}
			return errors.Errorf("ClusterResourceSet binding does not have any resources matching %q: %v", newSecretName, binding.Spec.Bindings)
		}, timeout).Should(Succeed())

		t.Log("Verifying ClusterResourceSetBinding is deleted when its cluster owner reference is deleted")
		g.Expect(env.Delete(ctx, testCluster)).To(Succeed())

		g.Eventually(func() bool {
			binding := &addonsv1.ClusterResourceSetBinding{}
			err := env.Get(ctx, clusterResourceSetBindingKey, binding)
			return apierrors.IsNotFound(err)
		}, timeout).Should(BeTrue())
	})

	t.Run("Should delete ClusterResourceSet from the bindings list when ClusterResourceSet is deleted", func(t *testing.T) {
		g := NewWithT(t)
		ns := setup(t, g)
		defer teardown(t, g, ns)

		t.Log("Updating the cluster with labels")
		testCluster.SetLabels(labels)
		g.Expect(env.Update(ctx, testCluster)).To(Succeed())

		t.Log("Creating a ClusterResourceSet instance that has same labels as selector")
		clusterResourceSetInstance2 := &addonsv1.ClusterResourceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterResourceSetName,
				Namespace: ns.Name,
			},
			Spec: addonsv1.ClusterResourceSetSpec{
				ClusterSelector: metav1.LabelSelector{
					MatchLabels: labels,
				},
				Resources: []addonsv1.ResourceRef{{Name: configmapName, Kind: "ConfigMap"}},
			},
		}
		// Create the ClusterResourceSet.
		g.Expect(env.Create(ctx, clusterResourceSetInstance2)).To(Succeed())

		t.Log("Creating a second ClusterResourceSet instance that has same labels as selector")
		clusterResourceSetInstance3 := &addonsv1.ClusterResourceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-clusterresourceset2",
				Namespace: ns.Name,
			},
			Spec: addonsv1.ClusterResourceSetSpec{
				ClusterSelector: metav1.LabelSelector{
					MatchLabels: labels,
				},
				Resources: []addonsv1.ResourceRef{{Name: configmapName, Kind: "ConfigMap"}, {Name: secretName, Kind: "Secret"}},
			},
		}
		// Create the ClusterResourceSet.
		g.Expect(env.Create(ctx, clusterResourceSetInstance3)).To(Succeed())

		t.Log("Verifying ClusterResourceSetBinding is created with 2 ClusterResourceSets")
		g.Eventually(func() bool {
			binding := &addonsv1.ClusterResourceSetBinding{}
			clusterResourceSetBindingKey := client.ObjectKey{
				Namespace: testCluster.Namespace,
				Name:      testCluster.Name,
			}
			err := env.Get(ctx, clusterResourceSetBindingKey, binding)
			if err != nil {
				return false
			}
			return len(binding.Spec.Bindings) == 2
		}, timeout).Should(BeTrue())

		t.Log("Verifying deleted CRS is deleted from ClusterResourceSetBinding")
		// Delete one of the CRS instances and wait until it is removed from the binding list.
		g.Expect(env.Delete(ctx, clusterResourceSetInstance2)).To(Succeed())
		g.Eventually(func() bool {
			binding := &addonsv1.ClusterResourceSetBinding{}
			clusterResourceSetBindingKey := client.ObjectKey{
				Namespace: testCluster.Namespace,
				Name:      testCluster.Name,
			}
			err := env.Get(ctx, clusterResourceSetBindingKey, binding)
			if err != nil {
				return false
			}
			return len(binding.Spec.Bindings) == 1
		}, timeout).Should(BeTrue())

		t.Log("Verifying ClusterResourceSetBinding is deleted after deleting all matching CRS objects")
		// Delete one of the CRS instances and wait until it is removed from the binding list.
		g.Expect(env.Delete(ctx, clusterResourceSetInstance3)).To(Succeed())
		g.Eventually(func() bool {
			binding := &addonsv1.ClusterResourceSetBinding{}
			clusterResourceSetBindingKey := client.ObjectKey{
				Namespace: testCluster.Namespace,
				Name:      testCluster.Name,
			}
			return env.Get(ctx, clusterResourceSetBindingKey, binding) != nil
		}, timeout).Should(BeTrue())

		t.Log("Deleting the Cluster")
		g.Expect(env.Delete(ctx, testCluster)).To(Succeed())
	})

	t.Run("Should add finalizer after reconcile", func(t *testing.T) {
		g := NewWithT(t)
		ns := setup(t, g)
		defer teardown(t, g, ns)

		dt := metav1.Now()
		clusterResourceSetInstance := &addonsv1.ClusterResourceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:              clusterResourceSetName,
				Namespace:         ns.Name,
				Finalizers:        []string{addonsv1.ClusterResourceSetFinalizer},
				DeletionTimestamp: &dt,
			},
			Spec: addonsv1.ClusterResourceSetSpec{
				ClusterSelector: metav1.LabelSelector{
					MatchLabels: labels,
				},
			},
		}
		// Create the ClusterResourceSet.
		g.Expect(env.Create(ctx, clusterResourceSetInstance)).To(Succeed())
		g.Eventually(func() bool {
			crsKey := client.ObjectKey{
				Namespace: clusterResourceSetInstance.Namespace,
				Name:      clusterResourceSetInstance.Name,
			}
			crs := &addonsv1.ClusterResourceSet{}

			err := env.Get(ctx, crsKey, crs)
			if err == nil {
				return len(crs.Finalizers) > 0
			}
			return false
		}, timeout).Should(BeTrue())
	})
}
