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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	expv1 "sigs.k8s.io/cluster-api/exp/api/v1alpha4"
	"sigs.k8s.io/cluster-api/feature"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"
)

const (
	clusterReconcileNamespace = "test-cluster-reconcile"
)

func TestClusterReconciler(t *testing.T) {
	ns, err := env.CreateNamespace(ctx, clusterReconcileNamespace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := env.Delete(ctx, ns); err != nil {
			t.Fatal(err)
		}
	}()

	t.Run("Should create a Cluster", func(t *testing.T) {
		g := NewWithT(t)

		instance := &clusterv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test1-",
				Namespace:    ns.Name,
			},
			Spec: clusterv1.ClusterSpec{},
		}

		// Create the Cluster object and expect the Reconcile and Deployment to be created
		g.Expect(env.Create(ctx, instance)).To(Succeed())
		key := client.ObjectKey{Namespace: instance.Namespace, Name: instance.Name}
		defer func() {
			err := env.Delete(ctx, instance)
			g.Expect(err).NotTo(HaveOccurred())
		}()

		// Make sure the Cluster exists.
		g.Eventually(func() bool {
			if err := env.Get(ctx, key, instance); err != nil {
				return false
			}
			return len(instance.Finalizers) > 0
		}, timeout).Should(BeTrue())
	})

	t.Run("Should successfully patch a cluster object if the status diff is empty but the spec diff is not", func(t *testing.T) {
		g := NewWithT(t)

		// Setup
		cluster := &clusterv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test2-",
				Namespace:    ns.Name,
			},
		}
		g.Expect(env.Create(ctx, cluster)).To(Succeed())
		key := client.ObjectKey{Name: cluster.Name, Namespace: cluster.Namespace}
		defer func() {
			err := env.Delete(ctx, cluster)
			g.Expect(err).NotTo(HaveOccurred())
		}()

		// Wait for reconciliation to happen.
		g.Eventually(func() bool {
			if err := env.Get(ctx, key, cluster); err != nil {
				return false
			}
			return len(cluster.Finalizers) > 0
		}, timeout).Should(BeTrue())

		// Patch
		g.Eventually(func() bool {
			ph, err := patch.NewHelper(cluster, env)
			g.Expect(err).NotTo(HaveOccurred())
			cluster.Spec.InfrastructureRef = &corev1.ObjectReference{Name: "test"}
			cluster.Spec.ControlPlaneRef = &corev1.ObjectReference{Name: "test-too"}
			g.Expect(ph.Patch(ctx, cluster, patch.WithStatusObservedGeneration{})).To(Succeed())
			return true
		}, timeout).Should(BeTrue())

		// Assertions
		g.Eventually(func() bool {
			instance := &clusterv1.Cluster{}
			if err := env.Get(ctx, key, instance); err != nil {
				return false
			}
			return instance.Spec.InfrastructureRef != nil &&
				instance.Spec.InfrastructureRef.Name == "test"
		}, timeout).Should(BeTrue())
	})

	t.Run("Should successfully patch a cluster object if the spec diff is empty but the status diff is not", func(t *testing.T) {
		g := NewWithT(t)

		// Setup
		cluster := &clusterv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test3-",
				Namespace:    ns.Name,
			},
		}
		g.Expect(env.Create(ctx, cluster)).To(Succeed())
		key := client.ObjectKey{Name: cluster.Name, Namespace: cluster.Namespace}
		defer func() {
			err := env.Delete(ctx, cluster)
			g.Expect(err).NotTo(HaveOccurred())
		}()

		// Wait for reconciliation to happen.
		g.Eventually(func() bool {
			if err := env.Get(ctx, key, cluster); err != nil {
				return false
			}
			return len(cluster.Finalizers) > 0
		}, timeout).Should(BeTrue())

		// Patch
		g.Eventually(func() bool {
			ph, err := patch.NewHelper(cluster, env)
			g.Expect(err).NotTo(HaveOccurred())
			cluster.Status.InfrastructureReady = true
			g.Expect(ph.Patch(ctx, cluster, patch.WithStatusObservedGeneration{})).To(Succeed())
			return true
		}, timeout).Should(BeTrue())

		// Assertions
		g.Eventually(func() bool {
			instance := &clusterv1.Cluster{}
			if err := env.Get(ctx, key, instance); err != nil {
				return false
			}
			return instance.Status.InfrastructureReady
		}, timeout).Should(BeTrue())
	})

	t.Run("Should successfully patch a cluster object if both the spec diff and status diff are non empty", func(t *testing.T) {
		g := NewWithT(t)

		// Setup
		cluster := &clusterv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test4-",
				Namespace:    ns.Name,
			},
		}

		g.Expect(env.Create(ctx, cluster)).To(Succeed())
		key := client.ObjectKey{Name: cluster.Name, Namespace: cluster.Namespace}
		defer func() {
			err := env.Delete(ctx, cluster)
			g.Expect(err).NotTo(HaveOccurred())
		}()

		// Wait for reconciliation to happen.
		g.Eventually(func() bool {
			if err := env.Get(ctx, key, cluster); err != nil {
				return false
			}
			return len(cluster.Finalizers) > 0
		}, timeout).Should(BeTrue())

		// Patch
		g.Eventually(func() bool {
			ph, err := patch.NewHelper(cluster, env)
			g.Expect(err).NotTo(HaveOccurred())
			cluster.Status.InfrastructureReady = true
			cluster.Spec.InfrastructureRef = &corev1.ObjectReference{Name: "test"}
			g.Expect(ph.Patch(ctx, cluster, patch.WithStatusObservedGeneration{})).To(Succeed())
			return true
		}, timeout).Should(BeTrue())

		// Assertions
		g.Eventually(func() bool {
			instance := &clusterv1.Cluster{}
			if err := env.Get(ctx, key, instance); err != nil {
				return false
			}
			return instance.Status.InfrastructureReady &&
				instance.Spec.InfrastructureRef != nil &&
				instance.Spec.InfrastructureRef.Name == "test"
		}, timeout).Should(BeTrue())
	})

	t.Run("Should re-apply finalizers if removed", func(t *testing.T) {
		g := NewWithT(t)

		// Setup
		cluster := &clusterv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test5-",
				Namespace:    ns.Name,
			},
		}
		g.Expect(env.Create(ctx, cluster)).To(Succeed())
		key := client.ObjectKey{Name: cluster.Name, Namespace: cluster.Namespace}
		defer func() {
			err := env.Delete(ctx, cluster)
			g.Expect(err).NotTo(HaveOccurred())
		}()

		// Wait for reconciliation to happen.
		g.Eventually(func() bool {
			if err := env.Get(ctx, key, cluster); err != nil {
				return false
			}
			return len(cluster.Finalizers) > 0
		}, timeout).Should(BeTrue())

		// Remove finalizers
		g.Eventually(func() bool {
			ph, err := patch.NewHelper(cluster, env)
			g.Expect(err).NotTo(HaveOccurred())
			cluster.SetFinalizers([]string{})
			g.Expect(ph.Patch(ctx, cluster, patch.WithStatusObservedGeneration{})).To(Succeed())
			return true
		}, timeout).Should(BeTrue())

		g.Expect(cluster.Finalizers).Should(BeEmpty())

		// Check finalizers are re-applied
		g.Eventually(func() []string {
			instance := &clusterv1.Cluster{}
			if err := env.Get(ctx, key, instance); err != nil {
				return []string{"not-empty"}
			}
			return instance.Finalizers
		}, timeout).ShouldNot(BeEmpty())
	})

	t.Run("Should successfully set ControlPlaneInitialized on the cluster object if controlplane is ready", func(t *testing.T) {
		g := NewWithT(t)

		cluster := &clusterv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test6-",
				Namespace:    ns.Name,
			},
		}

		g.Expect(env.Create(ctx, cluster)).To(Succeed())
		key := client.ObjectKey{Name: cluster.Name, Namespace: cluster.Namespace}
		defer func() {
			err := env.Delete(ctx, cluster)
			g.Expect(err).NotTo(HaveOccurred())
		}()
		g.Expect(env.CreateKubeconfigSecret(ctx, cluster)).To(Succeed())

		// Wait for reconciliation to happen.
		g.Eventually(func() bool {
			if err := env.Get(ctx, key, cluster); err != nil {
				return false
			}
			return len(cluster.Finalizers) > 0
		}, timeout).Should(BeTrue())

		// Create a node so we can speed up reconciliation. Otherwise, the machine reconciler will requeue the machine
		// after 10 seconds, potentially slowing down this test.
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "id-node-1",
			},
			Spec: corev1.NodeSpec{
				ProviderID: "aws:///id-node-1",
			},
		}

		g.Expect(env.Create(ctx, node)).To(Succeed())

		machine := &clusterv1.Machine{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test6-",
				Namespace:    ns.Name,
				Labels: map[string]string{
					clusterv1.MachineControlPlaneLabelName: "",
				},
			},
			Spec: clusterv1.MachineSpec{
				ClusterName: cluster.Name,
				ProviderID:  pointer.StringPtr("aws:///id-node-1"),
				Bootstrap: clusterv1.Bootstrap{
					DataSecretName: pointer.StringPtr(""),
				},
			},
		}
		machine.Spec.Bootstrap.DataSecretName = pointer.StringPtr("test6-bootstrapdata")
		g.Expect(env.Create(ctx, machine)).To(Succeed())
		key = client.ObjectKey{Name: machine.Name, Namespace: machine.Namespace}
		defer func() {
			err := env.Delete(ctx, machine)
			g.Expect(err).NotTo(HaveOccurred())
		}()

		// Wait for machine to be ready.
		//
		// [ncdc] Note, we're using an increased timeout because we've been seeing failures
		// in Prow for this particular block. It looks like it's sometimes taking more than 10 seconds (the value of
		// timeout) for the machine reconciler to add the finalizer and for the change to be persisted to etcd. If
		// we continue to see test timeouts here, that will likely point to something else being the problem, but
		// I've yet to determine any other possibility for the test flakes.
		g.Eventually(func() bool {
			if err := env.Get(ctx, key, machine); err != nil {
				return false
			}
			return len(machine.Finalizers) > 0
		}, timeout*3).Should(BeTrue())

		// Assertion
		key = client.ObjectKey{Name: cluster.Name, Namespace: cluster.Namespace}
		g.Eventually(func() bool {
			if err := env.Get(ctx, key, cluster); err != nil {
				return false
			}
			return conditions.IsTrue(cluster, clusterv1.ControlPlaneInitializedCondition)
		}, timeout).Should(BeTrue())
	})
}

func TestClusterReconcilerNodeRef(t *testing.T) {
	t.Run("machine to cluster", func(t *testing.T) {
		cluster := &clusterv1.Cluster{
			TypeMeta: metav1.TypeMeta{
				Kind: "Cluster",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "test",
			},
			Spec:   clusterv1.ClusterSpec{},
			Status: clusterv1.ClusterStatus{},
		}

		controlPlaneWithNoderef := &clusterv1.Machine{
			TypeMeta: metav1.TypeMeta{
				Kind: "Machine",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "controlPlaneWithNoderef",
				Namespace: "test",
				Labels: map[string]string{
					clusterv1.ClusterLabelName:             cluster.Name,
					clusterv1.MachineControlPlaneLabelName: "",
				},
			},
			Spec: clusterv1.MachineSpec{
				ClusterName: "test-cluster",
			},
			Status: clusterv1.MachineStatus{
				NodeRef: &corev1.ObjectReference{
					Kind:      "Node",
					Namespace: "test-node",
				},
			},
		}
		controlPlaneWithoutNoderef := &clusterv1.Machine{
			TypeMeta: metav1.TypeMeta{
				Kind: "Machine",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "controlPlaneWithoutNoderef",
				Namespace: "test",
				Labels: map[string]string{
					clusterv1.ClusterLabelName:             cluster.Name,
					clusterv1.MachineControlPlaneLabelName: "",
				},
			},
			Spec: clusterv1.MachineSpec{
				ClusterName: "test-cluster",
			},
		}
		nonControlPlaneWithNoderef := &clusterv1.Machine{
			TypeMeta: metav1.TypeMeta{
				Kind: "Machine",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "nonControlPlaneWitNoderef",
				Namespace: "test",
				Labels: map[string]string{
					clusterv1.ClusterLabelName: cluster.Name,
				},
			},
			Spec: clusterv1.MachineSpec{
				ClusterName: "test-cluster",
			},
			Status: clusterv1.MachineStatus{
				NodeRef: &corev1.ObjectReference{
					Kind:      "Node",
					Namespace: "test-node",
				},
			},
		}
		nonControlPlaneWithoutNoderef := &clusterv1.Machine{
			TypeMeta: metav1.TypeMeta{
				Kind: "Machine",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "nonControlPlaneWithoutNoderef",
				Namespace: "test",
				Labels: map[string]string{
					clusterv1.ClusterLabelName: cluster.Name,
				},
			},
			Spec: clusterv1.MachineSpec{
				ClusterName: "test-cluster",
			},
		}

		tests := []struct {
			name string
			o    client.Object
			want []ctrl.Request
		}{
			{
				name: "controlplane machine, noderef is set, should return cluster",
				o:    controlPlaneWithNoderef,
				want: []ctrl.Request{
					{
						NamespacedName: util.ObjectKey(cluster),
					},
				},
			},
			{
				name: "controlplane machine, noderef is not set",
				o:    controlPlaneWithoutNoderef,
				want: nil,
			},
			{
				name: "not controlplane machine, noderef is set",
				o:    nonControlPlaneWithNoderef,
				want: nil,
			},
			{
				name: "not controlplane machine, noderef is not set",
				o:    nonControlPlaneWithoutNoderef,
				want: nil,
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				g := NewWithT(t)

				r := &ClusterReconciler{
					Client: fake.NewClientBuilder().WithObjects(cluster, controlPlaneWithNoderef, controlPlaneWithoutNoderef, nonControlPlaneWithNoderef, nonControlPlaneWithoutNoderef).Build(),
				}
				requests := r.controlPlaneMachineToCluster(tt.o)
				g.Expect(requests).To(Equal(tt.want))
			})
		}
	})
}

type machineDeploymentBuilder struct {
	md clusterv1.MachineDeployment
}

func newMachineDeploymentBuilder() *machineDeploymentBuilder {
	return &machineDeploymentBuilder{}
}

func (b *machineDeploymentBuilder) named(name string) *machineDeploymentBuilder {
	b.md.Name = name
	return b
}

func (b *machineDeploymentBuilder) ownedBy(c *clusterv1.Cluster) *machineDeploymentBuilder {
	b.md.OwnerReferences = append(b.md.OwnerReferences, metav1.OwnerReference{
		APIVersion: clusterv1.GroupVersion.String(),
		Kind:       "Cluster",
		Name:       c.Name,
	})
	return b
}

func (b *machineDeploymentBuilder) build() clusterv1.MachineDeployment {
	return b.md
}

type machineSetBuilder struct {
	ms clusterv1.MachineSet
}

func newMachineSetBuilder() *machineSetBuilder {
	return &machineSetBuilder{}
}

func (b *machineSetBuilder) named(name string) *machineSetBuilder {
	b.ms.Name = name
	return b
}

func (b *machineSetBuilder) ownedBy(c *clusterv1.Cluster) *machineSetBuilder {
	b.ms.OwnerReferences = append(b.ms.OwnerReferences, metav1.OwnerReference{
		APIVersion: clusterv1.GroupVersion.String(),
		Kind:       "Cluster",
		Name:       c.Name,
	})
	return b
}

func (b *machineSetBuilder) build() clusterv1.MachineSet {
	return b.ms
}

type machineBuilder struct {
	m clusterv1.Machine
}

func newMachineBuilder() *machineBuilder {
	return &machineBuilder{}
}

func (b *machineBuilder) named(name string) *machineBuilder {
	b.m.Name = name
	return b
}

func (b *machineBuilder) ownedBy(c *clusterv1.Cluster) *machineBuilder {
	b.m.OwnerReferences = append(b.m.OwnerReferences, metav1.OwnerReference{
		APIVersion: clusterv1.GroupVersion.String(),
		Kind:       "Cluster",
		Name:       c.Name,
	})
	return b
}

func (b *machineBuilder) controlPlane() *machineBuilder {
	b.m.Labels = map[string]string{clusterv1.MachineControlPlaneLabelName: ""}
	return b
}

func (b *machineBuilder) build() clusterv1.Machine {
	return b.m
}

type machinePoolBuilder struct {
	mp expv1.MachinePool
}

func newMachinePoolBuilder() *machinePoolBuilder {
	return &machinePoolBuilder{}
}

func (b *machinePoolBuilder) named(name string) *machinePoolBuilder {
	b.mp.Name = name
	return b
}

func (b *machinePoolBuilder) ownedBy(c *clusterv1.Cluster) *machinePoolBuilder {
	b.mp.OwnerReferences = append(b.mp.OwnerReferences, metav1.OwnerReference{
		APIVersion: clusterv1.GroupVersion.String(),
		Kind:       "Cluster",
		Name:       c.Name,
	})
	return b
}

func (b *machinePoolBuilder) build() expv1.MachinePool {
	return b.mp
}

func TestFilterOwnedDescendants(t *testing.T) {
	_ = feature.MutableGates.Set("MachinePool=true")
	g := NewWithT(t)

	c := clusterv1.Cluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: clusterv1.GroupVersion.String(),
			Kind:       "Cluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "c",
		},
	}

	md1NotOwnedByCluster := newMachineDeploymentBuilder().named("md1").build()
	md2OwnedByCluster := newMachineDeploymentBuilder().named("md2").ownedBy(&c).build()
	md3NotOwnedByCluster := newMachineDeploymentBuilder().named("md3").build()
	md4OwnedByCluster := newMachineDeploymentBuilder().named("md4").ownedBy(&c).build()

	ms1NotOwnedByCluster := newMachineSetBuilder().named("ms1").build()
	ms2OwnedByCluster := newMachineSetBuilder().named("ms2").ownedBy(&c).build()
	ms3NotOwnedByCluster := newMachineSetBuilder().named("ms3").build()
	ms4OwnedByCluster := newMachineSetBuilder().named("ms4").ownedBy(&c).build()

	m1NotOwnedByCluster := newMachineBuilder().named("m1").build()
	m2OwnedByCluster := newMachineBuilder().named("m2").ownedBy(&c).build()
	m3ControlPlaneOwnedByCluster := newMachineBuilder().named("m3").ownedBy(&c).controlPlane().build()
	m4NotOwnedByCluster := newMachineBuilder().named("m4").build()
	m5OwnedByCluster := newMachineBuilder().named("m5").ownedBy(&c).build()
	m6ControlPlaneOwnedByCluster := newMachineBuilder().named("m6").ownedBy(&c).controlPlane().build()

	mp1NotOwnedByCluster := newMachinePoolBuilder().named("mp1").build()
	mp2OwnedByCluster := newMachinePoolBuilder().named("mp2").ownedBy(&c).build()
	mp3NotOwnedByCluster := newMachinePoolBuilder().named("mp3").build()
	mp4OwnedByCluster := newMachinePoolBuilder().named("mp4").ownedBy(&c).build()

	d := clusterDescendants{
		machineDeployments: clusterv1.MachineDeploymentList{
			Items: []clusterv1.MachineDeployment{
				md1NotOwnedByCluster,
				md2OwnedByCluster,
				md3NotOwnedByCluster,
				md4OwnedByCluster,
			},
		},
		machineSets: clusterv1.MachineSetList{
			Items: []clusterv1.MachineSet{
				ms1NotOwnedByCluster,
				ms2OwnedByCluster,
				ms3NotOwnedByCluster,
				ms4OwnedByCluster,
			},
		},
		controlPlaneMachines: clusterv1.MachineList{
			Items: []clusterv1.Machine{
				m3ControlPlaneOwnedByCluster,
				m6ControlPlaneOwnedByCluster,
			},
		},
		workerMachines: clusterv1.MachineList{
			Items: []clusterv1.Machine{
				m1NotOwnedByCluster,
				m2OwnedByCluster,
				m4NotOwnedByCluster,
				m5OwnedByCluster,
			},
		},
		machinePools: expv1.MachinePoolList{
			Items: []expv1.MachinePool{
				mp1NotOwnedByCluster,
				mp2OwnedByCluster,
				mp3NotOwnedByCluster,
				mp4OwnedByCluster,
			},
		},
	}

	actual, err := d.filterOwnedDescendants(&c)
	g.Expect(err).NotTo(HaveOccurred())

	expected := []client.Object{
		&mp2OwnedByCluster,
		&mp4OwnedByCluster,
		&md2OwnedByCluster,
		&md4OwnedByCluster,
		&ms2OwnedByCluster,
		&ms4OwnedByCluster,
		&m2OwnedByCluster,
		&m5OwnedByCluster,
		&m3ControlPlaneOwnedByCluster,
		&m6ControlPlaneOwnedByCluster,
	}

	g.Expect(actual).To(Equal(expected))
}

func TestDescendantsLength(t *testing.T) {
	g := NewWithT(t)

	d := clusterDescendants{
		machineDeployments: clusterv1.MachineDeploymentList{
			Items: []clusterv1.MachineDeployment{
				newMachineDeploymentBuilder().named("md1").build(),
			},
		},
		machineSets: clusterv1.MachineSetList{
			Items: []clusterv1.MachineSet{
				newMachineSetBuilder().named("ms1").build(),
				newMachineSetBuilder().named("ms2").build(),
			},
		},
		controlPlaneMachines: clusterv1.MachineList{
			Items: []clusterv1.Machine{
				newMachineBuilder().named("m1").build(),
				newMachineBuilder().named("m2").build(),
				newMachineBuilder().named("m3").build(),
			},
		},
		workerMachines: clusterv1.MachineList{
			Items: []clusterv1.Machine{
				newMachineBuilder().named("m3").build(),
				newMachineBuilder().named("m4").build(),
				newMachineBuilder().named("m5").build(),
				newMachineBuilder().named("m6").build(),
			},
		},
		machinePools: expv1.MachinePoolList{
			Items: []expv1.MachinePool{
				newMachinePoolBuilder().named("mp1").build(),
				newMachinePoolBuilder().named("mp2").build(),
				newMachinePoolBuilder().named("mp3").build(),
				newMachinePoolBuilder().named("mp4").build(),
				newMachinePoolBuilder().named("mp5").build(),
			},
		},
	}

	g.Expect(d.length()).To(Equal(15))
}

func TestReconcileControlPlaneInitializedControlPlaneRef(t *testing.T) {
	g := NewWithT(t)

	c := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "c",
		},
		Spec: clusterv1.ClusterSpec{
			ControlPlaneRef: &corev1.ObjectReference{
				APIVersion: "test.io/v1",
				Namespace:  "test",
				Name:       "foo",
			},
		},
	}

	r := &ClusterReconciler{}
	res, err := r.reconcileControlPlaneInitialized(ctx, c)
	g.Expect(res.IsZero()).To(BeTrue())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(conditions.Has(c, clusterv1.ControlPlaneInitializedCondition)).To(BeFalse())
}
