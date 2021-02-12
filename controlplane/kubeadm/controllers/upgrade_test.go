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

	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/pointer"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	"sigs.k8s.io/cluster-api/controlplane/kubeadm/internal"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestKubeadmControlPlaneReconciler_upgradeControlPlane(t *testing.T) {
	g := NewWithT(t)

	cluster, kcp, genericMachineTemplate := createClusterWithControlPlane()
	cluster.Spec.ControlPlaneEndpoint.Host = "nodomain.example.com"
	cluster.Spec.ControlPlaneEndpoint.Port = 6443
	kcp.Spec.Version = "v1.17.3"
	kcp.Spec.KubeadmConfigSpec.ClusterConfiguration = nil
	kcp.Spec.Replicas = pointer.Int32Ptr(1)
	setKCPHealthy(kcp)

	fakeClient := newFakeClient(g, cluster.DeepCopy(), kcp.DeepCopy(), genericMachineTemplate.DeepCopy())

	r := &KubeadmControlPlaneReconciler{
		Client:   fakeClient,
		recorder: record.NewFakeRecorder(32),
		managementCluster: &fakeManagementCluster{
			Management: &internal.Management{Client: fakeClient},
			Workload: fakeWorkloadCluster{
				Status: internal.ClusterStatus{Nodes: 1},
				KubeadmConfig: fakeKubeadmConfig{
					ConfigMap: &corev1.ConfigMap{},
				},
			},
		},
		managementClusterUncached: &fakeManagementCluster{
			Management: &internal.Management{Client: fakeClient},
			Workload: fakeWorkloadCluster{
				Status: internal.ClusterStatus{Nodes: 1},
				KubeadmConfig: fakeKubeadmConfig{
					ConfigMap: &corev1.ConfigMap{},
				},
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
	kcp.Spec.Version = "v1.17.4"

	// run upgrade the first time, expect we scale up
	needingUpgrade := internal.NewFilterableMachineCollectionFromMachineList(initialMachine)
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
	controlPlane.Machines = internal.NewFilterableMachineCollectionFromMachineList(bothMachines)

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

type machineOpt func(*clusterv1.Machine)

func machine(name string, opts ...machineOpt) *clusterv1.Machine {
	m := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}
