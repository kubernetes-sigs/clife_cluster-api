/*
Copyright 2021 The Kubernetes Authors.

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

package utils

import (
	"github.com/blang/semver"
	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime/schema"
	bootstrapv1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1alpha4"
	"sigs.k8s.io/cluster-api/bootstrap/kubeadm/types/v1beta1"
	"sigs.k8s.io/cluster-api/bootstrap/kubeadm/types/v1beta2"
	"sigs.k8s.io/cluster-api/bootstrap/kubeadm/types/v1beta3"

	"testing"
)

func TestKubeVersionToKubeadmAPIGroupVersion(t *testing.T) {
	type args struct {
		version semver.Version
	}
	tests := []struct {
		name    string
		args    args
		want    schema.GroupVersion
		wantErr bool
	}{
		{
			name: "fails when kubernetes version is too old",
			args: args{
				version: semver.MustParse("1.12.0"),
			},
			want:    schema.GroupVersion{},
			wantErr: true,
		},
		{
			name: "pass with minimum kubernetes version for kubeadm API v1beta1",
			args: args{
				version: semver.MustParse("1.13.0"),
			},
			want:    v1beta1.GroupVersion,
			wantErr: false,
		},
		{
			name: "pass with kubernetes version for kubeadm API v1beta1",
			args: args{
				version: semver.MustParse("1.14.99"),
			},
			want:    v1beta1.GroupVersion,
			wantErr: false,
		},
		{
			name: "pass with minimum kubernetes version for kubeadm API v1beta2",
			args: args{
				version: semver.MustParse("1.15.0"),
			},
			want:    v1beta2.GroupVersion,
			wantErr: false,
		},
		{
			name: "pass with kubernetes version for kubeadm API v1beta2",
			args: args{
				version: semver.MustParse("1.20.99"),
			},
			want:    v1beta2.GroupVersion,
			wantErr: false,
		},
		{
			name: "pass with minimum kubernetes version for kubeadm API v1beta3",
			args: args{
				version: semver.MustParse("1.22.0"),
			},
			want:    v1beta3.GroupVersion,
			wantErr: false,
		},
		{
			name: "pass with kubernetes version for kubeadm API v1beta3",
			args: args{
				version: semver.MustParse("1.23.99"),
			},
			want:    v1beta3.GroupVersion,
			wantErr: false,
		},
		{
			name: "pass with future kubernetes version",
			args: args{
				version: semver.MustParse("99.99.99"),
			},
			want:    v1beta3.GroupVersion,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			got, err := KubeVersionToKubeadmAPIGroupVersion(tt.args.version)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(got).To(Equal(tt.want))
		})
	}
}

func TestMarshalClusterConfigurationForVersion(t *testing.T) {
	type args struct {
		capiObj *bootstrapv1.ClusterConfiguration
		version semver.Version
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{
			name: "Generates a v1beta1 kubeadm configuration",
			args: args{
				capiObj: &bootstrapv1.ClusterConfiguration{},
				version: semver.MustParse("1.14.9"),
			},
			want: "apiServer: {}\n" +
				"apiVersion: kubeadm.k8s.io/v1beta1\n" + "" +
				"controllerManager: {}\n" +
				"dns: {}\n" +
				"etcd: {}\n" +
				"kind: ClusterConfiguration\n" +
				"networking: {}\n" +
				"scheduler: {}\n",
			wantErr: false,
		},
		{
			name: "Generates a v1beta2 kubeadm configuration",
			args: args{
				capiObj: &bootstrapv1.ClusterConfiguration{},
				version: semver.MustParse("1.15.0"),
			},
			want: "apiServer: {}\n" +
				"apiVersion: kubeadm.k8s.io/v1beta2\n" + "" +
				"controllerManager: {}\n" +
				"dns: {}\n" +
				"etcd: {}\n" +
				"kind: ClusterConfiguration\n" +
				"networking: {}\n" +
				"scheduler: {}\n",
			wantErr: false,
		},
		{
			name: "Generates a v1beta3 kubeadm configuration",
			args: args{
				capiObj: &bootstrapv1.ClusterConfiguration{},
				version: semver.MustParse("1.22.0"),
			},
			want: "apiServer: {}\n" +
				"apiVersion: kubeadm.k8s.io/v1beta3\n" + "" +
				"controllerManager: {}\n" +
				"dns: {}\n" +
				"etcd: {}\n" +
				"kind: ClusterConfiguration\n" +
				"networking: {}\n" +
				"scheduler: {}\n",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			got, err := MarshalClusterConfigurationForVersion(tt.args.capiObj, tt.args.version)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(got).To(Equal(tt.want), cmp.Diff(tt.want, got))
		})
	}
}

func TestMarshalClusterStatusForVersion(t *testing.T) {
	type args struct {
		capiObj *bootstrapv1.ClusterStatus
		version semver.Version
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{
			name: "Generates a v1beta1 kubeadm status",
			args: args{
				capiObj: &bootstrapv1.ClusterStatus{},
				version: semver.MustParse("1.14.9"),
			},
			want: "apiEndpoints: null\n" +
				"apiVersion: kubeadm.k8s.io/v1beta1\n" + "" +
				"kind: ClusterStatus\n",
			wantErr: false,
		},
		{
			name: "Generates a v1beta2 kubeadm status",
			args: args{
				capiObj: &bootstrapv1.ClusterStatus{},
				version: semver.MustParse("1.15.0"),
			},
			want: "apiEndpoints: null\n" +
				"apiVersion: kubeadm.k8s.io/v1beta2\n" + "" +
				"kind: ClusterStatus\n",
			wantErr: false,
		},
		{
			name: "Fails generating a v1beta3 kubeadm status",
			args: args{
				capiObj: &bootstrapv1.ClusterStatus{},
				version: semver.MustParse("1.22.0"),
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			got, err := MarshalClusterStatusForVersion(tt.args.capiObj, tt.args.version)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(got).To(Equal(tt.want), cmp.Diff(tt.want, got))
		})
	}
}

func TestMarshalInitConfigurationForVersion(t *testing.T) {
	type args struct {
		capiObj *bootstrapv1.InitConfiguration
		version semver.Version
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{
			name: "Generates a v1beta1 kubeadm configuration",
			args: args{
				capiObj: &bootstrapv1.InitConfiguration{},
				version: semver.MustParse("1.14.9"),
			},
			want: "apiVersion: kubeadm.k8s.io/v1beta1\n" +
				"kind: InitConfiguration\n" +
				"localAPIEndpoint:\n" +
				"  advertiseAddress: \"\"\n" +
				"  bindPort: 0\n" +
				"nodeRegistration: {}\n",
			wantErr: false,
		},
		{
			name: "Generates a v1beta2 kubeadm configuration",
			args: args{
				capiObj: &bootstrapv1.InitConfiguration{
					NodeRegistration: bootstrapv1.NodeRegistrationOptions{
						IgnorePreflightErrors: []string{"some-preflight-check"},
					},
				},
				version: semver.MustParse("1.15.0"),
			},
			want: "apiVersion: kubeadm.k8s.io/v1beta2\n" +
				"kind: InitConfiguration\n" +
				"localAPIEndpoint: {}\n" +
				"nodeRegistration:\n" +
				"  ignorePreflightErrors:\n" +
				"  - some-preflight-check\n",
			wantErr: false,
		},
		{
			name: "Generates a v1beta3 kubeadm configuration",
			args: args{
				capiObj: &bootstrapv1.InitConfiguration{
					NodeRegistration: bootstrapv1.NodeRegistrationOptions{
						IgnorePreflightErrors: []string{"some-preflight-check"},
					},
				},
				version: semver.MustParse("1.22.0"),
			},
			want: "apiVersion: kubeadm.k8s.io/v1beta3\n" +
				"kind: InitConfiguration\n" +
				"localAPIEndpoint: {}\n" +
				"nodeRegistration:\n" +
				"  ignorePreflightErrors:\n" +
				"  - some-preflight-check\n" +
				"  taints: null\n",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			got, err := MarshalInitConfigurationForVersion(tt.args.capiObj, tt.args.version)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(got).To(Equal(tt.want), cmp.Diff(tt.want, got))
		})
	}
}

func TestMarshalJoinConfigurationForVersion(t *testing.T) {
	type args struct {
		capiObj *bootstrapv1.JoinConfiguration
		version semver.Version
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{
			name: "Generates a v1beta1 kubeadm configuration",
			args: args{
				capiObj: &bootstrapv1.JoinConfiguration{},
				version: semver.MustParse("1.14.9"),
			},
			want: "apiVersion: kubeadm.k8s.io/v1beta1\n" + "" +
				"discovery: {}\n" +
				"kind: JoinConfiguration\n" +
				"nodeRegistration: {}\n",
			wantErr: false,
		},
		{
			name: "Generates a v1beta2 kubeadm configuration",
			args: args{
				capiObj: &bootstrapv1.JoinConfiguration{
					NodeRegistration: bootstrapv1.NodeRegistrationOptions{
						IgnorePreflightErrors: []string{"some-preflight-check"},
					},
				},
				version: semver.MustParse("1.15.0"),
			},
			want: "apiVersion: kubeadm.k8s.io/v1beta2\n" + "" +
				"discovery: {}\n" +
				"kind: JoinConfiguration\n" +
				"nodeRegistration:\n" +
				"  ignorePreflightErrors:\n" +
				"  - some-preflight-check\n",
			wantErr: false,
		},
		{
			name: "Generates a v1beta3 kubeadm configuration",
			args: args{
				capiObj: &bootstrapv1.JoinConfiguration{
					NodeRegistration: bootstrapv1.NodeRegistrationOptions{
						IgnorePreflightErrors: []string{"some-preflight-check"},
					},
				},
				version: semver.MustParse("1.22.0"),
			},
			want: "apiVersion: kubeadm.k8s.io/v1beta3\n" + "" +
				"discovery: {}\n" +
				"kind: JoinConfiguration\n" +
				"nodeRegistration:\n" +
				"  ignorePreflightErrors:\n" +
				"  - some-preflight-check\n" +
				"  taints: null\n",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			got, err := MarshalJoinConfigurationForVersion(tt.args.capiObj, tt.args.version)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(got).To(Equal(tt.want), cmp.Diff(tt.want, got))
		})
	}
}

func TestUnmarshalClusterConfiguration(t *testing.T) {
	type args struct {
		yaml string
	}
	tests := []struct {
		name    string
		args    args
		want    *bootstrapv1.ClusterConfiguration
		wantErr bool
	}{
		{
			name: "Parses a v1beta1 kubeadm configuration",
			args: args{
				yaml: "apiServer: {}\n" +
					"apiVersion: kubeadm.k8s.io/v1beta1\n" + "" +
					"controllerManager: {}\n" +
					"dns: {}\n" +
					"etcd: {}\n" +
					"kind: ClusterConfiguration\n" +
					"networking: {}\n" +
					"scheduler: {}\n",
			},
			want:    &bootstrapv1.ClusterConfiguration{},
			wantErr: false,
		},
		{
			name: "Parses a v1beta2 kubeadm configuration",
			args: args{
				yaml: "apiServer: {}\n" +
					"apiVersion: kubeadm.k8s.io/v1beta2\n" + "" +
					"controllerManager: {}\n" +
					"dns: {}\n" +
					"etcd: {}\n" +
					"kind: ClusterConfiguration\n" +
					"networking: {}\n" +
					"scheduler: {}\n",
			},
			want:    &bootstrapv1.ClusterConfiguration{},
			wantErr: false,
		},
		{
			name: "Parses a v1beta3 kubeadm configuration",
			args: args{
				yaml: "apiServer: {}\n" +
					"apiVersion: kubeadm.k8s.io/v1beta3\n" + "" +
					"controllerManager: {}\n" +
					"dns: {}\n" +
					"etcd: {}\n" +
					"kind: ClusterConfiguration\n" +
					"networking: {}\n" +
					"scheduler: {}\n",
			},
			want:    &bootstrapv1.ClusterConfiguration{},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			got, err := UnmarshalClusterConfiguration(tt.args.yaml)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(got).To(Equal(tt.want), cmp.Diff(tt.want, got))
		})
	}
}

func TestUnmarshalClusterStatus(t *testing.T) {
	type args struct {
		yaml string
	}
	tests := []struct {
		name    string
		args    args
		want    *bootstrapv1.ClusterStatus
		wantErr bool
	}{
		{
			name: "Parses a v1beta1 kubeadm configuration",
			args: args{
				yaml: "apiEndpoints: null\n" +
					"apiVersion: kubeadm.k8s.io/v1beta1\n" + "" +
					"kind: ClusterStatus\n",
			},
			want:    &bootstrapv1.ClusterStatus{},
			wantErr: false,
		},
		{
			name: "Parses a v1beta2 kubeadm configuration",
			args: args{
				yaml: "apiEndpoints: null\n" +
					"apiVersion: kubeadm.k8s.io/v1beta1\n" + "" +
					"kind: ClusterStatus\n",
			},
			want:    &bootstrapv1.ClusterStatus{},
			wantErr: false,
		},
		{
			name: "Fails parsing a v1beta3 kubeadm configuration",
			args: args{
				yaml: "apiEndpoints: null\n" +
					"apiVersion: kubeadm.k8s.io/v1beta3\n" + "" +
					"kind: ClusterStatus\n",
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			got, err := UnmarshalClusterStatus(tt.args.yaml)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(got).To(Equal(tt.want), cmp.Diff(tt.want, got))
		})
	}
}
