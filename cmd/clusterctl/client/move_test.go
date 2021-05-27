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

package client

import (
	"testing"

	. "github.com/onsi/gomega"
	clusterctlv1 "sigs.k8s.io/cluster-api/cmd/clusterctl/api/v1alpha3"
	"sigs.k8s.io/cluster-api/cmd/clusterctl/client/cluster"
	"sigs.k8s.io/cluster-api/cmd/clusterctl/client/config"
	"sigs.k8s.io/cluster-api/cmd/clusterctl/internal/test"
)

func Test_clusterctlClient_Move(t *testing.T) {
	type fields struct {
		client *fakeClient
	}
	type args struct {
		options MoveOptions
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			name: "does not return error if cluster client is found",
			fields: fields{
				client: fakeClientForMove(), // core v1.0.0 (v1.0.1 available), infra v2.0.0 (v2.0.1 available)
			},
			args: args{
				options: MoveOptions{
					FromKubeconfig: Kubeconfig{Path: "kubeconfig", Context: "mgmt-context"},
					ToKubeconfig:   Kubeconfig{Path: "kubeconfig", Context: "worker-context"},
				},
			},
			wantErr: false,
		},
		{
			name: "returns an error if from cluster client is not found",
			fields: fields{
				client: fakeClientForMove(), // core v1.0.0 (v1.0.1 available), infra v2.0.0 (v2.0.1 available)
			},
			args: args{
				options: MoveOptions{
					FromKubeconfig: Kubeconfig{Path: "kubeconfig", Context: "does-not-exist"},
					ToKubeconfig:   Kubeconfig{Path: "kubeconfig", Context: "worker-context"},
				},
			},
			wantErr: true,
		},
		{
			name: "returns an error if to cluster client is not found",
			fields: fields{
				client: fakeClientForMove(), // core v1.0.0 (v1.0.1 available), infra v2.0.0 (v2.0.1 available)
			},
			args: args{
				options: MoveOptions{
					FromKubeconfig: Kubeconfig{Path: "kubeconfig", Context: "mgmt-context"},
					ToKubeconfig:   Kubeconfig{Path: "kubeconfig", Context: "does-not-exist"},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			err := tt.fields.client.Move(tt.args.options)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).NotTo(HaveOccurred())
		})
	}
}

func fakeClientForMove() *fakeClient {
	core := config.NewProvider("cluster-api", "https://somewhere.com", clusterctlv1.CoreProviderType)
	infra := config.NewProvider("infra", "https://somewhere.com", clusterctlv1.InfrastructureProviderType)

	config1 := newFakeConfig().
		WithProvider(core).
		WithProvider(infra)

	cluster1 := newFakeCluster(cluster.Kubeconfig{Path: "kubeconfig", Context: "mgmt-context"}, config1).
		WithProviderInventory(core.Name(), core.Type(), "v1.0.0", "cluster-api-system", "").
		WithProviderInventory(infra.Name(), infra.Type(), "v2.0.0", "infra-system", "").
		WithObjectMover(&fakeObjectMover{}).
		WithObjs(test.FakeCAPISetupObjects()...)

	// Creating this cluster for move_test
	cluster2 := newFakeCluster(cluster.Kubeconfig{Path: "kubeconfig", Context: "worker-context"}, config1).
		WithProviderInventory(core.Name(), core.Type(), "v1.0.0", "cluster-api-system", "").
		WithProviderInventory(infra.Name(), infra.Type(), "v2.0.0", "infra-system", "").
		WithObjs(test.FakeCAPISetupObjects()...)

	client := newFakeClient(config1).
		WithCluster(cluster1).
		WithCluster(cluster2)

	return client
}

type fakeObjectMover struct {
	moveErr error
}

func (f *fakeObjectMover) Move(namespace, cluster string, toCluster cluster.Client, dryRun bool) error {
	return f.moveErr
}
