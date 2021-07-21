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

package tree

import (
	"context"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/cluster-api/cmd/clusterctl/internal/test"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func Test_Discovery(t *testing.T) {
	type nodeCheck func(*WithT, client.Object)
	type args struct {
		objs            []client.Object
		discoverOptions DiscoverOptions
	}
	tests := []struct {
		name          string
		args          args
		wantError     bool
		wantTree      map[string][]string
		wantNodeCheck map[string]nodeCheck
	}{
		{
			name: "Discovery with default discovery settings",
			args: args{
				discoverOptions: DiscoverOptions{},
				objs: test.NewFakeCluster("ns1", "cluster1").
					WithControlPlane(
						test.NewFakeControlPlane("cp").
							WithMachines(
								test.NewFakeMachine("cp1"),
							),
					).
					WithMachineDeployments(
						test.NewFakeMachineDeployment("md1").
							WithMachineSets(
								test.NewFakeMachineSet("ms1").
									WithMachines(
										test.NewFakeMachine("m1"),
										test.NewFakeMachine("m2"),
									),
							),
					).
					Objs(),
			},
			wantError: false,
			wantTree: map[string][]string{
				// Cluster should be parent of InfrastructureCluster, ControlPlane, and WorkerNodes
				"cluster.x-k8s.io/v1alpha4, Kind=Cluster, ns1/cluster1": {
					"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureCluster, ns1/cluster1",
					"controlplane.cluster.x-k8s.io/v1alpha4, Kind=GenericControlPlane, ns1/cp",
					"virtual.cluster.x-k8s.io/v1alpha4, ns1/Workers",
				},
				// InfrastructureCluster should be leaf
				"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureCluster, ns1/cluster1": {},
				// ControlPlane should have a machine
				"controlplane.cluster.x-k8s.io/v1alpha4, Kind=GenericControlPlane, ns1/cp": {
					"cluster.x-k8s.io/v1alpha4, Kind=Machine, ns1/cp1",
				},
				// Machine should be leaf (no echo)
				"cluster.x-k8s.io/v1alpha4, Kind=Machine, ns1/cp1": {},
				// Workers should have a machine deployment
				"virtual.cluster.x-k8s.io/v1alpha4, ns1/Workers": {
					"cluster.x-k8s.io/v1alpha4, Kind=MachineDeployment, ns1/md1",
				},
				// Machine deployment should have a group of machines (grouping)
				"cluster.x-k8s.io/v1alpha4, Kind=MachineDeployment, ns1/md1": {
					"virtual.cluster.x-k8s.io/v1alpha4, ns1/zzz_",
				},
			},
			wantNodeCheck: map[string]nodeCheck{
				// InfrastructureCluster should have a meta name
				"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureCluster, ns1/cluster1": func(g *WithT, obj client.Object) {
					g.Expect(GetMetaName(obj)).To(Equal("ClusterInfrastructure"))
				},
				// ControlPlane should have a meta name, be a grouping object
				"controlplane.cluster.x-k8s.io/v1alpha4, Kind=GenericControlPlane, ns1/cp": func(g *WithT, obj client.Object) {
					g.Expect(GetMetaName(obj)).To(Equal("ControlPlane"))
					g.Expect(IsGroupingObject(obj)).To(BeTrue())
				},
				// Workers should be a virtual node
				"virtual.cluster.x-k8s.io/v1alpha4, ns1/Workers": func(g *WithT, obj client.Object) {
					g.Expect(IsVirtualObject(obj)).To(BeTrue())
				},
				// Machine deployment should be a grouping object
				"cluster.x-k8s.io/v1alpha4, Kind=MachineDeployment, ns1/md1": func(g *WithT, obj client.Object) {
					g.Expect(IsGroupingObject(obj)).To(BeTrue())
				},
			},
		},
		{
			name: "Discovery with grouping disabled",
			args: args{
				discoverOptions: DiscoverOptions{
					DisableGrouping: true,
				},
				objs: test.NewFakeCluster("ns1", "cluster1").
					WithControlPlane(
						test.NewFakeControlPlane("cp").
							WithMachines(
								test.NewFakeMachine("cp1"),
							),
					).
					WithMachineDeployments(
						test.NewFakeMachineDeployment("md1").
							WithMachineSets(
								test.NewFakeMachineSet("ms1").
									WithMachines(
										test.NewFakeMachine("m1"),
										test.NewFakeMachine("m2"),
									),
							),
					).
					Objs(),
			},
			wantError: false,
			wantTree: map[string][]string{
				// Cluster should be parent of InfrastructureCluster, ControlPlane, and WorkerNodes
				"cluster.x-k8s.io/v1alpha4, Kind=Cluster, ns1/cluster1": {
					"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureCluster, ns1/cluster1",
					"controlplane.cluster.x-k8s.io/v1alpha4, Kind=GenericControlPlane, ns1/cp",
					"virtual.cluster.x-k8s.io/v1alpha4, ns1/Workers",
				},
				// InfrastructureCluster should be leaf
				"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureCluster, ns1/cluster1": {},
				// ControlPlane should have a machine
				"controlplane.cluster.x-k8s.io/v1alpha4, Kind=GenericControlPlane, ns1/cp": {
					"cluster.x-k8s.io/v1alpha4, Kind=Machine, ns1/cp1",
				},
				// Workers should have a machine deployment
				"virtual.cluster.x-k8s.io/v1alpha4, ns1/Workers": {
					"cluster.x-k8s.io/v1alpha4, Kind=MachineDeployment, ns1/md1",
				},
				// Machine deployment should have a group of machines
				"cluster.x-k8s.io/v1alpha4, Kind=MachineDeployment, ns1/md1": {
					"cluster.x-k8s.io/v1alpha4, Kind=Machine, ns1/m1",
					"cluster.x-k8s.io/v1alpha4, Kind=Machine, ns1/m2",
				},
				// Machine should be leaf (no echo)
				"cluster.x-k8s.io/v1alpha4, Kind=Machine, ns1/cp1": {},
				"cluster.x-k8s.io/v1alpha4, Kind=Machine, ns1/m1":  {},
				"cluster.x-k8s.io/v1alpha4, Kind=Machine, ns1/m2":  {},
			},
			wantNodeCheck: map[string]nodeCheck{
				// InfrastructureCluster should have a meta name
				"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureCluster, ns1/cluster1": func(g *WithT, obj client.Object) {
					g.Expect(GetMetaName(obj)).To(Equal("ClusterInfrastructure"))
				},
				// ControlPlane should have a meta name, should NOT be a grouping object
				"controlplane.cluster.x-k8s.io/v1alpha4, Kind=GenericControlPlane, ns1/cp": func(g *WithT, obj client.Object) {
					g.Expect(GetMetaName(obj)).To(Equal("ControlPlane"))
					g.Expect(IsGroupingObject(obj)).To(BeFalse())
				},
				// Workers should be a virtual node
				"virtual.cluster.x-k8s.io/v1alpha4, ns1/Workers": func(g *WithT, obj client.Object) {
					g.Expect(IsVirtualObject(obj)).To(BeTrue())
				},
				// Machine deployment should NOT be a grouping object
				"cluster.x-k8s.io/v1alpha4, Kind=MachineDeployment, ns1/md1": func(g *WithT, obj client.Object) {
					g.Expect(IsGroupingObject(obj)).To(BeFalse())
				},
			},
		},
		{
			name: "Discovery with grouping and no-echo disabled",
			args: args{
				discoverOptions: DiscoverOptions{
					DisableGrouping: true,
					DisableNoEcho:   true,
				},
				objs: test.NewFakeCluster("ns1", "cluster1").
					WithControlPlane(
						test.NewFakeControlPlane("cp").
							WithMachines(
								test.NewFakeMachine("cp1"),
							),
					).
					WithMachineDeployments(
						test.NewFakeMachineDeployment("md1").
							WithMachineSets(
								test.NewFakeMachineSet("ms1").
									WithMachines(
										test.NewFakeMachine("m1"),
									),
							),
					).
					Objs(),
			},
			wantError: false,
			wantTree: map[string][]string{
				// Cluster should be parent of InfrastructureCluster, ControlPlane, and WorkerNodes
				"cluster.x-k8s.io/v1alpha4, Kind=Cluster, ns1/cluster1": {
					"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureCluster, ns1/cluster1",
					"controlplane.cluster.x-k8s.io/v1alpha4, Kind=GenericControlPlane, ns1/cp",
					"virtual.cluster.x-k8s.io/v1alpha4, ns1/Workers",
				},
				// InfrastructureCluster should be leaf
				"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureCluster, ns1/cluster1": {},
				// ControlPlane should have a machine
				"controlplane.cluster.x-k8s.io/v1alpha4, Kind=GenericControlPlane, ns1/cp": {
					"cluster.x-k8s.io/v1alpha4, Kind=Machine, ns1/cp1",
				},
				// Machine should have infra machine and bootstrap (echo)
				"cluster.x-k8s.io/v1alpha4, Kind=Machine, ns1/cp1": {
					"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureMachine, ns1/cp1",
					"bootstrap.cluster.x-k8s.io/v1alpha4, Kind=GenericBootstrapConfig, ns1/cp1",
				},
				// Workers should have a machine deployment
				"virtual.cluster.x-k8s.io/v1alpha4, ns1/Workers": {
					"cluster.x-k8s.io/v1alpha4, Kind=MachineDeployment, ns1/md1",
				},
				// Machine deployment should have a group of machines
				"cluster.x-k8s.io/v1alpha4, Kind=MachineDeployment, ns1/md1": {
					"cluster.x-k8s.io/v1alpha4, Kind=Machine, ns1/m1",
				},
				// Machine should have infra machine and bootstrap (echo)
				"cluster.x-k8s.io/v1alpha4, Kind=Machine, ns1/m1": {
					"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureMachine, ns1/m1",
					"bootstrap.cluster.x-k8s.io/v1alpha4, Kind=GenericBootstrapConfig, ns1/m1",
				},
			},
			wantNodeCheck: map[string]nodeCheck{
				// InfrastructureCluster should have a meta name
				"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureCluster, ns1/cluster1": func(g *WithT, obj client.Object) {
					g.Expect(GetMetaName(obj)).To(Equal("ClusterInfrastructure"))
				},
				// ControlPlane should have a meta name, should NOT be a grouping object
				"controlplane.cluster.x-k8s.io/v1alpha4, Kind=GenericControlPlane, ns1/cp": func(g *WithT, obj client.Object) {
					g.Expect(GetMetaName(obj)).To(Equal("ControlPlane"))
					g.Expect(IsGroupingObject(obj)).To(BeFalse())
				},
				// Workers should be a virtual node
				"virtual.cluster.x-k8s.io/v1alpha4, ns1/Workers": func(g *WithT, obj client.Object) {
					g.Expect(IsVirtualObject(obj)).To(BeTrue())
				},
				// Machine deployment should NOT be a grouping object
				"cluster.x-k8s.io/v1alpha4, Kind=MachineDeployment, ns1/md1": func(g *WithT, obj client.Object) {
					g.Expect(IsGroupingObject(obj)).To(BeFalse())
				},
				// infra machines and boostrap should have meta names
				"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureMachine, ns1/cp1": func(g *WithT, obj client.Object) {
					g.Expect(GetMetaName(obj)).To(Equal("MachineInfrastructure"))
				},
				"bootstrap.cluster.x-k8s.io/v1alpha4, Kind=GenericBootstrapConfig, ns1/cp1": func(g *WithT, obj client.Object) {
					g.Expect(GetMetaName(obj)).To(Equal("BootstrapConfig"))
				},
				"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureMachine, ns1/m1": func(g *WithT, obj client.Object) {
					g.Expect(GetMetaName(obj)).To(Equal("MachineInfrastructure"))
				},
				"bootstrap.cluster.x-k8s.io/v1alpha4, Kind=GenericBootstrapConfig, ns1/m1": func(g *WithT, obj client.Object) {
					g.Expect(GetMetaName(obj)).To(Equal("BootstrapConfig"))
				},
			},
		},
		{
			name: "Discovery with no control plane or machine deployments",
			args: args{
				discoverOptions: DiscoverOptions{},
				objs:            test.NewFakeCluster("ns1", "cluster1").Objs(),
			},
			wantError:     true,
			wantTree:      map[string][]string{},
			wantNodeCheck: map[string]nodeCheck{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			client, err := test.NewFakeProxy().WithObjs(tt.args.objs...).NewClient()
			g.Expect(client).ToNot(BeNil())
			g.Expect(err).ToNot(HaveOccurred())

			tree, err := Discovery(context.TODO(), client, "ns1", "cluster1", tt.args.discoverOptions)
			if tt.wantError {
				g.Expect(tree).To(BeNil())
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(tree).ToNot(BeNil())
				g.Expect(err).ToNot(HaveOccurred())
			}

			for parent, wantChildren := range tt.wantTree {
				gotChildren := tree.GetObjectsByParent(types.UID(parent))
				g.Expect(wantChildren).To(HaveLen(len(gotChildren)), "%q doesn't have the expected number of children nodes", parent)

				for _, gotChild := range gotChildren {
					found := false
					for _, wantChild := range wantChildren {
						if strings.HasPrefix(string(gotChild.GetUID()), wantChild) {
							found = true
							break
						}
					}
					g.Expect(found).To(BeTrue(), "got child %q for parent %q, expecting [%s]", gotChild.GetUID(), parent, strings.Join(wantChildren, "] ["))

					if test, ok := tt.wantNodeCheck[string(gotChild.GetUID())]; ok {
						test(g, gotChild)
					}
				}
			}
		})
	}
}
