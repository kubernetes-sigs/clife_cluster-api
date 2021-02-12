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

	"github.com/blang/semver"
	corev1 "k8s.io/api/core/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	kubeadmv1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/types/v1beta1"
	"sigs.k8s.io/cluster-api/controlplane/kubeadm/internal"
	"sigs.k8s.io/cluster-api/controlplane/kubeadm/internal/machinefilters"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type fakeManagementCluster struct {
	// TODO: once all client interactions are moved to the Management cluster this can go away
	Management *internal.Management
	Machines   internal.FilterableMachineCollection
	Workload   fakeWorkloadCluster
	Reader     client.Reader
}

func (f *fakeManagementCluster) Get(ctx context.Context, key client.ObjectKey, obj client.Object) error {
	return f.Reader.Get(ctx, key, obj)
}

func (f *fakeManagementCluster) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return f.Reader.List(ctx, list, opts...)
}

func (f *fakeManagementCluster) GetWorkloadCluster(_ context.Context, _ client.ObjectKey) (internal.WorkloadCluster, error) {
	return f.Workload, nil
}

func (f *fakeManagementCluster) GetMachinesForCluster(c context.Context, n client.ObjectKey, filters ...machinefilters.Func) (internal.FilterableMachineCollection, error) {
	if f.Management != nil {
		return f.Management.GetMachinesForCluster(c, n, filters...)
	}
	return f.Machines, nil
}

type fakeWorkloadCluster struct {
	*internal.Workload
	Status            internal.ClusterStatus
	KubeadmConfig     fakeKubeadmConfig
	EtcdMembersResult []string
}

func (f fakeWorkloadCluster) ForwardEtcdLeadership(_ context.Context, _ *clusterv1.Machine, _ *clusterv1.Machine) error {
	return nil
}

func (f fakeWorkloadCluster) ReconcileEtcdMembers(ctx context.Context, nodeNames []string) ([]string, error) {
	return nil, nil
}

func (f fakeWorkloadCluster) ClusterStatus(_ context.Context) (internal.ClusterStatus, error) {
	return f.Status, nil
}

func (f fakeWorkloadCluster) AllowBootstrapTokensToGetNodes(ctx context.Context) error {
	return nil
}

func (f fakeWorkloadCluster) ReconcileKubeletRBACRole(ctx context.Context, version semver.Version) error {
	return nil
}

func (f fakeWorkloadCluster) ReconcileKubeletRBACBinding(ctx context.Context, version semver.Version) error {
	return nil
}

func (f fakeWorkloadCluster) UpdateKubeletConfigMap(ctx context.Context, version semver.Version) error {
	return nil
}

func (f fakeWorkloadCluster) RemoveEtcdMemberForMachine(ctx context.Context, machine *clusterv1.Machine) error {
	return nil
}

func (f fakeWorkloadCluster) RemoveMachineFromKubeadmConfigMap(ctx context.Context, machine *clusterv1.Machine) error {
	return nil
}

func (f fakeWorkloadCluster) EtcdMembers(_ context.Context) ([]string, error) {
	return f.EtcdMembersResult, nil
}

func (f fakeWorkloadCluster) GetKubeadmConfig(_ context.Context, _ client.ObjectKey) (internal.KubeadmConfig, error) {
	return f.KubeadmConfig, nil
}

func (f fakeWorkloadCluster) GetClient() client.Client {
	return nil
}

type fakeKubeadmConfig struct {
	ConfigMap *corev1.ConfigMap
}

func (f fakeKubeadmConfig) GetConfigMap() *corev1.ConfigMap {
	return f.ConfigMap
}

func (f fakeKubeadmConfig) RemoveAPIEndpoint(endpoint string) error {
	return nil
}

func (f fakeKubeadmConfig) ReconcileKubernetesVersion(version semver.Version) error {
	return nil
}

func (f fakeKubeadmConfig) ReconcileImageRepository(imageRepository string) error {
	return nil
}

func (f fakeKubeadmConfig) ReconcileEtcdImageMeta(imageMeta kubeadmv1.ImageMeta) error {
	return nil
}

func (f fakeKubeadmConfig) ReconcileCoreDNSImageInfo(repository, tag string) error {
	return nil
}

func (f fakeKubeadmConfig) ReconcileAPIServer(apiServer kubeadmv1.APIServer) error {
	return nil
}

func (f fakeKubeadmConfig) ReconcileControllerManager(controllerManager kubeadmv1.ControlPlaneComponent) error {
	return nil
}

func (f fakeKubeadmConfig) ReconcileScheduler(scheduler kubeadmv1.ControlPlaneComponent) error {
	return nil
}

type fakeMigrator struct {
	migrateCalled    bool
	migrateErr       error
	migratedCorefile string
}

func (m *fakeMigrator) Migrate(current, to, corefile string, deprecations bool) (string, error) {
	m.migrateCalled = true
	if m.migrateErr != nil {
		return "", m.migrateErr
	}
	return m.migratedCorefile, nil
}
