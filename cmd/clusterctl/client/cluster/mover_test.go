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
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	clusterctlv1 "sigs.k8s.io/cluster-api/cmd/clusterctl/api/v1alpha3"
	"sigs.k8s.io/cluster-api/cmd/clusterctl/internal/test"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type moveTestsFields struct {
	objs []client.Object
}

var moveTests = []struct {
	name           string
	fields         moveTestsFields
	wantMoveGroups [][]string
	wantErr        bool
}{
	{
		name: "Cluster",
		fields: moveTestsFields{
			objs: test.NewFakeCluster("ns1", "foo").Objs(),
		},
		wantMoveGroups: [][]string{
			{ // group 1
				"cluster.x-k8s.io/v1alpha4, Kind=Cluster, ns1/foo",
			},
			{ // group 2 (objects with ownerReferences in group 1)
				// owned by Clusters
				"/v1, Kind=Secret, ns1/foo-ca",
				"/v1, Kind=Secret, ns1/foo-kubeconfig",
				"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureCluster, ns1/foo",
			},
		},
		wantErr: false,
	},
	{
		name: "Cluster with external objects marked with move label",
		fields: moveTestsFields{
			objs: test.NewFakeCluster("ns1", "foo").WithCloudConfigSecret().Objs(),
		},
		wantMoveGroups: [][]string{
			{ // group 1
				"cluster.x-k8s.io/v1alpha4, Kind=Cluster, ns1/foo",
				// objects with force move flag
				"/v1, Kind=Secret, ns1/foo-cloud-config",
			},
			{ // group 2 (objects with ownerReferences in group 1)
				// owned by Clusters
				"/v1, Kind=Secret, ns1/foo-ca",
				"/v1, Kind=Secret, ns1/foo-kubeconfig",
				"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureCluster, ns1/foo",
			},
		},
		wantErr: false,
	},
	{
		name: "Cluster with machine",
		fields: moveTestsFields{
			objs: test.NewFakeCluster("ns1", "cluster1").
				WithMachines(
					test.NewFakeMachine("m1"),
					test.NewFakeMachine("m2"),
				).Objs(),
		},
		wantMoveGroups: [][]string{
			{ // group 1
				"cluster.x-k8s.io/v1alpha4, Kind=Cluster, ns1/cluster1",
			},
			{ // group 2 (objects with ownerReferences in group 1)
				// owned by Clusters
				"/v1, Kind=Secret, ns1/cluster1-kubeconfig",
				"/v1, Kind=Secret, ns1/cluster1-ca",
				"cluster.x-k8s.io/v1alpha4, Kind=Machine, ns1/m1",
				"cluster.x-k8s.io/v1alpha4, Kind=Machine, ns1/m2",
				"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureCluster, ns1/cluster1",
			},
			{ // group 3 (objects with ownerReferences in group 1,2)
				// owned by Machines
				"bootstrap.cluster.x-k8s.io/v1alpha4, Kind=GenericBootstrapConfig, ns1/m1",
				"bootstrap.cluster.x-k8s.io/v1alpha4, Kind=GenericBootstrapConfig, ns1/m2",
				"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureMachine, ns1/m1",
				"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureMachine, ns1/m2",
			},
			{ // group 4 (objects with ownerReferences in group 1,2,3)
				// owned by GenericBootstrapConfigs
				"/v1, Kind=Secret, ns1/cluster1-sa",
				"/v1, Kind=Secret, ns1/m1",
				"/v1, Kind=Secret, ns1/m2",
			},
		},
		wantErr: false,
	},
	{
		name: "Cluster with MachineSet",
		fields: moveTestsFields{
			objs: test.NewFakeCluster("ns1", "cluster1").
				WithMachineSets(
					test.NewFakeMachineSet("ms1").
						WithMachines(
							test.NewFakeMachine("m1"),
							test.NewFakeMachine("m2"),
						),
				).Objs(),
		},
		wantMoveGroups: [][]string{
			{ // group 1
				"cluster.x-k8s.io/v1alpha4, Kind=Cluster, ns1/cluster1",
			},
			{ // group 2 (objects with ownerReferences in group 1)
				// owned by Clusters
				"/v1, Kind=Secret, ns1/cluster1-ca",
				"/v1, Kind=Secret, ns1/cluster1-kubeconfig",
				"bootstrap.cluster.x-k8s.io/v1alpha4, Kind=GenericBootstrapConfigTemplate, ns1/ms1",
				"cluster.x-k8s.io/v1alpha4, Kind=MachineSet, ns1/ms1",
				"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureCluster, ns1/cluster1",
				"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureMachineTemplate, ns1/ms1",
			},
			{ // group 3 (objects with ownerReferences in group 1,2)
				// owned by MachineSets
				"cluster.x-k8s.io/v1alpha4, Kind=Machine, ns1/m1",
				"cluster.x-k8s.io/v1alpha4, Kind=Machine, ns1/m2",
			},
			{ // group 4 (objects with ownerReferences in group 1,2,3)
				// owned by Machines
				"bootstrap.cluster.x-k8s.io/v1alpha4, Kind=GenericBootstrapConfig, ns1/m1",
				"bootstrap.cluster.x-k8s.io/v1alpha4, Kind=GenericBootstrapConfig, ns1/m2",
				"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureMachine, ns1/m1",
				"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureMachine, ns1/m2",
			},
			{ // group 5 (objects with ownerReferences in group 1,2,3,4)
				// owned by GenericBootstrapConfigs
				"/v1, Kind=Secret, ns1/m1",
				"/v1, Kind=Secret, ns1/m2",
			},
		},
		wantErr: false,
	},
	{
		name: "Cluster with MachineDeployment",
		fields: moveTestsFields{
			objs: test.NewFakeCluster("ns1", "cluster1").
				WithMachineDeployments(
					test.NewFakeMachineDeployment("md1").
						WithMachineSets(
							test.NewFakeMachineSet("ms1").
								WithMachines(
									test.NewFakeMachine("m1"),
									test.NewFakeMachine("m2"),
								),
						),
				).Objs(),
		},
		wantMoveGroups: [][]string{
			{ // group 1
				"cluster.x-k8s.io/v1alpha4, Kind=Cluster, ns1/cluster1",
			},
			{ // group 2 (objects with ownerReferences in group 1)
				// owned by Clusters
				"/v1, Kind=Secret, ns1/cluster1-ca",
				"/v1, Kind=Secret, ns1/cluster1-kubeconfig",
				"bootstrap.cluster.x-k8s.io/v1alpha4, Kind=GenericBootstrapConfigTemplate, ns1/md1",
				"cluster.x-k8s.io/v1alpha4, Kind=MachineDeployment, ns1/md1",
				"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureCluster, ns1/cluster1",
				"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureMachineTemplate, ns1/md1",
			},
			{ // group 3 (objects with ownerReferences in group 1,2)
				// owned by MachineDeployments
				"cluster.x-k8s.io/v1alpha4, Kind=MachineSet, ns1/ms1",
			},
			{ // group 4 (objects with ownerReferences in group 1,2,3)
				// owned by MachineSets
				"cluster.x-k8s.io/v1alpha4, Kind=Machine, ns1/m1",
				"cluster.x-k8s.io/v1alpha4, Kind=Machine, ns1/m2",
			},
			{ // group 5 (objects with ownerReferences in group 1,2,3,4)
				// owned by Machines
				"bootstrap.cluster.x-k8s.io/v1alpha4, Kind=GenericBootstrapConfig, ns1/m1",
				"bootstrap.cluster.x-k8s.io/v1alpha4, Kind=GenericBootstrapConfig, ns1/m2",
				"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureMachine, ns1/m1",
				"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureMachine, ns1/m2",
			},
			{ // group 6 (objects with ownerReferences in group 1,2,3,5,6)
				// owned by GenericBootstrapConfigs
				"/v1, Kind=Secret, ns1/m1",
				"/v1, Kind=Secret, ns1/m2",
			},
		},
		wantErr: false,
	},
	{
		name: "Cluster with Control Plane",
		fields: moveTestsFields{
			objs: test.NewFakeCluster("ns1", "cluster1").
				WithControlPlane(
					test.NewFakeControlPlane("cp1").
						WithMachines(
							test.NewFakeMachine("m1"),
							test.NewFakeMachine("m2"),
						),
				).Objs(),
		},
		wantMoveGroups: [][]string{
			{ // group 1
				"cluster.x-k8s.io/v1alpha4, Kind=Cluster, ns1/cluster1",
			},
			{ // group 2 (objects with ownerReferences in group 1)
				// owned by Clusters
				"/v1, Kind=Secret, ns1/cluster1-ca",
				"controlplane.cluster.x-k8s.io/v1alpha4, Kind=GenericControlPlane, ns1/cp1",
				"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureCluster, ns1/cluster1",
				"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureMachineTemplate, ns1/cp1",
			},
			{ // group 3 (objects with ownerReferences in group 1,2)
				"/v1, Kind=Secret, ns1/cluster1-kubeconfig",
				"/v1, Kind=Secret, ns1/cluster1-sa",
				"cluster.x-k8s.io/v1alpha4, Kind=Machine, ns1/m1",
				"cluster.x-k8s.io/v1alpha4, Kind=Machine, ns1/m2",
			},
			{ // group 4 (objects with ownerReferences in group 1,2,3)
				// owned by Machines
				"bootstrap.cluster.x-k8s.io/v1alpha4, Kind=GenericBootstrapConfig, ns1/m1",
				"bootstrap.cluster.x-k8s.io/v1alpha4, Kind=GenericBootstrapConfig, ns1/m2",
				"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureMachine, ns1/m1",
				"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureMachine, ns1/m2",
			},
			{ // group 5 (objects with ownerReferences in group 1,2,3,4)
				// owned by GenericBootstrapConfigs
				"/v1, Kind=Secret, ns1/m1",
				"/v1, Kind=Secret, ns1/m2",
			},
		},
		wantErr: false,
	},
	{
		name: "Cluster with MachinePool",
		fields: moveTestsFields{
			objs: test.NewFakeCluster("ns1", "cluster1").
				WithMachinePools(
					test.NewFakeMachinePool("mp1"),
				).Objs(),
		},
		wantMoveGroups: [][]string{
			{ // group 1
				"cluster.x-k8s.io/v1alpha4, Kind=Cluster, ns1/cluster1",
			},
			{ // group 2 (objects with ownerReferences in group 1)
				// owned by Clusters
				"/v1, Kind=Secret, ns1/cluster1-ca",
				"/v1, Kind=Secret, ns1/cluster1-kubeconfig",
				"bootstrap.cluster.x-k8s.io/v1alpha4, Kind=GenericBootstrapConfigTemplate, ns1/mp1",
				"cluster.x-k8s.io/v1alpha4, Kind=MachinePool, ns1/mp1",
				"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureCluster, ns1/cluster1",
				"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureMachineTemplate, ns1/mp1",
			},
		},
		wantErr: false,
	},
	{
		name: "Two clusters",
		fields: moveTestsFields{
			objs: func() []client.Object {
				objs := []client.Object{}
				objs = append(objs, test.NewFakeCluster("ns1", "foo").Objs()...)
				objs = append(objs, test.NewFakeCluster("ns1", "bar").Objs()...)
				return objs
			}(),
		},
		wantMoveGroups: [][]string{
			{ // group 1
				"cluster.x-k8s.io/v1alpha4, Kind=Cluster, ns1/foo",
				"cluster.x-k8s.io/v1alpha4, Kind=Cluster, ns1/bar",
			},
			{ // group 2 (objects with ownerReferences in group 1)
				// owned by Clusters
				"/v1, Kind=Secret, ns1/foo-ca",
				"/v1, Kind=Secret, ns1/foo-kubeconfig",
				"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureCluster, ns1/foo",
				"/v1, Kind=Secret, ns1/bar-ca",
				"/v1, Kind=Secret, ns1/bar-kubeconfig",
				"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureCluster, ns1/bar",
			},
		},
	},
	{
		name: "Two clusters with a shared object",
		fields: moveTestsFields{
			objs: func() []client.Object {
				sharedInfrastructureTemplate := test.NewFakeInfrastructureTemplate("shared")

				objs := []client.Object{
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
		wantMoveGroups: [][]string{
			{ // group 1
				"cluster.x-k8s.io/v1alpha4, Kind=Cluster, ns1/cluster1",
				"cluster.x-k8s.io/v1alpha4, Kind=Cluster, ns1/cluster2",
			},
			{ // group 2 (objects with ownerReferences in group 1)
				// owned by Clusters
				"/v1, Kind=Secret, ns1/cluster1-ca",
				"/v1, Kind=Secret, ns1/cluster1-kubeconfig",
				"bootstrap.cluster.x-k8s.io/v1alpha4, Kind=GenericBootstrapConfigTemplate, ns1/cluster1-ms1",
				"cluster.x-k8s.io/v1alpha4, Kind=MachineSet, ns1/cluster1-ms1",
				"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureCluster, ns1/cluster1",
				"/v1, Kind=Secret, ns1/cluster2-ca",
				"/v1, Kind=Secret, ns1/cluster2-kubeconfig",
				"bootstrap.cluster.x-k8s.io/v1alpha4, Kind=GenericBootstrapConfigTemplate, ns1/cluster2-ms1",
				"cluster.x-k8s.io/v1alpha4, Kind=MachineSet, ns1/cluster2-ms1",
				"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureCluster, ns1/cluster2",
				"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureMachineTemplate, ns1/shared", // shared object
			},
			{ // group 3 (objects with ownerReferences in group 1,2)
				// owned by MachineSets
				"cluster.x-k8s.io/v1alpha4, Kind=Machine, ns1/cluster1-m1",
				"cluster.x-k8s.io/v1alpha4, Kind=Machine, ns1/cluster2-m1",
			},
			{ // group 4 (objects with ownerReferences in group 1,2,3)
				// owned by Machines
				"bootstrap.cluster.x-k8s.io/v1alpha4, Kind=GenericBootstrapConfig, ns1/cluster1-m1",
				"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureMachine, ns1/cluster1-m1",
				"bootstrap.cluster.x-k8s.io/v1alpha4, Kind=GenericBootstrapConfig, ns1/cluster2-m1",
				"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureMachine, ns1/cluster2-m1",
			},
			{ // group 5 (objects with ownerReferences in group 1,2,3,4)
				// owned by GenericBootstrapConfigs
				"/v1, Kind=Secret, ns1/cluster1-m1",
				"/v1, Kind=Secret, ns1/cluster2-m1",
			},
		},
	},
	{
		name: "A ClusterResourceSet applied to a cluster",
		fields: moveTestsFields{
			objs: func() []client.Object {
				objs := []client.Object{}
				objs = append(objs, test.NewFakeCluster("ns1", "cluster1").Objs()...)

				objs = append(objs, test.NewFakeClusterResourceSet("ns1", "crs1").
					WithSecret("resource-s1").
					WithConfigMap("resource-c1").
					ApplyToCluster(test.SelectClusterObj(objs, "ns1", "cluster1")).
					Objs()...)

				return objs
			}(),
		},
		wantMoveGroups: [][]string{
			{ // group 1
				// Cluster
				"cluster.x-k8s.io/v1alpha4, Kind=Cluster, ns1/cluster1",
				// ClusterResourceSet
				"addons.cluster.x-k8s.io/v1alpha4, Kind=ClusterResourceSet, ns1/crs1",
			},
			{ // group 2 (objects with ownerReferences in group 1)
				// owned by Clusters
				"/v1, Kind=Secret, ns1/cluster1-ca",
				"/v1, Kind=Secret, ns1/cluster1-kubeconfig",
				"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureCluster, ns1/cluster1",
				// owned by ClusterResourceSet
				"/v1, Kind=Secret, ns1/resource-s1",
				"/v1, Kind=ConfigMap, ns1/resource-c1",
				// owned by ClusterResourceSet & Cluster
				"addons.cluster.x-k8s.io/v1alpha4, Kind=ClusterResourceSetBinding, ns1/cluster1",
			},
		},
	},
	{
		name: "Cluster and global + namespaced external objects with force-move label",
		fields: moveTestsFields{
			func() []client.Object {
				objs := []client.Object{}
				objs = append(objs, test.NewFakeCluster("ns1", "foo").Objs()...)
				objs = append(objs, test.NewFakeExternalObject("ns1", "externalTest1").Objs()...)
				objs = append(objs, test.NewFakeExternalObject("", "externalTest2").Objs()...)
				return objs
			}(),
		},
		wantMoveGroups: [][]string{
			{ // group1
				"cluster.x-k8s.io/v1alpha4, Kind=Cluster, ns1/foo",
				"external.cluster.x-k8s.io/v1alpha4, Kind=GenericExternalObject, ns1/externalTest1",
				"external.cluster.x-k8s.io/v1alpha4, Kind=GenericExternalObject, /externalTest2",
			},
			{ // group 2 (objects with ownerReferences in group 1)
				// owned by Clusters
				"/v1, Kind=Secret, ns1/foo-ca",
				"/v1, Kind=Secret, ns1/foo-kubeconfig",
				"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureCluster, ns1/foo",
			},
		},
		wantErr: false,
	},
}

func Test_getMoveSequence(t *testing.T) {
	// NB. we are testing the move and move sequence using the same set of moveTests, but checking the results at different stages of the move process
	for _, tt := range moveTests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			// Create an objectGraph bound a source cluster with all the CRDs for the types involved in the test.
			graph := getObjectGraphWithObjs(tt.fields.objs)

			// Get all the types to be considered for discovery
			err := getFakeDiscoveryTypes(graph)
			g.Expect(err).NotTo(HaveOccurred())

			// trigger discovery the content of the source cluster
			g.Expect(graph.Discovery("", "")).To(Succeed())

			moveSequence := getMoveSequence(graph)
			g.Expect(moveSequence.groups).To(HaveLen(len(tt.wantMoveGroups)))

			for i, gotGroup := range moveSequence.groups {
				wantGroup := tt.wantMoveGroups[i]
				gotNodes := []string{}
				for _, node := range gotGroup {
					gotNodes = append(gotNodes, string(node.identity.UID))
				}

				g.Expect(gotNodes).To(ConsistOf(wantGroup))
			}
		})
	}
}

func Test_objectMover_move_dryRun(t *testing.T) {
	// NB. we are testing the move and move sequence using the same set of moveTests, but checking the results at different stages of the move process
	for _, tt := range moveTests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			// Create an objectGraph bound a source cluster with all the CRDs for the types involved in the test.
			graph := getObjectGraphWithObjs(tt.fields.objs)

			// Get all the types to be considered for discovery
			err := getFakeDiscoveryTypes(graph)
			g.Expect(err).NotTo(HaveOccurred())

			// trigger discovery the content of the source cluster
			g.Expect(graph.Discovery("", "")).To(Succeed())

			// gets a fakeProxy to an empty cluster with all the required CRDs
			toProxy := getFakeProxyWithCRDs()

			// Run move
			mover := objectMover{
				fromProxy: graph.proxy,
				dryRun:    true,
			}

			err = mover.move(graph, toProxy)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}

			g.Expect(err).NotTo(HaveOccurred())

			// check that the objects are kept in the source cluster and are not created in the target cluster
			csFrom, err := graph.proxy.NewClient()
			g.Expect(err).NotTo(HaveOccurred())

			csTo, err := toProxy.NewClient()
			g.Expect(err).NotTo(HaveOccurred())
			for _, node := range graph.uidToNode {
				key := client.ObjectKey{
					Namespace: node.identity.Namespace,
					Name:      node.identity.Name,
				}
				// objects are kept in source cluster as it's dry run
				oFrom := &unstructured.Unstructured{}
				oFrom.SetAPIVersion(node.identity.APIVersion)
				oFrom.SetKind(node.identity.Kind)

				if err := csFrom.Get(ctx, key, oFrom); err != nil {
					t.Errorf("error = %v when checking for %v kept in source cluster", err, key)
					continue
				}

				// objects are not created in target cluster as it's dry run
				oTo := &unstructured.Unstructured{}
				oTo.SetAPIVersion(node.identity.APIVersion)
				oTo.SetKind(node.identity.Kind)

				err := csTo.Get(ctx, key, oTo)
				if err == nil {
					if oFrom.GetNamespace() != "" {
						t.Errorf("%v created in target cluster which should not", key)
						continue
					}
				} else if !apierrors.IsNotFound(err) {
					t.Errorf("error = %v when checking for %v should not created ojects in target cluster", err, key)
					continue
				}
			}
		})
	}
}

func Test_objectMover_move(t *testing.T) {
	// NB. we are testing the move and move sequence using the same set of moveTests, but checking the results at different stages of the move process
	for _, tt := range moveTests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			// Create an objectGraph bound a source cluster with all the CRDs for the types involved in the test.
			graph := getObjectGraphWithObjs(tt.fields.objs)

			// Get all the types to be considered for discovery
			err := getFakeDiscoveryTypes(graph)
			g.Expect(err).NotTo(HaveOccurred())

			// trigger discovery the content of the source cluster
			g.Expect(graph.Discovery("", "")).To(Succeed())

			// gets a fakeProxy to an empty cluster with all the required CRDs
			toProxy := getFakeProxyWithCRDs()

			// Run move
			mover := objectMover{
				fromProxy: graph.proxy,
			}

			err = mover.move(graph, toProxy)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}

			g.Expect(err).NotTo(HaveOccurred())

			// check that the objects are removed from the source cluster and are created in the target cluster
			csFrom, err := graph.proxy.NewClient()
			g.Expect(err).NotTo(HaveOccurred())

			csTo, err := toProxy.NewClient()
			g.Expect(err).NotTo(HaveOccurred())

			for _, node := range graph.uidToNode {
				key := client.ObjectKey{
					Namespace: node.identity.Namespace,
					Name:      node.identity.Name,
				}

				// objects are deleted from the source cluster
				oFrom := &unstructured.Unstructured{}
				oFrom.SetAPIVersion(node.identity.APIVersion)
				oFrom.SetKind(node.identity.Kind)

				err := csFrom.Get(ctx, key, oFrom)
				if err == nil {
					if oFrom.GetNamespace() != "" {
						t.Errorf("%v not deleted in source cluster", key)
						continue
					}
				} else if !apierrors.IsNotFound(err) {
					t.Errorf("error = %v when checking for %v deleted in source cluster", err, key)
					continue
				}

				// objects are created in the target cluster
				oTo := &unstructured.Unstructured{}
				oTo.SetAPIVersion(node.identity.APIVersion)
				oTo.SetKind(node.identity.Kind)

				if err := csTo.Get(ctx, key, oTo); err != nil {
					t.Errorf("error = %v when checking for %v created in target cluster", err, key)
					continue
				}
			}
		})
	}
}

func Test_objectMover_checkProvisioningCompleted(t *testing.T) {
	type fields struct {
		objs []client.Object
	}
	tests := []struct {
		name    string
		fields  fields
		wantErr bool
	}{
		{
			name: "Blocks with a cluster without InfrastructureReady",
			fields: fields{
				objs: []client.Object{
					&clusterv1.Cluster{
						TypeMeta: metav1.TypeMeta{
							Kind:       "Cluster",
							APIVersion: clusterv1.GroupVersion.String(),
						},
						ObjectMeta: metav1.ObjectMeta{
							Namespace: "ns1",
							Name:      "cluster1",
						},
						Status: clusterv1.ClusterStatus{
							InfrastructureReady: false,
							Conditions: clusterv1.Conditions{
								*conditions.TrueCondition(clusterv1.ControlPlaneInitializedCondition),
							},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Blocks with a cluster without ControlPlaneInitialized",
			fields: fields{
				objs: []client.Object{
					&clusterv1.Cluster{
						TypeMeta: metav1.TypeMeta{
							Kind:       "Cluster",
							APIVersion: clusterv1.GroupVersion.String(),
						},
						ObjectMeta: metav1.ObjectMeta{
							Namespace: "ns1",
							Name:      "cluster1",
						},
						Status: clusterv1.ClusterStatus{
							InfrastructureReady: true,
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Blocks with a cluster with ControlPlaneInitialized=False",
			fields: fields{
				objs: []client.Object{
					&clusterv1.Cluster{
						TypeMeta: metav1.TypeMeta{
							Kind:       "Cluster",
							APIVersion: clusterv1.GroupVersion.String(),
						},
						ObjectMeta: metav1.ObjectMeta{
							Namespace: "ns1",
							Name:      "cluster1",
						},
						Status: clusterv1.ClusterStatus{
							InfrastructureReady: true,
							Conditions: clusterv1.Conditions{
								*conditions.FalseCondition(clusterv1.ControlPlaneInitializedCondition, "", clusterv1.ConditionSeverityInfo, ""),
							},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Blocks with a cluster without ControlPlaneReady",
			fields: fields{
				objs: []client.Object{
					&clusterv1.Cluster{
						TypeMeta: metav1.TypeMeta{
							Kind:       "Cluster",
							APIVersion: clusterv1.GroupVersion.String(),
						},
						ObjectMeta: metav1.ObjectMeta{
							Namespace: "ns1",
							Name:      "cluster1",
						},
						Spec: clusterv1.ClusterSpec{
							ControlPlaneRef: &corev1.ObjectReference{},
						},
						Status: clusterv1.ClusterStatus{
							InfrastructureReady: true,
							Conditions: clusterv1.Conditions{
								*conditions.TrueCondition(clusterv1.ControlPlaneInitializedCondition),
							},
							ControlPlaneReady: false,
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Blocks with a Machine Without NodeRef",
			fields: fields{
				objs: []client.Object{
					&clusterv1.Cluster{
						TypeMeta: metav1.TypeMeta{
							Kind:       "Cluster",
							APIVersion: clusterv1.GroupVersion.String(),
						},
						ObjectMeta: metav1.ObjectMeta{
							Namespace: "ns1",
							Name:      "cluster1",
							UID:       "cluster1",
						},
						Status: clusterv1.ClusterStatus{
							InfrastructureReady: true,
							Conditions: clusterv1.Conditions{
								*conditions.TrueCondition(clusterv1.ControlPlaneInitializedCondition),
							},
						},
					},
					&clusterv1.Machine{
						TypeMeta: metav1.TypeMeta{
							Kind:       "Machine",
							APIVersion: clusterv1.GroupVersion.String(),
						},
						ObjectMeta: metav1.ObjectMeta{
							Namespace: "ns1",
							Name:      "machine1",
							OwnerReferences: []metav1.OwnerReference{
								{
									APIVersion: clusterv1.GroupVersion.String(),
									Kind:       "Cluster",
									Name:       "cluster1",
									UID:        "cluster1",
								},
							},
						},
						Status: clusterv1.MachineStatus{
							NodeRef: nil,
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "Pass",
			fields: fields{
				objs: []client.Object{
					&clusterv1.Cluster{
						TypeMeta: metav1.TypeMeta{
							Kind:       "Cluster",
							APIVersion: clusterv1.GroupVersion.String(),
						},
						ObjectMeta: metav1.ObjectMeta{
							Namespace: "ns1",
							Name:      "cluster1",
							UID:       "cluster1",
						},
						Status: clusterv1.ClusterStatus{
							InfrastructureReady: true,
							Conditions: clusterv1.Conditions{
								*conditions.TrueCondition(clusterv1.ControlPlaneInitializedCondition),
							},
						},
					},
					&clusterv1.Machine{
						TypeMeta: metav1.TypeMeta{
							Kind:       "Machine",
							APIVersion: clusterv1.GroupVersion.String(),
						},
						ObjectMeta: metav1.ObjectMeta{
							Namespace: "ns1",
							Name:      "machine1",
							OwnerReferences: []metav1.OwnerReference{
								{
									APIVersion: clusterv1.GroupVersion.String(),
									Kind:       "Cluster",
									Name:       "cluster1",
									UID:        "cluster1",
								},
							},
						},
						Status: clusterv1.MachineStatus{
							NodeRef: &corev1.ObjectReference{},
						},
					},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			// Create an objectGraph bound a source cluster with all the CRDs for the types involved in the test.
			graph := getObjectGraphWithObjs(tt.fields.objs)

			// Get all the types to be considered for discovery
			err := getFakeDiscoveryTypes(graph)
			g.Expect(err).NotTo(HaveOccurred())

			// trigger discovery the content of the source cluster
			g.Expect(graph.Discovery("", "")).To(Succeed())

			o := &objectMover{
				fromProxy: graph.proxy,
			}
			err = o.checkProvisioningCompleted(graph)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}

func Test_objectsMoverService_checkTargetProviders(t *testing.T) {
	type fields struct {
		fromProxy Proxy
	}
	type args struct {
		toProxy   Proxy
		namespace string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			name: "move objects in single namespace, all the providers in place (lazy matching)",
			fields: fields{
				fromProxy: test.NewFakeProxy().
					WithProviderInventory("capi", clusterctlv1.CoreProviderType, "v1.0.0", "capi-system", "").
					WithProviderInventory("kubeadm", clusterctlv1.BootstrapProviderType, "v1.0.0", "cabpk-system", "").
					WithProviderInventory("capa", clusterctlv1.InfrastructureProviderType, "v1.0.0", "capa-system", ""),
			},
			args: args{
				namespace: "ns1", // a single namespaces
				toProxy: test.NewFakeProxy().
					WithProviderInventory("capi", clusterctlv1.CoreProviderType, "v1.0.0", "capi-system", "ns1").
					WithProviderInventory("kubeadm", clusterctlv1.BootstrapProviderType, "v1.0.0", "cabpk-system", "ns1").
					WithProviderInventory("capa", clusterctlv1.InfrastructureProviderType, "v1.0.0", "capa-system", "ns1"),
			},
			wantErr: false,
		},
		{
			name: "move objects in single namespace, all the providers in place but with a newer version (lazy matching)",
			fields: fields{
				fromProxy: test.NewFakeProxy().
					WithProviderInventory("capi", clusterctlv1.CoreProviderType, "v2.0.0", "capi-system", ""),
			},
			args: args{
				namespace: "ns1", // a single namespaces
				toProxy: test.NewFakeProxy().
					WithProviderInventory("capi", clusterctlv1.CoreProviderType, "v2.1.0", "capi-system", "ns1"), // Lazy matching
			},
			wantErr: false,
		},
		{
			name: "move objects in all namespaces, all the providers in place (exact matching)",
			fields: fields{
				fromProxy: test.NewFakeProxy().
					WithProviderInventory("capi", clusterctlv1.CoreProviderType, "v1.0.0", "capi-system", "").
					WithProviderInventory("kubeadm", clusterctlv1.BootstrapProviderType, "v1.0.0", "cabpk-system", "").
					WithProviderInventory("capa", clusterctlv1.InfrastructureProviderType, "v1.0.0", "capa-system", ""),
			},
			args: args{
				namespace: "", // all namespaces
				toProxy: test.NewFakeProxy().
					WithProviderInventory("capi", clusterctlv1.CoreProviderType, "v1.0.0", "capi-system", "").
					WithProviderInventory("kubeadm", clusterctlv1.BootstrapProviderType, "v1.0.0", "cabpk-system", "").
					WithProviderInventory("capa", clusterctlv1.InfrastructureProviderType, "v1.0.0", "capa-system", ""),
			},
			wantErr: false,
		},
		{
			name: "move objects in all namespaces, all the providers in place but with a newer version (exact matching)",
			fields: fields{
				fromProxy: test.NewFakeProxy().
					WithProviderInventory("capi", clusterctlv1.CoreProviderType, "v2.0.0", "capi-system", ""),
			},
			args: args{
				namespace: "", // all namespaces
				toProxy: test.NewFakeProxy().
					WithProviderInventory("capi", clusterctlv1.CoreProviderType, "v2.1.0", "capi-system", ""),
			},
			wantErr: false,
		},
		{
			name: "move objects in all namespaces, not exact matching",
			fields: fields{
				fromProxy: test.NewFakeProxy().
					WithProviderInventory("capi", clusterctlv1.CoreProviderType, "v2.0.0", "capi-system", ""),
			},
			args: args{
				namespace: "", // all namespaces
				toProxy: test.NewFakeProxy().
					WithProviderInventory("capi", clusterctlv1.CoreProviderType, "v2.1.0", "capi-system", "ns1"), // Lazy matching only
			},
			wantErr: true,
		},
		{
			name: "fails if a provider is missing",
			fields: fields{
				fromProxy: test.NewFakeProxy().
					WithProviderInventory("capi", clusterctlv1.CoreProviderType, "v2.0.0", "capi-system", ""),
			},
			args: args{
				namespace: "", // all namespaces
				toProxy:   test.NewFakeProxy(),
			},
			wantErr: true,
		},
		{
			name: "fails if a provider version is older than expected",
			fields: fields{
				fromProxy: test.NewFakeProxy().
					WithProviderInventory("capi", clusterctlv1.CoreProviderType, "v2.0.0", "capi-system", ""),
			},
			args: args{
				namespace: "", // all namespaces
				toProxy: test.NewFakeProxy().
					WithProviderInventory("capi", clusterctlv1.CoreProviderType, "v1.0.0", "capi1-system", ""),
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			o := &objectMover{
				fromProviderInventory: newInventoryClient(tt.fields.fromProxy, nil),
			}
			err := o.checkTargetProviders(tt.args.namespace, newInventoryClient(tt.args.toProxy, nil))
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}

func Test_objectMoverService_ensureNamespace(t *testing.T) {
	type args struct {
		toProxy   Proxy
		namespace string
	}

	namespace1 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "namespace-1",
		},
	}

	tests := []struct {
		name string
		args args
	}{
		{
			name: "ensureNamespace doesn't fail given an existing namespace",
			args: args{
				// Create a fake cluster target with namespace-1 already existing
				toProxy: test.NewFakeProxy().WithObjs(namespace1),
				// Ensure namespace-1 gets created
				namespace: "namespace-1",
			},
		},
		{
			name: "ensureNamespace doesn't fail if the namespace does not already exist in the target",
			args: args{
				// Create a fake empty client
				toProxy: test.NewFakeProxy(),
				// Ensure namespace-2 gets created
				namespace: "namespace-2",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			mover := objectMover{
				fromProxy: test.NewFakeProxy(),
			}

			err := mover.ensureNamespace(tt.args.toProxy, tt.args.namespace)
			g.Expect(err).NotTo(HaveOccurred())

			// Check that the namespaces either existed or were created in the
			// target.
			csTo, err := tt.args.toProxy.NewClient()
			g.Expect(err).ToNot(HaveOccurred())

			ns := &corev1.Namespace{}
			key := client.ObjectKey{
				// Search for this namespace
				Name: tt.args.namespace,
			}

			err = csTo.Get(ctx, key, ns)
			g.Expect(err).ToNot(HaveOccurred())
		})
	}
}

func Test_objectMoverService_ensureNamespaces(t *testing.T) {
	type args struct {
		toProxy Proxy
	}
	type fields struct {
		objs []client.Object
	}

	// Create some test runtime objects to be used in the tests
	namespace1 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "namespace-1",
		},
	}

	cluster1 := test.NewFakeCluster("namespace-1", "cluster-1")
	cluster2 := test.NewFakeCluster("namespace-2", "cluster-2")
	globalObj := test.NewFakeExternalObject("", "eo-1")

	clustersObjs := append(cluster1.Objs(), cluster2.Objs()...)

	tests := []struct {
		name               string
		fields             fields
		args               args
		expectedNamespaces []string
	}{
		{
			name: "ensureNamespaces doesn't fail given an existing namespace",
			fields: fields{
				objs: cluster1.Objs(),
			},
			args: args{
				toProxy: test.NewFakeProxy(),
			},
			expectedNamespaces: []string{"namespace-1"},
		},
		{
			name: "ensureNamespaces moves namespace-1 and namespace-2 to target",
			fields: fields{
				objs: clustersObjs,
			},
			args: args{
				toProxy: test.NewFakeProxy(),
			},
			expectedNamespaces: []string{"namespace-1", "namespace-2"},
		},
		{

			name: "ensureNamespaces moves namespace-2 to target which already has namespace-1",
			fields: fields{
				objs: cluster2.Objs(),
			},
			args: args{
				toProxy: test.NewFakeProxy().WithObjs(namespace1),
			},
			expectedNamespaces: []string{"namespace-1", "namespace-2"},
		},
		{
			name: "ensureNamespaces doesn't fail if no namespace is specified (cluster-wide)",
			fields: fields{
				objs: globalObj.Objs(),
			},
			args: args{
				toProxy: test.NewFakeProxy(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			graph := getObjectGraphWithObjs(tt.fields.objs)

			// Get all the types to be considered for discovery
			err := getFakeDiscoveryTypes(graph)
			g.Expect(err).NotTo(HaveOccurred())

			// Trigger discovery the content of the source cluster
			g.Expect(graph.Discovery("", "")).To(Succeed())

			mover := objectMover{
				fromProxy: graph.proxy,
			}

			err = mover.ensureNamespaces(graph, tt.args.toProxy)
			g.Expect(err).NotTo(HaveOccurred())

			// Check that the namespaces either existed or were created in the
			// target.
			csTo, err := tt.args.toProxy.NewClient()
			g.Expect(err).ToNot(HaveOccurred())

			namespaces := &corev1.NamespaceList{}

			err = csTo.List(ctx, namespaces, client.Continue(namespaces.Continue))
			g.Expect(err).ToNot(HaveOccurred())

			// Ensure length of namespaces matches what's expected to ensure we're handling
			// cluster-wide (namespace of "") objects
			g.Expect(namespaces.Items).To(HaveLen(len(tt.expectedNamespaces)))

			// Loop through each expected result to ensure that it is found in
			// the actual results.
			for _, expected := range tt.expectedNamespaces {
				exists := false
				for _, item := range namespaces.Items {
					if item.Name == expected {
						exists = true
					}
				}
				// If at any point a namespace was not found, it must have not
				// been moved to the target successfully.
				if !exists {
					t.Errorf("namespace: %v not found in target cluster", expected)
				}
			}
		})
	}
}

func Test_objectMoverService_moveByClusterName(t *testing.T) {

	type args struct {
		namespace, cluster string
	}

	tests := []struct {
		name           string
		fields         moveTestsFields
		wantMoveGroups [][]string
		wantErr        bool
		args           args
	}{
		{
			name: "Two clusters, should only move `foo`",
			fields: moveTestsFields{
				objs: func() []client.Object {
					objs := []client.Object{}
					objs = append(objs, test.NewFakeCluster("ns1", "foo").Objs()...)
					objs = append(objs, test.NewFakeCluster("ns1", "bar").Objs()...)
					return objs
				}(),
			},
			wantMoveGroups: [][]string{
				{ //group 1
					"cluster.x-k8s.io/v1alpha4, Kind=Cluster, ns1/foo",
				},
				{ //group 2 (objects with ownerReferences in group 1)
					// owned by Clusters
					"/v1, Kind=Secret, ns1/foo-ca",
					"/v1, Kind=Secret, ns1/foo-kubeconfig",
					"infrastructure.cluster.x-k8s.io/v1alpha4, Kind=GenericInfrastructureCluster, ns1/foo",
				},
			},
			args: args{
				namespace: "",
				cluster:   "foo",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			// Create an objectGraph bound a source cluster with all the CRDs for the types involved in the test.
			graph := getObjectGraphWithObjs(tt.fields.objs)

			// Get all the types to be considered for discovery
			err := getFakeDiscoveryTypes(graph)
			g.Expect(err).NotTo(HaveOccurred())

			// trigger discovery the content of the source cluster
			g.Expect(graph.Discovery(tt.args.namespace, tt.args.cluster)).To(Succeed())

			moveSequence := getMoveSequence(graph)
			g.Expect(moveSequence.groups).To(HaveLen(len(tt.wantMoveGroups)))

			for i, gotGroup := range moveSequence.groups {
				wantGroup := tt.wantMoveGroups[i]
				gotNodes := []string{}
				for _, node := range gotGroup {
					gotNodes = append(gotNodes, string(node.identity.UID))
				}

				g.Expect(gotNodes).To(ConsistOf(wantGroup))
			}

			// gets a fakeProxy to an empty cluster with all the required CRDs
			toProxy := getFakeProxyWithCRDs()

			// Run move
			mover := objectMover{
				fromProxy: graph.proxy,
			}

			err = mover.move(graph, toProxy)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}

			g.Expect(err).NotTo(HaveOccurred())

			// check that the objects are removed from the source cluster and are created in the target cluster
			csFrom, err := graph.proxy.NewClient()
			g.Expect(err).NotTo(HaveOccurred())

			csTo, err := toProxy.NewClient()
			g.Expect(err).NotTo(HaveOccurred())

			for _, node := range graph.uidToNode {
				key := client.ObjectKey{
					Namespace: node.identity.Namespace,
					Name:      node.identity.Name,
				}

				// objects are deleted from the source cluster
				oFrom := &unstructured.Unstructured{}
				oFrom.SetAPIVersion(node.identity.APIVersion)
				oFrom.SetKind(node.identity.Kind)

				err := csFrom.Get(ctx, key, oFrom)
				if err == nil {
					if oFrom.GetNamespace() != "" {
						t.Errorf("%v not deleted in source cluster", key)
						continue
					}
				} else if !apierrors.IsNotFound(err) {
					t.Errorf("error = %v when checking for %v deleted in source cluster", err, key)
					continue
				}

				// objects are created in the target cluster
				oTo := &unstructured.Unstructured{}
				oTo.SetAPIVersion(node.identity.APIVersion)
				oTo.SetKind(node.identity.Kind)

				if err := csTo.Get(ctx, key, oTo); err != nil {
					t.Errorf("error = %v when checking for %v created in target cluster", err, key)
					continue
				}
			}
		})
	}
}
