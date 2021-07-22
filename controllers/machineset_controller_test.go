/*
Copyright 2018 The Kubernetes Authors.

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
	"time"

	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	"sigs.k8s.io/cluster-api/controllers/external"
	"sigs.k8s.io/cluster-api/util"
)

var _ reconcile.Reconciler = &MachineSetReconciler{}

func TestMachineSetReconciler(t *testing.T) {
	setup := func(t *testing.T, g *WithT) (*corev1.Namespace, *clusterv1.Cluster) {
		t.Log("Creating the namespace")
		ns, err := env.CreateNamespace(ctx, "test-machine-set-reconciler")
		g.Expect(err).To(BeNil())

		t.Log("Creating the Cluster")
		cluster := &clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Namespace: ns.Name, Name: testClusterName}}
		g.Expect(env.Create(ctx, cluster)).To(Succeed())

		t.Log("Creating the Cluster Kubeconfig Secret")
		g.Expect(env.CreateKubeconfigSecret(ctx, cluster)).To(Succeed())

		return ns, cluster
	}

	teardown := func(t *testing.T, g *WithT, ns *corev1.Namespace, cluster *clusterv1.Cluster) {
		t.Log("Deleting the Cluster")
		g.Expect(env.Delete(ctx, cluster)).To(Succeed())
		t.Log("Deleting the namespace")
		g.Expect(env.Delete(ctx, ns)).To(Succeed())
	}

	t.Run("Should reconcile a MachineSet", func(t *testing.T) {
		g := NewWithT(t)
		namespace, testCluster := setup(t, g)
		defer teardown(t, g, namespace, testCluster)

		replicas := int32(2)
		version := "v1.14.2"
		instance := &clusterv1.MachineSet{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "ms-",
				Namespace:    namespace.Name,
				Labels: map[string]string{
					"label-1": "true",
				},
			},
			Spec: clusterv1.MachineSetSpec{
				ClusterName: testCluster.Name,
				Replicas:    &replicas,
				Selector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"label-1": "true",
					},
				},
				Template: clusterv1.MachineTemplateSpec{
					ObjectMeta: clusterv1.ObjectMeta{
						Labels: map[string]string{
							"label-1": "true",
						},
						Annotations: map[string]string{
							"annotation-1": "true",
							"precedence":   "MachineSet",
						},
					},
					Spec: clusterv1.MachineSpec{
						ClusterName: testCluster.Name,
						Version:     &version,
						Bootstrap: clusterv1.Bootstrap{
							ConfigRef: &corev1.ObjectReference{
								APIVersion: "bootstrap.cluster.x-k8s.io/v1alpha4",
								Kind:       "BootstrapMachineTemplate",
								Name:       "ms-template",
							},
						},
						InfrastructureRef: corev1.ObjectReference{
							APIVersion: "infrastructure.cluster.x-k8s.io/v1alpha4",
							Kind:       "InfrastructureMachineTemplate",
							Name:       "ms-template",
						},
					},
				},
			},
		}

		// Create bootstrap template resource.
		bootstrapResource := map[string]interface{}{
			"kind":       "BootstrapMachine",
			"apiVersion": "bootstrap.cluster.x-k8s.io/v1alpha4",
			"metadata":   map[string]interface{}{},
		}
		bootstrapTmpl := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"spec": map[string]interface{}{
					"template": bootstrapResource,
				},
			},
		}
		bootstrapTmpl.SetKind("BootstrapMachineTemplate")
		bootstrapTmpl.SetAPIVersion("bootstrap.cluster.x-k8s.io/v1alpha4")
		bootstrapTmpl.SetName("ms-template")
		bootstrapTmpl.SetNamespace(namespace.Name)
		g.Expect(env.Create(ctx, bootstrapTmpl)).To(Succeed())

		// Create infrastructure template resource.
		infraResource := map[string]interface{}{
			"kind":       "InfrastructureMachine",
			"apiVersion": "infrastructure.cluster.x-k8s.io/v1alpha4",
			"metadata": map[string]interface{}{
				"annotations": map[string]interface{}{
					"precedence": "InfrastructureMachineTemplate",
				},
			},
			"spec": map[string]interface{}{
				"size": "3xlarge",
			},
		}
		infraTmpl := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"spec": map[string]interface{}{
					"template": infraResource,
				},
			},
		}
		infraTmpl.SetKind("InfrastructureMachineTemplate")
		infraTmpl.SetAPIVersion("infrastructure.cluster.x-k8s.io/v1alpha4")
		infraTmpl.SetName("ms-template")
		infraTmpl.SetNamespace(namespace.Name)
		g.Expect(env.Create(ctx, infraTmpl)).To(Succeed())

		// Create the MachineSet.
		g.Expect(env.Create(ctx, instance)).To(Succeed())
		defer func() {
			g.Expect(env.Delete(ctx, instance)).To(Succeed())
		}()

		t.Log("Verifying the linked bootstrap template has a cluster owner reference")
		g.Eventually(func() bool {
			obj, err := external.Get(ctx, env, instance.Spec.Template.Spec.Bootstrap.ConfigRef, instance.Namespace)
			if err != nil {
				return false
			}

			return util.HasOwnerRef(obj.GetOwnerReferences(), metav1.OwnerReference{
				APIVersion: clusterv1.GroupVersion.String(),
				Kind:       "Cluster",
				Name:       testCluster.Name,
				UID:        testCluster.UID,
			})
		}, timeout).Should(BeTrue())

		t.Log("Verifying the linked infrastructure template has a cluster owner reference")
		g.Eventually(func() bool {
			obj, err := external.Get(ctx, env, &instance.Spec.Template.Spec.InfrastructureRef, instance.Namespace)
			if err != nil {
				return false
			}

			return util.HasOwnerRef(obj.GetOwnerReferences(), metav1.OwnerReference{
				APIVersion: clusterv1.GroupVersion.String(),
				Kind:       "Cluster",
				Name:       testCluster.Name,
				UID:        testCluster.UID,
			})
		}, timeout).Should(BeTrue())

		machines := &clusterv1.MachineList{}

		// Verify that we have 2 replicas.
		g.Eventually(func() int {
			if err := env.List(ctx, machines, client.InNamespace(namespace.Name)); err != nil {
				return -1
			}
			return len(machines.Items)
		}, timeout).Should(BeEquivalentTo(replicas))

		t.Log("Creating a InfrastructureMachine for each Machine")
		infraMachines := &unstructured.UnstructuredList{}
		infraMachines.SetAPIVersion("infrastructure.cluster.x-k8s.io/v1alpha4")
		infraMachines.SetKind("InfrastructureMachine")
		g.Eventually(func() int {
			if err := env.List(ctx, infraMachines, client.InNamespace(namespace.Name)); err != nil {
				return -1
			}
			return len(machines.Items)
		}, timeout).Should(BeEquivalentTo(replicas))
		for _, im := range infraMachines.Items {
			g.Expect(im.GetAnnotations()).To(HaveKeyWithValue("annotation-1", "true"), "have annotations of MachineTemplate applied")
			g.Expect(im.GetAnnotations()).To(HaveKeyWithValue("precedence", "MachineSet"), "the annotations from the MachineSpec template to overwrite the infrastructure template ones")
			g.Expect(im.GetLabels()).To(HaveKeyWithValue("label-1", "true"), "have labels of MachineTemplate applied")
		}

		// Set the infrastructure reference as ready.
		for _, m := range machines.Items {
			fakeBootstrapRefReady(*m.Spec.Bootstrap.ConfigRef, bootstrapResource, g)
			fakeInfrastructureRefReady(m.Spec.InfrastructureRef, infraResource, g)
		}

		// Try to delete 1 machine and check the MachineSet scales back up.
		machineToBeDeleted := machines.Items[0]
		g.Expect(env.Delete(ctx, &machineToBeDeleted)).To(Succeed())

		// Verify that the Machine has been deleted.
		g.Eventually(func() bool {
			key := client.ObjectKey{Name: machineToBeDeleted.Name, Namespace: machineToBeDeleted.Namespace}
			if err := env.Get(ctx, key, &machineToBeDeleted); apierrors.IsNotFound(err) || !machineToBeDeleted.DeletionTimestamp.IsZero() {
				return true
			}
			return false
		}, timeout).Should(BeTrue())

		// Verify that we have 2 replicas.
		g.Eventually(func() (ready int) {
			if err := env.List(ctx, machines, client.InNamespace(namespace.Name)); err != nil {
				return -1
			}
			for _, m := range machines.Items {
				if !m.DeletionTimestamp.IsZero() {
					continue
				}
				ready++
			}
			return
		}, timeout*3).Should(BeEquivalentTo(replicas))

		// Verify that each machine has the desired kubelet version,
		// create a fake node in Ready state, update NodeRef, and wait for a reconciliation request.
		for i := 0; i < len(machines.Items); i++ {
			m := machines.Items[i]
			if !m.DeletionTimestamp.IsZero() {
				// Skip deleted Machines
				continue
			}

			g.Expect(m.Spec.Version).ToNot(BeNil())
			g.Expect(*m.Spec.Version).To(BeEquivalentTo("v1.14.2"))
			fakeBootstrapRefReady(*m.Spec.Bootstrap.ConfigRef, bootstrapResource, g)
			providerID := fakeInfrastructureRefReady(m.Spec.InfrastructureRef, infraResource, g)
			fakeMachineNodeRef(&m, providerID, g)
		}

		// Verify that all Machines are Ready.
		g.Eventually(func() int32 {
			key := client.ObjectKey{Name: instance.Name, Namespace: instance.Namespace}
			if err := env.Get(ctx, key, instance); err != nil {
				return -1
			}
			return instance.Status.AvailableReplicas
		}, timeout).Should(BeEquivalentTo(replicas))

		// Validate that the controller set the cluster name label in selector.
		g.Expect(instance.Status.Selector).To(ContainSubstring(testCluster.Name))
	})
}

func TestMachineSetOwnerReference(t *testing.T) {
	testCluster := &clusterv1.Cluster{
		TypeMeta:   metav1.TypeMeta{Kind: "Cluster", APIVersion: clusterv1.GroupVersion.String()},
		ObjectMeta: metav1.ObjectMeta{Namespace: metav1.NamespaceDefault, Name: testClusterName},
	}

	ms1 := newMachineSet("machineset1", "valid-cluster")
	ms2 := newMachineSet("machineset2", "invalid-cluster")
	ms3 := newMachineSet("machineset3", "valid-cluster")
	ms3.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: clusterv1.GroupVersion.String(),
			Kind:       "MachineDeployment",
			Name:       "valid-machinedeployment",
		},
	}

	testCases := []struct {
		name               string
		request            reconcile.Request
		ms                 *clusterv1.MachineSet
		expectReconcileErr bool
		expectedOR         []metav1.OwnerReference
	}{
		{
			name: "should add cluster owner reference to machine set",
			request: reconcile.Request{
				NamespacedName: util.ObjectKey(ms1),
			},
			ms: ms1,
			expectedOR: []metav1.OwnerReference{
				{
					APIVersion: testCluster.APIVersion,
					Kind:       testCluster.Kind,
					Name:       testCluster.Name,
					UID:        testCluster.UID,
				},
			},
		},
		{
			name: "should not add cluster owner reference if machine is owned by a machine deployment",
			request: reconcile.Request{
				NamespacedName: util.ObjectKey(ms3),
			},
			ms: ms3,
			expectedOR: []metav1.OwnerReference{
				{
					APIVersion: clusterv1.GroupVersion.String(),
					Kind:       "MachineDeployment",
					Name:       "valid-machinedeployment",
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			msr := &MachineSetReconciler{
				Client: fake.NewClientBuilder().WithObjects(
					testCluster,
					ms1,
					ms2,
					ms3,
				).Build(),
				recorder: record.NewFakeRecorder(32),
			}

			_, err := msr.Reconcile(ctx, tc.request)
			if tc.expectReconcileErr {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}

			key := client.ObjectKey{Namespace: tc.ms.Namespace, Name: tc.ms.Name}
			var actual clusterv1.MachineSet
			if len(tc.expectedOR) > 0 {
				g.Expect(msr.Client.Get(ctx, key, &actual)).To(Succeed())
				g.Expect(actual.OwnerReferences).To(Equal(tc.expectedOR))
			} else {
				g.Expect(actual.OwnerReferences).To(BeEmpty())
			}
		})
	}
}

func TestMachineSetReconcile(t *testing.T) {
	testCluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Namespace: metav1.NamespaceDefault, Name: testClusterName},
	}

	t.Run("ignore machine sets marked for deletion", func(t *testing.T) {
		g := NewWithT(t)

		dt := metav1.Now()
		ms := &clusterv1.MachineSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "machineset1",
				Namespace:         metav1.NamespaceDefault,
				DeletionTimestamp: &dt,
			},
			Spec: clusterv1.MachineSetSpec{
				ClusterName: testClusterName,
			},
		}
		request := reconcile.Request{
			NamespacedName: util.ObjectKey(ms),
		}

		msr := &MachineSetReconciler{
			Client:   fake.NewClientBuilder().WithObjects(testCluster, ms).Build(),
			recorder: record.NewFakeRecorder(32),
		}
		result, err := msr.Reconcile(ctx, request)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result).To(Equal(reconcile.Result{}))
	})

	t.Run("records event if reconcile fails", func(t *testing.T) {
		g := NewWithT(t)

		ms := newMachineSet("machineset1", testClusterName)
		ms.Spec.Selector.MatchLabels = map[string]string{
			"--$-invalid": "true",
		}

		request := reconcile.Request{
			NamespacedName: util.ObjectKey(ms),
		}

		rec := record.NewFakeRecorder(32)
		msr := &MachineSetReconciler{
			Client:   fake.NewClientBuilder().WithObjects(testCluster, ms).Build(),
			recorder: rec,
		}
		_, _ = msr.Reconcile(ctx, request)
		g.Eventually(rec.Events).Should(Receive())
	})

	t.Run("reconcile successfully when labels are missing", func(t *testing.T) {
		g := NewWithT(t)

		ms := newMachineSet("machineset1", testClusterName)
		ms.Labels = nil
		ms.Spec.Selector.MatchLabels = nil
		ms.Spec.Template.Labels = nil

		request := reconcile.Request{
			NamespacedName: util.ObjectKey(ms),
		}

		rec := record.NewFakeRecorder(32)
		msr := &MachineSetReconciler{
			Client:   fake.NewClientBuilder().WithObjects(testCluster, ms).Build(),
			recorder: rec,
		}
		_, err := msr.Reconcile(ctx, request)
		g.Expect(err).NotTo(HaveOccurred())
	})
}

func TestMachineSetToMachines(t *testing.T) {
	machineSetList := []client.Object{
		&clusterv1.MachineSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "withMatchingLabels",
				Namespace: metav1.NamespaceDefault,
			},
			Spec: clusterv1.MachineSetSpec{
				Selector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"foo":                      "bar",
						clusterv1.ClusterLabelName: testClusterName,
					},
				},
			},
		},
	}
	controller := true
	m := clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "withOwnerRef",
			Namespace: metav1.NamespaceDefault,
			Labels: map[string]string{
				clusterv1.ClusterLabelName: testClusterName,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					Name:       "Owner",
					Kind:       "MachineSet",
					Controller: &controller,
				},
			},
		},
	}
	m2 := clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "noOwnerRefNoLabels",
			Namespace: metav1.NamespaceDefault,
			Labels: map[string]string{
				clusterv1.ClusterLabelName: testClusterName,
			},
		},
	}
	m3 := clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "withMatchingLabels",
			Namespace: metav1.NamespaceDefault,
			Labels: map[string]string{
				"foo":                      "bar",
				clusterv1.ClusterLabelName: testClusterName,
			},
		},
	}
	testsCases := []struct {
		name      string
		mapObject client.Object
		expected  []reconcile.Request
	}{
		{
			name:      "should return empty request when controller is set",
			mapObject: &m,
			expected:  []reconcile.Request{},
		},
		{
			name:      "should return nil if machine has no owner reference",
			mapObject: &m2,
			expected:  nil,
		},
		{
			name:      "should return request if machine set's labels matches machine's labels",
			mapObject: &m3,
			expected: []reconcile.Request{
				{NamespacedName: client.ObjectKey{Namespace: metav1.NamespaceDefault, Name: "withMatchingLabels"}},
			},
		},
	}

	r := &MachineSetReconciler{
		Client: fake.NewClientBuilder().WithObjects(append(machineSetList, &m, &m2, &m3)...).Build(),
	}

	for _, tc := range testsCases {
		t.Run(tc.name, func(t *testing.T) {
			gs := NewWithT(t)

			got := r.MachineToMachineSets(tc.mapObject)
			gs.Expect(got).To(Equal(tc.expected))
		})
	}
}

func TestShouldExcludeMachine(t *testing.T) {
	controller := true
	testCases := []struct {
		machineSet clusterv1.MachineSet
		machine    clusterv1.Machine
		expected   bool
	}{
		{
			machineSet: clusterv1.MachineSet{
				ObjectMeta: metav1.ObjectMeta{UID: "1"},
			},
			machine: clusterv1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "withNoMatchingOwnerRef",
					Namespace: metav1.NamespaceDefault,
					OwnerReferences: []metav1.OwnerReference{
						{
							Name:       "Owner",
							Kind:       "MachineSet",
							Controller: &controller,
							UID:        "not-1",
						},
					},
				},
			},
			expected: true,
		},
		{
			machineSet: clusterv1.MachineSet{
				ObjectMeta: metav1.ObjectMeta{UID: "1"},
			},
			machine: clusterv1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "withMatchingOwnerRef",
					Namespace: metav1.NamespaceDefault,
					OwnerReferences: []metav1.OwnerReference{
						{
							Name:       "Owner",
							Kind:       "MachineSet",
							Controller: &controller,
							UID:        "1",
						},
					},
				},
			},
			expected: false,
		},
		{
			machineSet: clusterv1.MachineSet{
				Spec: clusterv1.MachineSetSpec{
					Selector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"foo": "bar",
						},
					},
				},
			},
			machine: clusterv1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "withMatchingLabels",
					Namespace: metav1.NamespaceDefault,
					Labels: map[string]string{
						"foo": "bar",
					},
				},
			},
			expected: false,
		},
		{
			machineSet: clusterv1.MachineSet{},
			machine: clusterv1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "withDeletionTimestamp",
					Namespace:         metav1.NamespaceDefault,
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
					Labels: map[string]string{
						"foo": "bar",
					},
				},
			},
			expected: false,
		},
	}

	for _, tc := range testCases {
		g := NewWithT(t)

		got := shouldExcludeMachine(&tc.machineSet, &tc.machine)

		g.Expect(got).To(Equal(tc.expected))
	}
}

func TestAdoptOrphan(t *testing.T) {
	g := NewWithT(t)

	m := clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name: "orphanMachine",
		},
	}
	ms := clusterv1.MachineSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "adoptOrphanMachine",
		},
	}
	controller := true
	blockOwnerDeletion := true
	testCases := []struct {
		machineSet clusterv1.MachineSet
		machine    clusterv1.Machine
		expected   []metav1.OwnerReference
	}{
		{
			machine:    m,
			machineSet: ms,
			expected: []metav1.OwnerReference{
				{
					APIVersion:         clusterv1.GroupVersion.String(),
					Kind:               "MachineSet",
					Name:               "adoptOrphanMachine",
					UID:                "",
					Controller:         &controller,
					BlockOwnerDeletion: &blockOwnerDeletion,
				},
			},
		},
	}

	r := &MachineSetReconciler{
		Client: fake.NewClientBuilder().WithObjects(&m).Build(),
	}
	for _, tc := range testCases {
		g.Expect(r.adoptOrphan(ctx, tc.machineSet.DeepCopy(), tc.machine.DeepCopy())).To(Succeed())

		key := client.ObjectKey{Namespace: tc.machine.Namespace, Name: tc.machine.Name}
		g.Expect(r.Client.Get(ctx, key, &tc.machine)).To(Succeed())

		got := tc.machine.GetOwnerReferences()
		g.Expect(got).To(Equal(tc.expected))
	}
}

func newMachineSet(name, cluster string) *clusterv1.MachineSet {
	var replicas int32
	return &clusterv1.MachineSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceDefault,
			Labels: map[string]string{
				clusterv1.ClusterLabelName: cluster,
			},
		},
		Spec: clusterv1.MachineSetSpec{
			ClusterName: testClusterName,
			Replicas:    &replicas,
			Template: clusterv1.MachineTemplateSpec{
				ObjectMeta: clusterv1.ObjectMeta{
					Labels: map[string]string{
						clusterv1.ClusterLabelName: cluster,
					},
				},
			},
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					clusterv1.ClusterLabelName: cluster,
				},
			},
		},
	}
}
