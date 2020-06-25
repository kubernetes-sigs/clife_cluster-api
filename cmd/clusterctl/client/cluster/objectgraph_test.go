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

package cluster

import (
	"fmt"
	"sort"
	"testing"

	. "github.com/onsi/gomega"
	"github.com/pkg/errors"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/cluster-api/cmd/clusterctl/internal/test"
)

func TestObjectGraph_getDiscoveryTypeMetaList(t *testing.T) {
	type fields struct {
		proxy Proxy
	}
	tests := []struct {
		name    string
		fields  fields
		want    []discoveryTypeMeta
		wantErr bool
	}{
		{
			name: "Return CRDs + ConfigMap & Secrets",
			fields: fields{
				proxy: test.NewFakeProxy().
					WithObjs(
						test.FakeCustomResourceDefinition("foo", "Bar", apiextensionsv1.NamespaceScoped, "v2", "v1"), // NB. foo/v1 Bar is not a storage version, so it should be ignored
						test.FakeCustomResourceDefinition("foo", "Baz", apiextensionsv1.NamespaceScoped, "v1"),
						test.FakeCustomResourceDefinition("foo", "Qux", apiextensionsv1.ClusterScoped, "v1"),
					),
			},
			want: []discoveryTypeMeta{
				{APIVersion: "foo/v2", Kind: "Bar", Scope: apiextensionsv1.NamespaceScoped},
				{APIVersion: "foo/v1", Kind: "Baz", Scope: apiextensionsv1.NamespaceScoped},
				{APIVersion: "foo/v1", Kind: "Qux", Scope: apiextensionsv1.ClusterScoped},
				{APIVersion: "v1", Kind: "Secret", Scope: apiextensionsv1.NamespaceScoped},
				{APIVersion: "v1", Kind: "ConfigMap", Scope: apiextensionsv1.NamespaceScoped},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			graph := newObjectGraph(tt.fields.proxy)
			got, err := graph.getDiscoveryTypes()
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}

			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(got).To(ConsistOf(tt.want))
		})
	}
}

func sortTypeMetaList(list []metav1.TypeMeta) func(i int, j int) bool {
	return func(i, j int) bool {
		return list[i].GroupVersionKind().String() < list[j].GroupVersionKind().String()
	}
}

type wantGraphItem struct {
	virtual    bool
	owners     []string
	softOwners []string
}

type wantGraph struct {
	nodes map[string]wantGraphItem
}

func assertGraph(t *testing.T, got *objectGraph, want wantGraph) {
	g := NewWithT(t)

	g.Expect(len(got.uidToNode)).To(Equal(len(want.nodes)))

	for uid, wantNode := range want.nodes {
		gotNode, ok := got.uidToNode[types.UID(uid)]
		g.Expect(ok).To(BeTrue())
		g.Expect(gotNode.virtual).To(Equal(wantNode.virtual))
		g.Expect(gotNode.owners).To(HaveLen(len(wantNode.owners)))

		for _, wantOwner := range wantNode.owners {
			found := false
			for k := range gotNode.owners {
				if k.identity.UID == types.UID(wantOwner) {
					found = true
					break
				}
			}
			g.Expect(found).To(BeTrue())
		}

		g.Expect(gotNode.softOwners).To(HaveLen(len(wantNode.softOwners)))

		for _, wantOwner := range wantNode.softOwners {
			found := false
			for k := range gotNode.softOwners {
				if k.identity.UID == types.UID(wantOwner) {
					found = true
					break
				}
			}
			g.Expect(found).To(BeTrue())
		}
	}
}

func TestObjectGraph_addObj(t *testing.T) {
	type args struct {
		objs []*unstructured.Unstructured
	}

	tests := []struct {
		name string
		args args
		want wantGraph
	}{
		{
			name: "Add a single object",
			args: args{
				objs: []*unstructured.Unstructured{
					{
						Object: map[string]interface{}{
							"apiVersion": "a/v1",
							"kind":       "A",
							"metadata": map[string]interface{}{
								"namespace": "ns",
								"name":      "foo",
								"uid":       "1",
							},
						},
					},
				},
			},
			want: wantGraph{
				nodes: map[string]wantGraphItem{
					"1": { // the object: not virtual (observed), without owner ref
						virtual: false,
						owners:  nil,
					},
				},
			},
		},
		{
			name: "Add a single object with an owner ref",
			args: args{
				objs: []*unstructured.Unstructured{
					{
						Object: map[string]interface{}{
							"apiVersion": "a/v1",
							"kind":       "A",
							"metadata": map[string]interface{}{
								"namespace": "ns",
								"name":      "foo",
								"uid":       "1",
								"ownerReferences": []interface{}{
									map[string]interface{}{
										"apiVersion": "b/v1",
										"kind":       "B",
										"name":       "bar",
										"uid":        "2",
									},
								},
							},
						},
					},
				},
			},
			want: wantGraph{
				nodes: map[string]wantGraphItem{
					"1": { // the object: not virtual (observed), with 1 owner refs
						virtual: false,
						owners:  []string{"2"},
					},
					"2": { // the object owner: virtual (not yet observed), without owner refs
						virtual: true,
						owners:  nil,
					},
				},
			},
		},
		{
			name: "Add an object with an owner ref and its owner",
			args: args{
				objs: []*unstructured.Unstructured{
					{
						Object: map[string]interface{}{
							"apiVersion": "a/v1",
							"kind":       "A",
							"metadata": map[string]interface{}{
								"namespace": "ns",
								"name":      "foo",
								"uid":       "1",
								"ownerReferences": []interface{}{
									map[string]interface{}{
										"apiVersion": "b/v1",
										"kind":       "B",
										"name":       "bar",
										"uid":        "2",
									},
								},
							},
						},
					},
					{
						Object: map[string]interface{}{
							"apiVersion": "b/v1",
							"kind":       "B",
							"metadata": map[string]interface{}{
								"namespace": "ns",
								"name":      "bar",
								"uid":       "2",
							},
						},
					},
				},
			},
			want: wantGraph{
				nodes: map[string]wantGraphItem{
					"1": { // the object: not virtual (observed), with 1 owner refs
						virtual: false,
						owners:  []string{"2"},
					},
					"2": { // the object owner: not virtual (observed), without owner refs
						virtual: false,
						owners:  nil,
					},
				},
			},
		},
		{
			name: "Add an object with an owner ref and its owner (reverse discovery order)",
			args: args{
				objs: []*unstructured.Unstructured{
					{
						Object: map[string]interface{}{
							"apiVersion": "b/v1",
							"kind":       "B",
							"metadata": map[string]interface{}{
								"namespace": "ns",
								"name":      "bar",
								"uid":       "2",
							},
						},
					},
					{
						Object: map[string]interface{}{
							"apiVersion": "a/v1",
							"kind":       "A",
							"metadata": map[string]interface{}{
								"namespace": "ns",
								"name":      "foo",
								"uid":       "1",
								"ownerReferences": []interface{}{
									map[string]interface{}{
										"apiVersion": "b/v1",
										"kind":       "B",
										"name":       "bar",
										"uid":        "2",
									},
								},
							},
						},
					},
				},
			},
			want: wantGraph{
				nodes: map[string]wantGraphItem{
					"1": { // the object: not virtual (observed), with 1 owner refs
						virtual: false,
						owners:  []string{"2"},
					},
					"2": { // the object owner: not virtual (observed), without owner refs
						virtual: false,
						owners:  nil,
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			graph := newObjectGraph(nil)
			for _, o := range tt.args.objs {
				graph.addObj(o, nil)
			}

			assertGraph(t, graph, tt.want)
		})
	}
}

type objectGraphTestArgs struct {
	objs []runtime.Object
}

var objectGraphsTests = []struct {
	name    string
	args    objectGraphTestArgs
	want    wantGraph
	wantErr bool
}{
	{
		name: "Cluster",
		args: objectGraphTestArgs{
			objs: test.NewFakeCluster("ns1", "cluster1").Objs(),
		},
		want: wantGraph{
			nodes: map[string]wantGraphItem{
				"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1": {},
				"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureCluster, ns1/cluster1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1",
					},
				},
				"/v1, Kind=Secret, ns1/cluster1-ca": {
					softOwners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1", //NB. this secret is not linked to the cluster through owner ref
					},
				},
				"/v1, Kind=Secret, ns1/cluster1-kubeconfig": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1",
					},
				},
			},
		},
	},
	{
		name: "Two clusters",
		args: objectGraphTestArgs{
			objs: func() []runtime.Object {
				objs := []runtime.Object{}
				objs = append(objs, test.NewFakeCluster("ns1", "cluster1").Objs()...)
				objs = append(objs, test.NewFakeCluster("ns1", "cluster2").Objs()...)
				return objs
			}(),
		},
		want: wantGraph{
			nodes: map[string]wantGraphItem{
				"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1": {},
				"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureCluster, ns1/cluster1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1",
					},
				},
				"/v1, Kind=Secret, ns1/cluster1-ca": {
					softOwners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1", //NB. this secret is not linked to the cluster through owner ref
					},
				},
				"/v1, Kind=Secret, ns1/cluster1-kubeconfig": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1",
					},
				},
				"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster2": {},
				"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureCluster, ns1/cluster2": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster2",
					},
				},
				"/v1, Kind=Secret, ns1/cluster2-ca": {
					softOwners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster2", //NB. this secret is not linked to the cluster through owner ref
					},
				},
				"/v1, Kind=Secret, ns1/cluster2-kubeconfig": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster2",
					},
				},
			},
		},
	},
	{
		name: "Cluster with machine",
		args: objectGraphTestArgs{
			objs: test.NewFakeCluster("ns1", "cluster1").
				WithMachines(
					test.NewFakeMachine("m1"),
				).Objs(),
		},
		want: wantGraph{
			nodes: map[string]wantGraphItem{
				"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1": {},
				"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureCluster, ns1/cluster1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1",
					},
				},
				"/v1, Kind=Secret, ns1/cluster1-ca": {
					softOwners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1", //NB. this secret is not linked to the cluster through owner ref
					},
				},
				"/v1, Kind=Secret, ns1/cluster1-kubeconfig": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1",
					},
				},

				"cluster.x-k8s.io/v1alpha3, Kind=Machine, ns1/m1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1",
					},
				},
				"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureMachine, ns1/m1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Machine, ns1/m1",
					},
				},
				"bootstrap.cluster.x-k8s.io/v1alpha3, Kind=DummyBootstrapConfig, ns1/m1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Machine, ns1/m1",
					},
				},
				"/v1, Kind=Secret, ns1/m1": {
					owners: []string{
						"bootstrap.cluster.x-k8s.io/v1alpha3, Kind=DummyBootstrapConfig, ns1/m1",
					},
				},
				"/v1, Kind=Secret, ns1/cluster1-sa": {
					owners: []string{
						"bootstrap.cluster.x-k8s.io/v1alpha3, Kind=DummyBootstrapConfig, ns1/m1",
					},
				},
			},
		},
	},
	{
		name: "Cluster with MachineSet",
		args: objectGraphTestArgs{
			objs: test.NewFakeCluster("ns1", "cluster1").
				WithMachineSets(
					test.NewFakeMachineSet("ms1").
						WithMachines(test.NewFakeMachine("m1")),
				).Objs(),
		},
		want: wantGraph{
			nodes: map[string]wantGraphItem{
				"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1": {},
				"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureCluster, ns1/cluster1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1",
					},
				},
				"/v1, Kind=Secret, ns1/cluster1-ca": {
					softOwners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1", //NB. this secret is not linked to the cluster through owner ref
					},
				},
				"/v1, Kind=Secret, ns1/cluster1-kubeconfig": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1",
					},
				},

				"cluster.x-k8s.io/v1alpha3, Kind=MachineSet, ns1/ms1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1",
					},
				},
				"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureMachineTemplate, ns1/ms1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1",
					},
				},
				"bootstrap.cluster.x-k8s.io/v1alpha3, Kind=DummyBootstrapConfigTemplate, ns1/ms1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1",
					},
				},

				"cluster.x-k8s.io/v1alpha3, Kind=Machine, ns1/m1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=MachineSet, ns1/ms1",
					},
				},
				"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureMachine, ns1/m1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Machine, ns1/m1",
					},
				},
				"bootstrap.cluster.x-k8s.io/v1alpha3, Kind=DummyBootstrapConfig, ns1/m1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Machine, ns1/m1",
					},
				},
				"/v1, Kind=Secret, ns1/m1": {
					owners: []string{
						"bootstrap.cluster.x-k8s.io/v1alpha3, Kind=DummyBootstrapConfig, ns1/m1",
					},
				},
			},
		},
	},
	{
		name: "Cluster with MachineDeployment",
		args: objectGraphTestArgs{
			objs: test.NewFakeCluster("ns1", "cluster1").
				WithMachineDeployments(
					test.NewFakeMachineDeployment("md1").
						WithMachineSets(
							test.NewFakeMachineSet("ms1").
								WithMachines(
									test.NewFakeMachine("m1"),
								),
						),
				).Objs(),
		},
		want: wantGraph{
			nodes: map[string]wantGraphItem{
				"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1": {},
				"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureCluster, ns1/cluster1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1",
					},
				},
				"/v1, Kind=Secret, ns1/cluster1-ca": {
					softOwners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1", //NB. this secret is not linked to the cluster through owner ref
					},
				},
				"/v1, Kind=Secret, ns1/cluster1-kubeconfig": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1",
					},
				},

				"cluster.x-k8s.io/v1alpha3, Kind=MachineDeployment, ns1/md1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1",
					},
				},
				"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureMachineTemplate, ns1/md1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1",
					},
				},
				"bootstrap.cluster.x-k8s.io/v1alpha3, Kind=DummyBootstrapConfigTemplate, ns1/md1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1",
					},
				},

				"cluster.x-k8s.io/v1alpha3, Kind=MachineSet, ns1/ms1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=MachineDeployment, ns1/md1",
					},
				},

				"cluster.x-k8s.io/v1alpha3, Kind=Machine, ns1/m1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=MachineSet, ns1/ms1",
					},
				},
				"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureMachine, ns1/m1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Machine, ns1/m1",
					},
				},
				"bootstrap.cluster.x-k8s.io/v1alpha3, Kind=DummyBootstrapConfig, ns1/m1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Machine, ns1/m1",
					},
				},
				"/v1, Kind=Secret, ns1/m1": {
					owners: []string{
						"bootstrap.cluster.x-k8s.io/v1alpha3, Kind=DummyBootstrapConfig, ns1/m1",
					},
				},
			},
		},
	},
	{
		name: "Cluster with Control Plane",
		args: objectGraphTestArgs{
			objs: test.NewFakeCluster("ns1", "cluster1").
				WithControlPlane(
					test.NewFakeControlPlane("cp1").
						WithMachines(
							test.NewFakeMachine("m1"),
						),
				).Objs(),
		},
		want: wantGraph{
			nodes: map[string]wantGraphItem{
				"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1": {},
				"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureCluster, ns1/cluster1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1",
					},
				},
				"/v1, Kind=Secret, ns1/cluster1-ca": {
					softOwners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1", //NB. this secret is not linked to the cluster through owner ref
					},
				},

				"controlplane.cluster.x-k8s.io/v1alpha3, Kind=DummyControlPlane, ns1/cp1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1",
					},
				},
				"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureMachineTemplate, ns1/cp1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1",
					},
				},
				"/v1, Kind=Secret, ns1/cluster1-sa": {
					owners: []string{
						"controlplane.cluster.x-k8s.io/v1alpha3, Kind=DummyControlPlane, ns1/cp1",
					},
				},
				"/v1, Kind=Secret, ns1/cluster1-kubeconfig": {
					owners: []string{
						"controlplane.cluster.x-k8s.io/v1alpha3, Kind=DummyControlPlane, ns1/cp1",
					},
				},

				"cluster.x-k8s.io/v1alpha3, Kind=Machine, ns1/m1": {
					owners: []string{
						"controlplane.cluster.x-k8s.io/v1alpha3, Kind=DummyControlPlane, ns1/cp1",
					},
				},
				"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureMachine, ns1/m1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Machine, ns1/m1",
					},
				},
				"bootstrap.cluster.x-k8s.io/v1alpha3, Kind=DummyBootstrapConfig, ns1/m1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Machine, ns1/m1",
					},
				},
				"/v1, Kind=Secret, ns1/m1": {
					owners: []string{
						"bootstrap.cluster.x-k8s.io/v1alpha3, Kind=DummyBootstrapConfig, ns1/m1",
					},
				},
			},
		},
	},
	{
		name: "Cluster with MachinePool",
		args: objectGraphTestArgs{
			objs: test.NewFakeCluster("ns1", "cluster1").
				WithMachinePools(
					test.NewFakeMachinePool("mp1"),
				).Objs(),
		},
		want: wantGraph{
			nodes: map[string]wantGraphItem{
				"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1": {},
				"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureCluster, ns1/cluster1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1",
					},
				},
				"/v1, Kind=Secret, ns1/cluster1-ca": {
					softOwners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1", //NB. this secret is not linked to the cluster through owner ref
					},
				},
				"/v1, Kind=Secret, ns1/cluster1-kubeconfig": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1",
					},
				},

				"exp.cluster.x-k8s.io/v1alpha3, Kind=MachinePool, ns1/mp1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1",
					},
				},
				"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureMachineTemplate, ns1/mp1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1",
					},
				},
				"bootstrap.cluster.x-k8s.io/v1alpha3, Kind=DummyBootstrapConfigTemplate, ns1/mp1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1",
					},
				},
			},
		},
	},
	{
		name: "Two clusters with shared objects",
		args: objectGraphTestArgs{
			objs: func() []runtime.Object {
				sharedInfrastructureTemplate := test.NewFakeInfrastructureTemplate("shared")

				objs := []runtime.Object{
					sharedInfrastructureTemplate,
				}

				objs = append(objs, test.NewFakeCluster("ns1", "cluster1").
					WithMachineSets(
						test.NewFakeMachineSet("cluster1-ms1").
							WithInfrastructureTemplate(sharedInfrastructureTemplate).
							WithMachines(
								test.NewFakeMachine("cluster1-m1"),
							),
					).Objs()...)

				objs = append(objs, test.NewFakeCluster("ns1", "cluster2").
					WithMachineSets(
						test.NewFakeMachineSet("cluster2-ms1").
							WithInfrastructureTemplate(sharedInfrastructureTemplate).
							WithMachines(
								test.NewFakeMachine("cluster2-m1"),
							),
					).Objs()...)

				return objs
			}(),
		},
		want: wantGraph{
			nodes: map[string]wantGraphItem{

				"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureMachineTemplate, ns1/shared": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1",
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster2",
					},
				},

				"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1": {},
				"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureCluster, ns1/cluster1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1",
					},
				},
				"/v1, Kind=Secret, ns1/cluster1-ca": {
					softOwners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1", //NB. this secret is not linked to the cluster through owner ref
					},
				},
				"/v1, Kind=Secret, ns1/cluster1-kubeconfig": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1",
					},
				},

				"cluster.x-k8s.io/v1alpha3, Kind=MachineSet, ns1/cluster1-ms1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1",
					},
				},
				"bootstrap.cluster.x-k8s.io/v1alpha3, Kind=DummyBootstrapConfigTemplate, ns1/cluster1-ms1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1",
					},
				},

				"cluster.x-k8s.io/v1alpha3, Kind=Machine, ns1/cluster1-m1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=MachineSet, ns1/cluster1-ms1",
					},
				},
				"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureMachine, ns1/cluster1-m1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Machine, ns1/cluster1-m1",
					},
				},
				"bootstrap.cluster.x-k8s.io/v1alpha3, Kind=DummyBootstrapConfig, ns1/cluster1-m1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Machine, ns1/cluster1-m1",
					},
				},
				"/v1, Kind=Secret, ns1/cluster1-m1": {
					owners: []string{
						"bootstrap.cluster.x-k8s.io/v1alpha3, Kind=DummyBootstrapConfig, ns1/cluster1-m1",
					},
				},
				"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster2": {},
				"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureCluster, ns1/cluster2": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster2",
					},
				},
				"/v1, Kind=Secret, ns1/cluster2-ca": {
					softOwners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster2", //NB. this secret is not linked to the cluster through owner ref
					},
				},
				"/v1, Kind=Secret, ns1/cluster2-kubeconfig": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster2",
					},
				},

				"cluster.x-k8s.io/v1alpha3, Kind=MachineSet, ns1/cluster2-ms1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster2",
					},
				},
				"bootstrap.cluster.x-k8s.io/v1alpha3, Kind=DummyBootstrapConfigTemplate, ns1/cluster2-ms1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster2",
					},
				},

				"cluster.x-k8s.io/v1alpha3, Kind=Machine, ns1/cluster2-m1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=MachineSet, ns1/cluster2-ms1",
					},
				},
				"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureMachine, ns1/cluster2-m1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Machine, ns1/cluster2-m1",
					},
				},
				"bootstrap.cluster.x-k8s.io/v1alpha3, Kind=DummyBootstrapConfig, ns1/cluster2-m1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Machine, ns1/cluster2-m1",
					},
				},
				"/v1, Kind=Secret, ns1/cluster2-m1": {
					owners: []string{
						"bootstrap.cluster.x-k8s.io/v1alpha3, Kind=DummyBootstrapConfig, ns1/cluster2-m1",
					},
				},
			},
		},
	},
	{
		name: "Two cluster with the same principal",
		args: objectGraphTestArgs{
			objs: func() []runtime.Object {
				principal := test.NewInfrastructurePrincipal("principal")
				objs := []runtime.Object{principal}
				objs = append(objs, test.NewFakeCluster("ns1", "cluster1").WithPrincipal(principal).Objs()...)
				objs = append(objs, test.NewFakeCluster("ns1", "cluster2").WithPrincipal(principal).Objs()...)
				return objs
			}(),
		},
		want: wantGraph{
			nodes: map[string]wantGraphItem{
				"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1": {},
				"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureCluster, ns1/cluster1": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1",
						"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructurePrincipal, /principal", // the principal should be owner of the infrastructure cluster
					},
				},
				"/v1, Kind=Secret, ns1/cluster1-ca": {
					softOwners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1", //NB. this secret is not linked to the cluster through owner ref
					},
				},
				"/v1, Kind=Secret, ns1/cluster1-kubeconfig": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1",
					},
				},
				"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster2": {},
				"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureCluster, ns1/cluster2": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster2",
						"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructurePrincipal, /principal", // the principal should be owner of the infrastructure cluster
					},
				},
				"/v1, Kind=Secret, ns1/cluster2-ca": {
					softOwners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster2", //NB. this secret is not linked to the cluster through owner ref
					},
				},
				"/v1, Kind=Secret, ns1/cluster2-kubeconfig": {
					owners: []string{
						"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster2",
					},
				},
				"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructurePrincipal, /principal": {},
			},
		},
	},
}

func getDetachedObjectGraphWihObjs(objs []runtime.Object) (*objectGraph, error) {
	types := getDetachedTypesList()
	graph := newObjectGraph(nil) // detached from any cluster
	for _, o := range objs {
		u := &unstructured.Unstructured{}
		if err := test.FakeScheme.Convert(o, u, nil); err != nil {
			return nil, errors.Wrap(err, "failed to convert object in unstructured")
		}
		graph.addObj(u, types)
	}
	return graph, nil
}

func getDetachedTypesList() discoveryTypeMetas {
	t := discoveryTypeMetas{}
	for _, crd := range test.FakeCRDList() {
		for _, version := range crd.Spec.Versions {
			if !version.Storage {
				continue
			}
			t = append(t, discoveryTypeMeta{
				Kind: crd.Spec.Names.Kind,
				APIVersion: metav1.GroupVersion{
					Group:   crd.Spec.Group,
					Version: version.Name,
				}.String(),
				Scope: crd.Spec.Scope,
			})
		}
	}
	t = append(t, discoveryTypeMeta{Kind: "Secret", APIVersion: "v1", Scope: apiextensionsv1.NamespaceScoped})
	t = append(t, discoveryTypeMeta{Kind: "ConfigMap", APIVersion: "v1", Scope: apiextensionsv1.NamespaceScoped})
	return t
}

func TestObjectGraph_addObj_WithFakeObjects(t *testing.T) {
	// NB. we are testing the graph is properly built starting from objects (this test) or from the same objects read from the cluster (TestGraphBuilder_Discovery)
	for _, tt := range objectGraphsTests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			graph, err := getDetachedObjectGraphWihObjs(tt.args.objs)
			g.Expect(err).NotTo(HaveOccurred())

			// call setSoftOwnership so there is functional parity with discovery
			graph.setSoftOwnership()

			assertGraph(t, graph, tt.want)
		})
	}
}

func getObjectGraphWithObjs(objs []runtime.Object) *objectGraph {
	fromProxy := getFakeProxyWithCRDs()

	for _, o := range objs {
		fromProxy.WithObjs(o)
	}

	return newObjectGraph(fromProxy)
}

func getFakeProxyWithCRDs() *test.FakeProxy {
	proxy := test.NewFakeProxy()
	for _, o := range test.FakeCRDList() {
		proxy.WithObjs(o)
	}
	return proxy
}

func getFakeDiscoveryTypes(graph *objectGraph) ([]discoveryTypeMeta, error) {
	discoveryTypes, err := graph.getDiscoveryTypes()
	if err != nil {
		return nil, err
	}

	// Given that the Fake client behaves in a different way than real client, for this test we are required to add the List suffix to all the types.
	for i := range discoveryTypes {
		discoveryTypes[i].Kind = fmt.Sprintf("%sList", discoveryTypes[i].Kind)
	}
	return discoveryTypes, nil
}

func TestObjectGraph_Discovery(t *testing.T) {
	// NB. we are testing the graph is properly built starting from objects (TestGraphBuilder_addObj_WithFakeObjects) or from the same objects read from the cluster (this test).
	for _, tt := range objectGraphsTests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			// Create an objectGraph bound to a source cluster with all the CRDs for the types involved in the test.
			graph := getObjectGraphWithObjs(tt.args.objs)

			// Get all the types to be considered for discovery
			discoveryTypes, err := getFakeDiscoveryTypes(graph)
			g.Expect(err).NotTo(HaveOccurred())

			// finally test discovery
			err = graph.Discovery("ns1", discoveryTypes)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}

			g.Expect(err).NotTo(HaveOccurred())
			assertGraph(t, graph, tt.want)
		})
	}
}

func TestObjectGraph_DiscoveryByNamespace(t *testing.T) {
	type args struct {
		namespace string
		objs      []runtime.Object
	}
	var tests = []struct {
		name    string
		args    args
		want    wantGraph
		wantErr bool
	}{
		{
			name: "two clusters, in different namespaces, read both",
			args: args{
				namespace: "", // read all the namespaces
				objs: func() []runtime.Object {
					objs := []runtime.Object{}
					objs = append(objs, test.NewFakeCluster("ns1", "cluster1").Objs()...)
					objs = append(objs, test.NewFakeCluster("ns2", "cluster1").Objs()...)
					return objs
				}(),
			},
			want: wantGraph{
				nodes: map[string]wantGraphItem{
					"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1": {},
					"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureCluster, ns1/cluster1": {
						owners: []string{
							"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1",
						},
					},
					"/v1, Kind=Secret, ns1/cluster1-ca": {
						softOwners: []string{
							"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1", //NB. this secret is not linked to the cluster through owner ref
						},
					},
					"/v1, Kind=Secret, ns1/cluster1-kubeconfig": {
						owners: []string{
							"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1",
						},
					},
					"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns2/cluster1": {},
					"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureCluster, ns2/cluster1": {
						owners: []string{
							"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns2/cluster1",
						},
					},
					"/v1, Kind=Secret, ns2/cluster1-ca": {
						softOwners: []string{
							"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns2/cluster1", //NB. this secret is not linked to the cluster through owner ref
						},
					},
					"/v1, Kind=Secret, ns2/cluster1-kubeconfig": {
						owners: []string{
							"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns2/cluster1",
						},
					},
				},
			},
		},
		{
			name: "two clusters, in different namespaces, read only 1",
			args: args{
				namespace: "ns1", // read only from ns1
				objs: func() []runtime.Object {
					objs := []runtime.Object{}
					objs = append(objs, test.NewFakeCluster("ns1", "cluster1").Objs()...)
					objs = append(objs, test.NewFakeCluster("ns2", "cluster1").Objs()...)
					return objs
				}(),
			},
			want: wantGraph{
				nodes: map[string]wantGraphItem{
					"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1": {},
					"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureCluster, ns1/cluster1": {
						owners: []string{
							"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1",
						},
					},
					"/v1, Kind=Secret, ns1/cluster1-ca": {
						softOwners: []string{
							"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1", //NB. this secret is not linked to the cluster through owner ref
						},
					},
					"/v1, Kind=Secret, ns1/cluster1-kubeconfig": {
						owners: []string{
							"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1",
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			// Create an objectGraph bound to a source cluster with all the CRDs for the types involved in the test.
			graph := getObjectGraphWithObjs(tt.args.objs)

			// Get all the types to be considered for discovery
			discoveryTypes, err := getFakeDiscoveryTypes(graph)
			g.Expect(err).NotTo(HaveOccurred())

			// finally test discovery
			err = graph.Discovery(tt.args.namespace, discoveryTypes)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}

			g.Expect(err).NotTo(HaveOccurred())
			assertGraph(t, graph, tt.want)
		})
	}
}

func Test_objectGraph_setSoftOwnership(t *testing.T) {
	type fields struct {
		objs []runtime.Object
	}
	tests := []struct {
		name        string
		fields      fields
		wantSecrets map[string][]string
	}{
		{
			name: "A cluster with a soft owned secret",
			fields: fields{
				objs: test.NewFakeCluster("ns1", "foo").Objs(),
			},
			wantSecrets: map[string][]string{ // wantSecrets is a map[node UID] --> list of soft owner UIDs
				"/v1, Kind=Secret, ns1/foo-ca": { // the ca secret has no explicit OwnerRef to the cluster, so it should be identified as a soft ownership
					"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/foo",
				},
				"/v1, Kind=Secret, ns1/foo-kubeconfig": {}, // the kubeconfig secret has explicit OwnerRef to the cluster, so it should NOT be identified as a soft ownership
			},
		},
		{
			name: "A cluster with a soft owned secret (cluster name with - in the middle)",
			fields: fields{
				objs: test.NewFakeCluster("ns1", "foo-bar").Objs(),
			},
			wantSecrets: map[string][]string{ // wantSecrets is a map[node UID] --> list of soft owner UIDs
				"/v1, Kind=Secret, ns1/foo-bar-ca": { // the ca secret has no explicit OwnerRef to the cluster, so it should be identified as a soft ownership
					"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/foo-bar",
				},
				"/v1, Kind=Secret, ns1/foo-bar-kubeconfig": {}, // the kubeconfig secret has explicit OwnerRef to the cluster, so it should NOT be identified as a soft ownership
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			graph, err := getDetachedObjectGraphWihObjs(tt.fields.objs)
			g.Expect(err).NotTo(HaveOccurred())

			graph.setSoftOwnership()

			gotSecrets := graph.getSecrets()
			g.Expect(gotSecrets).To(HaveLen(len(tt.wantSecrets)))

			for _, secret := range gotSecrets {
				wantObjects, ok := tt.wantSecrets[string(secret.identity.UID)]
				g.Expect(ok).To(BeTrue())

				gotObjects := []string{}
				for softOwners := range secret.softOwners {
					gotObjects = append(gotObjects, string(softOwners.identity.UID))
				}

				g.Expect(gotObjects).To(ConsistOf(wantObjects))
			}
		})
	}
}

func Test_objectGraph_setClusterTenants(t *testing.T) {
	type fields struct {
		objs []runtime.Object
	}
	tests := []struct {
		name         string
		fields       fields
		wantClusters map[string][]string
	}{
		{
			name: "One cluster",
			fields: fields{
				objs: test.NewFakeCluster("ns1", "foo").Objs(),
			},
			wantClusters: map[string][]string{ // wantClusters is a map[Cluster.UID] --> list of UIDs
				"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/foo": {
					"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/foo", // the cluster should be tenant of itself
					"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureCluster, ns1/foo",
					"/v1, Kind=Secret, ns1/foo-ca", // the ca secret is a soft owned
					"/v1, Kind=Secret, ns1/foo-kubeconfig",
				},
			},
		},
		{
			name: "Object not owned by a cluster should be ignored",
			fields: fields{
				objs: func() []runtime.Object {
					objs := []runtime.Object{}
					objs = append(objs, test.NewFakeCluster("ns1", "foo").Objs()...)
					objs = append(objs, test.NewFakeInfrastructureTemplate("orphan")) // orphan object, not owned by  any cluster
					return objs
				}(),
			},
			wantClusters: map[string][]string{ // wantClusters is a map[Cluster.UID] --> list of UIDs
				"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/foo": {
					"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/foo", // the cluster should be tenant of itself
					"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureCluster, ns1/foo",
					"/v1, Kind=Secret, ns1/foo-ca", // the ca secret is a soft owned
					"/v1, Kind=Secret, ns1/foo-kubeconfig",
				},
			},
		},
		{
			name: "Two clusters",
			fields: fields{
				objs: func() []runtime.Object {
					objs := []runtime.Object{}
					objs = append(objs, test.NewFakeCluster("ns1", "foo").Objs()...)
					objs = append(objs, test.NewFakeCluster("ns1", "bar").Objs()...)
					return objs
				}(),
			},
			wantClusters: map[string][]string{ // wantClusters is a map[Cluster.UID] --> list of UIDs
				"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/foo": {
					"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/foo", // the cluster should be tenant of itself
					"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureCluster, ns1/foo",
					"/v1, Kind=Secret, ns1/foo-ca", // the ca secret is a soft owned
					"/v1, Kind=Secret, ns1/foo-kubeconfig",
				},
				"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/bar": {
					"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/bar", // the cluster should be tenant of itself
					"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureCluster, ns1/bar",
					"/v1, Kind=Secret, ns1/bar-ca", // the ca secret is a soft owned
					"/v1, Kind=Secret, ns1/bar-kubeconfig",
				},
			},
		},
		{
			name: "Two clusters with a shared object",
			fields: fields{
				objs: func() []runtime.Object {
					sharedInfrastructureTemplate := test.NewFakeInfrastructureTemplate("shared")

					objs := []runtime.Object{
						sharedInfrastructureTemplate,
					}

					objs = append(objs, test.NewFakeCluster("ns1", "cluster1").
						WithMachineSets(
							test.NewFakeMachineSet("cluster1-ms1").
								WithInfrastructureTemplate(sharedInfrastructureTemplate).
								WithMachines(
									test.NewFakeMachine("cluster1-m1"),
								),
						).Objs()...)

					objs = append(objs, test.NewFakeCluster("ns1", "cluster2").
						WithMachineSets(
							test.NewFakeMachineSet("cluster2-ms1").
								WithInfrastructureTemplate(sharedInfrastructureTemplate).
								WithMachines(
									test.NewFakeMachine("cluster2-m1"),
								),
						).Objs()...)

					return objs
				}(),
			},
			wantClusters: map[string][]string{ // wantClusters is a map[Cluster.UID] --> list of UIDs
				"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1": {
					"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureMachineTemplate, ns1/shared", // the shared object should be in both lists
					"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1",                                         // the cluster should be tenant of itself
					"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureCluster, ns1/cluster1",
					"/v1, Kind=Secret, ns1/cluster1-ca", // the ca secret is a soft owned
					"/v1, Kind=Secret, ns1/cluster1-kubeconfig",
					"cluster.x-k8s.io/v1alpha3, Kind=MachineSet, ns1/cluster1-ms1",
					"bootstrap.cluster.x-k8s.io/v1alpha3, Kind=DummyBootstrapConfigTemplate, ns1/cluster1-ms1",
					"cluster.x-k8s.io/v1alpha3, Kind=Machine, ns1/cluster1-m1",
					"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureMachine, ns1/cluster1-m1",
					"bootstrap.cluster.x-k8s.io/v1alpha3, Kind=DummyBootstrapConfig, ns1/cluster1-m1",
					"/v1, Kind=Secret, ns1/cluster1-m1",
				},
				"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster2": {
					"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureMachineTemplate, ns1/shared", // the shared object should be in both lists
					"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster2",                                         // the cluster should be tenant of itself
					"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureCluster, ns1/cluster2",
					"/v1, Kind=Secret, ns1/cluster2-ca", // the ca secret is a soft owned
					"/v1, Kind=Secret, ns1/cluster2-kubeconfig",
					"cluster.x-k8s.io/v1alpha3, Kind=MachineSet, ns1/cluster2-ms1",
					"bootstrap.cluster.x-k8s.io/v1alpha3, Kind=DummyBootstrapConfigTemplate, ns1/cluster2-ms1",
					"cluster.x-k8s.io/v1alpha3, Kind=Machine, ns1/cluster2-m1",
					"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureMachine, ns1/cluster2-m1",
					"bootstrap.cluster.x-k8s.io/v1alpha3, Kind=DummyBootstrapConfig, ns1/cluster2-m1",
					"/v1, Kind=Secret, ns1/cluster2-m1",
				},
			},
		},
		{
			name: "Two cluster with the same principal",
			fields: fields{
				objs: func() []runtime.Object {
					principal := test.NewInfrastructurePrincipal("principal")
					objs := []runtime.Object{principal}
					objs = append(objs, test.NewFakeCluster("ns1", "cluster1").WithPrincipal(principal).Objs()...)
					objs = append(objs, test.NewFakeCluster("ns1", "cluster2").WithPrincipal(principal).Objs()...)
					return objs
				}(),
			},
			wantClusters: map[string][]string{ // wantClusters is a map[Cluster.UID] --> list of UIDs
				"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1": {
					"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster1", // the cluster should be tenant of itself
					"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureCluster, ns1/cluster1",
					"/v1, Kind=Secret, ns1/cluster1-ca", // the ca secret is a soft owned
					"/v1, Kind=Secret, ns1/cluster1-kubeconfig",
					"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructurePrincipal, /principal", // the principal should list both clusters as tenants
				},
				"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster2": {
					"cluster.x-k8s.io/v1alpha3, Kind=Cluster, ns1/cluster2", // the cluster should be tenant of itself
					"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructureCluster, ns1/cluster2",
					"/v1, Kind=Secret, ns1/cluster2-ca", // the ca secret is a soft owned
					"/v1, Kind=Secret, ns1/cluster2-kubeconfig",
					"infrastructure.cluster.x-k8s.io/v1alpha3, Kind=DummyInfrastructurePrincipal, /principal", // the principal should list both clusters as tenants
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			gb, err := getDetachedObjectGraphWihObjs(tt.fields.objs)
			g.Expect(err).NotTo(HaveOccurred())

			// we want to check that soft dependent nodes are considered part of the cluster, so we make sure to call SetSoftDependants before SetClusterTenants
			gb.setSoftOwnership()

			// SetClusterTenants
			gb.setClusterTenants()

			// we want to check that principal are considered part of the cluster, so we make sure to call setClusterPrincipalsTenants
			gb.setClusterPrincipalsTenants()

			gotClusters := gb.getClusters()
			sort.Slice(gotClusters, func(i, j int) bool {
				return gotClusters[i].identity.UID < gotClusters[j].identity.UID
			})

			g.Expect(gotClusters).To(HaveLen(len(tt.wantClusters)))

			for _, cluster := range gotClusters {
				wantTenants, ok := tt.wantClusters[string(cluster.identity.UID)]
				g.Expect(ok).To(BeTrue())

				gotTenants := []string{}
				for _, node := range gb.uidToNode {
					for c := range node.tenantClusters {
						if c.identity.UID == cluster.identity.UID {
							gotTenants = append(gotTenants, string(node.identity.UID))
						}
					}
				}

				g.Expect(gotTenants).To(ConsistOf(wantTenants))
			}
		})
	}
}
