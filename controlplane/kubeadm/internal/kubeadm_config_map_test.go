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

package internal

import (
	"errors"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	"github.com/blang/semver"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeadmv1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/types/v1beta1"
	"sigs.k8s.io/yaml"
)

func TestUpdateKubernetesVersion(t *testing.T) {
	kconf := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kubeadmconfig",
			Namespace: metav1.NamespaceSystem,
		},
		Data: map[string]string{
			clusterConfigurationKey: `
apiVersion: kubeadm.k8s.io/v1beta2
kind: ClusterConfiguration
kubernetesVersion: v1.16.1
`,
		},
	}

	kubeadmConfigNoKey := kconf.DeepCopy()
	delete(kubeadmConfigNoKey.Data, clusterConfigurationKey)

	kubeadmConfigBadData := kconf.DeepCopy()
	kubeadmConfigBadData.Data[clusterConfigurationKey] = `something`

	tests := []struct {
		name      string
		version   semver.Version
		config    *corev1.ConfigMap
		expectErr bool
	}{
		{
			name:      "updates the config map",
			version:   semver.MustParse("1.17.2"),
			config:    kconf,
			expectErr: false,
		},
		{
			name:      "returns error if cannot find config map",
			expectErr: true,
		},
		{
			name:      "returns error if config has bad data",
			config:    kubeadmConfigBadData,
			expectErr: true,
		},
		{
			name:      "returns error if config doesn't have cluster config key",
			config:    kubeadmConfigNoKey,
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			conf := tt.config.DeepCopy()
			k := NewKubeadmConfig(conf)
			err := k.ReconcileKubernetesVersion(tt.version)
			if tt.expectErr {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(conf.Data[clusterConfigurationKey]).To(ContainSubstring("kubernetesVersion: v1.17.2"))
		})
	}
}

func Test_kubeadmConfig_RemoveAPIEndpoint(t *testing.T) {
	g := NewWithT(t)
	original := &corev1.ConfigMap{
		Data: map[string]string{
			"ClusterStatus": `apiEndpoints:
  ip-10-0-0-1.ec2.internal:
    advertiseAddress: 10.0.0.1
    bindPort: 6443
  ip-10-0-0-2.ec2.internal:
    advertiseAddress: 10.0.0.2
    bindPort: 6443
    someFieldThatIsAddedInTheFuture: bar
  ip-10-0-0-3.ec2.internal:
    advertiseAddress: 10.0.0.3
    bindPort: 6443
    someFieldThatIsAddedInTheFuture: baz
  ip-10-0-0-4.ec2.internal:
    advertiseAddress: 10.0.0.4
    bindPort: 6443
    someFieldThatIsAddedInTheFuture: fizzbuzz
apiVersion: kubeadm.k8s.io/vNbetaM
kind: ClusterStatus`,
		},
	}
	kc := NewKubeadmConfig(original)
	g.Expect(kc.RemoveAPIEndpoint("ip-10-0-0-3.ec2.internal")).ToNot(HaveOccurred())
	g.Expect(kc.GetConfigMap().Data).To(HaveKey("ClusterStatus"))
	var status struct {
		APIEndpoints map[string]interface{} `yaml:"apiEndpoints"`
		APIVersion   string                 `yaml:"apiVersion"`
		Kind         string                 `yaml:"kind"`

		Extra map[string]interface{} `yaml:",inline"`
	}
	g.Expect(yaml.UnmarshalStrict([]byte(kc.GetConfigMap().Data["ClusterStatus"]), &status)).To(Succeed())
	g.Expect(status.Extra).To(BeEmpty())

	g.Expect(status.APIEndpoints).To(SatisfyAll(
		HaveLen(3),
		HaveKey("ip-10-0-0-1.ec2.internal"),
		HaveKey("ip-10-0-0-2.ec2.internal"),
		HaveKey("ip-10-0-0-4.ec2.internal"),
		WithTransform(func(ep map[string]interface{}) interface{} {
			return ep["ip-10-0-0-4.ec2.internal"]
		}, SatisfyAll(
			HaveKeyWithValue("advertiseAddress", "10.0.0.4"),
			HaveKey("bindPort"),
			HaveKey("someFieldThatIsAddedInTheFuture"),
		)),
	))
}

func TestUpdateEtcdMeta(t *testing.T) {

	tests := []struct {
		name                      string
		clusterConfigurationValue string
		imageRepository           string
		imageTag                  string
		expectChanged             bool
		expectErr                 error
	}{
		{
			name: "it should set the values, if they were empty",
			clusterConfigurationValue: `
apiVersion: kubeadm.k8s.io/v1beta2
kind: ClusterConfiguration
etcd:
  local:
    dataDir: /var/lib/etcd
`,
			imageRepository: "gcr.io/k8s/etcd",
			imageTag:        "0.10.9",
			expectChanged:   true,
		},
		{
			name: "it should return false with no error, if there are no changes",
			clusterConfigurationValue: `
apiVersion: kubeadm.k8s.io/v1beta2
kind: ClusterConfiguration
etcd:
  local:
    dataDir: /var/lib/etcd
    imageRepository: "gcr.io/k8s/etcd"
    imageTag: "0.10.9"
`,
			imageRepository: "gcr.io/k8s/etcd",
			imageTag:        "0.10.9",
			expectChanged:   false,
		},
		{
			name: "it shouldn't write empty strings",
			clusterConfigurationValue: `
apiVersion: kubeadm.k8s.io/v1beta2
kind: ClusterConfiguration
etcd:
  local:
    dataDir: /var/lib/etcd
`,
			imageRepository: "",
			imageTag:        "",
			expectChanged:   false,
		},
		{
			name: "it should overwrite imageTag",
			clusterConfigurationValue: `
apiVersion: kubeadm.k8s.io/v1beta2
kind: ClusterConfiguration
etcd:
  local:
    imageTag: 0.10.8
    dataDir: /var/lib/etcd
`,
			imageTag:      "0.10.9",
			expectChanged: true,
		},
		{
			name: "it should overwrite imageRepository",
			clusterConfigurationValue: `
apiVersion: kubeadm.k8s.io/v1beta2
kind: ClusterConfiguration
etcd:
  local:
    imageRepository: another-custom-repo
    dataDir: /var/lib/etcd
`,
			imageRepository: "gcr.io/k8s/etcd",
			expectChanged:   true,
		},
		{
			name: "it should error if it's not a valid k8s object",
			clusterConfigurationValue: `
etcd:
  local:
    imageRepository: another-custom-repo
    dataDir: /var/lib/etcd
`,
			expectErr: errors.New("Object 'Kind' is missing"),
		},
		{
			name: "it should error if the current value is a type we don't expect",
			clusterConfigurationValue: `
apiVersion: kubeadm.k8s.io/v1beta2
kind: ClusterConfiguration
etcd:
  local:
    imageRepository: true
    dataDir: /var/lib/etcd
`,
			expectErr: errors.New(".etcd.local.imageRepository accessor error"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)

			kconfig := NewKubeadmConfig(&corev1.ConfigMap{
				Data: map[string]string{
					clusterConfigurationKey: test.clusterConfigurationValue,
				},
			})

			imageMeta := kubeadmv1.ImageMeta{
				ImageRepository: test.imageRepository,
				ImageTag:        test.imageTag,
			}

			err := kconfig.ReconcileEtcdImageMeta(imageMeta)
			if test.expectErr == nil {
				g.Expect(err).ToNot(HaveOccurred())
			} else {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring(test.expectErr.Error()))
			}

			if test.expectChanged {
				if test.imageRepository != "" {
					g.Expect(kconfig.GetConfigMap().Data[clusterConfigurationKey]).To(ContainSubstring(test.imageRepository))
				}
				if test.imageTag != "" {
					g.Expect(kconfig.GetConfigMap().Data[clusterConfigurationKey]).To(ContainSubstring(test.imageTag))
				}
			}

		})
	}
}

func Test_kubeadmConfig_UpdateCoreDNSImageInfo(t *testing.T) {
	cm := &corev1.ConfigMap{
		Data: map[string]string{
			"ClusterConfiguration": `apiServer:
  extraArgs:
    authorization-mode: Node,RBAC
    cloud-provider: aws
  timeoutForControlPlane: 4m0s
apiVersion: kubeadm.k8s.io/v1beta2
certificatesDir: /etc/kubernetes/pki
clusterName: foobar
controlPlaneEndpoint: foobar.us-east-2.elb.amazonaws.com
controllerManager:
  extraArgs:
    cloud-provider: aws
dns:
  type: CoreDNS
etcd:
  local:
    dataDir: /var/lib/etcd
imageRepository: k8s.gcr.io
kind: ClusterConfiguration
kubernetesVersion: v1.16.1
networking:
  dnsDomain: cluster.local
  podSubnet: 192.168.0.0/16
  serviceSubnet: 10.96.0.0/12
scheduler: {}`,
		},
	}

	badcm := &corev1.ConfigMap{
		Data: map[string]string{
			"ClusterConfiguration": `apiServer:
  extraArgs:
    authorization-mode: Node,RBAC
	...`,
		},
	}

	tests := []struct {
		name      string
		cm        *corev1.ConfigMap
		expectErr bool
	}{
		{
			name:      "sets the image repository and tag",
			cm:        cm,
			expectErr: false,
		},
		{
			name:      "returns error if unable to convert yaml",
			cm:        badcm,
			expectErr: true,
		},
		{
			name:      "returns error if cannot find cluster config key",
			cm:        &corev1.ConfigMap{Data: map[string]string{}},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			imageRepository := "gcr.io/example"
			imageTag := "v1.0.1-sometag"
			kc := NewKubeadmConfig(tt.cm)

			if tt.expectErr {
				g.Expect(kc.ReconcileCoreDNSImageInfo(imageRepository, imageTag)).ToNot(Succeed())
				return
			}
			g.Expect(kc.ReconcileCoreDNSImageInfo(imageRepository, imageTag)).To(Succeed())
			g.Expect(kc.GetConfigMap().Data).To(HaveKey(clusterConfigurationKey))

			type dns struct {
				Type            string `yaml:"type"`
				ImageRepository string `yaml:"imageRepository"`
				ImageTag        string `yaml:"imageTag"`
			}
			var actualClusterConfig struct {
				DNS dns `yaml:"dns"`
			}

			g.Expect(yaml.Unmarshal([]byte(kc.GetConfigMap().Data[clusterConfigurationKey]), &actualClusterConfig)).To(Succeed())
			actualDNS := actualClusterConfig.DNS
			g.Expect(actualDNS.Type).To(BeEquivalentTo(kubeadmv1.CoreDNS))
			g.Expect(actualDNS.ImageRepository).To(Equal(imageRepository))
			g.Expect(actualDNS.ImageTag).To(Equal(imageTag))
		})
	}
}

func TestUpdateImageRepository(t *testing.T) {

	tests := []struct {
		name            string
		data            map[string]string
		imageRepository string
		expected        string
		expectErr       error
	}{
		{
			name: "it should set the values, if they were empty",
			data: map[string]string{
				clusterConfigurationKey: `
apiVersion: kubeadm.k8s.io/v1beta2
kind: ClusterConfiguration
imageRepository: k8s.gcr.io
`},
			imageRepository: "example.com/k8s",
			expected:        "example.com/k8s",
		},
		{
			name: "it shouldn't write empty strings",
			data: map[string]string{
				clusterConfigurationKey: `
apiVersion: kubeadm.k8s.io/v1beta2
kind: ClusterConfiguration
imageRepository: k8s.gcr.io
`},
			imageRepository: "",
			expected:        "k8s.gcr.io",
		},
		{
			name: "it should error if it's not a valid k8s object",
			data: map[string]string{
				clusterConfigurationKey: `
imageRepository: "cool"
`},
			imageRepository: "example.com/k8s",
			expectErr:       errors.New("Object 'Kind' is missing"),
		},
		{
			name:            "returns an error if config map doesn't have the cluster config data key",
			data:            map[string]string{},
			imageRepository: "example.com/k8s",
			expectErr:       errors.New("unable to find \"ClusterConfiguration\" key in kubeadm ConfigMap"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)

			kconfig := NewKubeadmConfig(&corev1.ConfigMap{
				Data: test.data,
			})

			err := kconfig.ReconcileImageRepository(test.imageRepository)
			if test.expectErr == nil {
				g.Expect(err).ToNot(HaveOccurred())
			} else {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring(test.expectErr.Error()))
			}

			g.Expect(kconfig.GetConfigMap().Data[clusterConfigurationKey]).To(ContainSubstring(test.expected))
		})
	}
}

func TestAPIServer(t *testing.T) {

	tests := []struct {
		name         string
		data         map[string]string
		newAPIServer kubeadmv1.APIServer
		expected     string
		expectErr    error
	}{
		{
			name: "it should set the values when no api server config is present",
			data: map[string]string{
				clusterConfigurationKey: `apiVersion: kubeadm.k8s.io/v1beta2
kind: ClusterConfiguration
`},
			newAPIServer: kubeadmv1.APIServer{
				ControlPlaneComponent: kubeadmv1.ControlPlaneComponent{
					ExtraArgs: map[string]string{
						"foo": "bar",
					},
				},
				CertSANs: []string{"foo", "bar"},
			},
			expected: `apiServer:
  certSANs:
  - foo
  - bar
  extraArgs:
    foo: bar
apiVersion: kubeadm.k8s.io/v1beta2
kind: ClusterConfiguration
`,
		},
		{
			name: "it should override existing config with the values set in spec",
			data: map[string]string{
				clusterConfigurationKey: `apiVersion: kubeadm.k8s.io/v1beta2
kind: ClusterConfiguration
apiServer:
  certSANs:
  - foo
  - bar
  extraArgs:
   foo: bar
  extraVolumes:
   - name: mount1
     hostPath: /foo/bar
     mountPath: /bar/baz
  timeoutForControlPlane: 4m0s
`},
			newAPIServer: kubeadmv1.APIServer{
				ControlPlaneComponent: kubeadmv1.ControlPlaneComponent{
					ExtraArgs: map[string]string{
						"bar":     "baz",
						"someKey": "someVal",
					},
					ExtraVolumes: []kubeadmv1.HostPathMount{
						{
							Name:      "mount2",
							HostPath:  "/bar/baz",
							MountPath: "/foo/bar",
						},
						{
							Name:      "anotherMount",
							HostPath:  "/a/b",
							MountPath: "/c/d",
						},
					},
				},
				CertSANs:               []string{"foo", "bar", "baz"},
				TimeoutForControlPlane: &metav1.Duration{Duration: 5 * time.Minute},
			},
			expected: `apiServer:
  certSANs:
  - foo
  - bar
  - baz
  extraArgs:
    bar: baz
    someKey: someVal
  extraVolumes:
  - hostPath: /bar/baz
    mountPath: /foo/bar
    name: mount2
  - hostPath: /a/b
    mountPath: /c/d
    name: anotherMount
  timeoutForControlPlane: 5m0s
apiVersion: kubeadm.k8s.io/v1beta2
kind: ClusterConfiguration
`,
		},
		{
			name: "it should not do anything if there are no changes",
			data: map[string]string{
				clusterConfigurationKey: `apiServer:
  certSANs:
  - foo
  - bar
  extraArgs:
   foo: bar
   bar: baz
  extraVolumes:
  - hostPath: /foo/bar
    mountPath: /bar/baz
    name: mount1
  - hostPath: /a/b
    mountPath: /c/d
    name: mount2   
  timeoutForControlPlane: 3m0s 
apiVersion: kubeadm.k8s.io/v1beta2
kind: ClusterConfiguration
`},
			newAPIServer: kubeadmv1.APIServer{
				ControlPlaneComponent: kubeadmv1.ControlPlaneComponent{
					ExtraArgs: map[string]string{"foo": "bar", "bar": "baz"},
					ExtraVolumes: []kubeadmv1.HostPathMount{{
						Name:      "mount1",
						HostPath:  "/foo/bar",
						MountPath: "/bar/baz",
					},
						{
							Name:      "mount2",
							HostPath:  "/a/b",
							MountPath: "/c/d",
						},
					},
				},
				CertSANs:               []string{"foo", "bar"},
				TimeoutForControlPlane: &metav1.Duration{Duration: 3 * time.Minute},
			},
			expected: `apiServer:
  certSANs:
  - foo
  - bar
  extraArgs:
   foo: bar
   bar: baz
  extraVolumes:
  - hostPath: /foo/bar
    mountPath: /bar/baz
    name: mount1
  - hostPath: /a/b
    mountPath: /c/d
    name: mount2   
  timeoutForControlPlane: 3m0s 
apiVersion: kubeadm.k8s.io/v1beta2
kind: ClusterConfiguration
`,
		},
		{
			name: "it should return error when the config is invalid",
			data: map[string]string{
				clusterConfigurationKey: `apiServer: invalidJson`},
			newAPIServer: kubeadmv1.APIServer{
				CertSANs: []string{"foo", "bar"},
			},
			expectErr: errors.New(""),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)

			kconfig := NewKubeadmConfig(&corev1.ConfigMap{
				Data: test.data,
			})

			err := kconfig.ReconcileAPIServer(test.newAPIServer)
			if test.expectErr == nil {
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(kconfig.GetConfigMap().Data[clusterConfigurationKey]).Should(Equal(test.expected))
			} else {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring(test.expectErr.Error()))
			}

		})
	}
}

func TestControllerManager(t *testing.T) {

	tests := []struct {
		name                 string
		data                 map[string]string
		newControllerManager kubeadmv1.ControlPlaneComponent
		expected             string
		expectErr            error
	}{
		{
			name: "it should set the values when no controller manager config is present",
			data: map[string]string{
				clusterConfigurationKey: `apiVersion: kubeadm.k8s.io/v1beta2
kind: ClusterConfiguration
`},
			newControllerManager: kubeadmv1.ControlPlaneComponent{
				ExtraArgs: map[string]string{
					"foo": "bar",
				},
				ExtraVolumes: []kubeadmv1.HostPathMount{{Name: "mount1", HostPath: "/foo", MountPath: "/bar"}},
			},
			expected: `apiVersion: kubeadm.k8s.io/v1beta2
controllerManager:
  extraArgs:
    foo: bar
  extraVolumes:
  - hostPath: /foo
    mountPath: /bar
    name: mount1
kind: ClusterConfiguration
`,
		},
		{
			name: "it should override existing config with the values set in spec",
			data: map[string]string{
				clusterConfigurationKey: `apiVersion: kubeadm.k8s.io/v1beta2
kind: ClusterConfiguration
controllerManager:
  extraArgs:
   foo: bar
  extraVolumes:
   - name: mount1
     hostPath: /foo/bar
     mountPath: /bar/baz
`},
			newControllerManager: kubeadmv1.ControlPlaneComponent{
				ExtraArgs: map[string]string{
					"bar":     "baz",
					"someKey": "someVal",
				},
				ExtraVolumes: []kubeadmv1.HostPathMount{
					{
						Name:      "mount2",
						HostPath:  "/bar/baz",
						MountPath: "/foo/bar",
					},
					{
						Name:      "anotherMount",
						HostPath:  "/a/b",
						MountPath: "/c/d",
					},
				},
			},
			expected: `apiVersion: kubeadm.k8s.io/v1beta2
controllerManager:
  extraArgs:
    bar: baz
    someKey: someVal
  extraVolumes:
  - hostPath: /bar/baz
    mountPath: /foo/bar
    name: mount2
  - hostPath: /a/b
    mountPath: /c/d
    name: anotherMount
kind: ClusterConfiguration
`,
		},
		{
			name: "it should not do anything if there are no changes",
			data: map[string]string{
				clusterConfigurationKey: `controllerManager:
  extraArgs:
   foo: bar
   bar: baz
  extraVolumes:
  - hostPath: /foo/bar
    mountPath: /bar/baz
    name: mount1
  - hostPath: /a/b
    mountPath: /c/d
    name: mount2
apiVersion: kubeadm.k8s.io/v1beta2
kind: ClusterConfiguration
`},
			newControllerManager: kubeadmv1.ControlPlaneComponent{
				ExtraArgs: map[string]string{"foo": "bar", "bar": "baz"},
				ExtraVolumes: []kubeadmv1.HostPathMount{{
					Name:      "mount1",
					HostPath:  "/foo/bar",
					MountPath: "/bar/baz",
				},
					{
						Name:      "mount2",
						HostPath:  "/a/b",
						MountPath: "/c/d",
					},
				},
			},
			expected: `controllerManager:
  extraArgs:
   foo: bar
   bar: baz
  extraVolumes:
  - hostPath: /foo/bar
    mountPath: /bar/baz
    name: mount1
  - hostPath: /a/b
    mountPath: /c/d
    name: mount2
apiVersion: kubeadm.k8s.io/v1beta2
kind: ClusterConfiguration
`,
		},
		{
			name: "it should return error when the config is invalid",
			data: map[string]string{
				clusterConfigurationKey: `controllerManager: invalidJson`},
			newControllerManager: kubeadmv1.ControlPlaneComponent{
				ExtraArgs: map[string]string{"foo": "bar", "bar": "baz"},
			},
			expectErr: errors.New(""),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)

			kconfig := NewKubeadmConfig(&corev1.ConfigMap{
				Data: test.data,
			})

			err := kconfig.ReconcileControllerManager(test.newControllerManager)
			if test.expectErr == nil {
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(kconfig.GetConfigMap().Data[clusterConfigurationKey]).Should(Equal(test.expected))
			} else {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring(test.expectErr.Error()))
			}

		})
	}
}

func TestScheduler(t *testing.T) {

	tests := []struct {
		name         string
		data         map[string]string
		newScheduler kubeadmv1.ControlPlaneComponent
		expected     string
		expectErr    error
	}{
		{
			name: "it should set the values when no scheduler config is present",
			data: map[string]string{
				clusterConfigurationKey: `apiVersion: kubeadm.k8s.io/v1beta2
kind: ClusterConfiguration
`},
			newScheduler: kubeadmv1.ControlPlaneComponent{
				ExtraArgs: map[string]string{
					"foo": "bar",
				},
				ExtraVolumes: []kubeadmv1.HostPathMount{{Name: "mount1", HostPath: "/foo", MountPath: "/bar"}},
			},
			expected: `apiVersion: kubeadm.k8s.io/v1beta2
kind: ClusterConfiguration
scheduler:
  extraArgs:
    foo: bar
  extraVolumes:
  - hostPath: /foo
    mountPath: /bar
    name: mount1
`,
		},
		{
			name: "it should override existing config with the values set in spec",
			data: map[string]string{
				clusterConfigurationKey: `apiVersion: kubeadm.k8s.io/v1beta2
kind: ClusterConfiguration
scheduler:
  extraArgs:
   foo: bar
  extraVolumes:
   - name: mount1
     hostPath: /foo/bar
     mountPath: /bar/baz
`},
			newScheduler: kubeadmv1.ControlPlaneComponent{
				ExtraArgs: map[string]string{
					"bar":     "baz",
					"someKey": "someVal",
				},
				ExtraVolumes: []kubeadmv1.HostPathMount{
					{
						Name:      "mount2",
						HostPath:  "/bar/baz",
						MountPath: "/foo/bar",
					},
					{
						Name:      "anotherMount",
						HostPath:  "/a/b",
						MountPath: "/c/d",
					},
				},
			},
			expected: `apiVersion: kubeadm.k8s.io/v1beta2
kind: ClusterConfiguration
scheduler:
  extraArgs:
    bar: baz
    someKey: someVal
  extraVolumes:
  - hostPath: /bar/baz
    mountPath: /foo/bar
    name: mount2
  - hostPath: /a/b
    mountPath: /c/d
    name: anotherMount
`,
		},
		{
			name: "it should not do anything if there are no changes",
			data: map[string]string{
				clusterConfigurationKey: `scheduler:
  extraArgs:
   foo: bar
   bar: baz
  extraVolumes:
  - hostPath: /foo/bar
    mountPath: /bar/baz
    name: mount1
  - hostPath: /a/b
    mountPath: /c/d
    name: mount2
apiVersion: kubeadm.k8s.io/v1beta2
kind: ClusterConfiguration
`},
			newScheduler: kubeadmv1.ControlPlaneComponent{
				ExtraArgs: map[string]string{"foo": "bar", "bar": "baz"},
				ExtraVolumes: []kubeadmv1.HostPathMount{{
					Name:      "mount1",
					HostPath:  "/foo/bar",
					MountPath: "/bar/baz",
				},
					{
						Name:      "mount2",
						HostPath:  "/a/b",
						MountPath: "/c/d",
					},
				},
			},
			expected: `scheduler:
  extraArgs:
   foo: bar
   bar: baz
  extraVolumes:
  - hostPath: /foo/bar
    mountPath: /bar/baz
    name: mount1
  - hostPath: /a/b
    mountPath: /c/d
    name: mount2
apiVersion: kubeadm.k8s.io/v1beta2
kind: ClusterConfiguration
`,
		},
		{
			name: "it should return error when the config is invalid",
			data: map[string]string{
				clusterConfigurationKey: `scheduler: invalidJson`},
			newScheduler: kubeadmv1.ControlPlaneComponent{
				ExtraArgs: map[string]string{"foo": "bar", "bar": "baz"},
			},
			expectErr: errors.New(""),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)

			kconfig := NewKubeadmConfig(&corev1.ConfigMap{
				Data: test.data,
			})

			err := kconfig.ReconcileScheduler(test.newScheduler)
			if test.expectErr == nil {
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(kconfig.GetConfigMap().Data[clusterConfigurationKey]).Should(Equal(test.expected))
			} else {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring(test.expectErr.Error()))
			}
		})
	}
}
