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
	"testing"

	"sigs.k8s.io/cluster-api/util/collections"

	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/pointer"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	bootstrapv1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1alpha4"
	"sigs.k8s.io/cluster-api/controlplane/kubeadm/internal"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const UpdatedVersion string = "v1.17.4"
const Host string = "nodomain.example.com"

func TestKubeadmControlPlaneReconciler_RolloutStrategy_ScaleUp(t *testing.T) {
	g := NewWithT(t)

	cluster, kcp, genericMachineTemplate := createClusterWithControlPlane(metav1.NamespaceDefault)
	cluster.Spec.ControlPlaneEndpoint.Host = Host
	cluster.Spec.ControlPlaneEndpoint.Port = 6443
	kcp.Spec.KubeadmConfigSpec.ClusterConfiguration = nil
	kcp.Spec.Replicas = pointer.Int32Ptr(1)
	setKCPHealthy(kcp)

	fakeClient := newFakeClient(cluster.DeepCopy(), kcp.DeepCopy(), genericMachineTemplate.DeepCopy())

	r := &KubeadmControlPlaneReconciler{
		Client:   fakeClient,
		recorder: record.NewFakeRecorder(32),
		managementCluster: &fakeManagementCluster{
			Management: &internal.Management{Client: fakeClient},
			Workload: fakeWorkloadCluster{
				Status: internal.ClusterStatus{Nodes: 1},
			},
		},
		managementClusterUncached: &fakeManagementCluster{
			Management: &internal.Management{Client: fakeClient},
			Workload: fakeWorkloadCluster{
				Status: internal.ClusterStatus{Nodes: 1},
			},
		},
	}
	controlPlane := &internal.ControlPlane{
		KCP:      kcp,
		Cluster:  cluster,
		Machines: nil,
	}

	result, err := r.initializeControlPlane(ctx, cluster, kcp, controlPlane)
	g.Expect(result).To(Equal(ctrl.Result{Requeue: true}))
	g.Expect(err).NotTo(HaveOccurred())

	// initial setup
	initialMachine := &clusterv1.MachineList{}
	g.Expect(fakeClient.List(ctx, initialMachine, client.InNamespace(cluster.Namespace))).To(Succeed())
	g.Expect(initialMachine.Items).To(HaveLen(1))
	for i := range initialMachine.Items {
		setMachineHealthy(&initialMachine.Items[i])
	}

	// change the KCP spec so the machine becomes outdated
	kcp.Spec.Version = UpdatedVersion

	// run upgrade the first time, expect we scale up
	needingUpgrade := collections.FromMachineList(initialMachine)
	controlPlane.Machines = needingUpgrade
	result, err = r.upgradeControlPlane(ctx, cluster, kcp, controlPlane, needingUpgrade)
	g.Expect(result).To(Equal(ctrl.Result{Requeue: true}))
	g.Expect(err).To(BeNil())
	bothMachines := &clusterv1.MachineList{}
	g.Expect(fakeClient.List(ctx, bothMachines, client.InNamespace(cluster.Namespace))).To(Succeed())
	g.Expect(bothMachines.Items).To(HaveLen(2))

	// run upgrade a second time, simulate that the node has not appeared yet but the machine exists

	// Unhealthy control plane will be detected during reconcile loop and upgrade will never be called.
	result, err = r.reconcile(context.Background(), cluster, kcp)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(result).To(Equal(ctrl.Result{RequeueAfter: preflightFailedRequeueAfter}))
	g.Expect(fakeClient.List(context.Background(), bothMachines, client.InNamespace(cluster.Namespace))).To(Succeed())
	g.Expect(bothMachines.Items).To(HaveLen(2))

	// manually increase number of nodes, make control plane healthy again
	r.managementCluster.(*fakeManagementCluster).Workload.Status.Nodes++
	for i := range bothMachines.Items {
		setMachineHealthy(&bothMachines.Items[i])
	}
	controlPlane.Machines = collections.FromMachineList(bothMachines)

	// run upgrade the second time, expect we scale down
	result, err = r.upgradeControlPlane(ctx, cluster, kcp, controlPlane, controlPlane.Machines)
	g.Expect(err).To(BeNil())
	g.Expect(result).To(Equal(ctrl.Result{Requeue: true}))
	finalMachine := &clusterv1.MachineList{}
	g.Expect(fakeClient.List(ctx, finalMachine, client.InNamespace(cluster.Namespace))).To(Succeed())
	g.Expect(finalMachine.Items).To(HaveLen(1))

	// assert that the deleted machine is the oldest, initial machine
	g.Expect(finalMachine.Items[0].Name).ToNot(Equal(initialMachine.Items[0].Name))
	g.Expect(finalMachine.Items[0].CreationTimestamp.Time).To(BeTemporally(">", initialMachine.Items[0].CreationTimestamp.Time))
}

func TestKubeadmControlPlaneReconciler_RolloutStrategy_ScaleDown(t *testing.T) {
	version := "v1.17.3"
	g := NewWithT(t)

	cluster, kcp, tmpl := createClusterWithControlPlane(metav1.NamespaceDefault)
	cluster.Spec.ControlPlaneEndpoint.Host = "nodomain.example.com1"
	cluster.Spec.ControlPlaneEndpoint.Port = 6443
	kcp.Spec.Replicas = pointer.Int32Ptr(3)
	kcp.Spec.RolloutStrategy.RollingUpdate.MaxSurge.IntVal = 0
	setKCPHealthy(kcp)

	fmc := &fakeManagementCluster{
		Machines: collections.Machines{},
		Workload: fakeWorkloadCluster{
			Status: internal.ClusterStatus{Nodes: 3},
		},
	}
	objs := []client.Object{cluster.DeepCopy(), kcp.DeepCopy(), tmpl.DeepCopy()}
	for i := 0; i < 3; i++ {
		name := fmt.Sprintf("test-%d", i)
		m := &clusterv1.Machine{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: cluster.Namespace,
				Name:      name,
				Labels:    internal.ControlPlaneMachineLabelsForCluster(kcp, cluster.Name),
			},
			Spec: clusterv1.MachineSpec{
				Bootstrap: clusterv1.Bootstrap{
					ConfigRef: &corev1.ObjectReference{
						APIVersion: bootstrapv1.GroupVersion.String(),
						Kind:       "KubeadmConfig",
						Name:       name,
					},
				},
				Version: &version,
			},
		}
		cfg := &bootstrapv1.KubeadmConfig{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: cluster.Namespace,
				Name:      name,
			},
		}
		objs = append(objs, m, cfg)
		fmc.Machines.Insert(m)
	}
	fakeClient := newFakeClient(objs...)
	fmc.Reader = fakeClient
	r := &KubeadmControlPlaneReconciler{
		Client:                    fakeClient,
		managementCluster:         fmc,
		managementClusterUncached: fmc,
	}

	controlPlane := &internal.ControlPlane{
		KCP:      kcp,
		Cluster:  cluster,
		Machines: nil,
	}

	result, err := r.reconcile(ctx, cluster, kcp)
	g.Expect(result).To(Equal(ctrl.Result{}))
	g.Expect(err).NotTo(HaveOccurred())

	machineList := &clusterv1.MachineList{}
	g.Expect(fakeClient.List(ctx, machineList, client.InNamespace(cluster.Namespace))).To(Succeed())
	g.Expect(machineList.Items).To(HaveLen(3))
	for i := range machineList.Items {
		setMachineHealthy(&machineList.Items[i])
	}

	// change the KCP spec so the machine becomes outdated
	kcp.Spec.Version = UpdatedVersion

	// run upgrade, expect we scale down
	needingUpgrade := collections.FromMachineList(machineList)
	controlPlane.Machines = needingUpgrade
	result, err = r.upgradeControlPlane(ctx, cluster, kcp, controlPlane, needingUpgrade)
	g.Expect(result).To(Equal(ctrl.Result{Requeue: true}))
	g.Expect(err).To(BeNil())
	remainingMachines := &clusterv1.MachineList{}
	g.Expect(fakeClient.List(ctx, remainingMachines, client.InNamespace(cluster.Namespace))).To(Succeed())
	g.Expect(remainingMachines.Items).To(HaveLen(2))
}

type machineOpt func(*clusterv1.Machine)

func machine(name string, opts ...machineOpt) *clusterv1.Machine {
	m := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceDefault,
		},
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}
