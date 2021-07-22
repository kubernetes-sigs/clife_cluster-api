/*
Copyright 2017 The Kubernetes Authors.

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

package patch

import (
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	"sigs.k8s.io/cluster-api/controllers/external"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestPatchHelper(t *testing.T) {
	ns, err := env.CreateNamespace(ctx, "test-patch-helper")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := env.Delete(ctx, ns); err != nil {
			t.Fatal(err)
		}
	}()

	t.Run("should patch an unstructured object", func(t *testing.T) {
		obj := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"kind":       "BootstrapMachine",
				"apiVersion": "bootstrap.cluster.x-k8s.io/v1alpha4",
				"metadata": map[string]interface{}{
					"generateName": "test-bootstrap-",
					"namespace":    ns.Name,
				},
			},
		}

		t.Run("adding an owner reference, preserving its status", func(t *testing.T) {
			g := NewWithT(t)

			t.Log("Creating the unstructured object")
			g.Expect(env.Create(ctx, obj)).To(Succeed())
			defer func() {
				g.Expect(env.Delete(ctx, obj)).To(Succeed())
			}()
			key := client.ObjectKey{Name: obj.GetName(), Namespace: obj.GetNamespace()}

			t.Log("Checking that the object has been created")
			g.Eventually(func() error {
				obj := obj.DeepCopy()
				return env.Get(ctx, key, obj)
			}).Should(Succeed())

			obj.Object["status"] = map[string]interface{}{
				"ready": true,
			}
			g.Expect(env.Status().Update(ctx, obj)).To(Succeed())

			t.Log("Creating a new patch helper")
			patcher, err := NewHelper(obj, env)
			g.Expect(err).NotTo(HaveOccurred())

			t.Log("Modifying the OwnerReferences")
			refs := []metav1.OwnerReference{
				{
					APIVersion: "cluster.x-k8s.io/v1alpha4",
					Kind:       "Cluster",
					Name:       "test",
					UID:        types.UID("fake-uid"),
				},
			}
			obj.SetOwnerReferences(refs)

			t.Log("Patching the unstructured object")
			g.Expect(patcher.Patch(ctx, obj)).To(Succeed())

			t.Log("Validating that the status has been preserved")
			ready, err := external.IsReady(obj)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(ready).To(BeTrue())

			t.Log("Validating the object has been updated")
			g.Eventually(func() bool {
				objAfter := obj.DeepCopy()
				if err := env.Get(ctx, key, objAfter); err != nil {
					return false
				}
				return reflect.DeepEqual(obj.GetOwnerReferences(), objAfter.GetOwnerReferences())
			}, timeout).Should(BeTrue())
		})
	})

	t.Run("Should patch conditions", func(t *testing.T) {
		t.Run("on a corev1.Node object", func(t *testing.T) {
			g := NewWithT(t)

			conditionTime := metav1.Date(2015, 1, 1, 12, 0, 0, 0, metav1.Now().Location())
			obj := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "node-patch-test-",
					Namespace:    ns.Name,
					Annotations: map[string]string{
						"test": "1",
					},
				},
			}

			t.Log("Creating a Node object")
			g.Expect(env.Create(ctx, obj)).To(Succeed())
			defer func() {
				g.Expect(env.Delete(ctx, obj)).To(Succeed())
			}()
			key := client.ObjectKey{Name: obj.GetName()}

			t.Log("Checking that the object has been created")
			g.Eventually(func() error {
				obj := obj.DeepCopy()
				return env.Get(ctx, key, obj)
			}).Should(Succeed())

			t.Log("Creating a new patch helper")
			patcher, err := NewHelper(obj, env)
			g.Expect(err).NotTo(HaveOccurred())

			t.Log("Appending a new condition")
			condition := corev1.NodeCondition{
				Type:               "CustomCondition",
				Status:             corev1.ConditionTrue,
				LastHeartbeatTime:  conditionTime,
				LastTransitionTime: conditionTime,
				Reason:             "reason",
				Message:            "message",
			}
			obj.Status.Conditions = append(obj.Status.Conditions, condition)

			t.Log("Patching the Node")
			g.Expect(patcher.Patch(ctx, obj)).To(Succeed())

			t.Log("Validating the object has been updated")
			g.Eventually(func() bool {
				objAfter := obj.DeepCopy()
				g.Expect(env.Get(ctx, key, objAfter)).To(Succeed())

				ok, _ := ContainElement(condition).Match(objAfter.Status.Conditions)
				return ok
			}, timeout).Should(BeTrue())
		})

		t.Run("on a clusterv1.Cluster object", func(t *testing.T) {
			obj := &clusterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "test-",
					Namespace:    ns.Name,
				},
			}

			t.Run("should mark it ready", func(t *testing.T) {
				g := NewWithT(t)

				obj := obj.DeepCopy()

				t.Log("Creating the object")
				g.Expect(env.Create(ctx, obj)).To(Succeed())
				defer func() {
					g.Expect(env.Delete(ctx, obj)).To(Succeed())
				}()
				key := client.ObjectKey{Name: obj.Name, Namespace: obj.Namespace}

				t.Log("Checking that the object has been created")
				g.Eventually(func() error {
					obj := obj.DeepCopy()
					return env.Get(ctx, key, obj)
				}).Should(Succeed())

				t.Log("Creating a new patch helper")
				patcher, err := NewHelper(obj, env)
				g.Expect(err).NotTo(HaveOccurred())

				t.Log("Marking Ready=True")
				conditions.MarkTrue(obj, clusterv1.ReadyCondition)

				t.Log("Patching the object")
				g.Expect(patcher.Patch(ctx, obj)).To(Succeed())

				t.Log("Validating the object has been updated")
				g.Eventually(func() bool {
					objAfter := obj.DeepCopy()
					if err := env.Get(ctx, key, objAfter); err != nil {
						return false
					}
					return cmp.Equal(obj.Status.Conditions, objAfter.Status.Conditions)
				}, timeout).Should(BeTrue())
			})

			t.Run("should recover if there is a resolvable conflict", func(t *testing.T) {
				g := NewWithT(t)

				obj := obj.DeepCopy()

				t.Log("Creating the object")
				g.Expect(env.Create(ctx, obj)).To(Succeed())
				defer func() {
					g.Expect(env.Delete(ctx, obj)).To(Succeed())
				}()
				key := client.ObjectKey{Name: obj.Name, Namespace: obj.Namespace}

				t.Log("Checking that the object has been created")
				g.Eventually(func() error {
					obj := obj.DeepCopy()
					return env.Get(ctx, key, obj)
				}).Should(Succeed())

				objCopy := obj.DeepCopy()

				t.Log("Marking a custom condition to be false")
				conditions.MarkFalse(objCopy, clusterv1.ConditionType("TestCondition"), "reason", clusterv1.ConditionSeverityInfo, "message")
				g.Expect(env.Status().Update(ctx, objCopy)).To(Succeed())

				t.Log("Validating that the local object's resource version is behind")
				g.Expect(obj.ResourceVersion).NotTo(Equal(objCopy.ResourceVersion))

				t.Log("Creating a new patch helper")
				patcher, err := NewHelper(obj, env)
				g.Expect(err).NotTo(HaveOccurred())

				t.Log("Marking Ready=True")
				conditions.MarkTrue(obj, clusterv1.ReadyCondition)

				t.Log("Patching the object")
				g.Expect(patcher.Patch(ctx, obj)).To(Succeed())

				t.Log("Validating the object has been updated")
				g.Eventually(func() bool {
					objAfter := obj.DeepCopy()
					if err := env.Get(ctx, key, objAfter); err != nil {
						return false
					}

					testConditionCopy := conditions.Get(objCopy, "TestCondition")
					testConditionAfter := conditions.Get(objAfter, "TestCondition")

					readyBefore := conditions.Get(obj, clusterv1.ReadyCondition)
					readyAfter := conditions.Get(objAfter, clusterv1.ReadyCondition)

					return cmp.Equal(testConditionCopy, testConditionAfter) && cmp.Equal(readyBefore, readyAfter)
				}, timeout).Should(BeTrue())
			})

			t.Run("should recover if there is a resolvable conflict, incl. patch spec and status", func(t *testing.T) {
				g := NewWithT(t)

				obj := obj.DeepCopy()

				t.Log("Creating the object")
				g.Expect(env.Create(ctx, obj)).To(Succeed())
				defer func() {
					g.Expect(env.Delete(ctx, obj)).To(Succeed())
				}()
				key := client.ObjectKey{Name: obj.Name, Namespace: obj.Namespace}

				t.Log("Checking that the object has been created")
				g.Eventually(func() error {
					obj := obj.DeepCopy()
					return env.Get(ctx, key, obj)
				}).Should(Succeed())

				objCopy := obj.DeepCopy()

				t.Log("Marking a custom condition to be false")
				conditions.MarkFalse(objCopy, clusterv1.ConditionType("TestCondition"), "reason", clusterv1.ConditionSeverityInfo, "message")
				g.Expect(env.Status().Update(ctx, objCopy)).To(Succeed())

				t.Log("Validating that the local object's resource version is behind")
				g.Expect(obj.ResourceVersion).NotTo(Equal(objCopy.ResourceVersion))

				t.Log("Creating a new patch helper")
				patcher, err := NewHelper(obj, env)
				g.Expect(err).NotTo(HaveOccurred())

				t.Log("Changing the object spec, status, and adding Ready=True condition")
				obj.Spec.Paused = true
				obj.Spec.ControlPlaneEndpoint.Host = "test://endpoint"
				obj.Spec.ControlPlaneEndpoint.Port = 8443
				obj.Status.Phase = "custom-phase"
				conditions.MarkTrue(obj, clusterv1.ReadyCondition)

				t.Log("Patching the object")
				g.Expect(patcher.Patch(ctx, obj)).To(Succeed())

				t.Log("Validating the object has been updated")
				objAfter := obj.DeepCopy()
				g.Eventually(func() bool {
					if err := env.Get(ctx, key, objAfter); err != nil {
						return false
					}

					testConditionCopy := conditions.Get(objCopy, "TestCondition")
					testConditionAfter := conditions.Get(objAfter, "TestCondition")

					readyBefore := conditions.Get(obj, clusterv1.ReadyCondition)
					readyAfter := conditions.Get(objAfter, clusterv1.ReadyCondition)

					return cmp.Equal(testConditionCopy, testConditionAfter) && cmp.Equal(readyBefore, readyAfter) &&
						obj.Spec.Paused == objAfter.Spec.Paused &&
						obj.Spec.ControlPlaneEndpoint == objAfter.Spec.ControlPlaneEndpoint &&
						obj.Status.Phase == objAfter.Status.Phase
				}, timeout).Should(BeTrue(), cmp.Diff(obj, objAfter))
			})

			t.Run("should return an error if there is an unresolvable conflict", func(t *testing.T) {
				g := NewWithT(t)

				obj := obj.DeepCopy()

				t.Log("Creating the object")
				g.Expect(env.Create(ctx, obj)).To(Succeed())
				defer func() {
					g.Expect(env.Delete(ctx, obj)).To(Succeed())
				}()
				key := client.ObjectKey{Name: obj.Name, Namespace: obj.Namespace}

				t.Log("Checking that the object has been created")
				g.Eventually(func() error {
					obj := obj.DeepCopy()
					return env.Get(ctx, key, obj)
				}).Should(Succeed())

				objCopy := obj.DeepCopy()

				t.Log("Marking a custom condition to be false")
				conditions.MarkFalse(objCopy, clusterv1.ReadyCondition, "reason", clusterv1.ConditionSeverityInfo, "message")
				g.Expect(env.Status().Update(ctx, objCopy)).To(Succeed())

				t.Log("Validating that the local object's resource version is behind")
				g.Expect(obj.ResourceVersion).NotTo(Equal(objCopy.ResourceVersion))

				t.Log("Creating a new patch helper")
				patcher, err := NewHelper(obj, env)
				g.Expect(err).NotTo(HaveOccurred())

				t.Log("Marking Ready=True")
				conditions.MarkTrue(obj, clusterv1.ReadyCondition)

				t.Log("Patching the object")
				g.Expect(patcher.Patch(ctx, obj)).NotTo(Succeed())

				t.Log("Validating the object has not been updated")
				g.Eventually(func() bool {
					objAfter := obj.DeepCopy()
					if err := env.Get(ctx, key, objAfter); err != nil {
						return false
					}
					ok, _ := ContainElement(objCopy.Status.Conditions[0]).Match(objAfter.Status.Conditions)
					return ok
				}, timeout).Should(BeTrue())
			})

			t.Run("should not return an error if there is an unresolvable conflict but the conditions is owned by the controller", func(t *testing.T) {
				g := NewWithT(t)

				obj := obj.DeepCopy()

				t.Log("Creating the object")
				g.Expect(env.Create(ctx, obj)).To(Succeed())
				defer func() {
					g.Expect(env.Delete(ctx, obj)).To(Succeed())
				}()
				key := client.ObjectKey{Name: obj.Name, Namespace: obj.Namespace}

				t.Log("Checking that the object has been created")
				g.Eventually(func() error {
					obj := obj.DeepCopy()
					return env.Get(ctx, key, obj)
				}).Should(Succeed())

				objCopy := obj.DeepCopy()

				t.Log("Marking a custom condition to be false")
				conditions.MarkFalse(objCopy, clusterv1.ReadyCondition, "reason", clusterv1.ConditionSeverityInfo, "message")
				g.Expect(env.Status().Update(ctx, objCopy)).To(Succeed())

				t.Log("Validating that the local object's resource version is behind")
				g.Expect(obj.ResourceVersion).NotTo(Equal(objCopy.ResourceVersion))

				t.Log("Creating a new patch helper")
				patcher, err := NewHelper(obj, env)
				g.Expect(err).NotTo(HaveOccurred())

				t.Log("Marking Ready=True")
				conditions.MarkTrue(obj, clusterv1.ReadyCondition)

				t.Log("Patching the object")
				g.Expect(patcher.Patch(ctx, obj, WithOwnedConditions{Conditions: []clusterv1.ConditionType{clusterv1.ReadyCondition}})).To(Succeed())

				t.Log("Validating the object has been updated")
				g.Eventually(func() bool {
					objAfter := obj.DeepCopy()
					if err := env.Get(ctx, key, objAfter); err != nil {
						return false
					}

					readyBefore := conditions.Get(obj, clusterv1.ReadyCondition)
					readyAfter := conditions.Get(objAfter, clusterv1.ReadyCondition)

					return cmp.Equal(readyBefore, readyAfter)
				}, timeout).Should(BeTrue())
			})

			t.Run("should not return an error if there is an unresolvable conflict when force overwrite is enabled", func(t *testing.T) {
				g := NewWithT(t)

				obj := obj.DeepCopy()

				t.Log("Creating the object")
				g.Expect(env.Create(ctx, obj)).To(Succeed())
				defer func() {
					g.Expect(env.Delete(ctx, obj)).To(Succeed())
				}()
				key := client.ObjectKey{Name: obj.Name, Namespace: obj.Namespace}

				t.Log("Checking that the object has been created")
				g.Eventually(func() error {
					obj := obj.DeepCopy()
					return env.Get(ctx, key, obj)
				}).Should(Succeed())

				objCopy := obj.DeepCopy()

				t.Log("Marking a custom condition to be false")
				conditions.MarkFalse(objCopy, clusterv1.ReadyCondition, "reason", clusterv1.ConditionSeverityInfo, "message")
				g.Expect(env.Status().Update(ctx, objCopy)).To(Succeed())

				t.Log("Validating that the local object's resource version is behind")
				g.Expect(obj.ResourceVersion).NotTo(Equal(objCopy.ResourceVersion))

				t.Log("Creating a new patch helper")
				patcher, err := NewHelper(obj, env)
				g.Expect(err).NotTo(HaveOccurred())

				t.Log("Marking Ready=True")
				conditions.MarkTrue(obj, clusterv1.ReadyCondition)

				t.Log("Patching the object")
				g.Expect(patcher.Patch(ctx, obj, WithForceOverwriteConditions{})).To(Succeed())

				t.Log("Validating the object has been updated")
				g.Eventually(func() bool {
					objAfter := obj.DeepCopy()
					if err := env.Get(ctx, key, objAfter); err != nil {
						return false
					}

					readyBefore := conditions.Get(obj, clusterv1.ReadyCondition)
					readyAfter := conditions.Get(objAfter, clusterv1.ReadyCondition)

					return cmp.Equal(readyBefore, readyAfter)
				}, timeout).Should(BeTrue())
			})
		})
	})

	t.Run("Should patch a clusterv1.Cluster", func(t *testing.T) {
		obj := &clusterv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-",
				Namespace:    ns.Name,
			},
		}

		t.Run("add a finalizer", func(t *testing.T) {
			g := NewWithT(t)

			obj := obj.DeepCopy()

			t.Log("Creating the object")
			g.Expect(env.Create(ctx, obj)).To(Succeed())
			defer func() {
				g.Expect(env.Delete(ctx, obj)).To(Succeed())
			}()
			key := client.ObjectKey{Name: obj.Name, Namespace: obj.Namespace}

			t.Log("Checking that the object has been created")
			g.Eventually(func() error {
				obj := obj.DeepCopy()
				return env.Get(ctx, key, obj)
			}).Should(Succeed())

			t.Log("Creating a new patch helper")
			patcher, err := NewHelper(obj, env)
			g.Expect(err).NotTo(HaveOccurred())

			t.Log("Adding a finalizer")
			obj.Finalizers = append(obj.Finalizers, clusterv1.ClusterFinalizer)

			t.Log("Patching the object")
			g.Expect(patcher.Patch(ctx, obj)).To(Succeed())

			t.Log("Validating the object has been updated")
			g.Eventually(func() bool {
				objAfter := obj.DeepCopy()
				if err := env.Get(ctx, key, objAfter); err != nil {
					return false
				}

				return reflect.DeepEqual(obj.Finalizers, objAfter.Finalizers)
			}, timeout).Should(BeTrue())
		})

		t.Run("removing finalizers", func(t *testing.T) {
			g := NewWithT(t)

			obj := obj.DeepCopy()
			obj.Finalizers = append(obj.Finalizers, clusterv1.ClusterFinalizer)

			t.Log("Creating the object")
			g.Expect(env.Create(ctx, obj)).To(Succeed())
			defer func() {
				g.Expect(env.Delete(ctx, obj)).To(Succeed())
			}()
			key := client.ObjectKey{Name: obj.Name, Namespace: obj.Namespace}

			t.Log("Checking that the object has been created")
			g.Eventually(func() error {
				obj := obj.DeepCopy()
				return env.Get(ctx, key, obj)
			}).Should(Succeed())

			t.Log("Creating a new patch helper")
			patcher, err := NewHelper(obj, env)
			g.Expect(err).NotTo(HaveOccurred())

			t.Log("Removing the finalizers")
			obj.SetFinalizers(nil)

			t.Log("Patching the object")
			g.Expect(patcher.Patch(ctx, obj)).To(Succeed())

			t.Log("Validating the object has been updated")
			g.Eventually(func() bool {
				objAfter := obj.DeepCopy()
				if err := env.Get(ctx, key, objAfter); err != nil {
					return false
				}

				return len(objAfter.Finalizers) == 0
			}, timeout).Should(BeTrue())
		})

		t.Run("updating spec", func(t *testing.T) {
			g := NewWithT(t)

			obj := obj.DeepCopy()

			t.Log("Creating the object")
			g.Expect(env.Create(ctx, obj)).To(Succeed())
			defer func() {
				g.Expect(env.Delete(ctx, obj)).To(Succeed())
			}()
			key := client.ObjectKey{Name: obj.Name, Namespace: obj.Namespace}

			t.Log("Checking that the object has been created")
			g.Eventually(func() error {
				obj := obj.DeepCopy()
				return env.Get(ctx, key, obj)
			}).Should(Succeed())

			t.Log("Creating a new patch helper")
			patcher, err := NewHelper(obj, env)
			g.Expect(err).NotTo(HaveOccurred())

			t.Log("Updating the object spec")
			obj.Spec.Paused = true
			obj.Spec.InfrastructureRef = &corev1.ObjectReference{
				Kind:      "test-kind",
				Name:      "test-ref",
				Namespace: ns.Name,
			}

			t.Log("Patching the object")
			g.Expect(patcher.Patch(ctx, obj)).To(Succeed())

			t.Log("Validating the object has been updated")
			g.Eventually(func() bool {
				objAfter := obj.DeepCopy()
				if err := env.Get(ctx, key, objAfter); err != nil {
					return false
				}

				return objAfter.Spec.Paused == true &&
					reflect.DeepEqual(obj.Spec.InfrastructureRef, objAfter.Spec.InfrastructureRef)
			}, timeout).Should(BeTrue())
		})

		t.Run("updating status", func(t *testing.T) {
			g := NewWithT(t)

			obj := obj.DeepCopy()

			t.Log("Creating the object")
			g.Expect(env.Create(ctx, obj)).To(Succeed())
			defer func() {
				g.Expect(env.Delete(ctx, obj)).To(Succeed())
			}()
			key := client.ObjectKey{Name: obj.Name, Namespace: obj.Namespace}

			t.Log("Checking that the object has been created")
			g.Eventually(func() error {
				obj := obj.DeepCopy()
				return env.Get(ctx, key, obj)
			}).Should(Succeed())

			t.Log("Creating a new patch helper")
			patcher, err := NewHelper(obj, env)
			g.Expect(err).NotTo(HaveOccurred())

			t.Log("Updating the object status")
			obj.Status.InfrastructureReady = true

			t.Log("Patching the object")
			g.Expect(patcher.Patch(ctx, obj)).To(Succeed())

			t.Log("Validating the object has been updated")
			g.Eventually(func() bool {
				objAfter := obj.DeepCopy()
				if err := env.Get(ctx, key, objAfter); err != nil {
					return false
				}
				return reflect.DeepEqual(objAfter.Status, obj.Status)
			}, timeout).Should(BeTrue())
		})

		t.Run("updating both spec, status, and adding a condition", func(t *testing.T) {
			g := NewWithT(t)

			obj := obj.DeepCopy()

			t.Log("Creating the object")
			g.Expect(env.Create(ctx, obj)).To(Succeed())
			defer func() {
				g.Expect(env.Delete(ctx, obj)).To(Succeed())
			}()
			key := client.ObjectKey{Name: obj.Name, Namespace: obj.Namespace}

			t.Log("Checking that the object has been created")
			g.Eventually(func() error {
				obj := obj.DeepCopy()
				return env.Get(ctx, key, obj)
			}).Should(Succeed())

			t.Log("Creating a new patch helper")
			patcher, err := NewHelper(obj, env)
			g.Expect(err).NotTo(HaveOccurred())

			t.Log("Updating the object spec")
			obj.Spec.Paused = true
			obj.Spec.InfrastructureRef = &corev1.ObjectReference{
				Kind:      "test-kind",
				Name:      "test-ref",
				Namespace: ns.Name,
			}

			t.Log("Updating the object status")
			obj.Status.InfrastructureReady = true

			t.Log("Setting Ready condition")
			conditions.MarkTrue(obj, clusterv1.ReadyCondition)

			t.Log("Patching the object")
			g.Expect(patcher.Patch(ctx, obj)).To(Succeed())

			t.Log("Validating the object has been updated")
			g.Eventually(func() bool {
				objAfter := obj.DeepCopy()
				if err := env.Get(ctx, key, objAfter); err != nil {
					return false
				}

				return obj.Status.InfrastructureReady == objAfter.Status.InfrastructureReady &&
					conditions.IsTrue(objAfter, clusterv1.ReadyCondition) &&
					reflect.DeepEqual(obj.Spec, objAfter.Spec)
			}, timeout).Should(BeTrue())
		})
	})

	t.Run("Should update Status.ObservedGeneration when using WithStatusObservedGeneration option", func(t *testing.T) {
		obj := &clusterv1.MachineSet{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-ms",
				Namespace:    ns.Name,
			},
			Spec: clusterv1.MachineSetSpec{
				ClusterName: "test1",
				Template: clusterv1.MachineTemplateSpec{
					Spec: clusterv1.MachineSpec{
						ClusterName: "test1",
					},
				},
			},
		}

		t.Run("when updating spec", func(t *testing.T) {
			g := NewWithT(t)

			obj := obj.DeepCopy()

			t.Log("Creating the MachineSet object")
			g.Expect(env.Create(ctx, obj)).To(Succeed())
			defer func() {
				g.Expect(env.Delete(ctx, obj)).To(Succeed())
			}()
			key := client.ObjectKey{Name: obj.Name, Namespace: obj.Namespace}

			t.Log("Checking that the object has been created")
			g.Eventually(func() error {
				obj := obj.DeepCopy()
				return env.Get(ctx, key, obj)
			}).Should(Succeed())

			t.Log("Creating a new patch helper")
			patcher, err := NewHelper(obj, env)
			g.Expect(err).NotTo(HaveOccurred())

			t.Log("Updating the object spec")
			obj.Spec.Replicas = pointer.Int32Ptr(10)

			t.Log("Patching the object")
			g.Expect(patcher.Patch(ctx, obj, WithStatusObservedGeneration{})).To(Succeed())

			t.Log("Validating the object has been updated")
			g.Eventually(func() bool {
				objAfter := obj.DeepCopy()
				if err := env.Get(ctx, key, objAfter); err != nil {
					return false
				}

				return reflect.DeepEqual(obj.Spec, objAfter.Spec) &&
					obj.GetGeneration() == objAfter.Status.ObservedGeneration
			}, timeout).Should(BeTrue())
		})

		t.Run("when updating spec, status, and metadata", func(t *testing.T) {
			g := NewWithT(t)

			obj := obj.DeepCopy()

			t.Log("Creating the MachineSet object")
			g.Expect(env.Create(ctx, obj)).To(Succeed())
			defer func() {
				g.Expect(env.Delete(ctx, obj)).To(Succeed())
			}()
			key := client.ObjectKey{Name: obj.Name, Namespace: obj.Namespace}

			t.Log("Checking that the object has been created")
			g.Eventually(func() error {
				obj := obj.DeepCopy()
				return env.Get(ctx, key, obj)
			}).Should(Succeed())

			t.Log("Creating a new patch helper")
			patcher, err := NewHelper(obj, env)
			g.Expect(err).NotTo(HaveOccurred())

			t.Log("Updating the object spec")
			obj.Spec.Replicas = pointer.Int32Ptr(10)

			t.Log("Updating the object status")
			obj.Status.AvailableReplicas = 6
			obj.Status.ReadyReplicas = 6

			t.Log("Updating the object metadata")
			obj.ObjectMeta.Annotations = map[string]string{
				"test1": "annotation",
			}

			t.Log("Patching the object")
			g.Expect(patcher.Patch(ctx, obj, WithStatusObservedGeneration{})).To(Succeed())

			t.Log("Validating the object has been updated")
			g.Eventually(func() bool {
				objAfter := obj.DeepCopy()
				if err := env.Get(ctx, key, objAfter); err != nil {
					return false
				}

				return reflect.DeepEqual(obj.Spec, objAfter.Spec) &&
					reflect.DeepEqual(obj.Status, objAfter.Status) &&
					obj.GetGeneration() == objAfter.Status.ObservedGeneration
			}, timeout).Should(BeTrue())
		})

		t.Run("without any changes", func(t *testing.T) {
			g := NewWithT(t)

			obj := obj.DeepCopy()

			t.Log("Creating the MachineSet object")
			g.Expect(env.Create(ctx, obj)).To(Succeed())
			defer func() {
				g.Expect(env.Delete(ctx, obj)).To(Succeed())
			}()
			key := client.ObjectKey{Name: obj.Name, Namespace: obj.Namespace}

			t.Log("Checking that the object has been created")
			g.Eventually(func() error {
				obj := obj.DeepCopy()
				return env.Get(ctx, key, obj)
			}).Should(Succeed())

			obj.Status.ObservedGeneration = obj.GetGeneration()
			lastGeneration := obj.GetGeneration()
			g.Expect(env.Status().Update(ctx, obj))

			t.Log("Creating a new patch helper")
			patcher, err := NewHelper(obj, env)
			g.Expect(err).NotTo(HaveOccurred())

			t.Log("Patching the object")
			g.Expect(patcher.Patch(ctx, obj, WithStatusObservedGeneration{})).To(Succeed())

			t.Log("Validating the object has been updated")
			g.Eventually(func() bool {
				objAfter := obj.DeepCopy()
				if err := env.Get(ctx, key, objAfter); err != nil {
					return false
				}
				return lastGeneration == objAfter.Status.ObservedGeneration
			}, timeout).Should(BeTrue())
		})
	})

	t.Run("Should error if the object isn't the same", func(t *testing.T) {
		g := NewWithT(t)

		cluster := &clusterv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-",
				Namespace:    ns.Name,
			},
		}

		machineSet := &clusterv1.MachineSet{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-ms",
				Namespace:    ns.Name,
			},
			Spec: clusterv1.MachineSetSpec{
				ClusterName: "test1",
				Template: clusterv1.MachineTemplateSpec{
					Spec: clusterv1.MachineSpec{
						ClusterName: "test1",
					},
				},
			},
		}

		g.Expect(env.Create(ctx, cluster)).To(Succeed())
		defer func() {
			g.Expect(env.Delete(ctx, cluster)).To(Succeed())
		}()
		g.Expect(env.Create(ctx, machineSet)).To(Succeed())
		defer func() {
			g.Expect(env.Delete(ctx, machineSet)).To(Succeed())
		}()

		patcher, err := NewHelper(cluster, env)
		g.Expect(err).NotTo(HaveOccurred())

		g.Expect(patcher.Patch(ctx, machineSet)).NotTo(Succeed())
	})
}

func TestNewHelperNil(t *testing.T) {
	var x *appsv1.Deployment
	g := NewWithT(t)
	_, err := NewHelper(x, nil)
	g.Expect(err).NotTo(BeNil())
	_, err = NewHelper(nil, nil)
	g.Expect(err).NotTo(BeNil())
}
