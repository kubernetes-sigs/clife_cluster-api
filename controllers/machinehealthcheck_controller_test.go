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
	"errors"
	"fmt"

	"sort"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	"sigs.k8s.io/cluster-api/controllers/remote"
	capierrors "sigs.k8s.io/cluster-api/errors"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"
)

func TestMachineHealthCheck_Reconcile(t *testing.T) {
	ns, err := env.CreateNamespace(ctx, "test-mhc")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := env.Delete(ctx, ns); err != nil {
			t.Fatal(err)
		}
	}()

	t.Run("it should ensure the correct cluster-name label when no existing labels exist", func(t *testing.T) {
		g := NewWithT(t)
		cluster := createCluster(g, ns.Name)

		mhc := newMachineHealthCheck(cluster.Namespace, cluster.Name)
		mhc.Labels = map[string]string{}

		g.Expect(env.Create(ctx, mhc)).To(Succeed())
		defer func(do ...client.Object) {
			g.Expect(env.Cleanup(ctx, do...)).To(Succeed())
		}(cluster, mhc)

		g.Eventually(func() map[string]string {
			err := env.Get(ctx, util.ObjectKey(mhc), mhc)
			if err != nil {
				return nil
			}
			return mhc.GetLabels()
		}).Should(HaveKeyWithValue(clusterv1.ClusterLabelName, cluster.Name))
	})

	t.Run("it should ensure the correct cluster-name label when the label has the wrong value", func(t *testing.T) {
		g := NewWithT(t)
		cluster := createCluster(g, ns.Name)

		mhc := newMachineHealthCheck(cluster.Namespace, cluster.Name)
		mhc.Labels = map[string]string{
			clusterv1.ClusterLabelName: "wrong-cluster",
		}

		g.Expect(env.Create(ctx, mhc)).To(Succeed())
		defer func(do ...client.Object) {
			g.Expect(env.Cleanup(ctx, do...)).To(Succeed())
		}(cluster, mhc)

		g.Eventually(func() map[string]string {
			err := env.Get(ctx, util.ObjectKey(mhc), mhc)
			if err != nil {
				return nil
			}
			return mhc.GetLabels()
		}).Should(HaveKeyWithValue(clusterv1.ClusterLabelName, cluster.Name))
	})

	t.Run("it should ensure the correct cluster-name label when other labels are present", func(t *testing.T) {
		g := NewWithT(t)
		cluster := createCluster(g, ns.Name)

		mhc := newMachineHealthCheck(cluster.Namespace, cluster.Name)
		mhc.Labels = map[string]string{
			"extra-label": "1",
		}

		g.Expect(env.Create(ctx, mhc)).To(Succeed())
		defer func(do ...client.Object) {
			g.Expect(env.Cleanup(ctx, do...)).To(Succeed())
		}(cluster, mhc)

		g.Eventually(func() map[string]string {
			err := env.Get(ctx, util.ObjectKey(mhc), mhc)
			if err != nil {
				return nil
			}
			return mhc.GetLabels()
		}).Should(And(
			HaveKeyWithValue(clusterv1.ClusterLabelName, cluster.Name),
			HaveKeyWithValue("extra-label", "1"),
			HaveLen(2),
		))
	})

	t.Run("it should ensure an owner reference is present when no existing ones exist", func(t *testing.T) {
		g := NewWithT(t)
		cluster := createCluster(g, ns.Name)

		mhc := newMachineHealthCheck(cluster.Namespace, cluster.Name)
		mhc.OwnerReferences = []metav1.OwnerReference{}

		g.Expect(env.Create(ctx, mhc)).To(Succeed())
		defer func(do ...client.Object) {
			g.Expect(env.Cleanup(ctx, do...)).To(Succeed())
		}(cluster, mhc)

		g.Eventually(func() []metav1.OwnerReference {
			err := env.Get(ctx, util.ObjectKey(mhc), mhc)
			if err != nil {
				fmt.Printf("error cannot retrieve mhc in ctx: %v", err)
				return nil
			}
			return mhc.GetOwnerReferences()
		}, timeout, 100*time.Millisecond).Should(And(
			HaveLen(1),
			ContainElement(ownerReferenceForCluster(ctx, g, cluster)),
		))
	})

	t.Run("it should ensure an owner reference is present when modifying existing ones", func(t *testing.T) {
		g := NewWithT(t)
		cluster := createCluster(g, ns.Name)

		mhc := newMachineHealthCheck(cluster.Namespace, cluster.Name)
		mhc.OwnerReferences = []metav1.OwnerReference{
			{Kind: "Foo", APIVersion: "foo.bar.baz/v1", Name: "Bar", UID: "12345"},
		}

		g.Expect(env.Create(ctx, mhc)).To(Succeed())
		defer func(do ...client.Object) {
			g.Expect(env.Cleanup(ctx, do...)).To(Succeed())
		}(cluster, mhc)

		g.Eventually(func() []metav1.OwnerReference {
			err := env.Get(ctx, util.ObjectKey(mhc), mhc)
			if err != nil {
				return nil
			}
			return mhc.GetOwnerReferences()
		}, timeout, 100*time.Millisecond).Should(And(
			ContainElements(
				metav1.OwnerReference{Kind: "Foo", APIVersion: "foo.bar.baz/v1", Name: "Bar", UID: "12345"},
				ownerReferenceForCluster(ctx, g, cluster)),
			HaveLen(2),
		))
	})

	t.Run("it ignores Machines not matching the label selector", func(t *testing.T) {
		g := NewWithT(t)
		cluster := createCluster(g, ns.Name)

		mhc := newMachineHealthCheck(cluster.Namespace, cluster.Name)

		g.Expect(env.Create(ctx, mhc)).To(Succeed())
		defer func(do ...client.Object) {
			g.Expect(env.Cleanup(ctx, do...)).To(Succeed())
		}(cluster, mhc)

		// Healthy nodes and machines matching the MHC's label selector.
		_, machines, cleanup := createMachinesWithNodes(g, cluster,
			count(2),
			firstMachineAsControlPlane(),
			createNodeRefForMachine(true),
			nodeStatus(corev1.ConditionTrue),
			machineLabels(mhc.Spec.Selector.MatchLabels),
		)
		defer cleanup()
		targetMachines := make([]string, len(machines))
		for i, m := range machines {
			targetMachines[i] = m.Name
		}
		sort.Strings(targetMachines)

		// Healthy nodes and machines NOT matching the MHC's label selector.
		_, _, cleanup2 := createMachinesWithNodes(g, cluster,
			count(2),
			createNodeRefForMachine(true),
			nodeStatus(corev1.ConditionTrue),
		)
		defer cleanup2()

		// Make sure the status matches.
		g.Eventually(func() *clusterv1.MachineHealthCheckStatus {
			err := env.Get(ctx, util.ObjectKey(mhc), mhc)
			if err != nil {
				return nil
			}
			return &mhc.Status
		}, 5*time.Second, 100*time.Millisecond).Should(MatchMachineHealthCheckStatus(&clusterv1.MachineHealthCheckStatus{
			ExpectedMachines:    2,
			CurrentHealthy:      2,
			RemediationsAllowed: 2,
			ObservedGeneration:  1,
			Targets:             targetMachines,
			Conditions: clusterv1.Conditions{
				{
					Type:   clusterv1.RemediationAllowedCondition,
					Status: corev1.ConditionTrue,
				},
			},
		}))
	})

	t.Run("it doesn't mark anything unhealthy when cluster infrastructure is not ready", func(t *testing.T) {
		g := NewWithT(t)
		cluster := createCluster(g, ns.Name)

		patchHelper, err := patch.NewHelper(cluster, env.Client)
		g.Expect(err).To(BeNil())

		conditions.MarkFalse(cluster, clusterv1.InfrastructureReadyCondition, "SomeReason", clusterv1.ConditionSeverityError, "")
		g.Expect(patchHelper.Patch(ctx, cluster)).To(Succeed())

		mhc := newMachineHealthCheck(cluster.Namespace, cluster.Name)

		g.Expect(env.Create(ctx, mhc)).To(Succeed())
		defer func(do ...client.Object) {
			g.Expect(env.Cleanup(ctx, do...)).To(Succeed())
		}(cluster, mhc)

		// Healthy nodes and machines.
		_, machines, cleanup := createMachinesWithNodes(g, cluster,
			count(2),
			firstMachineAsControlPlane(),
			createNodeRefForMachine(true),
			machineLabels(mhc.Spec.Selector.MatchLabels),
		)
		defer cleanup()
		targetMachines := make([]string, len(machines))
		for i, m := range machines {
			targetMachines[i] = m.Name
		}
		sort.Strings(targetMachines)

		// Make sure the status matches.
		g.Eventually(func() *clusterv1.MachineHealthCheckStatus {
			err := env.Get(ctx, util.ObjectKey(mhc), mhc)
			if err != nil {
				return nil
			}
			return &mhc.Status
		}).Should(MatchMachineHealthCheckStatus(&clusterv1.MachineHealthCheckStatus{
			ExpectedMachines:    2,
			CurrentHealthy:      2,
			RemediationsAllowed: 2,
			ObservedGeneration:  1,
			Targets:             targetMachines,
			Conditions: clusterv1.Conditions{
				{
					Type:   clusterv1.RemediationAllowedCondition,
					Status: corev1.ConditionTrue,
				},
			},
		}))
	})

	t.Run("it doesn't mark anything unhealthy when all Machines are healthy", func(t *testing.T) {
		g := NewWithT(t)
		cluster := createCluster(g, ns.Name)

		mhc := newMachineHealthCheck(cluster.Namespace, cluster.Name)

		g.Expect(env.Create(ctx, mhc)).To(Succeed())
		defer func(do ...client.Object) {
			g.Expect(env.Cleanup(ctx, do...)).To(Succeed())
		}(cluster, mhc)

		// Healthy nodes and machines.
		_, machines, cleanup := createMachinesWithNodes(g, cluster,
			count(2),
			firstMachineAsControlPlane(),
			createNodeRefForMachine(true),
			nodeStatus(corev1.ConditionTrue),
			machineLabels(mhc.Spec.Selector.MatchLabels),
		)
		defer cleanup()
		targetMachines := make([]string, len(machines))
		for i, m := range machines {
			targetMachines[i] = m.Name
		}
		sort.Strings(targetMachines)

		// Make sure the status matches.
		g.Eventually(func() *clusterv1.MachineHealthCheckStatus {
			err := env.Get(ctx, util.ObjectKey(mhc), mhc)
			if err != nil {
				return nil
			}
			return &mhc.Status
		}).Should(MatchMachineHealthCheckStatus(&clusterv1.MachineHealthCheckStatus{
			ExpectedMachines:    2,
			CurrentHealthy:      2,
			RemediationsAllowed: 2,
			ObservedGeneration:  1,
			Targets:             targetMachines,
			Conditions: clusterv1.Conditions{
				{
					Type:   clusterv1.RemediationAllowedCondition,
					Status: corev1.ConditionTrue,
				},
			},
		}))
	})

	t.Run("it marks unhealthy machines for remediation when there is one unhealthy Machine", func(t *testing.T) {
		g := NewWithT(t)
		cluster := createCluster(g, ns.Name)

		mhc := newMachineHealthCheck(cluster.Namespace, cluster.Name)

		g.Expect(env.Create(ctx, mhc)).To(Succeed())
		defer func(do ...client.Object) {
			g.Expect(env.Cleanup(ctx, do...)).To(Succeed())
		}(cluster, mhc)

		// Healthy nodes and machines.
		_, machines, cleanup1 := createMachinesWithNodes(g, cluster,
			count(2),
			firstMachineAsControlPlane(),
			createNodeRefForMachine(true),
			nodeStatus(corev1.ConditionTrue),
			machineLabels(mhc.Spec.Selector.MatchLabels),
		)
		defer cleanup1()
		// Unhealthy nodes and machines.
		_, unhealthyMachines, cleanup2 := createMachinesWithNodes(g, cluster,
			count(1),
			createNodeRefForMachine(true),
			nodeStatus(corev1.ConditionUnknown),
			machineLabels(mhc.Spec.Selector.MatchLabels),
		)
		defer cleanup2()
		machines = append(machines, unhealthyMachines...)
		targetMachines := make([]string, len(machines))
		for i, m := range machines {
			targetMachines[i] = m.Name
		}
		sort.Strings(targetMachines)

		// Make sure the status matches.
		g.Eventually(func() *clusterv1.MachineHealthCheckStatus {
			err := env.Get(ctx, util.ObjectKey(mhc), mhc)
			if err != nil {
				return nil
			}
			return &mhc.Status
		}).Should(MatchMachineHealthCheckStatus(&clusterv1.MachineHealthCheckStatus{
			ExpectedMachines:    3,
			CurrentHealthy:      2,
			RemediationsAllowed: 2,
			ObservedGeneration:  1,
			Targets:             targetMachines,
			Conditions: clusterv1.Conditions{
				{
					Type:   clusterv1.RemediationAllowedCondition,
					Status: corev1.ConditionTrue,
				},
			},
		}))
	})

	t.Run("it marks unhealthy machines for remediation when there a Machine has a failure reason", func(t *testing.T) {
		g := NewWithT(t)
		cluster := createCluster(g, ns.Name)

		mhc := newMachineHealthCheck(cluster.Namespace, cluster.Name)

		g.Expect(env.Create(ctx, mhc)).To(Succeed())
		defer func(do ...client.Object) {
			g.Expect(env.Cleanup(ctx, do...)).To(Succeed())
		}(cluster, mhc)

		// Healthy nodes and machines.
		_, machines, cleanup1 := createMachinesWithNodes(g, cluster,
			count(2),
			firstMachineAsControlPlane(),
			createNodeRefForMachine(true),
			nodeStatus(corev1.ConditionTrue),
			machineLabels(mhc.Spec.Selector.MatchLabels),
		)
		defer cleanup1()
		// Machine with failure reason.
		_, unhealthyMachines, cleanup2 := createMachinesWithNodes(g, cluster,
			count(1),
			createNodeRefForMachine(true),
			nodeStatus(corev1.ConditionTrue),
			machineLabels(mhc.Spec.Selector.MatchLabels),
			machineFailureReason("some failure"),
		)
		defer cleanup2()
		machines = append(machines, unhealthyMachines...)
		targetMachines := make([]string, len(machines))
		for i, m := range machines {
			targetMachines[i] = m.Name
		}
		sort.Strings(targetMachines)

		// Make sure the status matches.
		g.Eventually(func() *clusterv1.MachineHealthCheckStatus {
			err := env.Get(ctx, util.ObjectKey(mhc), mhc)
			if err != nil {
				return nil
			}
			return &mhc.Status
		}).Should(MatchMachineHealthCheckStatus(&clusterv1.MachineHealthCheckStatus{
			ExpectedMachines:    3,
			CurrentHealthy:      2,
			RemediationsAllowed: 2,
			ObservedGeneration:  1,
			Targets:             targetMachines,
			Conditions: clusterv1.Conditions{
				{
					Type:   clusterv1.RemediationAllowedCondition,
					Status: corev1.ConditionTrue,
				},
			},
		}))
	})

	t.Run("it marks unhealthy machines for remediation when there a Machine has a failure message", func(t *testing.T) {
		g := NewWithT(t)
		cluster := createCluster(g, ns.Name)

		mhc := newMachineHealthCheck(cluster.Namespace, cluster.Name)

		g.Expect(env.Create(ctx, mhc)).To(Succeed())
		defer func(do ...client.Object) {
			g.Expect(env.Cleanup(ctx, do...)).To(Succeed())
		}(cluster, mhc)

		// Healthy nodes and machines.
		_, machines, cleanup1 := createMachinesWithNodes(g, cluster,
			count(2),
			firstMachineAsControlPlane(),
			createNodeRefForMachine(true),
			nodeStatus(corev1.ConditionTrue),
			machineLabels(mhc.Spec.Selector.MatchLabels),
		)
		defer cleanup1()
		// Machine with failure message.
		_, unhealthyMachines, cleanup2 := createMachinesWithNodes(g, cluster,
			count(1),
			createNodeRefForMachine(true),
			nodeStatus(corev1.ConditionTrue),
			machineLabels(mhc.Spec.Selector.MatchLabels),
			machineFailureMessage("some failure"),
		)
		defer cleanup2()
		machines = append(machines, unhealthyMachines...)
		targetMachines := make([]string, len(machines))
		for i, m := range machines {
			targetMachines[i] = m.Name
		}
		sort.Strings(targetMachines)

		// Make sure the status matches.
		g.Eventually(func() *clusterv1.MachineHealthCheckStatus {
			err := env.Get(ctx, util.ObjectKey(mhc), mhc)
			if err != nil {
				return nil
			}
			return &mhc.Status
		}).Should(MatchMachineHealthCheckStatus(&clusterv1.MachineHealthCheckStatus{
			ExpectedMachines:    3,
			CurrentHealthy:      2,
			RemediationsAllowed: 2,
			ObservedGeneration:  1,
			Targets:             targetMachines,
			Conditions: clusterv1.Conditions{
				{
					Type:   clusterv1.RemediationAllowedCondition,
					Status: corev1.ConditionTrue,
				},
			},
		}))
	})

	t.Run("it marks unhealthy machines for remediation when the unhealthy Machines exceed MaxUnhealthy", func(t *testing.T) {
		g := NewWithT(t)
		cluster := createCluster(g, ns.Name)

		mhc := newMachineHealthCheck(cluster.Namespace, cluster.Name)
		maxUnhealthy := intstr.Parse("40%")
		mhc.Spec.MaxUnhealthy = &maxUnhealthy

		g.Expect(env.Create(ctx, mhc)).To(Succeed())
		defer func(do ...client.Object) {
			g.Expect(env.Cleanup(ctx, do...)).To(Succeed())
		}(cluster, mhc)

		// Healthy nodes and machines.
		_, machines, cleanup1 := createMachinesWithNodes(g, cluster,
			count(1),
			firstMachineAsControlPlane(),
			createNodeRefForMachine(true),
			nodeStatus(corev1.ConditionTrue),
			machineLabels(mhc.Spec.Selector.MatchLabels),
		)
		defer cleanup1()
		// Unhealthy nodes and machines.
		_, unhealthyMachines, cleanup2 := createMachinesWithNodes(g, cluster,
			count(2),
			createNodeRefForMachine(true),
			nodeStatus(corev1.ConditionUnknown),
			machineLabels(mhc.Spec.Selector.MatchLabels),
		)
		defer cleanup2()
		machines = append(machines, unhealthyMachines...)
		targetMachines := make([]string, len(machines))
		for i, m := range machines {
			targetMachines[i] = m.Name
		}
		sort.Strings(targetMachines)

		// Make sure the status matches.
		g.Eventually(func() *clusterv1.MachineHealthCheckStatus {
			err := env.Get(ctx, util.ObjectKey(mhc), mhc)
			if err != nil {
				return nil
			}
			return &mhc.Status
		}).Should(MatchMachineHealthCheckStatus(&clusterv1.MachineHealthCheckStatus{
			ExpectedMachines:    3,
			CurrentHealthy:      1,
			RemediationsAllowed: 0,
			ObservedGeneration:  1,
			Targets:             targetMachines,
			Conditions: clusterv1.Conditions{
				{
					Type:     clusterv1.RemediationAllowedCondition,
					Status:   corev1.ConditionFalse,
					Severity: clusterv1.ConditionSeverityWarning,
					Reason:   clusterv1.TooManyUnhealthyReason,
					Message:  "Remediation is not allowed, the number of not started or unhealthy machines exceeds maxUnhealthy (total: 3, unhealthy: 2, maxUnhealthy: 40%)",
				},
			},
		}))

		// Calculate how many Machines have health check succeeded = false.
		g.Eventually(func() (unhealthy int) {
			machines := &clusterv1.MachineList{}
			err := env.List(ctx, machines, client.MatchingLabels{
				"selector": mhc.Spec.Selector.MatchLabels["selector"],
			})
			if err != nil {
				return -1
			}

			for i := range machines.Items {
				if conditions.IsFalse(&machines.Items[i], clusterv1.MachineHealthCheckSuccededCondition) {
					unhealthy++
				}
			}
			return
		}).Should(Equal(2))

		// Calculate how many Machines have been remediated.
		g.Eventually(func() (remediated int) {
			machines := &clusterv1.MachineList{}
			err := env.List(ctx, machines, client.MatchingLabels{
				"selector": mhc.Spec.Selector.MatchLabels["selector"],
			})
			if err != nil {
				return -1
			}

			for i := range machines.Items {
				if conditions.IsTrue(&machines.Items[i], clusterv1.MachineOwnerRemediatedCondition) {
					remediated++
				}
			}
			return
		}).Should(Equal(0))
	})

	t.Run("it marks unhealthy machines for remediation when number of unhealthy machines is within unhealthyRange", func(t *testing.T) {
		g := NewWithT(t)
		cluster := createCluster(g, ns.Name)

		mhc := newMachineHealthCheck(cluster.Namespace, cluster.Name)
		unhealthyRange := "[1-3]"
		mhc.Spec.UnhealthyRange = &unhealthyRange

		g.Expect(env.Create(ctx, mhc)).To(Succeed())
		defer func(do ...client.Object) {
			g.Expect(env.Cleanup(ctx, do...)).To(Succeed())
		}(cluster, mhc)

		// Healthy nodes and machines.
		_, machines, cleanup1 := createMachinesWithNodes(g, cluster,
			count(2),
			firstMachineAsControlPlane(),
			createNodeRefForMachine(true),
			nodeStatus(corev1.ConditionTrue),
			machineLabels(mhc.Spec.Selector.MatchLabels),
		)
		defer cleanup1()
		// Unhealthy nodes and machines.
		_, unhealthyMachines, cleanup2 := createMachinesWithNodes(g, cluster,
			count(1),
			createNodeRefForMachine(true),
			nodeStatus(corev1.ConditionUnknown),
			machineLabels(mhc.Spec.Selector.MatchLabels),
		)
		defer cleanup2()
		machines = append(machines, unhealthyMachines...)
		targetMachines := make([]string, len(machines))
		for i, m := range machines {
			targetMachines[i] = m.Name
		}
		sort.Strings(targetMachines)

		// Make sure the status matches.
		g.Eventually(func() *clusterv1.MachineHealthCheckStatus {
			err := env.Get(ctx, util.ObjectKey(mhc), mhc)
			if err != nil {
				return nil
			}
			return &mhc.Status
		}).Should(MatchMachineHealthCheckStatus(&clusterv1.MachineHealthCheckStatus{
			ExpectedMachines:    3,
			CurrentHealthy:      2,
			RemediationsAllowed: 2,
			ObservedGeneration:  1,
			Targets:             targetMachines,
			Conditions: clusterv1.Conditions{
				{
					Type:   clusterv1.RemediationAllowedCondition,
					Status: corev1.ConditionTrue,
				},
			},
		}))
	})

	t.Run("it marks unhealthy machines for remediation when the unhealthy Machines is not within UnhealthyRange", func(t *testing.T) {
		g := NewWithT(t)
		cluster := createCluster(g, ns.Name)

		mhc := newMachineHealthCheck(cluster.Namespace, cluster.Name)
		unhealthyRange := "[3-5]"
		mhc.Spec.UnhealthyRange = &unhealthyRange

		g.Expect(env.Create(ctx, mhc)).To(Succeed())
		defer func(do ...client.Object) {
			g.Expect(env.Cleanup(ctx, do...)).To(Succeed())
		}(cluster, mhc)

		// Healthy nodes and machines.
		_, machines, cleanup1 := createMachinesWithNodes(g, cluster,
			count(1),
			firstMachineAsControlPlane(),
			createNodeRefForMachine(true),
			nodeStatus(corev1.ConditionTrue),
			machineLabels(mhc.Spec.Selector.MatchLabels),
		)
		defer cleanup1()
		// Unhealthy nodes and machines.
		_, unhealthyMachines, cleanup2 := createMachinesWithNodes(g, cluster,
			count(2),
			createNodeRefForMachine(true),
			nodeStatus(corev1.ConditionUnknown),
			machineLabels(mhc.Spec.Selector.MatchLabels),
		)
		defer cleanup2()
		machines = append(machines, unhealthyMachines...)
		targetMachines := make([]string, len(machines))
		for i, m := range machines {
			targetMachines[i] = m.Name
		}
		sort.Strings(targetMachines)

		// Make sure the status matches.
		g.Eventually(func() *clusterv1.MachineHealthCheckStatus {
			err := env.Get(ctx, util.ObjectKey(mhc), mhc)
			if err != nil {
				return nil
			}
			return &mhc.Status
		}).Should(MatchMachineHealthCheckStatus(&clusterv1.MachineHealthCheckStatus{
			ExpectedMachines:    3,
			CurrentHealthy:      1,
			RemediationsAllowed: 0,
			ObservedGeneration:  1,
			Targets:             targetMachines,
			Conditions: clusterv1.Conditions{
				{
					Type:     clusterv1.RemediationAllowedCondition,
					Status:   corev1.ConditionFalse,
					Severity: clusterv1.ConditionSeverityWarning,
					Reason:   clusterv1.TooManyUnhealthyReason,
					Message:  "Remediation is not allowed, the number of not started or unhealthy machines does not fall within the range (total: 3, unhealthy: 2, unhealthyRange: [3-5])",
				},
			},
		}))

		// Calculate how many Machines have health check succeeded = false.
		g.Eventually(func() (unhealthy int) {
			machines := &clusterv1.MachineList{}
			err := env.List(ctx, machines, client.MatchingLabels{
				"selector": mhc.Spec.Selector.MatchLabels["selector"],
			})
			if err != nil {
				return -1
			}

			for i := range machines.Items {
				if conditions.IsFalse(&machines.Items[i], clusterv1.MachineHealthCheckSuccededCondition) {
					unhealthy++
				}
			}
			return
		}).Should(Equal(2))

		// Calculate how many Machines have been remediated.
		g.Eventually(func() (remediated int) {
			machines := &clusterv1.MachineList{}
			err := env.List(ctx, machines, client.MatchingLabels{
				"selector": mhc.Spec.Selector.MatchLabels["selector"],
			})
			if err != nil {
				return -1
			}

			for i := range machines.Items {
				if conditions.Get(&machines.Items[i], clusterv1.MachineOwnerRemediatedCondition) != nil {
					remediated++
				}
			}
			return
		}).Should(Equal(0))
	})

	t.Run("when a Machine has no Node ref for less than the NodeStartupTimeout", func(t *testing.T) {
		g := NewWithT(t)
		cluster := createCluster(g, ns.Name)

		// After the cluster exists, we have to set the infrastructure ready condition; otherwise, MachineHealthChecks
		// will never fail when nodeStartupTimeout is exceeded.
		patchHelper, err := patch.NewHelper(cluster, env.GetClient())
		g.Expect(err).ToNot(HaveOccurred())

		conditions.MarkTrue(cluster, clusterv1.InfrastructureReadyCondition)
		g.Expect(patchHelper.Patch(ctx, cluster)).To(Succeed())

		mhc := newMachineHealthCheck(cluster.Namespace, cluster.Name)
		mhc.Spec.NodeStartupTimeout = &metav1.Duration{Duration: 5 * time.Hour}

		g.Expect(env.Create(ctx, mhc)).To(Succeed())
		defer func(do ...client.Object) {
			g.Expect(env.Cleanup(ctx, do...)).To(Succeed())
		}(cluster, mhc)

		// Healthy nodes and machines.
		_, machines, cleanup1 := createMachinesWithNodes(g, cluster,
			count(2),
			firstMachineAsControlPlane(),
			createNodeRefForMachine(true),
			nodeStatus(corev1.ConditionTrue),
			machineLabels(mhc.Spec.Selector.MatchLabels),
		)
		defer cleanup1()
		// Unhealthy nodes and machines.
		_, unhealthyMachines, cleanup2 := createMachinesWithNodes(g, cluster,
			count(1),
			createNodeRefForMachine(false),
			nodeStatus(corev1.ConditionUnknown),
			machineLabels(mhc.Spec.Selector.MatchLabels),
		)
		defer cleanup2()
		machines = append(machines, unhealthyMachines...)
		targetMachines := make([]string, len(machines))
		for i, m := range machines {
			targetMachines[i] = m.Name
		}
		sort.Strings(targetMachines)

		// Make sure the status matches.
		g.Eventually(func() *clusterv1.MachineHealthCheckStatus {
			err := env.Get(ctx, util.ObjectKey(mhc), mhc)
			if err != nil {
				return nil
			}
			return &mhc.Status
		}).Should(MatchMachineHealthCheckStatus(&clusterv1.MachineHealthCheckStatus{
			ExpectedMachines:    3,
			CurrentHealthy:      2,
			RemediationsAllowed: 2,
			ObservedGeneration:  1,
			Targets:             targetMachines,
			Conditions: clusterv1.Conditions{
				{
					Type:   clusterv1.RemediationAllowedCondition,
					Status: corev1.ConditionTrue,
				},
			},
		}))

		// Calculate how many Machines have health check succeeded = false.
		g.Eventually(func() (unhealthy int) {
			machines := &clusterv1.MachineList{}
			err := env.List(ctx, machines, client.MatchingLabels{
				"selector": mhc.Spec.Selector.MatchLabels["selector"],
			})
			if err != nil {
				return -1
			}

			for i := range machines.Items {
				if conditions.IsFalse(&machines.Items[i], clusterv1.MachineHealthCheckSuccededCondition) {
					unhealthy++
				}
			}
			return
		}).Should(Equal(0))

		// Calculate how many Machines have been remediated.
		g.Eventually(func() (remediated int) {
			machines := &clusterv1.MachineList{}
			err := env.List(ctx, machines, client.MatchingLabels{
				"selector": mhc.Spec.Selector.MatchLabels["selector"],
			})
			if err != nil {
				return -1
			}

			for i := range machines.Items {
				if conditions.IsTrue(&machines.Items[i], clusterv1.MachineOwnerRemediatedCondition) {
					remediated++
				}
			}
			return
		}).Should(Equal(0))
	})

	t.Run("when a Machine has no Node ref for longer than the NodeStartupTimeout", func(t *testing.T) {
		// FIXME: Resolve flaky/failing test
		t.Skip("skipping until made stable")
		g := NewWithT(t)
		cluster := createCluster(g, ns.Name)

		mhc := newMachineHealthCheck(cluster.Namespace, cluster.Name)
		mhc.Spec.NodeStartupTimeout = &metav1.Duration{Duration: time.Second}

		g.Expect(env.Create(ctx, mhc)).To(Succeed())
		defer func(do ...client.Object) {
			g.Expect(env.Cleanup(ctx, do...)).To(Succeed())
		}(cluster, mhc)

		// Healthy nodes and machines.
		_, machines, cleanup1 := createMachinesWithNodes(g, cluster,
			count(2),
			firstMachineAsControlPlane(),
			createNodeRefForMachine(true),
			nodeStatus(corev1.ConditionTrue),
			machineLabels(mhc.Spec.Selector.MatchLabels),
		)
		defer cleanup1()
		// Unhealthy nodes and machines.
		_, unhealthyMachines, cleanup2 := createMachinesWithNodes(g, cluster,
			count(1),
			createNodeRefForMachine(false),
			nodeStatus(corev1.ConditionUnknown),
			machineLabels(mhc.Spec.Selector.MatchLabels),
		)
		defer cleanup2()
		machines = append(machines, unhealthyMachines...)

		targetMachines := make([]string, len(machines))
		for i, m := range machines {
			targetMachines[i] = m.Name
		}
		sort.Strings(targetMachines)

		// Make sure the MHC status matches. We have two healthy machines and
		// one unhealthy.
		g.Eventually(func() *clusterv1.MachineHealthCheckStatus {
			err := env.Get(ctx, util.ObjectKey(mhc), mhc)
			if err != nil {
				fmt.Printf("error retrieving mhc: %v", err)
				return nil
			}
			return &mhc.Status
		}, timeout, 100*time.Millisecond).Should(MatchMachineHealthCheckStatus(&clusterv1.MachineHealthCheckStatus{
			ExpectedMachines:    3,
			CurrentHealthy:      2,
			RemediationsAllowed: 2,
			ObservedGeneration:  1,
			Targets:             targetMachines,
			Conditions: clusterv1.Conditions{
				{
					Type:   clusterv1.RemediationAllowedCondition,
					Status: corev1.ConditionTrue,
				},
			},
		}))

		// Calculate how many Machines have health check succeeded = false.
		g.Eventually(func() (unhealthy int) {
			machines := &clusterv1.MachineList{}
			err := env.List(ctx, machines, client.MatchingLabels{
				"selector": mhc.Spec.Selector.MatchLabels["selector"],
			})
			if err != nil {
				fmt.Printf("error retrieving list: %v", err)
				return -1
			}

			for i := range machines.Items {
				if conditions.IsFalse(&machines.Items[i], clusterv1.MachineHealthCheckSuccededCondition) {
					unhealthy++
				}
			}
			return
		}, timeout, 100*time.Millisecond).Should(Equal(1))

		// Calculate how many Machines have been remediated.
		g.Eventually(func() (remediated int) {
			machines := &clusterv1.MachineList{}
			err := env.List(ctx, machines, client.MatchingLabels{
				"selector": mhc.Spec.Selector.MatchLabels["selector"],
			})
			if err != nil {
				return -1
			}

			for i := range machines.Items {
				if conditions.Get(&machines.Items[i], clusterv1.MachineOwnerRemediatedCondition) != nil {
					remediated++
				}
			}
			return
		}, timeout, 100*time.Millisecond).Should(Equal(1))
	})

	t.Run("when a Machine's Node has gone away", func(t *testing.T) {
		// FIXME: Resolve flaky/failing test
		t.Skip("skipping until made stable")
		g := NewWithT(t)
		cluster := createCluster(g, ns.Name)

		mhc := newMachineHealthCheck(cluster.Namespace, cluster.Name)

		g.Expect(env.Create(ctx, mhc)).To(Succeed())
		defer func(do ...client.Object) {
			g.Expect(env.Cleanup(ctx, do...)).To(Succeed())
		}(cluster, mhc)

		// Healthy nodes and machines.
		nodes, machines, cleanup := createMachinesWithNodes(g, cluster,
			count(3),
			firstMachineAsControlPlane(),
			createNodeRefForMachine(true),
			nodeStatus(corev1.ConditionTrue),
			machineLabels(mhc.Spec.Selector.MatchLabels),
		)
		defer cleanup()
		targetMachines := make([]string, len(machines))
		for i, m := range machines {
			targetMachines[i] = m.Name
		}
		sort.Strings(targetMachines)

		// Forcibly remove the last machine's node.
		g.Eventually(func() bool {
			nodeToBeRemoved := nodes[2]
			if err := env.Delete(ctx, nodeToBeRemoved); err != nil {
				return apierrors.IsNotFound(err)
			}
			return apierrors.IsNotFound(env.Get(ctx, util.ObjectKey(nodeToBeRemoved), nodeToBeRemoved))
		}).Should(BeTrue())

		// Make sure the status matches.
		g.Eventually(func() *clusterv1.MachineHealthCheckStatus {
			err := env.Get(ctx, util.ObjectKey(mhc), mhc)
			if err != nil {
				return nil
			}
			return &mhc.Status
		}).Should(MatchMachineHealthCheckStatus(&clusterv1.MachineHealthCheckStatus{
			ExpectedMachines:    3,
			CurrentHealthy:      2,
			RemediationsAllowed: 2,
			ObservedGeneration:  1,
			Targets:             targetMachines,
			Conditions: clusterv1.Conditions{
				{
					Type:   clusterv1.RemediationAllowedCondition,
					Status: corev1.ConditionTrue,
				},
			},
		}))

		// Calculate how many Machines have health check succeeded = false.
		g.Eventually(func() (unhealthy int) {
			machines := &clusterv1.MachineList{}
			err := env.List(ctx, machines, client.MatchingLabels{
				"selector": mhc.Spec.Selector.MatchLabels["selector"],
			})
			if err != nil {
				return -1
			}

			for i := range machines.Items {
				if conditions.IsFalse(&machines.Items[i], clusterv1.MachineHealthCheckSuccededCondition) {
					unhealthy++
				}
			}
			return
		}).Should(Equal(1))

		// Calculate how many Machines have been remediated.
		g.Eventually(func() (remediated int) {
			machines := &clusterv1.MachineList{}
			err := env.List(ctx, machines, client.MatchingLabels{
				"selector": mhc.Spec.Selector.MatchLabels["selector"],
			})
			if err != nil {
				return -1
			}

			for i := range machines.Items {
				if conditions.Get(&machines.Items[i], clusterv1.MachineOwnerRemediatedCondition) != nil {
					remediated++
				}
			}
			return
		}, timeout, 100*time.Millisecond).Should(Equal(1))
	})

	t.Run("should react when a Node transitions to unhealthy", func(t *testing.T) {
		g := NewWithT(t)
		cluster := createCluster(g, ns.Name)

		mhc := newMachineHealthCheck(cluster.Namespace, cluster.Name)

		g.Expect(env.Create(ctx, mhc)).To(Succeed())
		defer func(do ...client.Object) {
			g.Expect(env.Cleanup(ctx, do...)).To(Succeed())
		}(cluster, mhc)

		// Healthy nodes and machines.
		nodes, machines, cleanup := createMachinesWithNodes(g, cluster,
			count(1),
			firstMachineAsControlPlane(),
			createNodeRefForMachine(true),
			nodeStatus(corev1.ConditionTrue),
			machineLabels(mhc.Spec.Selector.MatchLabels),
		)
		defer cleanup()
		targetMachines := make([]string, len(machines))
		for i, m := range machines {
			targetMachines[i] = m.Name
		}
		sort.Strings(targetMachines)

		// Make sure the status matches.
		g.Eventually(func() *clusterv1.MachineHealthCheckStatus {
			err := env.Get(ctx, util.ObjectKey(mhc), mhc)
			if err != nil {
				return nil
			}
			return &mhc.Status
		}).Should(MatchMachineHealthCheckStatus(&clusterv1.MachineHealthCheckStatus{
			ExpectedMachines:    1,
			CurrentHealthy:      1,
			RemediationsAllowed: 1,
			ObservedGeneration:  1,
			Targets:             targetMachines,
			Conditions: clusterv1.Conditions{
				{
					Type:   clusterv1.RemediationAllowedCondition,
					Status: corev1.ConditionTrue,
				},
			},
		}))

		// Transition the node to unhealthy.
		node := nodes[0]
		nodePatch := client.MergeFrom(node.DeepCopy())
		node.Status.Conditions = []corev1.NodeCondition{
			{
				Type:               corev1.NodeReady,
				Status:             corev1.ConditionUnknown,
				LastTransitionTime: metav1.NewTime(time.Now().Add(-10 * time.Minute)),
			},
		}
		g.Expect(env.Status().Patch(ctx, node, nodePatch)).To(Succeed())

		// Make sure the status matches.
		g.Eventually(func() *clusterv1.MachineHealthCheckStatus {
			err := env.Get(ctx, util.ObjectKey(mhc), mhc)
			if err != nil {
				return nil
			}
			return &mhc.Status
		}).Should(MatchMachineHealthCheckStatus(&clusterv1.MachineHealthCheckStatus{
			ExpectedMachines:   1,
			CurrentHealthy:     0,
			ObservedGeneration: 1,
			Targets:            targetMachines,
			Conditions: clusterv1.Conditions{
				{
					Type:   clusterv1.RemediationAllowedCondition,
					Status: corev1.ConditionTrue,
				},
			},
		}))

		// Calculate how many Machines have health check succeeded = false.
		g.Eventually(func() (unhealthy int) {
			machines := &clusterv1.MachineList{}
			err := env.List(ctx, machines, client.MatchingLabels{
				"selector": mhc.Spec.Selector.MatchLabels["selector"],
			})
			if err != nil {
				return -1
			}

			for i := range machines.Items {
				if conditions.IsFalse(&machines.Items[i], clusterv1.MachineHealthCheckSuccededCondition) {
					unhealthy++
				}
			}
			return
		}).Should(Equal(1))

		// Calculate how many Machines have been marked for remediation
		g.Eventually(func() (remediated int) {
			machines := &clusterv1.MachineList{}
			err := env.List(ctx, machines, client.MatchingLabels{
				"selector": mhc.Spec.Selector.MatchLabels["selector"],
			})
			if err != nil {
				return -1
			}

			for i := range machines.Items {
				if conditions.IsFalse(&machines.Items[i], clusterv1.MachineOwnerRemediatedCondition) {
					remediated++
				}
			}
			return
		}).Should(Equal(1))
	})

	t.Run("when in a MachineSet, unhealthy machines should be deleted", func(t *testing.T) {
		g := NewWithT(t)
		cluster := createCluster(g, ns.Name)

		// Create 1 control plane machine so MHC can proceed
		_, _, cleanup := createMachinesWithNodes(g, cluster,
			count(1),
			firstMachineAsControlPlane(),
			createNodeRefForMachine(true),
			nodeStatus(corev1.ConditionTrue),
		)
		defer cleanup()

		mhc := newMachineHealthCheck(cluster.Namespace, cluster.Name)
		// Create infrastructure template resource.
		infraResource := map[string]interface{}{
			"kind":       "InfrastructureMachine",
			"apiVersion": "infrastructure.cluster.x-k8s.io/v1alpha4",
			"metadata":   map[string]interface{}{},
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
		infraTmpl.SetGenerateName("mhc-ms-template-")
		infraTmpl.SetNamespace(mhc.Namespace)

		g.Expect(env.Create(ctx, infraTmpl)).To(Succeed())

		machineSet := &clusterv1.MachineSet{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "mhc-ms-",
				Namespace:    mhc.Namespace,
			},
			Spec: clusterv1.MachineSetSpec{
				ClusterName: cluster.Name,
				Replicas:    pointer.Int32Ptr(1),
				Selector:    mhc.Spec.Selector,
				Template: clusterv1.MachineTemplateSpec{
					ObjectMeta: clusterv1.ObjectMeta{
						Labels: mhc.Spec.Selector.MatchLabels,
					},
					Spec: clusterv1.MachineSpec{
						ClusterName: cluster.Name,
						Bootstrap: clusterv1.Bootstrap{
							DataSecretName: pointer.StringPtr("test-data-secret-name"),
						},
						InfrastructureRef: corev1.ObjectReference{
							APIVersion: "infrastructure.cluster.x-k8s.io/v1alpha4",
							Kind:       "InfrastructureMachineTemplate",
							Name:       infraTmpl.GetName(),
						},
					},
				},
			},
		}
		machineSet.Default()
		g.Expect(env.Create(ctx, machineSet)).To(Succeed())

		// Ensure machines have been created.
		g.Eventually(func() int {
			machines := &clusterv1.MachineList{}
			err := env.List(ctx, machines, client.MatchingLabels{
				"selector": mhc.Spec.Selector.MatchLabels["selector"],
			})
			if err != nil {
				return -1
			}
			return len(machines.Items)
		}, timeout, 100*time.Millisecond).Should(Equal(1))

		// Create the MachineHealthCheck instance.
		mhc.Spec.NodeStartupTimeout = &metav1.Duration{Duration: time.Second}

		g.Expect(env.Create(ctx, mhc)).To(Succeed())
		// defer cleanup for all the objects that have been created
		defer func(do ...client.Object) {
			g.Expect(env.Cleanup(ctx, do...)).To(Succeed())
		}(cluster, mhc, infraTmpl, machineSet)

		// Pause the MachineSet reconciler to delay the deletion of the
		// Machine, because the MachineSet controller deletes the Machine when
		// it is marked unhealthy by MHC.
		machineSetPatch := client.MergeFrom(machineSet.DeepCopy())
		machineSet.Annotations = map[string]string{
			clusterv1.PausedAnnotation: "",
		}
		g.Expect(env.Patch(ctx, machineSet, machineSetPatch)).To(Succeed())

		// Calculate how many Machines have health check succeeded = false.
		g.Eventually(func() (unhealthy int) {
			machines := &clusterv1.MachineList{}
			err := env.List(ctx, machines, client.MatchingLabels{
				"selector": mhc.Spec.Selector.MatchLabels["selector"],
			})
			if err != nil {
				return -1
			}

			for i := range machines.Items {
				if conditions.IsFalse(&machines.Items[i], clusterv1.MachineHealthCheckSuccededCondition) {
					unhealthy++
				}
			}
			return
		}, timeout, 100*time.Millisecond).Should(Equal(1))

		// Calculate how many Machines should be remediated.
		var unhealthyMachine *clusterv1.Machine
		g.Eventually(func() (remediated int) {
			machines := &clusterv1.MachineList{}
			err := env.List(ctx, machines, client.MatchingLabels{
				"selector": mhc.Spec.Selector.MatchLabels["selector"],
			})
			if err != nil {
				return -1
			}

			for i := range machines.Items {
				if conditions.Get(&machines.Items[i], clusterv1.MachineOwnerRemediatedCondition) != nil {
					unhealthyMachine = machines.Items[i].DeepCopy()
					remediated++
				}
			}
			return
		}, timeout, 100*time.Millisecond).Should(Equal(1))

		// Unpause the MachineSet reconciler.
		machineSetPatch = client.MergeFrom(machineSet.DeepCopy())
		delete(machineSet.Annotations, clusterv1.PausedAnnotation)
		g.Expect(env.Patch(ctx, machineSet, machineSetPatch)).To(Succeed())

		// Make sure the Machine gets deleted.
		g.Eventually(func() bool {
			machine := unhealthyMachine.DeepCopy()
			err := env.Get(ctx, util.ObjectKey(unhealthyMachine), machine)
			return apierrors.IsNotFound(err) || !machine.DeletionTimestamp.IsZero()
		}, timeout, 100*time.Millisecond)
	})

	t.Run("when a machine is paused", func(t *testing.T) {
		// FIXME: Resolve flaky/failing test
		t.Skip("skipping until made stable")
		g := NewWithT(t)
		cluster := createCluster(g, ns.Name)

		mhc := newMachineHealthCheck(cluster.Namespace, cluster.Name)

		g.Expect(env.Create(ctx, mhc)).To(Succeed())
		defer func(do ...client.Object) {
			g.Expect(env.Cleanup(ctx, do...)).To(Succeed())
		}(cluster, mhc)

		// Healthy nodes and machines.
		nodes, machines, cleanup := createMachinesWithNodes(g, cluster,
			count(1),
			firstMachineAsControlPlane(),
			createNodeRefForMachine(true),
			nodeStatus(corev1.ConditionTrue),
			machineLabels(mhc.Spec.Selector.MatchLabels),
		)
		defer cleanup()
		targetMachines := make([]string, len(machines))
		for i, m := range machines {
			targetMachines[i] = m.Name
		}
		sort.Strings(targetMachines)

		// Make sure the status matches.
		g.Eventually(func() *clusterv1.MachineHealthCheckStatus {
			err := env.Get(ctx, util.ObjectKey(mhc), mhc)
			if err != nil {
				return nil
			}
			return &mhc.Status
		}).Should(MatchMachineHealthCheckStatus(&clusterv1.MachineHealthCheckStatus{
			ExpectedMachines:   1,
			CurrentHealthy:     1,
			ObservedGeneration: 1,
			Targets:            targetMachines,
			Conditions: clusterv1.Conditions{
				{
					Type:   clusterv1.RemediationAllowedCondition,
					Status: corev1.ConditionTrue,
				},
			},
		}))

		// Pause the machine
		machinePatch := client.MergeFrom(machines[0].DeepCopy())
		machines[0].Annotations = map[string]string{
			clusterv1.PausedAnnotation: "",
		}
		g.Expect(env.Patch(ctx, machines[0], machinePatch)).To(Succeed())

		// Transition the node to unhealthy.
		node := nodes[0]
		nodePatch := client.MergeFrom(node.DeepCopy())
		node.Status.Conditions = []corev1.NodeCondition{
			{
				Type:               corev1.NodeReady,
				Status:             corev1.ConditionUnknown,
				LastTransitionTime: metav1.NewTime(time.Now().Add(-10 * time.Minute)),
			},
		}
		g.Expect(env.Status().Patch(ctx, node, nodePatch)).To(Succeed())

		// Make sure the status matches.
		g.Eventually(func() *clusterv1.MachineHealthCheckStatus {
			err := env.Get(ctx, util.ObjectKey(mhc), mhc)
			if err != nil {
				return nil
			}
			return &mhc.Status
		}).Should(MatchMachineHealthCheckStatus(&clusterv1.MachineHealthCheckStatus{
			ExpectedMachines:    1,
			CurrentHealthy:      0,
			RemediationsAllowed: 0,
			ObservedGeneration:  1,
			Targets:             targetMachines,
			Conditions: clusterv1.Conditions{
				{
					Type:   clusterv1.RemediationAllowedCondition,
					Status: corev1.ConditionTrue,
				},
			},
		}))

		// Calculate how many Machines have health check succeeded = false.
		g.Eventually(func() (unhealthy int) {
			machines := &clusterv1.MachineList{}
			err := env.List(ctx, machines, client.MatchingLabels{
				"selector": mhc.Spec.Selector.MatchLabels["selector"],
			})
			if err != nil {
				return -1
			}

			for i := range machines.Items {
				if conditions.IsFalse(&machines.Items[i], clusterv1.MachineHealthCheckSuccededCondition) {
					unhealthy++
				}
			}
			return
		}).Should(Equal(1))

		// Calculate how many Machines have been remediated.
		g.Eventually(func() (remediated int) {
			machines := &clusterv1.MachineList{}
			err := env.List(ctx, machines, client.MatchingLabels{
				"selector": mhc.Spec.Selector.MatchLabels["selector"],
			})
			if err != nil {
				return -1
			}

			for i := range machines.Items {
				if conditions.Get(&machines.Items[i], clusterv1.MachineOwnerRemediatedCondition) != nil {
					remediated++
				}
			}
			return
		}).Should(Equal(0))
	})

	t.Run("When remediationTemplate is set and node transitions to unhealthy, new Remediation Request should be created", func(t *testing.T) {
		g := NewWithT(t)
		cluster := createCluster(g, ns.Name)

		// Create remediation template resource.
		infraRemediationResource := map[string]interface{}{
			"kind":       "InfrastructureRemediation",
			"apiVersion": "infrastructure.cluster.x-k8s.io/v1alpha3",
			"metadata":   map[string]interface{}{},
			"spec": map[string]interface{}{
				"size": "3xlarge",
			},
		}
		infraRemediationTmpl := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"spec": map[string]interface{}{
					"template": infraRemediationResource,
				},
			},
		}
		infraRemediationTmpl.SetKind("InfrastructureRemediationTemplate")
		infraRemediationTmpl.SetAPIVersion("infrastructure.cluster.x-k8s.io/v1alpha3")
		infraRemediationTmpl.SetGenerateName("remediation-template-name-")
		infraRemediationTmpl.SetNamespace(cluster.Namespace)
		g.Expect(env.Create(ctx, infraRemediationTmpl)).To(Succeed())

		remediationTemplate := &corev1.ObjectReference{
			APIVersion: "infrastructure.cluster.x-k8s.io/v1alpha3",
			Kind:       "InfrastructureRemediationTemplate",
			Name:       infraRemediationTmpl.GetName(),
		}

		mhc := newMachineHealthCheck(cluster.Namespace, cluster.Name)
		mhc.Spec.RemediationTemplate = remediationTemplate
		g.Expect(env.Create(ctx, mhc)).To(Succeed())
		defer func(do ...client.Object) {
			g.Expect(env.Cleanup(ctx, do...)).To(Succeed())
		}(cluster, mhc, infraRemediationTmpl)

		// Healthy nodes and machines.
		nodes, machines, cleanup := createMachinesWithNodes(g, cluster,
			count(1),
			firstMachineAsControlPlane(),
			createNodeRefForMachine(true),
			nodeStatus(corev1.ConditionTrue),
			machineLabels(mhc.Spec.Selector.MatchLabels),
		)
		defer cleanup()
		targetMachines := make([]string, len(machines))
		for i, m := range machines {
			targetMachines[i] = m.Name
		}
		sort.Strings(targetMachines)

		// Make sure the status matches.
		g.Eventually(func() *clusterv1.MachineHealthCheckStatus {
			err := env.Get(ctx, util.ObjectKey(mhc), mhc)
			if err != nil {
				return nil
			}
			return &mhc.Status
		}).Should(MatchMachineHealthCheckStatus(&clusterv1.MachineHealthCheckStatus{
			ExpectedMachines:    1,
			CurrentHealthy:      1,
			RemediationsAllowed: 1,
			ObservedGeneration:  1,
			Targets:             targetMachines,
			Conditions: clusterv1.Conditions{
				{
					Type:   clusterv1.RemediationAllowedCondition,
					Status: corev1.ConditionTrue,
				},
			},
		}))

		// Transition the node to unhealthy.
		node := nodes[0]
		nodePatch := client.MergeFrom(node.DeepCopy())
		node.Status.Conditions = []corev1.NodeCondition{
			{
				Type:               corev1.NodeReady,
				Status:             corev1.ConditionUnknown,
				LastTransitionTime: metav1.NewTime(time.Now().Add(-10 * time.Minute)),
			},
		}
		g.Expect(env.Status().Patch(ctx, node, nodePatch)).To(Succeed())

		// Make sure the status matches.
		g.Eventually(func() *clusterv1.MachineHealthCheckStatus {
			err := env.Get(ctx, util.ObjectKey(mhc), mhc)
			if err != nil {
				return nil
			}
			return &mhc.Status
		}).Should(MatchMachineHealthCheckStatus(&clusterv1.MachineHealthCheckStatus{
			ExpectedMachines:    1,
			CurrentHealthy:      0,
			RemediationsAllowed: 0,
			ObservedGeneration:  1,
			Targets:             targetMachines,
			Conditions: clusterv1.Conditions{
				{
					Type:   clusterv1.RemediationAllowedCondition,
					Status: corev1.ConditionTrue,
				},
			},
		}))

		// Calculate how many Machines have health check succeeded = false.
		g.Eventually(func() (unhealthy int) {
			machines := &clusterv1.MachineList{}
			err := env.List(ctx, machines, client.MatchingLabels{
				"selector": mhc.Spec.Selector.MatchLabels["selector"],
			})
			if err != nil {
				return -1
			}

			for i := range machines.Items {
				if conditions.IsFalse(&machines.Items[i], clusterv1.MachineHealthCheckSuccededCondition) {
					unhealthy++
				}
			}
			return
		}).Should(Equal(1))

		ref := corev1.ObjectReference{
			APIVersion: "infrastructure.cluster.x-k8s.io/v1alpha3",
			Kind:       "InfrastructureRemediation",
		}

		obj := util.ObjectReferenceToUnstructured(ref)
		// Make sure the Remeditaion Request is created.
		g.Eventually(func() *unstructured.Unstructured {
			key := client.ObjectKey{
				Namespace: machines[0].Namespace,
				Name:      machines[0].Name,
			}
			err := env.Get(ctx, key, obj)
			if err != nil {
				return nil
			}
			return obj
		}, timeout, 100*time.Millisecond).ShouldNot(BeNil())
		g.Expect(obj.GetOwnerReferences()).To(HaveLen(1))
		g.Expect(obj.GetOwnerReferences()[0].Name).To(Equal(machines[0].Name))
	})

	t.Run("When remediationTemplate is set and node transitions back to healthy, new Remediation Request should be deleted", func(t *testing.T) {
		g := NewWithT(t)
		cluster := createCluster(g, ns.Name)

		// Create remediation template resource.
		infraRemediationResource := map[string]interface{}{
			"kind":       "InfrastructureRemediation",
			"apiVersion": "infrastructure.cluster.x-k8s.io/v1alpha3",
			"metadata":   map[string]interface{}{},
			"spec": map[string]interface{}{
				"size": "3xlarge",
			},
		}
		infraRemediationTmpl := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"spec": map[string]interface{}{
					"template": infraRemediationResource,
				},
			},
		}
		infraRemediationTmpl.SetKind("InfrastructureRemediationTemplate")
		infraRemediationTmpl.SetAPIVersion("infrastructure.cluster.x-k8s.io/v1alpha3")
		infraRemediationTmpl.SetGenerateName("remediation-template-name-")
		infraRemediationTmpl.SetNamespace(cluster.Namespace)
		g.Expect(env.Create(ctx, infraRemediationTmpl)).To(Succeed())

		remediationTemplate := &corev1.ObjectReference{
			APIVersion: "infrastructure.cluster.x-k8s.io/v1alpha3",
			Kind:       "InfrastructureRemediationTemplate",
			Name:       infraRemediationTmpl.GetName(),
		}

		mhc := newMachineHealthCheck(cluster.Namespace, cluster.Name)
		mhc.Spec.RemediationTemplate = remediationTemplate
		g.Expect(env.Create(ctx, mhc)).To(Succeed())
		defer func(do ...client.Object) {
			g.Expect(env.Cleanup(ctx, do...)).To(Succeed())
		}(cluster, mhc, infraRemediationTmpl)

		// Healthy nodes and machines.
		nodes, machines, cleanup := createMachinesWithNodes(g, cluster,
			count(1),
			firstMachineAsControlPlane(),
			createNodeRefForMachine(true),
			nodeStatus(corev1.ConditionTrue),
			machineLabels(mhc.Spec.Selector.MatchLabels),
		)
		defer cleanup()
		targetMachines := make([]string, len(machines))
		for i, m := range machines {
			targetMachines[i] = m.Name
		}
		sort.Strings(targetMachines)

		// Make sure the status matches.
		g.Eventually(func() *clusterv1.MachineHealthCheckStatus {
			err := env.Get(ctx, util.ObjectKey(mhc), mhc)
			if err != nil {
				return nil
			}
			return &mhc.Status
		}).Should(MatchMachineHealthCheckStatus(&clusterv1.MachineHealthCheckStatus{
			ExpectedMachines:    1,
			CurrentHealthy:      1,
			RemediationsAllowed: 1,
			ObservedGeneration:  1,
			Targets:             targetMachines,
			Conditions: clusterv1.Conditions{
				{
					Type:   clusterv1.RemediationAllowedCondition,
					Status: corev1.ConditionTrue,
				},
			},
		}))

		// Transition the node to unhealthy.
		node := nodes[0]
		nodePatch := client.MergeFrom(node.DeepCopy())
		node.Status.Conditions = []corev1.NodeCondition{
			{
				Type:               corev1.NodeReady,
				Status:             corev1.ConditionUnknown,
				LastTransitionTime: metav1.NewTime(time.Now().Add(-10 * time.Minute)),
			},
		}
		g.Expect(env.Status().Patch(ctx, node, nodePatch)).To(Succeed())

		// Make sure the status matches.
		g.Eventually(func() *clusterv1.MachineHealthCheckStatus {
			err := env.Get(ctx, util.ObjectKey(mhc), mhc)
			if err != nil {
				return nil
			}
			return &mhc.Status
		}).Should(MatchMachineHealthCheckStatus(&clusterv1.MachineHealthCheckStatus{
			ExpectedMachines:    1,
			CurrentHealthy:      0,
			RemediationsAllowed: 0,
			ObservedGeneration:  1,
			Targets:             targetMachines,
			Conditions: clusterv1.Conditions{
				{
					Type:   clusterv1.RemediationAllowedCondition,
					Status: corev1.ConditionTrue,
				},
			},
		}))

		// Calculate how many Machines have health check succeeded = false.
		g.Eventually(func() (unhealthy int) {
			machines := &clusterv1.MachineList{}
			err := env.List(ctx, machines, client.MatchingLabels{
				"selector": mhc.Spec.Selector.MatchLabels["selector"],
			})
			if err != nil {
				return -1
			}

			for i := range machines.Items {
				if conditions.IsFalse(&machines.Items[i], clusterv1.MachineHealthCheckSuccededCondition) {
					unhealthy++
				}
			}
			return
		}).Should(Equal(1))

		// Transition the node back to healthy.
		node = nodes[0]
		nodePatch = client.MergeFrom(node.DeepCopy())
		node.Status.Conditions = []corev1.NodeCondition{
			{
				Type:               corev1.NodeReady,
				Status:             corev1.ConditionTrue,
				LastTransitionTime: metav1.NewTime(time.Now().Add(-10 * time.Minute)),
			},
		}
		g.Expect(env.Status().Patch(ctx, node, nodePatch)).To(Succeed())

		// Make sure the status matches.
		g.Eventually(func() *clusterv1.MachineHealthCheckStatus {
			err := env.Get(ctx, util.ObjectKey(mhc), mhc)
			if err != nil {
				return nil
			}
			return &mhc.Status
		}).Should(MatchMachineHealthCheckStatus(&clusterv1.MachineHealthCheckStatus{
			ExpectedMachines:    1,
			CurrentHealthy:      1,
			RemediationsAllowed: 1,
			ObservedGeneration:  1,
			Targets:             targetMachines,
			Conditions: clusterv1.Conditions{
				{
					Type:   clusterv1.RemediationAllowedCondition,
					Status: corev1.ConditionTrue,
				},
			},
		}))

		// Calculate how many Machines have health check succeeded = false.
		g.Eventually(func() (unhealthy int) {
			machines := &clusterv1.MachineList{}
			err := env.List(ctx, machines, client.MatchingLabels{
				"selector": mhc.Spec.Selector.MatchLabels["selector"],
			})
			if err != nil {
				return -1
			}

			for i := range machines.Items {
				if conditions.IsFalse(&machines.Items[i], clusterv1.MachineHealthCheckSuccededCondition) {
					unhealthy++
				}
			}
			return
		}).Should(Equal(0))

		ref := corev1.ObjectReference{
			APIVersion: "infrastructure.cluster.x-k8s.io/v1alpha3",
			Kind:       "InfrastructureRemediation",
		}

		obj := util.ObjectReferenceToUnstructured(ref)
		// Make sure the Remediation Request is deleted.
		g.Eventually(func() *unstructured.Unstructured {
			key := client.ObjectKey{
				Namespace: machines[0].Namespace,
				Name:      machines[0].Name,
			}
			err := env.Get(ctx, key, obj)
			if err != nil {
				return nil
			}
			return obj
		}, timeout, 100*time.Millisecond).Should(BeNil())
	})
}

func TestClusterToMachineHealthCheck(t *testing.T) {
	fakeClient := fake.NewClientBuilder().Build()

	r := &MachineHealthCheckReconciler{
		Client: fakeClient,
	}

	namespace := metav1.NamespaceDefault
	clusterName := testClusterName
	labels := make(map[string]string)

	mhc1 := newMachineHealthCheckWithLabels("mhc1", namespace, clusterName, labels)
	mhc1Req := reconcile.Request{NamespacedName: types.NamespacedName{Namespace: mhc1.Namespace, Name: mhc1.Name}}
	mhc2 := newMachineHealthCheckWithLabels("mhc2", namespace, clusterName, labels)
	mhc2Req := reconcile.Request{NamespacedName: types.NamespacedName{Namespace: mhc2.Namespace, Name: mhc2.Name}}
	mhc3 := newMachineHealthCheckWithLabels("mhc3", namespace, "othercluster", labels)
	mhc4 := newMachineHealthCheckWithLabels("mhc4", "othernamespace", clusterName, labels)
	cluster1 := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
		},
	}

	testCases := []struct {
		name     string
		toCreate []clusterv1.MachineHealthCheck
		object   client.Object
		expected []reconcile.Request
	}{
		{
			name:     "when a MachineHealthCheck exists for the Cluster in the same namespace",
			toCreate: []clusterv1.MachineHealthCheck{*mhc1},
			object:   cluster1,
			expected: []reconcile.Request{mhc1Req},
		},
		{
			name:     "when 2 MachineHealthChecks exists for the Cluster in the same namespace",
			toCreate: []clusterv1.MachineHealthCheck{*mhc1, *mhc2},
			object:   cluster1,
			expected: []reconcile.Request{mhc1Req, mhc2Req},
		},
		{
			name:     "when a MachineHealthCheck exists for another Cluster in the same namespace",
			toCreate: []clusterv1.MachineHealthCheck{*mhc3},
			object:   cluster1,
			expected: []reconcile.Request{},
		},
		{
			name:     "when a MachineHealthCheck exists for another Cluster in another namespace",
			toCreate: []clusterv1.MachineHealthCheck{*mhc4},
			object:   cluster1,
			expected: []reconcile.Request{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gs := NewWithT(t)

			for _, obj := range tc.toCreate {
				o := obj
				gs.Expect(r.Client.Create(ctx, &o)).To(Succeed())
				defer func() {
					gs.Expect(r.Client.Delete(ctx, &o)).To(Succeed())
				}()
				// Check the cache is populated
				getObj := func() error {
					return r.Client.Get(ctx, util.ObjectKey(&o), &clusterv1.MachineHealthCheck{})
				}
				gs.Eventually(getObj).Should(Succeed())
			}

			got := r.clusterToMachineHealthCheck(tc.object)
			gs.Expect(got).To(ConsistOf(tc.expected))
		})
	}
}

func TestMachineToMachineHealthCheck(t *testing.T) {
	fakeClient := fake.NewClientBuilder().Build()

	r := &MachineHealthCheckReconciler{
		Client: fakeClient,
	}

	namespace := metav1.NamespaceDefault
	clusterName := testClusterName
	nodeName := "node1"
	labels := map[string]string{"cluster": "foo", "nodepool": "bar"}

	mhc1 := newMachineHealthCheckWithLabels("mhc1", namespace, clusterName, labels)
	mhc1Req := reconcile.Request{NamespacedName: types.NamespacedName{Namespace: mhc1.Namespace, Name: mhc1.Name}}
	mhc2 := newMachineHealthCheckWithLabels("mhc2", namespace, clusterName, labels)
	mhc2Req := reconcile.Request{NamespacedName: types.NamespacedName{Namespace: mhc2.Namespace, Name: mhc2.Name}}
	mhc3 := newMachineHealthCheckWithLabels("mhc3", namespace, clusterName, map[string]string{"cluster": "foo", "nodepool": "other"})
	mhc4 := newMachineHealthCheckWithLabels("mhc4", "othernamespace", clusterName, labels)
	machine1 := newTestMachine("machine1", namespace, clusterName, nodeName, labels)

	testCases := []struct {
		name     string
		toCreate []clusterv1.MachineHealthCheck
		object   client.Object
		expected []reconcile.Request
	}{
		{
			name:     "when a MachineHealthCheck matches labels for the Machine in the same namespace",
			toCreate: []clusterv1.MachineHealthCheck{*mhc1},
			object:   machine1,
			expected: []reconcile.Request{mhc1Req},
		},
		{
			name:     "when 2 MachineHealthChecks match labels for the Machine in the same namespace",
			toCreate: []clusterv1.MachineHealthCheck{*mhc1, *mhc2},
			object:   machine1,
			expected: []reconcile.Request{mhc1Req, mhc2Req},
		},
		{
			name:     "when a MachineHealthCheck does not match labels for the Machine in the same namespace",
			toCreate: []clusterv1.MachineHealthCheck{*mhc3},
			object:   machine1,
			expected: []reconcile.Request{},
		},
		{
			name:     "when a MachineHealthCheck matches labels for the Machine in another namespace",
			toCreate: []clusterv1.MachineHealthCheck{*mhc4},
			object:   machine1,
			expected: []reconcile.Request{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gs := NewWithT(t)

			for _, obj := range tc.toCreate {
				o := obj
				gs.Expect(r.Client.Create(ctx, &o)).To(Succeed())
				defer func() {
					gs.Expect(r.Client.Delete(ctx, &o)).To(Succeed())
				}()
				// Check the cache is populated
				getObj := func() error {
					return r.Client.Get(ctx, util.ObjectKey(&o), &clusterv1.MachineHealthCheck{})
				}
				gs.Eventually(getObj).Should(Succeed())
			}

			got := r.machineToMachineHealthCheck(tc.object)
			gs.Expect(got).To(ConsistOf(tc.expected))
		})
	}
}

func TestNodeToMachineHealthCheck(t *testing.T) {
	fakeClient := fake.NewClientBuilder().Build()

	r := &MachineHealthCheckReconciler{
		Client: fakeClient,
	}

	namespace := metav1.NamespaceDefault
	clusterName := testClusterName
	nodeName := "node1"
	labels := map[string]string{"cluster": "foo", "nodepool": "bar"}

	mhc1 := newMachineHealthCheckWithLabels("mhc1", namespace, clusterName, labels)
	mhc1Req := reconcile.Request{NamespacedName: types.NamespacedName{Namespace: mhc1.Namespace, Name: mhc1.Name}}
	mhc2 := newMachineHealthCheckWithLabels("mhc2", namespace, clusterName, labels)
	mhc2Req := reconcile.Request{NamespacedName: types.NamespacedName{Namespace: mhc2.Namespace, Name: mhc2.Name}}
	mhc3 := newMachineHealthCheckWithLabels("mhc3", namespace, "othercluster", labels)
	mhc4 := newMachineHealthCheckWithLabels("mhc4", "othernamespace", clusterName, labels)

	machine1 := newTestMachine("machine1", namespace, clusterName, nodeName, labels)
	machine2 := newTestMachine("machine2", namespace, clusterName, nodeName, labels)

	node1 := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
		},
	}

	testCases := []struct {
		name        string
		mhcToCreate []clusterv1.MachineHealthCheck
		mToCreate   []clusterv1.Machine
		object      client.Object
		expected    []reconcile.Request
	}{
		{
			name:        "when no Machine exists for the Node",
			mhcToCreate: []clusterv1.MachineHealthCheck{*mhc1},
			mToCreate:   []clusterv1.Machine{},
			object:      node1,
			expected:    []reconcile.Request{},
		},
		{
			name:        "when two Machines exist for the Node",
			mhcToCreate: []clusterv1.MachineHealthCheck{*mhc1},
			mToCreate:   []clusterv1.Machine{*machine1, *machine2},
			object:      node1,
			expected:    []reconcile.Request{},
		},
		{
			name:        "when no MachineHealthCheck exists for the Node in the Machine's namespace",
			mhcToCreate: []clusterv1.MachineHealthCheck{*mhc4},
			mToCreate:   []clusterv1.Machine{*machine1},
			object:      node1,
			expected:    []reconcile.Request{},
		},
		{
			name:        "when a MachineHealthCheck exists for the Node in the Machine's namespace",
			mhcToCreate: []clusterv1.MachineHealthCheck{*mhc1},
			mToCreate:   []clusterv1.Machine{*machine1},
			object:      node1,
			expected:    []reconcile.Request{mhc1Req},
		},
		{
			name:        "when two MachineHealthChecks exist for the Node in the Machine's namespace",
			mhcToCreate: []clusterv1.MachineHealthCheck{*mhc1, *mhc2},
			mToCreate:   []clusterv1.Machine{*machine1},
			object:      node1,
			expected:    []reconcile.Request{mhc1Req, mhc2Req},
		},
		{
			name:        "when a MachineHealthCheck exists for the Node, but not in the Machine's cluster",
			mhcToCreate: []clusterv1.MachineHealthCheck{*mhc3},
			mToCreate:   []clusterv1.Machine{*machine1},
			object:      node1,
			expected:    []reconcile.Request{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gs := NewWithT(t)

			for _, obj := range tc.mhcToCreate {
				o := obj
				gs.Expect(r.Client.Create(ctx, &o)).To(Succeed())
				defer func() {
					gs.Expect(r.Client.Delete(ctx, &o)).To(Succeed())
				}()
				// Check the cache is populated
				key := util.ObjectKey(&o)
				getObj := func() error {
					return r.Client.Get(ctx, key, &clusterv1.MachineHealthCheck{})
				}
				gs.Eventually(getObj).Should(Succeed())
			}
			for _, obj := range tc.mToCreate {
				o := obj
				gs.Expect(r.Client.Create(ctx, &o)).To(Succeed())
				defer func() {
					gs.Expect(r.Client.Delete(ctx, &o)).To(Succeed())
				}()
				// Ensure the status is set (required for matching node to machine)
				o.Status = obj.Status
				gs.Expect(r.Client.Status().Update(ctx, &o)).To(Succeed())

				// Check the cache is up to date with the status update
				key := util.ObjectKey(&o)
				checkStatus := func() clusterv1.MachineStatus {
					m := &clusterv1.Machine{}
					err := r.Client.Get(ctx, key, m)
					if err != nil {
						return clusterv1.MachineStatus{}
					}
					return m.Status
				}
				gs.Eventually(checkStatus).Should(Equal(o.Status))
			}

			got := r.nodeToMachineHealthCheck(tc.object)
			gs.Expect(got).To(ConsistOf(tc.expected))
		})
	}
}

func TestIsAllowedRemediation(t *testing.T) {
	testCases := []struct {
		name               string
		maxUnhealthy       *intstr.IntOrString
		expectedMachines   int32
		currentHealthy     int32
		allowed            bool
		observedGeneration int64
	}{
		{
			name:             "when maxUnhealthy is not set",
			maxUnhealthy:     nil,
			expectedMachines: int32(3),
			currentHealthy:   int32(0),
			allowed:          false,
		},
		{
			name:             "when maxUnhealthy is not an int or percentage",
			maxUnhealthy:     &intstr.IntOrString{Type: intstr.String, StrVal: "abcdef"},
			expectedMachines: int32(5),
			currentHealthy:   int32(2),
			allowed:          false,
		},
		{
			name:             "when maxUnhealthy is an int less than current unhealthy",
			maxUnhealthy:     &intstr.IntOrString{Type: intstr.Int, IntVal: int32(1)},
			expectedMachines: int32(3),
			currentHealthy:   int32(1),
			allowed:          false,
		},
		{
			name:             "when maxUnhealthy is an int equal to current unhealthy",
			maxUnhealthy:     &intstr.IntOrString{Type: intstr.Int, IntVal: int32(2)},
			expectedMachines: int32(3),
			currentHealthy:   int32(1),
			allowed:          true,
		},
		{
			name:             "when maxUnhealthy is an int greater than current unhealthy",
			maxUnhealthy:     &intstr.IntOrString{Type: intstr.Int, IntVal: int32(3)},
			expectedMachines: int32(3),
			currentHealthy:   int32(1),
			allowed:          true,
		},
		{
			name:             "when maxUnhealthy is a percentage less than current unhealthy",
			maxUnhealthy:     &intstr.IntOrString{Type: intstr.String, StrVal: "50%"},
			expectedMachines: int32(5),
			currentHealthy:   int32(2),
			allowed:          false,
		},
		{
			name:             "when maxUnhealthy is a percentage equal to current unhealthy",
			maxUnhealthy:     &intstr.IntOrString{Type: intstr.String, StrVal: "60%"},
			expectedMachines: int32(5),
			currentHealthy:   int32(2),
			allowed:          true,
		},
		{
			name:             "when maxUnhealthy is a percentage greater than current unhealthy",
			maxUnhealthy:     &intstr.IntOrString{Type: intstr.String, StrVal: "70%"},
			expectedMachines: int32(5),
			currentHealthy:   int32(2),
			allowed:          true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			mhc := &clusterv1.MachineHealthCheck{
				Spec: clusterv1.MachineHealthCheckSpec{
					MaxUnhealthy:       tc.maxUnhealthy,
					NodeStartupTimeout: &metav1.Duration{Duration: 1 * time.Millisecond},
				},
				Status: clusterv1.MachineHealthCheckStatus{
					ExpectedMachines:   tc.expectedMachines,
					CurrentHealthy:     tc.currentHealthy,
					ObservedGeneration: tc.observedGeneration,
				},
			}

			remediationAllowed, _, _ := isAllowedRemediation(mhc)
			g.Expect(remediationAllowed).To(Equal(tc.allowed))
		})
	}
}

func TestGetMaxUnhealthy(t *testing.T) {
	testCases := []struct {
		name                 string
		maxUnhealthy         *intstr.IntOrString
		expectedMaxUnhealthy int
		actualMachineCount   int32
		expectedErr          error
	}{
		{
			name:                 "when maxUnhealthy is nil",
			maxUnhealthy:         nil,
			expectedMaxUnhealthy: 0,
			actualMachineCount:   7,
			expectedErr:          errors.New("spec.maxUnhealthy must be set"),
		},
		{
			name:                 "when maxUnhealthy is not an int or percentage",
			maxUnhealthy:         &intstr.IntOrString{Type: intstr.String, StrVal: "abcdef"},
			expectedMaxUnhealthy: 0,
			actualMachineCount:   3,
			expectedErr:          errors.New("invalid value for IntOrString: invalid type: string is not a percentage"),
		},
		{
			name:                 "when maxUnhealthy is an int",
			maxUnhealthy:         &intstr.IntOrString{Type: intstr.Int, IntVal: 3},
			actualMachineCount:   2,
			expectedMaxUnhealthy: 3,
			expectedErr:          nil,
		},
		{
			name:                 "when maxUnhealthy is a 40% (of 5)",
			maxUnhealthy:         &intstr.IntOrString{Type: intstr.String, StrVal: "40%"},
			actualMachineCount:   5,
			expectedMaxUnhealthy: 2,
			expectedErr:          nil,
		},
		{
			name:                 "when maxUnhealthy is a 60% (of 7)",
			maxUnhealthy:         &intstr.IntOrString{Type: intstr.String, StrVal: "60%"},
			actualMachineCount:   7,
			expectedMaxUnhealthy: 4,
			expectedErr:          nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			mhc := &clusterv1.MachineHealthCheck{
				Spec: clusterv1.MachineHealthCheckSpec{
					MaxUnhealthy: tc.maxUnhealthy,
				},
				Status: clusterv1.MachineHealthCheckStatus{
					ExpectedMachines: tc.actualMachineCount,
				},
			}

			maxUnhealthy, err := getMaxUnhealthy(mhc)
			if tc.expectedErr != nil {
				g.Expect(err).To(MatchError(tc.expectedErr.Error()))
			} else {
				g.Expect(err).ToNot(HaveOccurred())
			}
			g.Expect(maxUnhealthy).To(Equal(tc.expectedMaxUnhealthy))
		})
	}
}

func ownerReferenceForCluster(ctx context.Context, g *WithT, c *clusterv1.Cluster) metav1.OwnerReference {
	// Fetch the cluster to populate the UID
	cc := &clusterv1.Cluster{}
	g.Expect(env.GetClient().Get(ctx, util.ObjectKey(c), cc)).To(Succeed())

	return metav1.OwnerReference{
		APIVersion: clusterv1.GroupVersion.String(),
		Kind:       "Cluster",
		Name:       cc.Name,
		UID:        cc.UID,
	}
}

// createCluster creates a Cluster and KubeconfigSecret for that cluster in said namespace.
func createCluster(g *WithT, namespaceName string) *clusterv1.Cluster {
	cluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-cluster-",
			Namespace:    namespaceName,
		},
	}

	g.Expect(env.Create(ctx, cluster)).To(Succeed())

	// Make sure the cluster is in the cache before proceeding
	g.Eventually(func() error {
		var cl clusterv1.Cluster
		return env.Get(ctx, util.ObjectKey(cluster), &cl)
	}, timeout, 100*time.Millisecond).Should(Succeed())

	// This is required for MHC to perform checks
	patchHelper, err := patch.NewHelper(cluster, env.Client)
	g.Expect(err).To(BeNil())
	conditions.MarkTrue(cluster, clusterv1.InfrastructureReadyCondition)
	g.Expect(patchHelper.Patch(ctx, cluster)).To(Succeed())

	// Wait for cluster in cache to be updated post-patch
	g.Eventually(func() bool {
		err := env.Get(ctx, util.ObjectKey(cluster), cluster)
		if err != nil {
			return false
		}

		return conditions.IsTrue(cluster, clusterv1.InfrastructureReadyCondition)
	}, timeout, 100*time.Millisecond).Should(BeTrue())

	g.Expect(env.CreateKubeconfigSecret(ctx, cluster)).To(Succeed())

	return cluster
}

// newRunningMachine creates a Machine object with a Status.Phase == Running.
func newRunningMachine(c *clusterv1.Cluster, labels map[string]string) *clusterv1.Machine {
	return &clusterv1.Machine{
		TypeMeta: metav1.TypeMeta{
			APIVersion: clusterv1.GroupVersion.String(),
			Kind:       "Machine",
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-mhc-machine-",
			Namespace:    c.Namespace,
			Labels:       labels,
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: c.Name,
			Bootstrap: clusterv1.Bootstrap{
				DataSecretName: pointer.StringPtr("data-secret-name"),
			},
		},
		Status: clusterv1.MachineStatus{
			InfrastructureReady: true,
			BootstrapReady:      true,
			Phase:               string(clusterv1.MachinePhaseRunning),
			ObservedGeneration:  1,
		},
	}
}

func newInfraMachine(machine *clusterv1.Machine) (*unstructured.Unstructured, string) {
	providerID := fmt.Sprintf("test:////%v", uuid.NewUUID())
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "infrastructure.cluster.x-k8s.io/v1alpha4",
			"kind":       "InfrastructureMachine",
			"metadata": map[string]interface{}{
				"generateName": "test-mhc-machine-infra-",
				"namespace":    machine.Namespace,
			},
			"spec": map[string]interface{}{
				"providerID": providerID,
			},
		},
	}, providerID
}

type machinesWithNodes struct {
	count                      int
	nodeStatus                 corev1.ConditionStatus
	createNodeRefForMachine    bool
	firstMachineAsControlPlane bool
	labels                     map[string]string
	failureReason              string
	failureMessage             string
}

type machineWithNodesOption func(m *machinesWithNodes)

func count(n int) machineWithNodesOption {
	return func(m *machinesWithNodes) {
		m.count = n
	}
}

func firstMachineAsControlPlane() machineWithNodesOption {
	return func(m *machinesWithNodes) {
		m.firstMachineAsControlPlane = true
	}
}

func nodeStatus(s corev1.ConditionStatus) machineWithNodesOption {
	return func(m *machinesWithNodes) {
		m.nodeStatus = s
	}
}

func createNodeRefForMachine(b bool) machineWithNodesOption {
	return func(m *machinesWithNodes) {
		m.createNodeRefForMachine = b
	}
}

func machineLabels(l map[string]string) machineWithNodesOption {
	return func(m *machinesWithNodes) {
		m.labels = l
	}
}

func machineFailureReason(s string) machineWithNodesOption {
	return func(m *machinesWithNodes) {
		m.failureReason = s
	}
}

func machineFailureMessage(s string) machineWithNodesOption {
	return func(m *machinesWithNodes) {
		m.failureMessage = s
	}
}

func createMachinesWithNodes(
	g *WithT,
	c *clusterv1.Cluster,
	opts ...machineWithNodesOption,
) ([]*corev1.Node, []*clusterv1.Machine, func()) {
	o := &machinesWithNodes{}
	for _, op := range opts {
		op(o)
	}

	var (
		nodes         []*corev1.Node
		machines      []*clusterv1.Machine
		infraMachines []*unstructured.Unstructured
	)

	for i := 0; i < o.count; i++ {
		machine := newRunningMachine(c, o.labels)
		if i == 0 && o.firstMachineAsControlPlane {
			if machine.Labels == nil {
				machine.Labels = make(map[string]string)
			}
			machine.Labels[clusterv1.MachineControlPlaneLabelName] = ""
		}
		infraMachine, providerID := newInfraMachine(machine)
		g.Expect(env.Create(ctx, infraMachine)).To(Succeed())
		infraMachines = append(infraMachines, infraMachine)
		fmt.Printf("inframachine created: %s\n", infraMachine.GetName())
		// Patch the status of the InfraMachine and mark it as ready.
		// NB. Status cannot be set during object creation so we need to patch
		// it separately.
		infraMachinePatch := client.MergeFrom(infraMachine.DeepCopy())
		g.Expect(unstructured.SetNestedField(infraMachine.Object, true, "status", "ready")).To(Succeed())
		g.Expect(env.Status().Patch(ctx, infraMachine, infraMachinePatch)).To(Succeed())

		machine.Spec.InfrastructureRef = corev1.ObjectReference{
			APIVersion: infraMachine.GetAPIVersion(),
			Kind:       infraMachine.GetKind(),
			Name:       infraMachine.GetName(),
		}
		g.Expect(env.Create(ctx, machine)).To(Succeed())
		fmt.Printf("machine created: %s\n", machine.GetName())

		// Before moving on we want to ensure that the machine has a valid
		// status. That is, LastUpdated should not be nil.
		g.Eventually(func() *metav1.Time {
			k := client.ObjectKey{
				Name:      machine.GetName(),
				Namespace: machine.GetNamespace(),
			}
			err := env.Get(ctx, k, machine)
			if err != nil {
				return nil
			}
			return machine.Status.LastUpdated
		}, timeout, 100*time.Millisecond).ShouldNot(BeNil())

		machinePatchHelper, err := patch.NewHelper(machine, env.Client)
		g.Expect(err).To(BeNil())

		if o.createNodeRefForMachine {
			// Create node
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "test-mhc-node-",
				},
				Spec: corev1.NodeSpec{
					ProviderID: providerID,
				},
			}

			g.Expect(env.Create(ctx, node)).To(Succeed())
			fmt.Printf("node created: %s\n", node.GetName())

			// Patch node status
			nodePatchHelper, err := patch.NewHelper(node, env.Client)
			g.Expect(err).To(BeNil())

			node.Status.Conditions = []corev1.NodeCondition{
				{
					Type:               corev1.NodeReady,
					Status:             o.nodeStatus,
					LastTransitionTime: metav1.NewTime(time.Now().Add(-10 * time.Minute)),
				},
			}

			g.Expect(nodePatchHelper.Patch(ctx, node)).To(Succeed())

			nodes = append(nodes, node)

			machine.Status.NodeRef = &corev1.ObjectReference{
				Name: node.Name,
			}
		}

		if o.failureReason != "" {
			failureReason := capierrors.MachineStatusError(o.failureReason)
			machine.Status.FailureReason = &failureReason
		}
		if o.failureMessage != "" {
			machine.Status.FailureMessage = pointer.StringPtr(o.failureMessage)
		}

		// Adding one second to ensure there is a difference from the
		// original time so that the patch works. That is, ensure the
		// precision isn't lost during conversions.
		lastUp := metav1.NewTime(machine.Status.LastUpdated.Add(time.Second))
		machine.Status.LastUpdated = &lastUp

		// Patch the machine to record the status changes
		g.Expect(machinePatchHelper.Patch(ctx, machine)).To(Succeed())

		machines = append(machines, machine)
	}

	cleanup := func() {
		fmt.Println("Cleaning up nodes, machines and infra machines.")
		for _, n := range nodes {
			if err := env.Delete(ctx, n); !apierrors.IsNotFound(err) {
				g.Expect(err).NotTo(HaveOccurred())
			}
		}
		for _, m := range machines {
			g.Expect(env.Delete(ctx, m)).To(Succeed())
		}
		for _, im := range infraMachines {
			if err := env.Delete(ctx, im); !apierrors.IsNotFound(err) {
				g.Expect(err).NotTo(HaveOccurred())
			}
		}
	}

	return nodes, machines, cleanup
}

func newMachineHealthCheckWithLabels(name, namespace, cluster string, labels map[string]string) *clusterv1.MachineHealthCheck {
	l := make(map[string]string, len(labels))
	for k, v := range labels {
		l[k] = v
	}
	l[clusterv1.ClusterLabelName] = cluster

	mhc := newMachineHealthCheck(namespace, cluster)
	mhc.SetName(name)
	mhc.Labels = l
	mhc.Spec.Selector.MatchLabels = l

	return mhc
}

func newMachineHealthCheck(namespace, clusterName string) *clusterv1.MachineHealthCheck {
	maxUnhealthy := intstr.FromString("100%")
	return &clusterv1.MachineHealthCheck{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-mhc-",
			Namespace:    namespace,
		},
		Spec: clusterv1.MachineHealthCheckSpec{
			ClusterName: clusterName,
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"selector": string(uuid.NewUUID()),
				},
			},
			MaxUnhealthy:       &maxUnhealthy,
			NodeStartupTimeout: &metav1.Duration{Duration: 1 * time.Millisecond},
			UnhealthyConditions: []clusterv1.UnhealthyCondition{
				{
					Type:    corev1.NodeReady,
					Status:  corev1.ConditionUnknown,
					Timeout: metav1.Duration{Duration: 5 * time.Minute},
				},
			},
		},
	}
}

func TestPatchTargets(t *testing.T) {
	g := NewWithT(t)

	namespace := metav1.NamespaceDefault
	clusterName := testClusterName
	defaultCluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
		},
	}
	labels := map[string]string{"cluster": "foo", "nodepool": "bar"}

	mhc := newMachineHealthCheckWithLabels("mhc", namespace, clusterName, labels)
	machine1 := newTestMachine("machine1", namespace, clusterName, "nodeName", labels)
	machine1.ResourceVersion = "999"
	conditions.MarkTrue(machine1, clusterv1.MachineHealthCheckSuccededCondition)
	machine2 := machine1.DeepCopy()
	machine2.Name = "machine2"

	cl := fake.NewClientBuilder().WithObjects(
		machine1,
		machine2,
		mhc,
	).Build()
	r := &MachineHealthCheckReconciler{
		Client:   cl,
		recorder: record.NewFakeRecorder(32),
		Tracker:  remote.NewTestClusterCacheTracker(log.NullLogger{}, cl, scheme.Scheme, client.ObjectKey{Name: clusterName, Namespace: namespace}, "machinehealthcheck-watchClusterNodes"),
	}

	// To make the patch fail, create patchHelper with a different client.
	fakeMachine := machine1.DeepCopy()
	fakeMachine.Name = "fake"
	patchHelper, _ := patch.NewHelper(fakeMachine, fake.NewClientBuilder().WithObjects(fakeMachine).Build())
	// healthCheckTarget with fake patchHelper, patch should fail on this target.
	target1 := healthCheckTarget{
		MHC:         mhc,
		Machine:     machine1,
		patchHelper: patchHelper,
		Node:        &corev1.Node{},
	}

	// healthCheckTarget with correct patchHelper.
	patchHelper2, _ := patch.NewHelper(machine2, cl)
	target3 := healthCheckTarget{
		MHC:         mhc,
		Machine:     machine2,
		patchHelper: patchHelper2,
		Node:        &corev1.Node{},
	}

	// Target with wrong patch helper will fail but the other one will be patched.
	g.Expect(len(r.patchUnhealthyTargets(context.TODO(), log.NullLogger{}, []healthCheckTarget{target1, target3}, defaultCluster, mhc))).To(BeNumerically(">", 0))
	g.Expect(cl.Get(ctx, client.ObjectKey{Name: machine2.Name, Namespace: machine2.Namespace}, machine2)).NotTo(HaveOccurred())
	g.Expect(conditions.Get(machine2, clusterv1.MachineOwnerRemediatedCondition).Status).To(Equal(corev1.ConditionFalse))

	// Target with wrong patch helper will fail but the other one will be patched.
	g.Expect(len(r.patchHealthyTargets(context.TODO(), log.NullLogger{}, []healthCheckTarget{target1, target3}, mhc))).To(BeNumerically(">", 0))
}
