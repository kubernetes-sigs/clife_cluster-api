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
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	addonsv1 "sigs.k8s.io/cluster-api/exp/addons/api/v1alpha3"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	timeout              = time.Second * 15
	defaultNamespaceName = "default"
)

var _ = Describe("ClusterResourceSet Reconciler", func() {

	var testCluster *clusterv1.Cluster
	var clusterName string

	var configmapName = "test-configmap"
	var secretName = "test-secret"

	BeforeEach(func() {
		clusterName = fmt.Sprintf("cluster-%s", util.RandomString(6))
		testCluster = &clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: defaultNamespaceName}}

		By("Creating the Cluster")
		Expect(testEnv.Create(ctx, testCluster)).To(Succeed())
		By("Creating the remote Cluster kubeconfig")
		Expect(testEnv.CreateKubeconfigSecret(testCluster)).To(Succeed())
		testConfigmap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configmapName,
				Namespace: defaultNamespaceName,
			},
			Data: map[string]string{
				"cm": `metadata:
 name: resource-configmap
 namespace: default
kind: ConfigMap
apiVersion: v1`,
			},
		}
		testEnv.Create(ctx, testConfigmap)
		testSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: defaultNamespaceName,
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
		By("Creating a Secret and a ConfigMap with ConfigMap in their data field")
		testEnv.Create(ctx, testConfigmap)
		testEnv.Create(ctx, testSecret)
	})
	AfterEach(func() {
		By("Deleting the Kubeconfigsecret")
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName + "-kubeconfig",
				Namespace: defaultNamespaceName,
			},
		}
		Expect(testEnv.Delete(ctx, secret)).To(Succeed())

		clusterResourceSetInstance := &addonsv1.ClusterResourceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-clusterresourceset",
				Namespace: defaultNamespaceName,
			},
		}

		err := testEnv.Get(ctx, client.ObjectKey{Namespace: clusterResourceSetInstance.Namespace, Name: clusterResourceSetInstance.Name}, clusterResourceSetInstance)
		if err == nil {
			Expect(testEnv.Delete(ctx, clusterResourceSetInstance)).To(Succeed())
		}

		Eventually(func() bool {
			crsKey := client.ObjectKey{
				Namespace: clusterResourceSetInstance.Namespace,
				Name:      clusterResourceSetInstance.Name,
			}
			crs := &addonsv1.ClusterResourceSet{}
			err := testEnv.Get(ctx, crsKey, crs)
			return err != nil
		}, timeout).Should(BeTrue())
	})

	It("Should reconcile a ClusterResourceSet with multiple resources when a cluster with matching label exists", func() {
		By("Updating the cluster with labels")
		labels := map[string]string{"foo": "bar"}
		testCluster.SetLabels(labels)
		Expect(testEnv.Update(ctx, testCluster)).To(Succeed())

		By("Creating a ClusterResourceSet instance that has same labels as selector")
		clusterResourceSetInstance := &addonsv1.ClusterResourceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-clusterresourceset",
				Namespace: defaultNamespaceName,
			},
			Spec: addonsv1.ClusterResourceSetSpec{
				ClusterSelector: metav1.LabelSelector{
					MatchLabels: labels,
				},
				Resources: []addonsv1.ResourceRef{{Name: configmapName, Kind: "ConfigMap"}, {Name: secretName, Kind: "Secret"}},
			},
		}
		// Create the ClusterResourceSet.
		Expect(testEnv.Create(ctx, clusterResourceSetInstance)).To(Succeed())

		By("Verifying ClusterResourceSetBinding is created with cluster owner reference")
		Eventually(func() bool {
			binding := &addonsv1.ClusterResourceSetBinding{}
			clusterResourceSetBindingKey := client.ObjectKey{
				Namespace: testCluster.Namespace,
				Name:      testCluster.Name,
			}
			err := testEnv.Get(ctx, clusterResourceSetBindingKey, binding)
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
		By("Deleting the Cluster")
		Expect(testEnv.Delete(ctx, testCluster)).To(Succeed())
	})
	It("Should reconcile a cluster when its labels are changed to match a ClusterResourceSet's selector", func() {

		labels := map[string]string{"foo": "bar"}

		clusterResourceSetInstance := &addonsv1.ClusterResourceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-clusterresourceset",
				Namespace: defaultNamespaceName,
			},
			Spec: addonsv1.ClusterResourceSetSpec{
				ClusterSelector: metav1.LabelSelector{
					MatchLabels: labels,
				},
			},
		}
		// Create the ClusterResourceSet.
		Expect(testEnv.Create(ctx, clusterResourceSetInstance)).To(Succeed())

		testCluster.SetLabels(labels)
		Expect(testEnv.Update(ctx, testCluster)).To(Succeed())

		By("Verifying ClusterResourceSetBinding is created with cluster owner reference")
		Eventually(func() bool {
			binding := &addonsv1.ClusterResourceSetBinding{}
			clusterResourceSetBindingKey := client.ObjectKey{
				Namespace: testCluster.Namespace,
				Name:      testCluster.Name,
			}
			err := testEnv.Get(ctx, clusterResourceSetBindingKey, binding)
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
		Eventually(func() bool {
			binding := &addonsv1.ClusterResourceSetBinding{}

			err := testEnv.Get(ctx, clusterResourceSetBindingKey, binding)
			return err == nil
		}, timeout).Should(BeTrue())

		By("Verifying ClusterResourceSetBinding is deleted when its cluster owner reference is deleted")
		Expect(testEnv.Delete(ctx, testCluster)).To(Succeed())

		Eventually(func() bool {
			binding := &addonsv1.ClusterResourceSetBinding{}
			err := testEnv.Get(ctx, clusterResourceSetBindingKey, binding)
			return apierrors.IsNotFound(err)
		}, timeout).Should(BeTrue())
	})
	It("Should reconcile a ClusterResourceSet when a Secret resource is created that is part of ClusterResourceSet resources", func() {
		labels := map[string]string{"foo2": "bar2"}
		newSecretName := "test-secret3"

		clusterResourceSetInstance := &addonsv1.ClusterResourceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-clusterresourceset",
				Namespace: defaultNamespaceName,
			},
			Spec: addonsv1.ClusterResourceSetSpec{
				ClusterSelector: metav1.LabelSelector{
					MatchLabels: labels,
				},
				Resources: []addonsv1.ResourceRef{{Name: newSecretName, Kind: "Secret"}},
			},
		}
		// Create the ClusterResourceSet.
		Expect(testEnv.Create(ctx, clusterResourceSetInstance)).To(Succeed())

		testCluster.SetLabels(labels)
		Expect(testEnv.Update(ctx, testCluster)).To(Succeed())

		By("Verifying ClusterResourceSetBinding is created with cluster owner reference")
		// Wait until ClusterResourceSetBinding is created for the Cluster
		clusterResourceSetBindingKey := client.ObjectKey{
			Namespace: testCluster.Namespace,
			Name:      testCluster.Name,
		}
		Eventually(func() bool {
			binding := &addonsv1.ClusterResourceSetBinding{}

			err := testEnv.Get(ctx, clusterResourceSetBindingKey, binding)
			return err == nil
		}, timeout).Should(BeTrue())

		// Initially ConfigMap is missing, so no resources in the binding.
		Eventually(func() bool {
			binding := &addonsv1.ClusterResourceSetBinding{}

			err := testEnv.Get(ctx, clusterResourceSetBindingKey, binding)
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
		testSec := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      newSecretName,
				Namespace: defaultNamespaceName,
			},
			Type: addonsv1.ClusterResourceSetSecretType,
			Data: map[string][]byte{},
		}
		Expect(testEnv.Create(ctx, testSec)).To(Succeed())

		// When the ConfigMap resource is created, CRS should get reconciled immediately.
		Eventually(func() bool {
			binding := &addonsv1.ClusterResourceSetBinding{}

			err := testEnv.Get(ctx, clusterResourceSetBindingKey, binding)
			if err == nil {
				if len(binding.Spec.Bindings[0].Resources) > 0 && binding.Spec.Bindings[0].Resources[0].Name == newSecretName {
					return true
				}
			}
			return false
		}, timeout).Should(BeTrue())
		Expect(testEnv.Delete(ctx, testSec)).To(Succeed())
	})
	It("Should reconcile a ClusterResourceSet when a ConfigMap resource is created that is part of ClusterResourceSet resources", func() {
		labels := map[string]string{"foo2": "bar2"}
		newCMName := "test-configmap3"

		clusterResourceSetInstance := &addonsv1.ClusterResourceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-clusterresourceset",
				Namespace: defaultNamespaceName,
			},
			Spec: addonsv1.ClusterResourceSetSpec{
				ClusterSelector: metav1.LabelSelector{
					MatchLabels: labels,
				},
				Resources: []addonsv1.ResourceRef{{Name: newCMName, Kind: "ConfigMap"}},
			},
		}
		// Create the ClusterResourceSet.
		Expect(testEnv.Create(ctx, clusterResourceSetInstance)).To(Succeed())

		testCluster.SetLabels(labels)
		Expect(testEnv.Update(ctx, testCluster)).To(Succeed())

		By("Verifying ClusterResourceSetBinding is created with cluster owner reference")
		// Wait until ClusterResourceSetBinding is created for the Cluster
		clusterResourceSetBindingKey := client.ObjectKey{
			Namespace: testCluster.Namespace,
			Name:      testCluster.Name,
		}
		Eventually(func() bool {
			binding := &addonsv1.ClusterResourceSetBinding{}

			err := testEnv.Get(ctx, clusterResourceSetBindingKey, binding)
			return err == nil
		}, timeout).Should(BeTrue())

		// Initially ConfigMap is missing, so no resources in the binding.
		Eventually(func() bool {
			binding := &addonsv1.ClusterResourceSetBinding{}

			err := testEnv.Get(ctx, clusterResourceSetBindingKey, binding)
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

		testConfigmap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      newCMName,
				Namespace: defaultNamespaceName,
			},
			Data: map[string]string{},
		}
		Expect(testEnv.Create(ctx, testConfigmap)).To(Succeed())

		// When the ConfigMap resource is created, CRS should get reconciled immediately.
		Eventually(func() bool {
			binding := &addonsv1.ClusterResourceSetBinding{}

			err := testEnv.Get(ctx, clusterResourceSetBindingKey, binding)
			if err == nil {
				if len(binding.Spec.Bindings[0].Resources) > 0 && binding.Spec.Bindings[0].Resources[0].Name == newCMName {
					return true
				}
			}
			return false
		}, timeout).Should(BeTrue())
		Expect(testEnv.Delete(ctx, testConfigmap)).To(Succeed())
	})
	It("Should delete ClusterResourceSet from the bindings list when ClusterResourceSet is deleted", func() {
		By("Updating the cluster with labels")
		labels := map[string]string{"foo": "bar"}
		testCluster.SetLabels(labels)
		Expect(testEnv.Update(ctx, testCluster)).To(Succeed())

		By("Creating a ClusterResourceSet instance that has same labels as selector")
		clusterResourceSetInstance2 := &addonsv1.ClusterResourceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-clusterresourceset",
				Namespace: defaultNamespaceName,
			},
			Spec: addonsv1.ClusterResourceSetSpec{
				ClusterSelector: metav1.LabelSelector{
					MatchLabels: labels,
				},
				Resources: []addonsv1.ResourceRef{{Name: configmapName, Kind: "ConfigMap"}},
			},
		}
		// Create the ClusterResourceSet.
		Expect(testEnv.Create(ctx, clusterResourceSetInstance2)).To(Succeed())

		By("Creating a second ClusterResourceSet instance that has same labels as selector")
		clusterResourceSetInstance3 := &addonsv1.ClusterResourceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-clusterresourceset2",
				Namespace: defaultNamespaceName,
			},
			Spec: addonsv1.ClusterResourceSetSpec{
				ClusterSelector: metav1.LabelSelector{
					MatchLabels: labels,
				},
				Resources: []addonsv1.ResourceRef{{Name: configmapName, Kind: "ConfigMap"}, {Name: secretName, Kind: "Secret"}},
			},
		}
		// Create the ClusterResourceSet.
		Expect(testEnv.Create(ctx, clusterResourceSetInstance3)).To(Succeed())

		By("Verifying ClusterResourceSetBinding is created with 2 ClusterResourceSets")
		Eventually(func() bool {
			binding := &addonsv1.ClusterResourceSetBinding{}
			clusterResourceSetBindingKey := client.ObjectKey{
				Namespace: testCluster.Namespace,
				Name:      testCluster.Name,
			}
			err := testEnv.Get(ctx, clusterResourceSetBindingKey, binding)
			if err != nil {
				return false
			}
			return len(binding.Spec.Bindings) == 2
		}, timeout).Should(BeTrue())

		By("Verifying deleted CRS is deleted from ClusterResourceSetBinding")
		// Delete one of the CRS instances and wait until it is removed from the binding list.
		Expect(testEnv.Delete(ctx, clusterResourceSetInstance2)).To(Succeed())
		Eventually(func() bool {
			binding := &addonsv1.ClusterResourceSetBinding{}
			clusterResourceSetBindingKey := client.ObjectKey{
				Namespace: testCluster.Namespace,
				Name:      testCluster.Name,
			}
			err := testEnv.Get(ctx, clusterResourceSetBindingKey, binding)
			if err != nil {
				return false
			}
			return len(binding.Spec.Bindings) == 1
		}, timeout).Should(BeTrue())

		By("Verifying ClusterResourceSetBinding  is deleted after deleting all matching CRS objects")
		// Delete one of the CRS instances and wait until it is removed from the binding list.
		Expect(testEnv.Delete(ctx, clusterResourceSetInstance3)).To(Succeed())
		Eventually(func() bool {
			binding := &addonsv1.ClusterResourceSetBinding{}
			clusterResourceSetBindingKey := client.ObjectKey{
				Namespace: testCluster.Namespace,
				Name:      testCluster.Name,
			}
			return testEnv.Get(ctx, clusterResourceSetBindingKey, binding) != nil
		}, timeout).Should(BeTrue())

		By("Deleting the Cluster")
		Expect(testEnv.Delete(ctx, testCluster)).To(Succeed())
	})
	It("Should add finalizer after reconcile", func() {
		dt := metav1.Now()
		labels := map[string]string{"foo": "bar"}
		clusterResourceSetInstance := &addonsv1.ClusterResourceSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "test-clusterresourceset",
				Namespace:         defaultNamespaceName,
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
		Expect(testEnv.Create(ctx, clusterResourceSetInstance)).To(Succeed())
		Eventually(func() bool {
			crsKey := client.ObjectKey{
				Namespace: clusterResourceSetInstance.Namespace,
				Name:      clusterResourceSetInstance.Name,
			}
			crs := &addonsv1.ClusterResourceSet{}

			err := testEnv.Get(ctx, crsKey, crs)
			if err == nil {
				return len(crs.Finalizers) > 0
			}
			return false
		}, timeout).Should(BeTrue())
	})
})
