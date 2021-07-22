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
	"fmt"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	utilpointer "k8s.io/utils/pointer"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	controlplanev1 "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1alpha4"
	"sigs.k8s.io/cluster-api/controlplane/kubeadm/internal"
	"sigs.k8s.io/cluster-api/util/collections"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestReconcileUnhealthyMachines(t *testing.T) {
	g := NewWithT(t)
	ctx := context.TODO()
	r := &KubeadmControlPlaneReconciler{
		Client:   env.GetClient(),
		recorder: record.NewFakeRecorder(32),
	}
	ns, err := env.CreateNamespace(ctx, "ns1")
	g.Expect(err).ToNot(HaveOccurred())
	defer func() {
		g.Expect(env.Cleanup(ctx, ns)).To(Succeed())
	}()

	t.Run("Remediation does not happen if there are no unhealthy machines", func(t *testing.T) {
		g := NewWithT(t)

		controlPlane := &internal.ControlPlane{
			KCP:      &controlplanev1.KubeadmControlPlane{},
			Cluster:  &clusterv1.Cluster{},
			Machines: collections.New(),
		}
		ret, err := r.reconcileUnhealthyMachines(context.TODO(), controlPlane)

		g.Expect(ret.IsZero()).To(BeTrue()) // Remediation skipped
		g.Expect(err).ToNot(HaveOccurred())
	})
	t.Run("reconcileUnhealthyMachines return early if the machine to be remediated is marked for deletion", func(t *testing.T) {
		g := NewWithT(t)

		m := getDeletingMachine(ns.Name, "m1-unhealthy-deleting-", withMachineHealthCheckFailed())
		conditions.MarkFalse(m, clusterv1.MachineHealthCheckSuccededCondition, clusterv1.MachineHasFailureReason, clusterv1.ConditionSeverityWarning, "")
		conditions.MarkFalse(m, clusterv1.MachineOwnerRemediatedCondition, clusterv1.WaitingForRemediationReason, clusterv1.ConditionSeverityWarning, "")
		controlPlane := &internal.ControlPlane{
			KCP:      &controlplanev1.KubeadmControlPlane{},
			Cluster:  &clusterv1.Cluster{},
			Machines: collections.FromMachines(m),
		}
		ret, err := r.reconcileUnhealthyMachines(context.TODO(), controlPlane)

		g.Expect(ret.IsZero()).To(BeTrue()) // Remediation skipped
		g.Expect(err).ToNot(HaveOccurred())
	})
	t.Run("Remediation does not happen if desired replicas <= 1", func(t *testing.T) {
		g := NewWithT(t)

		m := createMachine(ctx, g, ns.Name, "m1-unhealthy-", withMachineHealthCheckFailed())
		controlPlane := &internal.ControlPlane{
			KCP: &controlplanev1.KubeadmControlPlane{Spec: controlplanev1.KubeadmControlPlaneSpec{
				Replicas: utilpointer.Int32Ptr(1),
				RolloutStrategy: &controlplanev1.RolloutStrategy{
					RollingUpdate: &controlplanev1.RollingUpdate{
						MaxSurge: &intstr.IntOrString{
							IntVal: 1,
						},
					},
				},
			}},
			Cluster:  &clusterv1.Cluster{},
			Machines: collections.FromMachines(m),
		}
		ret, err := r.reconcileUnhealthyMachines(context.TODO(), controlPlane)

		g.Expect(ret.IsZero()).To(BeTrue()) // Remediation skipped
		g.Expect(err).ToNot(HaveOccurred())
		assertMachineCondition(ctx, g, m, clusterv1.MachineOwnerRemediatedCondition, corev1.ConditionFalse, clusterv1.WaitingForRemediationReason, clusterv1.ConditionSeverityWarning, "KCP can't remediate if current replicas are less or equal then 1")

		g.Expect(env.Cleanup(ctx, m)).To(Succeed())
	})
	t.Run("Remediation does not happen if number of machines lower than desired", func(t *testing.T) {
		g := NewWithT(t)

		m1 := createMachine(ctx, g, ns.Name, "m1-unhealthy-", withMachineHealthCheckFailed())
		m2 := createMachine(ctx, g, ns.Name, "m2-healthy-")
		controlPlane := &internal.ControlPlane{
			KCP: &controlplanev1.KubeadmControlPlane{Spec: controlplanev1.KubeadmControlPlaneSpec{
				Replicas:        utilpointer.Int32Ptr(3),
				RolloutStrategy: &controlplanev1.RolloutStrategy{},
			}},
			Cluster:  &clusterv1.Cluster{},
			Machines: collections.FromMachines(m1, m2),
		}
		ret, err := r.reconcileUnhealthyMachines(context.TODO(), controlPlane)

		g.Expect(ret.IsZero()).To(BeTrue()) // Remediation skipped
		g.Expect(err).ToNot(HaveOccurred())
		assertMachineCondition(ctx, g, m1, clusterv1.MachineOwnerRemediatedCondition, corev1.ConditionFalse, clusterv1.WaitingForRemediationReason, clusterv1.ConditionSeverityWarning, "KCP waiting for having at least 3 control plane machines before triggering remediation")

		g.Expect(env.Cleanup(ctx, m1, m2)).To(Succeed())
	})
	t.Run("Remediation does not happen if there is a deleting machine", func(t *testing.T) {
		g := NewWithT(t)

		m1 := createMachine(ctx, g, ns.Name, "m1-unhealthy-", withMachineHealthCheckFailed())
		m2 := createMachine(ctx, g, ns.Name, "m2-healthy-")
		m3 := getDeletingMachine(ns.Name, "m3-deleting") // NB. This machine is not created, it gets only added to control plane
		controlPlane := &internal.ControlPlane{
			KCP: &controlplanev1.KubeadmControlPlane{Spec: controlplanev1.KubeadmControlPlaneSpec{
				Replicas: utilpointer.Int32Ptr(3),
			}},
			Cluster:  &clusterv1.Cluster{},
			Machines: collections.FromMachines(m1, m2, m3),
		}
		ret, err := r.reconcileUnhealthyMachines(context.TODO(), controlPlane)

		g.Expect(ret.IsZero()).To(BeTrue()) // Remediation skipped
		g.Expect(err).ToNot(HaveOccurred())
		assertMachineCondition(ctx, g, m1, clusterv1.MachineOwnerRemediatedCondition, corev1.ConditionFalse, clusterv1.WaitingForRemediationReason, clusterv1.ConditionSeverityWarning, "KCP waiting for control plane machine deletion to complete before triggering remediation")

		g.Expect(env.Cleanup(ctx, m1, m2)).To(Succeed())
	})
	t.Run("Remediation does not happen if there is at least one additional unhealthy etcd member on a 3 machine CP", func(t *testing.T) {
		g := NewWithT(t)

		m1 := createMachine(ctx, g, ns.Name, "m1-mhc-unhealthy-", withMachineHealthCheckFailed())
		m2 := createMachine(ctx, g, ns.Name, "m2-etcd-unhealthy-", withUnhealthyEtcdMember())
		m3 := createMachine(ctx, g, ns.Name, "m3-etcd-healthy-", withHealthyEtcdMember())

		controlPlane := &internal.ControlPlane{
			KCP: &controlplanev1.KubeadmControlPlane{Spec: controlplanev1.KubeadmControlPlaneSpec{
				Replicas: utilpointer.Int32Ptr(3),
			}},
			Cluster:  &clusterv1.Cluster{},
			Machines: collections.FromMachines(m1, m2, m3),
		}

		r := &KubeadmControlPlaneReconciler{
			Client:   env.GetClient(),
			recorder: record.NewFakeRecorder(32),
			managementCluster: &fakeManagementCluster{
				Workload: fakeWorkloadCluster{
					EtcdMembersResult: nodes(controlPlane.Machines),
				},
			},
		}

		ret, err := r.reconcileUnhealthyMachines(context.TODO(), controlPlane)

		g.Expect(ret.IsZero()).To(BeTrue()) // Remediation skipped
		g.Expect(err).ToNot(HaveOccurred())
		assertMachineCondition(ctx, g, m1, clusterv1.MachineOwnerRemediatedCondition, corev1.ConditionFalse, clusterv1.WaitingForRemediationReason, clusterv1.ConditionSeverityWarning, "KCP can't remediate this machine because this could result in etcd loosing quorum")

		g.Expect(env.Cleanup(ctx, m1, m2, m3)).To(Succeed())
	})
	t.Run("Remediation does not happen if there is at least two additional unhealthy etcd member on a 5 machine CP", func(t *testing.T) {
		g := NewWithT(t)

		m1 := createMachine(ctx, g, ns.Name, "m1-mhc-unhealthy-", withMachineHealthCheckFailed())
		m2 := createMachine(ctx, g, ns.Name, "m2-etcd-unhealthy-", withUnhealthyEtcdMember())
		m3 := createMachine(ctx, g, ns.Name, "m3-etcd-unhealthy-", withUnhealthyEtcdMember())
		m4 := createMachine(ctx, g, ns.Name, "m4-etcd-healthy-", withHealthyEtcdMember())
		m5 := createMachine(ctx, g, ns.Name, "m5-etcd-healthy-", withHealthyEtcdMember())

		controlPlane := &internal.ControlPlane{
			KCP: &controlplanev1.KubeadmControlPlane{Spec: controlplanev1.KubeadmControlPlaneSpec{
				Replicas: utilpointer.Int32Ptr(5),
			}},
			Cluster:  &clusterv1.Cluster{},
			Machines: collections.FromMachines(m1, m2, m3, m4, m5),
		}

		r := &KubeadmControlPlaneReconciler{
			Client:   env.GetClient(),
			recorder: record.NewFakeRecorder(32),
			managementCluster: &fakeManagementCluster{
				Workload: fakeWorkloadCluster{
					EtcdMembersResult: nodes(controlPlane.Machines),
				},
			},
		}

		ret, err := r.reconcileUnhealthyMachines(context.TODO(), controlPlane)

		g.Expect(ret.IsZero()).To(BeTrue()) // Remediation skipped
		g.Expect(err).ToNot(HaveOccurred())
		assertMachineCondition(ctx, g, m1, clusterv1.MachineOwnerRemediatedCondition, corev1.ConditionFalse, clusterv1.WaitingForRemediationReason, clusterv1.ConditionSeverityWarning, "KCP can't remediate this machine because this could result in etcd loosing quorum")

		g.Expect(env.Cleanup(ctx, m1, m2, m3, m4, m5)).To(Succeed())
	})
	t.Run("Remediation deletes unhealthy machine - 2 CP (during 1 CP rolling upgrade)", func(t *testing.T) {
		g := NewWithT(t)

		m1 := createMachine(ctx, g, ns.Name, "m1-unhealthy-", withMachineHealthCheckFailed())
		patchHelper, err := patch.NewHelper(m1, env.GetClient())
		g.Expect(err).ToNot(HaveOccurred())
		m1.ObjectMeta.Finalizers = []string{"wait-before-delete"}
		g.Expect(patchHelper.Patch(ctx, m1))

		m2 := createMachine(ctx, g, ns.Name, "m2-healthy-", withHealthyEtcdMember())

		controlPlane := &internal.ControlPlane{
			KCP: &controlplanev1.KubeadmControlPlane{Spec: controlplanev1.KubeadmControlPlaneSpec{
				Replicas: utilpointer.Int32Ptr(2),
				Version:  "v1.19.1",
			}},
			Cluster:  &clusterv1.Cluster{},
			Machines: collections.FromMachines(m1, m2),
		}

		r := &KubeadmControlPlaneReconciler{
			Client:   env.GetClient(),
			recorder: record.NewFakeRecorder(32),
			managementCluster: &fakeManagementCluster{
				Workload: fakeWorkloadCluster{
					EtcdMembersResult: nodes(controlPlane.Machines),
				},
			},
		}

		ret, err := r.reconcileUnhealthyMachines(context.TODO(), controlPlane)

		g.Expect(ret.IsZero()).To(BeFalse()) // Remediation completed, requeue
		g.Expect(err).ToNot(HaveOccurred())

		assertMachineCondition(ctx, g, m1, clusterv1.MachineOwnerRemediatedCondition, corev1.ConditionFalse, clusterv1.RemediationInProgressReason, clusterv1.ConditionSeverityWarning, "")

		err = env.Get(ctx, client.ObjectKey{Namespace: m1.Namespace, Name: m1.Name}, m1)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(m1.ObjectMeta.DeletionTimestamp.IsZero()).To(BeFalse())

		patchHelper, err = patch.NewHelper(m1, env.GetClient())
		g.Expect(err).ToNot(HaveOccurred())
		m1.ObjectMeta.Finalizers = nil
		g.Expect(patchHelper.Patch(ctx, m1))

		g.Expect(env.Cleanup(ctx, m1, m2)).To(Succeed())
	})
	t.Run("Remediation deletes unhealthy machine - 3 CP", func(t *testing.T) {
		g := NewWithT(t)

		m1 := createMachine(ctx, g, ns.Name, "m1-unhealthy-", withMachineHealthCheckFailed())
		patchHelper, err := patch.NewHelper(m1, env.GetClient())
		g.Expect(err).ToNot(HaveOccurred())
		m1.ObjectMeta.Finalizers = []string{"wait-before-delete"}
		g.Expect(patchHelper.Patch(ctx, m1))

		m2 := createMachine(ctx, g, ns.Name, "m2-healthy-", withHealthyEtcdMember())
		m3 := createMachine(ctx, g, ns.Name, "m3-healthy-", withHealthyEtcdMember())

		controlPlane := &internal.ControlPlane{
			KCP: &controlplanev1.KubeadmControlPlane{Spec: controlplanev1.KubeadmControlPlaneSpec{
				Replicas: utilpointer.Int32Ptr(3),
				Version:  "v1.19.1",
			}},
			Cluster:  &clusterv1.Cluster{},
			Machines: collections.FromMachines(m1, m2, m3),
		}

		r := &KubeadmControlPlaneReconciler{
			Client:   env.GetClient(),
			recorder: record.NewFakeRecorder(32),
			managementCluster: &fakeManagementCluster{
				Workload: fakeWorkloadCluster{
					EtcdMembersResult: nodes(controlPlane.Machines),
				},
			},
		}

		ret, err := r.reconcileUnhealthyMachines(context.TODO(), controlPlane)

		g.Expect(ret.IsZero()).To(BeFalse()) // Remediation completed, requeue
		g.Expect(err).ToNot(HaveOccurred())

		assertMachineCondition(ctx, g, m1, clusterv1.MachineOwnerRemediatedCondition, corev1.ConditionFalse, clusterv1.RemediationInProgressReason, clusterv1.ConditionSeverityWarning, "")

		err = env.Get(ctx, client.ObjectKey{Namespace: m1.Namespace, Name: m1.Name}, m1)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(m1.ObjectMeta.DeletionTimestamp.IsZero()).To(BeFalse())

		patchHelper, err = patch.NewHelper(m1, env.GetClient())
		g.Expect(err).ToNot(HaveOccurred())
		m1.ObjectMeta.Finalizers = nil
		g.Expect(patchHelper.Patch(ctx, m1))

		g.Expect(env.Cleanup(ctx, m1, m2, m3)).To(Succeed())
	})
	t.Run("Remediation deletes unhealthy machine - 4 CP (during 3 CP rolling upgrade)", func(t *testing.T) {
		g := NewWithT(t)

		m1 := createMachine(ctx, g, ns.Name, "m1-unhealthy-", withMachineHealthCheckFailed())
		patchHelper, err := patch.NewHelper(m1, env.GetClient())
		g.Expect(err).ToNot(HaveOccurred())
		m1.ObjectMeta.Finalizers = []string{"wait-before-delete"}
		g.Expect(patchHelper.Patch(ctx, m1))

		m2 := createMachine(ctx, g, ns.Name, "m2-healthy-", withHealthyEtcdMember())
		m3 := createMachine(ctx, g, ns.Name, "m3-healthy-", withHealthyEtcdMember())
		m4 := createMachine(ctx, g, ns.Name, "m4-healthy-", withHealthyEtcdMember())

		controlPlane := &internal.ControlPlane{
			KCP: &controlplanev1.KubeadmControlPlane{Spec: controlplanev1.KubeadmControlPlaneSpec{
				Replicas: utilpointer.Int32Ptr(4),
				Version:  "v1.19.1",
			}},
			Cluster:  &clusterv1.Cluster{},
			Machines: collections.FromMachines(m1, m2, m3, m4),
		}

		r := &KubeadmControlPlaneReconciler{
			Client:   env.GetClient(),
			recorder: record.NewFakeRecorder(32),
			managementCluster: &fakeManagementCluster{
				Workload: fakeWorkloadCluster{
					EtcdMembersResult: nodes(controlPlane.Machines),
				},
			},
		}

		ret, err := r.reconcileUnhealthyMachines(context.TODO(), controlPlane)

		g.Expect(ret.IsZero()).To(BeFalse()) // Remediation completed, requeue
		g.Expect(err).ToNot(HaveOccurred())

		assertMachineCondition(ctx, g, m1, clusterv1.MachineOwnerRemediatedCondition, corev1.ConditionFalse, clusterv1.RemediationInProgressReason, clusterv1.ConditionSeverityWarning, "")

		err = env.Get(ctx, client.ObjectKey{Namespace: m1.Namespace, Name: m1.Name}, m1)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(m1.ObjectMeta.DeletionTimestamp.IsZero()).To(BeFalse())

		patchHelper, err = patch.NewHelper(m1, env.GetClient())
		g.Expect(err).ToNot(HaveOccurred())
		m1.ObjectMeta.Finalizers = nil
		g.Expect(patchHelper.Patch(ctx, m1))

		g.Expect(env.Cleanup(ctx, m1, m2, m3, m4)).To(Succeed())
	})
}

func TestCanSafelyRemoveEtcdMember(t *testing.T) {
	g := NewWithT(t)
	ctx := context.TODO()

	ns, err := env.CreateNamespace(ctx, "ns1")
	g.Expect(err).ToNot(HaveOccurred())
	defer func() {
		g.Expect(env.Cleanup(ctx, ns)).To(Succeed())
	}()

	t.Run("Can't safely remediate 1 machine CP", func(t *testing.T) {
		g := NewWithT(t)

		m1 := createMachine(ctx, g, ns.Name, "m1-mhc-unhealthy-", withMachineHealthCheckFailed())

		controlPlane := &internal.ControlPlane{
			KCP: &controlplanev1.KubeadmControlPlane{Spec: controlplanev1.KubeadmControlPlaneSpec{
				Replicas: utilpointer.Int32Ptr(1),
			}},
			Cluster:  &clusterv1.Cluster{},
			Machines: collections.FromMachines(m1),
		}

		r := &KubeadmControlPlaneReconciler{
			Client:   env.GetClient(),
			recorder: record.NewFakeRecorder(32),
			managementCluster: &fakeManagementCluster{
				Workload: fakeWorkloadCluster{
					EtcdMembersResult: nodes(controlPlane.Machines),
				},
			},
		}

		ret, err := r.canSafelyRemoveEtcdMember(context.TODO(), controlPlane, m1)
		g.Expect(ret).To(BeFalse())
		g.Expect(err).ToNot(HaveOccurred())

		g.Expect(env.Cleanup(ctx, m1)).To(Succeed())
	})
	t.Run("Can safely remediate 2 machine CP without additional etcd member failures", func(t *testing.T) {
		g := NewWithT(t)

		m1 := createMachine(ctx, g, ns.Name, "m1-mhc-unhealthy-", withMachineHealthCheckFailed())
		m2 := createMachine(ctx, g, ns.Name, "m2-etcd-healthy-", withHealthyEtcdMember())

		controlPlane := &internal.ControlPlane{
			KCP: &controlplanev1.KubeadmControlPlane{Spec: controlplanev1.KubeadmControlPlaneSpec{
				Replicas: utilpointer.Int32Ptr(3),
			}},
			Cluster:  &clusterv1.Cluster{},
			Machines: collections.FromMachines(m1, m2),
		}

		r := &KubeadmControlPlaneReconciler{
			Client:   env.GetClient(),
			recorder: record.NewFakeRecorder(32),
			managementCluster: &fakeManagementCluster{
				Workload: fakeWorkloadCluster{
					EtcdMembersResult: nodes(controlPlane.Machines),
				},
			},
		}

		ret, err := r.canSafelyRemoveEtcdMember(context.TODO(), controlPlane, m1)
		g.Expect(ret).To(BeTrue())
		g.Expect(err).ToNot(HaveOccurred())

		g.Expect(env.Cleanup(ctx, m1, m2)).To(Succeed())
	})
	t.Run("Can safely remediate 2 machines CP when the etcd member being remediated is missing", func(t *testing.T) {
		g := NewWithT(t)

		m1 := createMachine(ctx, g, ns.Name, "m1-mhc-unhealthy-", withMachineHealthCheckFailed())
		m2 := createMachine(ctx, g, ns.Name, "m2-etcd-healthy-", withHealthyEtcdMember())

		controlPlane := &internal.ControlPlane{
			KCP: &controlplanev1.KubeadmControlPlane{Spec: controlplanev1.KubeadmControlPlaneSpec{
				Replicas: utilpointer.Int32Ptr(3),
			}},
			Cluster:  &clusterv1.Cluster{},
			Machines: collections.FromMachines(m1, m2),
		}

		members := make([]string, 0, len(controlPlane.Machines)-1)
		for _, n := range nodes(controlPlane.Machines) {
			if !strings.Contains(n, "m1-mhc-unhealthy-") {
				members = append(members, n)
			}
		}

		r := &KubeadmControlPlaneReconciler{
			Client:   env.GetClient(),
			recorder: record.NewFakeRecorder(32),
			managementCluster: &fakeManagementCluster{
				Workload: fakeWorkloadCluster{
					EtcdMembersResult: members,
				},
			},
		}

		ret, err := r.canSafelyRemoveEtcdMember(context.TODO(), controlPlane, m1)
		g.Expect(ret).To(BeTrue())
		g.Expect(err).ToNot(HaveOccurred())

		g.Expect(env.Cleanup(ctx, m1, m2)).To(Succeed())
	})
	t.Run("Can't safely remediate 2 machines CP with one additional etcd member failure", func(t *testing.T) {
		g := NewWithT(t)

		m1 := createMachine(ctx, g, ns.Name, "m1-mhc-unhealthy-", withMachineHealthCheckFailed())
		m2 := createMachine(ctx, g, ns.Name, "m2-etcd-unhealthy-", withUnhealthyEtcdMember())

		controlPlane := &internal.ControlPlane{
			KCP: &controlplanev1.KubeadmControlPlane{Spec: controlplanev1.KubeadmControlPlaneSpec{
				Replicas: utilpointer.Int32Ptr(3),
			}},
			Cluster:  &clusterv1.Cluster{},
			Machines: collections.FromMachines(m1, m2),
		}

		r := &KubeadmControlPlaneReconciler{
			Client:   env.GetClient(),
			recorder: record.NewFakeRecorder(32),
			managementCluster: &fakeManagementCluster{
				Workload: fakeWorkloadCluster{
					EtcdMembersResult: nodes(controlPlane.Machines),
				},
			},
		}

		ret, err := r.canSafelyRemoveEtcdMember(context.TODO(), controlPlane, m1)
		g.Expect(ret).To(BeFalse())
		g.Expect(err).ToNot(HaveOccurred())

		g.Expect(env.Cleanup(ctx, m1, m2)).To(Succeed())
	})
	t.Run("Can safely remediate 3 machines CP without additional etcd member failures", func(t *testing.T) {
		g := NewWithT(t)

		m1 := createMachine(ctx, g, ns.Name, "m1-mhc-unhealthy-", withMachineHealthCheckFailed())
		m2 := createMachine(ctx, g, ns.Name, "m2-etcd-healthy-", withHealthyEtcdMember())
		m3 := createMachine(ctx, g, ns.Name, "m3-etcd-healthy-", withHealthyEtcdMember())

		controlPlane := &internal.ControlPlane{
			KCP: &controlplanev1.KubeadmControlPlane{Spec: controlplanev1.KubeadmControlPlaneSpec{
				Replicas: utilpointer.Int32Ptr(3),
			}},
			Cluster:  &clusterv1.Cluster{},
			Machines: collections.FromMachines(m1, m2, m3),
		}

		r := &KubeadmControlPlaneReconciler{
			Client:   env.GetClient(),
			recorder: record.NewFakeRecorder(32),
			managementCluster: &fakeManagementCluster{
				Workload: fakeWorkloadCluster{
					EtcdMembersResult: nodes(controlPlane.Machines),
				},
			},
		}

		ret, err := r.canSafelyRemoveEtcdMember(context.TODO(), controlPlane, m1)
		g.Expect(ret).To(BeTrue())
		g.Expect(err).ToNot(HaveOccurred())

		g.Expect(env.Cleanup(ctx, m1, m2, m3)).To(Succeed())
	})
	t.Run("Can safely remediate 3 machines CP when the etcd member being remediated is missing", func(t *testing.T) {
		g := NewWithT(t)

		m1 := createMachine(ctx, g, ns.Name, "m1-mhc-unhealthy-", withMachineHealthCheckFailed())
		m2 := createMachine(ctx, g, ns.Name, "m2-etcd-healthy-", withHealthyEtcdMember())
		m3 := createMachine(ctx, g, ns.Name, "m3-etcd-healthy-", withHealthyEtcdMember())

		controlPlane := &internal.ControlPlane{
			KCP: &controlplanev1.KubeadmControlPlane{Spec: controlplanev1.KubeadmControlPlaneSpec{
				Replicas: utilpointer.Int32Ptr(3),
			}},
			Cluster:  &clusterv1.Cluster{},
			Machines: collections.FromMachines(m1, m2, m3),
		}

		members := make([]string, 0, len(controlPlane.Machines)-1)
		for _, n := range nodes(controlPlane.Machines) {
			if !strings.Contains(n, "m1-mhc-unhealthy-") {
				members = append(members, n)
			}
		}

		r := &KubeadmControlPlaneReconciler{
			Client:   env.GetClient(),
			recorder: record.NewFakeRecorder(32),
			managementCluster: &fakeManagementCluster{
				Workload: fakeWorkloadCluster{
					EtcdMembersResult: members,
				},
			},
		}

		ret, err := r.canSafelyRemoveEtcdMember(context.TODO(), controlPlane, m1)
		g.Expect(ret).To(BeTrue())
		g.Expect(err).ToNot(HaveOccurred())

		g.Expect(env.Cleanup(ctx, m1, m2, m3)).To(Succeed())
	})
	t.Run("Can't safely remediate 3 machines CP with one additional etcd member failure", func(t *testing.T) {
		g := NewWithT(t)

		m1 := createMachine(ctx, g, ns.Name, "m1-mhc-unhealthy-", withMachineHealthCheckFailed())
		m2 := createMachine(ctx, g, ns.Name, "m2-etcd-unhealthy-", withUnhealthyEtcdMember())
		m3 := createMachine(ctx, g, ns.Name, "m3-etcd-healthy-", withHealthyEtcdMember())

		controlPlane := &internal.ControlPlane{
			KCP: &controlplanev1.KubeadmControlPlane{Spec: controlplanev1.KubeadmControlPlaneSpec{
				Replicas: utilpointer.Int32Ptr(3),
			}},
			Cluster:  &clusterv1.Cluster{},
			Machines: collections.FromMachines(m1, m2, m3),
		}

		r := &KubeadmControlPlaneReconciler{
			Client:   env.GetClient(),
			recorder: record.NewFakeRecorder(32),
			managementCluster: &fakeManagementCluster{
				Workload: fakeWorkloadCluster{
					EtcdMembersResult: nodes(controlPlane.Machines),
				},
			},
		}

		ret, err := r.canSafelyRemoveEtcdMember(context.TODO(), controlPlane, m1)
		g.Expect(ret).To(BeFalse())
		g.Expect(err).ToNot(HaveOccurred())

		g.Expect(env.Cleanup(ctx, m1, m2, m3)).To(Succeed())
	})
	t.Run("Can safely remediate 5 machines CP less than 2 additional etcd member failures", func(t *testing.T) {
		g := NewWithT(t)

		m1 := createMachine(ctx, g, ns.Name, "m1-mhc-unhealthy-", withMachineHealthCheckFailed())
		m2 := createMachine(ctx, g, ns.Name, "m2-etcd-unhealthy-", withUnhealthyEtcdMember())
		m3 := createMachine(ctx, g, ns.Name, "m3-etcd-healthy-", withHealthyEtcdMember())
		m4 := createMachine(ctx, g, ns.Name, "m4-etcd-healthy-", withHealthyEtcdMember())
		m5 := createMachine(ctx, g, ns.Name, "m5-etcd-healthy-", withHealthyEtcdMember())

		controlPlane := &internal.ControlPlane{
			KCP: &controlplanev1.KubeadmControlPlane{Spec: controlplanev1.KubeadmControlPlaneSpec{
				Replicas: utilpointer.Int32Ptr(5),
			}},
			Cluster:  &clusterv1.Cluster{},
			Machines: collections.FromMachines(m1, m2, m3, m4, m5),
		}

		r := &KubeadmControlPlaneReconciler{
			Client:   env.GetClient(),
			recorder: record.NewFakeRecorder(32),
			managementCluster: &fakeManagementCluster{
				Workload: fakeWorkloadCluster{
					EtcdMembersResult: nodes(controlPlane.Machines),
				},
			},
		}

		ret, err := r.canSafelyRemoveEtcdMember(context.TODO(), controlPlane, m1)
		g.Expect(ret).To(BeTrue())
		g.Expect(err).ToNot(HaveOccurred())

		g.Expect(env.Cleanup(ctx, m1, m2, m3, m4, m5)).To(Succeed())
	})
	t.Run("Can't safely remediate 5 machines CP with 2 additional etcd member failures", func(t *testing.T) {
		g := NewWithT(t)

		m1 := createMachine(ctx, g, ns.Name, "m1-mhc-unhealthy-", withMachineHealthCheckFailed())
		m2 := createMachine(ctx, g, ns.Name, "m2-etcd-unhealthy-", withUnhealthyEtcdMember())
		m3 := createMachine(ctx, g, ns.Name, "m3-etcd-unhealthy-", withUnhealthyEtcdMember())
		m4 := createMachine(ctx, g, ns.Name, "m4-etcd-healthy-", withHealthyEtcdMember())
		m5 := createMachine(ctx, g, ns.Name, "m5-etcd-healthy-", withHealthyEtcdMember())

		controlPlane := &internal.ControlPlane{
			KCP: &controlplanev1.KubeadmControlPlane{Spec: controlplanev1.KubeadmControlPlaneSpec{
				Replicas: utilpointer.Int32Ptr(7),
			}},
			Cluster:  &clusterv1.Cluster{},
			Machines: collections.FromMachines(m1, m2, m3, m4, m5),
		}

		r := &KubeadmControlPlaneReconciler{
			Client:   env.GetClient(),
			recorder: record.NewFakeRecorder(32),
			managementCluster: &fakeManagementCluster{
				Workload: fakeWorkloadCluster{
					EtcdMembersResult: nodes(controlPlane.Machines),
				},
			},
		}

		ret, err := r.canSafelyRemoveEtcdMember(context.TODO(), controlPlane, m1)
		g.Expect(ret).To(BeFalse())
		g.Expect(err).ToNot(HaveOccurred())

		g.Expect(env.Cleanup(ctx, m1, m2, m3, m4, m5)).To(Succeed())
	})
	t.Run("Can safely remediate 7 machines CP with less than 3 additional etcd member failures", func(t *testing.T) {
		g := NewWithT(t)

		m1 := createMachine(ctx, g, ns.Name, "m1-mhc-unhealthy-", withMachineHealthCheckFailed())
		m2 := createMachine(ctx, g, ns.Name, "m2-etcd-unhealthy-", withUnhealthyEtcdMember())
		m3 := createMachine(ctx, g, ns.Name, "m3-etcd-unhealthy-", withUnhealthyEtcdMember())
		m4 := createMachine(ctx, g, ns.Name, "m4-etcd-healthy-", withHealthyEtcdMember())
		m5 := createMachine(ctx, g, ns.Name, "m5-etcd-healthy-", withHealthyEtcdMember())
		m6 := createMachine(ctx, g, ns.Name, "m6-etcd-healthy-", withHealthyEtcdMember())
		m7 := createMachine(ctx, g, ns.Name, "m7-etcd-healthy-", withHealthyEtcdMember())

		controlPlane := &internal.ControlPlane{
			KCP: &controlplanev1.KubeadmControlPlane{Spec: controlplanev1.KubeadmControlPlaneSpec{
				Replicas: utilpointer.Int32Ptr(7),
			}},
			Cluster:  &clusterv1.Cluster{},
			Machines: collections.FromMachines(m1, m2, m3, m4, m5, m6, m7),
		}

		r := &KubeadmControlPlaneReconciler{
			Client:   env.GetClient(),
			recorder: record.NewFakeRecorder(32),
			managementCluster: &fakeManagementCluster{
				Workload: fakeWorkloadCluster{
					EtcdMembersResult: nodes(controlPlane.Machines),
				},
			},
		}

		ret, err := r.canSafelyRemoveEtcdMember(context.TODO(), controlPlane, m1)
		g.Expect(ret).To(BeTrue())
		g.Expect(err).ToNot(HaveOccurred())

		g.Expect(env.Cleanup(ctx, m1, m2, m3, m4, m5, m6, m7)).To(Succeed())
	})
	t.Run("Can't safely remediate 7 machines CP with 3 additional etcd member failures", func(t *testing.T) {
		g := NewWithT(t)

		m1 := createMachine(ctx, g, ns.Name, "m1-mhc-unhealthy-", withMachineHealthCheckFailed())
		m2 := createMachine(ctx, g, ns.Name, "m2-etcd-unhealthy-", withUnhealthyEtcdMember())
		m3 := createMachine(ctx, g, ns.Name, "m3-etcd-unhealthy-", withUnhealthyEtcdMember())
		m4 := createMachine(ctx, g, ns.Name, "m4-etcd-unhealthy-", withUnhealthyEtcdMember())
		m5 := createMachine(ctx, g, ns.Name, "m5-etcd-healthy-", withHealthyEtcdMember())
		m6 := createMachine(ctx, g, ns.Name, "m6-etcd-healthy-", withHealthyEtcdMember())
		m7 := createMachine(ctx, g, ns.Name, "m7-etcd-healthy-", withHealthyEtcdMember())

		controlPlane := &internal.ControlPlane{
			KCP: &controlplanev1.KubeadmControlPlane{Spec: controlplanev1.KubeadmControlPlaneSpec{
				Replicas: utilpointer.Int32Ptr(5),
			}},
			Cluster:  &clusterv1.Cluster{},
			Machines: collections.FromMachines(m1, m2, m3, m4, m5, m6, m7),
		}

		r := &KubeadmControlPlaneReconciler{
			Client:   env.GetClient(),
			recorder: record.NewFakeRecorder(32),
			managementCluster: &fakeManagementCluster{
				Workload: fakeWorkloadCluster{
					EtcdMembersResult: nodes(controlPlane.Machines),
				},
			},
		}

		ret, err := r.canSafelyRemoveEtcdMember(context.TODO(), controlPlane, m1)
		g.Expect(ret).To(BeFalse())
		g.Expect(err).ToNot(HaveOccurred())

		g.Expect(env.Cleanup(ctx, m1, m2, m3, m4, m5, m6, m7)).To(Succeed())
	})
}

func nodes(machines collections.Machines) []string {
	nodes := make([]string, 0, machines.Len())
	for _, m := range machines {
		if m.Status.NodeRef != nil {
			nodes = append(nodes, m.Status.NodeRef.Name)
		}
	}
	return nodes
}

type machineOption func(*clusterv1.Machine)

func withMachineHealthCheckFailed() machineOption {
	return func(machine *clusterv1.Machine) {
		conditions.MarkFalse(machine, clusterv1.MachineHealthCheckSuccededCondition, clusterv1.MachineHasFailureReason, clusterv1.ConditionSeverityWarning, "")
		conditions.MarkFalse(machine, clusterv1.MachineOwnerRemediatedCondition, clusterv1.WaitingForRemediationReason, clusterv1.ConditionSeverityWarning, "")
	}
}

func withHealthyEtcdMember() machineOption {
	return func(machine *clusterv1.Machine) {
		conditions.MarkTrue(machine, controlplanev1.MachineEtcdMemberHealthyCondition)
	}
}

func withUnhealthyEtcdMember() machineOption {
	return func(machine *clusterv1.Machine) {
		conditions.MarkFalse(machine, controlplanev1.MachineEtcdMemberHealthyCondition, controlplanev1.EtcdMemberUnhealthyReason, clusterv1.ConditionSeverityError, "")
	}
}

func withNodeRef(ref string) machineOption {
	return func(machine *clusterv1.Machine) {
		machine.Status.NodeRef = &corev1.ObjectReference{
			Kind: "Node",
			Name: ref,
		}
	}
}

func createMachine(ctx context.Context, g *WithT, namespace, name string, options ...machineOption) *clusterv1.Machine {
	m := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    namespace,
			GenerateName: name,
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: "cluster",
			Bootstrap: clusterv1.Bootstrap{
				DataSecretName: utilpointer.StringPtr("secret"),
			},
		},
	}
	g.Expect(env.Create(ctx, m)).To(Succeed())

	patchHelper, err := patch.NewHelper(m, env.GetClient())
	g.Expect(err).ToNot(HaveOccurred())

	for _, opt := range append(options, withNodeRef(fmt.Sprintf("node-%s", m.Name))) {
		opt(m)
	}

	g.Expect(patchHelper.Patch(ctx, m))
	return m
}

func getDeletingMachine(namespace, name string, options ...machineOption) *clusterv1.Machine {
	deletionTime := metav1.Now()
	m := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:         namespace,
			Name:              name,
			DeletionTimestamp: &deletionTime,
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: "cluster",
			Bootstrap: clusterv1.Bootstrap{
				DataSecretName: utilpointer.StringPtr("secret"),
			},
		},
	}

	for _, opt := range append(options, withNodeRef(fmt.Sprintf("node-%s", m.Name))) {
		opt(m)
	}
	return m
}

func assertMachineCondition(ctx context.Context, g *WithT, m *clusterv1.Machine, t clusterv1.ConditionType, status corev1.ConditionStatus, reason string, severity clusterv1.ConditionSeverity, message string) {
	g.Eventually(func() error {
		if err := env.Get(ctx, client.ObjectKey{Namespace: m.Namespace, Name: m.Name}, m); err != nil {
			return err
		}
		c := conditions.Get(m, t)
		if c == nil {
			return errors.Errorf("condition %q was nil", t)
		}
		if c.Status != status {
			return errors.Errorf("condition %q status %q did not match %q", t, c.Status, status)
		}
		if c.Reason != reason {
			return errors.Errorf("condition %q reason %q did not match %q", t, c.Reason, reason)
		}
		if c.Severity != severity {
			return errors.Errorf("condition %q severity %q did not match %q", t, c.Status, status)
		}
		if c.Message != message {
			return errors.Errorf("condition %q message %q did not match %q", t, c.Message, message)
		}
		return nil
	}, 10*time.Second).Should(Succeed())
}
