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

package v1alpha4

import (
	"testing"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
	bootstrapv1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1alpha4"
	utildefaulting "sigs.k8s.io/cluster-api/util/defaulting"
)

func TestKubeadmControlPlaneDefault(t *testing.T) {
	g := NewWithT(t)

	kcp := &KubeadmControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "foo",
		},
		Spec: KubeadmControlPlaneSpec{
			Version: "v1.18.3",
			MachineTemplate: KubeadmControlPlaneMachineTemplate{
				InfrastructureRef: corev1.ObjectReference{
					APIVersion: "test/v1alpha1",
					Kind:       "UnknownInfraMachine",
					Name:       "foo",
				},
			},
			RolloutStrategy: &RolloutStrategy{},
		},
	}
	updateDefaultingValidationKCP := kcp.DeepCopy()
	updateDefaultingValidationKCP.Spec.Version = "v1.18.3"
	updateDefaultingValidationKCP.Spec.MachineTemplate.InfrastructureRef = corev1.ObjectReference{
		APIVersion: "test/v1alpha1",
		Kind:       "UnknownInfraMachine",
		Name:       "foo",
		Namespace:  "foo",
	}
	t.Run("for KubeadmControlPLane", utildefaulting.DefaultValidateTest(updateDefaultingValidationKCP))
	kcp.Default()

	g.Expect(kcp.Spec.MachineTemplate.InfrastructureRef.Namespace).To(Equal(kcp.Namespace))
	g.Expect(kcp.Spec.Version).To(Equal("v1.18.3"))
	g.Expect(kcp.Spec.RolloutStrategy.Type).To(Equal(RollingUpdateStrategyType))
	g.Expect(kcp.Spec.RolloutStrategy.RollingUpdate.MaxSurge.IntVal).To(Equal(int32(1)))
}

func TestKubeadmControlPlaneValidateCreate(t *testing.T) {
	valid := &KubeadmControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "foo",
		},
		Spec: KubeadmControlPlaneSpec{
			MachineTemplate: KubeadmControlPlaneMachineTemplate{
				InfrastructureRef: corev1.ObjectReference{
					APIVersion: "test/v1alpha1",
					Kind:       "UnknownInfraMachine",
					Namespace:  "foo",
					Name:       "infraTemplate",
				},
			},
			Replicas: pointer.Int32Ptr(1),
			Version:  "v1.19.0",
			RolloutStrategy: &RolloutStrategy{
				Type: RollingUpdateStrategyType,
				RollingUpdate: &RollingUpdate{
					MaxSurge: &intstr.IntOrString{
						IntVal: 1,
					},
				},
			},
		},
	}

	invalidMaxSurge := valid.DeepCopy()
	invalidMaxSurge.Spec.RolloutStrategy.RollingUpdate.MaxSurge.IntVal = int32(3)

	invalidNamespace := valid.DeepCopy()
	invalidNamespace.Spec.MachineTemplate.InfrastructureRef.Namespace = "bar"

	missingReplicas := valid.DeepCopy()
	missingReplicas.Spec.Replicas = nil

	zeroReplicas := valid.DeepCopy()
	zeroReplicas.Spec.Replicas = pointer.Int32Ptr(0)

	evenReplicas := valid.DeepCopy()
	evenReplicas.Spec.Replicas = pointer.Int32Ptr(2)

	evenReplicasExternalEtcd := evenReplicas.DeepCopy()
	evenReplicasExternalEtcd.Spec.KubeadmConfigSpec = bootstrapv1.KubeadmConfigSpec{
		ClusterConfiguration: &bootstrapv1.ClusterConfiguration{
			Etcd: bootstrapv1.Etcd{
				External: &bootstrapv1.ExternalEtcd{},
			},
		},
	}

	validVersion := valid.DeepCopy()
	validVersion.Spec.Version = "v1.16.6"

	invalidVersion1 := valid.DeepCopy()
	invalidVersion1.Spec.Version = "vv1.16.6"

	invalidVersion2 := valid.DeepCopy()
	invalidVersion2.Spec.Version = "1.16.6"

	withInvalidIgnitionConfiguration := valid.DeepCopy()
	withInvalidIgnitionConfiguration.Spec.KubeadmConfigSpec.Format = bootstrapv1.Ignition

	tests := []struct {
		name      string
		expectErr bool
		kcp       *KubeadmControlPlane
	}{
		{
			name:      "should succeed when given a valid config",
			expectErr: false,
			kcp:       valid,
		},
		{
			name:      "should return error when kubeadmControlPlane namespace and infrastructureTemplate  namespace mismatch",
			expectErr: true,
			kcp:       invalidNamespace,
		},
		{
			name:      "should return error when replicas is nil",
			expectErr: true,
			kcp:       missingReplicas,
		},
		{
			name:      "should return error when replicas is zero",
			expectErr: true,
			kcp:       zeroReplicas,
		},
		{
			name:      "should return error when replicas is even",
			expectErr: true,
			kcp:       evenReplicas,
		},
		{
			name:      "should allow even replicas when using external etcd",
			expectErr: false,
			kcp:       evenReplicasExternalEtcd,
		},
		{
			name:      "should succeed when given a valid semantic version with prepended 'v'",
			expectErr: false,
			kcp:       validVersion,
		},
		{
			name:      "should error when given a valid semantic version without 'v'",
			expectErr: true,
			kcp:       invalidVersion2,
		},
		{
			name:      "should return error when given an invalid semantic version",
			expectErr: true,
			kcp:       invalidVersion1,
		},
		{
			name:      "should return error when maxSurge is not 1",
			expectErr: true,
			kcp:       invalidMaxSurge,
		},
		{
			name:      "validates Ignition configuration",
			expectErr: true,
			kcp:       withInvalidIgnitionConfiguration,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			if tt.expectErr {
				g.Expect(tt.kcp.ValidateCreate()).NotTo(Succeed())
			} else {
				g.Expect(tt.kcp.ValidateCreate()).To(Succeed())
			}
		})
	}
}

func TestKubeadmControlPlaneValidateUpdate(t *testing.T) {
	before := &KubeadmControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "foo",
		},
		Spec: KubeadmControlPlaneSpec{
			MachineTemplate: KubeadmControlPlaneMachineTemplate{
				InfrastructureRef: corev1.ObjectReference{
					APIVersion: "test/v1alpha1",
					Kind:       "UnknownInfraMachine",
					Namespace:  "foo",
					Name:       "infraTemplate",
				},
			},
			Replicas: pointer.Int32Ptr(1),
			RolloutStrategy: &RolloutStrategy{
				Type: RollingUpdateStrategyType,
				RollingUpdate: &RollingUpdate{
					MaxSurge: &intstr.IntOrString{
						IntVal: 1,
					},
				},
			},
			KubeadmConfigSpec: bootstrapv1.KubeadmConfigSpec{
				InitConfiguration: &bootstrapv1.InitConfiguration{
					LocalAPIEndpoint: bootstrapv1.APIEndpoint{
						AdvertiseAddress: "127.0.0.1",
						BindPort:         int32(443),
					},
					NodeRegistration: bootstrapv1.NodeRegistrationOptions{
						Name: "test",
					},
				},
				ClusterConfiguration: &bootstrapv1.ClusterConfiguration{
					ClusterName: "test",
					DNS: bootstrapv1.DNS{
						ImageMeta: bootstrapv1.ImageMeta{
							ImageRepository: "k8s.gcr.io/coredns",
							ImageTag:        "1.6.5",
						},
					},
				},
				JoinConfiguration: &bootstrapv1.JoinConfiguration{
					Discovery: bootstrapv1.Discovery{
						Timeout: &metav1.Duration{
							Duration: 10 * time.Minute,
						},
					},
					NodeRegistration: bootstrapv1.NodeRegistrationOptions{
						Name: "test",
					},
				},
				PreKubeadmCommands: []string{
					"test", "foo",
				},
				PostKubeadmCommands: []string{
					"test", "foo",
				},
				Files: []bootstrapv1.File{
					{
						Path: "test",
					},
				},
				Users: []bootstrapv1.User{
					{
						Name: "user",
						SSHAuthorizedKeys: []string{
							"ssh-rsa foo",
						},
					},
				},
				NTP: &bootstrapv1.NTP{
					Servers: []string{"test-server-1", "test-server-2"},
					Enabled: pointer.BoolPtr(true),
				},
			},
			Version: "v1.16.6",
		},
	}

	updateMaxSurgeVal := before.DeepCopy()
	updateMaxSurgeVal.Spec.RolloutStrategy.RollingUpdate.MaxSurge.IntVal = int32(0)
	updateMaxSurgeVal.Spec.Replicas = pointer.Int32Ptr(3)

	wrongReplicaCountForScaleIn := before.DeepCopy()
	wrongReplicaCountForScaleIn.Spec.RolloutStrategy.RollingUpdate.MaxSurge.IntVal = int32(0)

	invalidUpdateKubeadmConfigInit := before.DeepCopy()
	invalidUpdateKubeadmConfigInit.Spec.KubeadmConfigSpec.InitConfiguration = &bootstrapv1.InitConfiguration{}

	validUpdateKubeadmConfigInit := before.DeepCopy()
	validUpdateKubeadmConfigInit.Spec.KubeadmConfigSpec.InitConfiguration.NodeRegistration = bootstrapv1.NodeRegistrationOptions{}

	invalidUpdateKubeadmConfigCluster := before.DeepCopy()
	invalidUpdateKubeadmConfigCluster.Spec.KubeadmConfigSpec.ClusterConfiguration = &bootstrapv1.ClusterConfiguration{}

	invalidUpdateKubeadmConfigJoin := before.DeepCopy()
	invalidUpdateKubeadmConfigJoin.Spec.KubeadmConfigSpec.JoinConfiguration = &bootstrapv1.JoinConfiguration{}

	validUpdateKubeadmConfigJoin := before.DeepCopy()
	validUpdateKubeadmConfigJoin.Spec.KubeadmConfigSpec.JoinConfiguration.NodeRegistration = bootstrapv1.NodeRegistrationOptions{}

	validUpdate := before.DeepCopy()
	validUpdate.Labels = map[string]string{"blue": "green"}
	validUpdate.Spec.KubeadmConfigSpec.PreKubeadmCommands = []string{"ab", "abc"}
	validUpdate.Spec.KubeadmConfigSpec.PostKubeadmCommands = []string{"ab", "abc"}
	validUpdate.Spec.KubeadmConfigSpec.Files = []bootstrapv1.File{
		{
			Path: "ab",
		},
		{
			Path: "abc",
		},
	}
	validUpdate.Spec.Version = "v1.17.1"
	validUpdate.Spec.KubeadmConfigSpec.Users = []bootstrapv1.User{
		{
			Name: "bar",
			SSHAuthorizedKeys: []string{
				"ssh-rsa bar",
				"ssh-rsa foo",
			},
		},
	}
	validUpdate.Spec.MachineTemplate.InfrastructureRef.Name = "orange"
	validUpdate.Spec.Replicas = pointer.Int32Ptr(5)
	now := metav1.NewTime(time.Now())
	validUpdate.Spec.RolloutAfter = &now

	scaleToZero := before.DeepCopy()
	scaleToZero.Spec.Replicas = pointer.Int32Ptr(0)

	scaleToEven := before.DeepCopy()
	scaleToEven.Spec.Replicas = pointer.Int32Ptr(2)

	invalidNamespace := before.DeepCopy()
	invalidNamespace.Spec.MachineTemplate.InfrastructureRef.Namespace = "bar"

	missingReplicas := before.DeepCopy()
	missingReplicas.Spec.Replicas = nil

	etcdLocalImageTag := before.DeepCopy()
	etcdLocalImageTag.Spec.KubeadmConfigSpec.ClusterConfiguration.Etcd.Local = &bootstrapv1.LocalEtcd{
		ImageMeta: bootstrapv1.ImageMeta{
			ImageTag: "v9.1.1",
		},
	}

	etcdLocalImageBuildTag := before.DeepCopy()
	etcdLocalImageBuildTag.Spec.KubeadmConfigSpec.ClusterConfiguration.Etcd.Local = &bootstrapv1.LocalEtcd{
		ImageMeta: bootstrapv1.ImageMeta{
			ImageTag: "v9.1.1_validBuild1",
		},
	}

	etcdLocalImageInvalidTag := before.DeepCopy()
	etcdLocalImageInvalidTag.Spec.KubeadmConfigSpec.ClusterConfiguration.Etcd.Local = &bootstrapv1.LocalEtcd{
		ImageMeta: bootstrapv1.ImageMeta{
			ImageTag: "v9.1.1+invalidBuild1",
		},
	}

	unsetEtcd := etcdLocalImageTag.DeepCopy()
	unsetEtcd.Spec.KubeadmConfigSpec.ClusterConfiguration.Etcd.Local = nil

	networking := before.DeepCopy()
	networking.Spec.KubeadmConfigSpec.ClusterConfiguration.Networking.DNSDomain = "some dns domain"

	kubernetesVersion := before.DeepCopy()
	kubernetesVersion.Spec.KubeadmConfigSpec.ClusterConfiguration.KubernetesVersion = "some kubernetes version"

	prevKCPWithVersion := func(version string) *KubeadmControlPlane {
		prev := before.DeepCopy()
		prev.Spec.Version = version
		return prev
	}
	skipMinorControlPlaneVersion := prevKCPWithVersion("v1.18.1")
	emptyControlPlaneVersion := prevKCPWithVersion("")

	controlPlaneEndpoint := before.DeepCopy()
	controlPlaneEndpoint.Spec.KubeadmConfigSpec.ClusterConfiguration.ControlPlaneEndpoint = "some control plane endpoint"

	apiServer := before.DeepCopy()
	apiServer.Spec.KubeadmConfigSpec.ClusterConfiguration.APIServer = bootstrapv1.APIServer{
		ControlPlaneComponent: bootstrapv1.ControlPlaneComponent{
			ExtraArgs:    map[string]string{"foo": "bar"},
			ExtraVolumes: []bootstrapv1.HostPathMount{{Name: "mount1"}},
		},
		TimeoutForControlPlane: &metav1.Duration{Duration: 5 * time.Minute},
		CertSANs:               []string{"foo", "bar"},
	}

	controllerManager := before.DeepCopy()
	controllerManager.Spec.KubeadmConfigSpec.ClusterConfiguration.ControllerManager = bootstrapv1.ControlPlaneComponent{
		ExtraArgs:    map[string]string{"controller manager field": "controller manager value"},
		ExtraVolumes: []bootstrapv1.HostPathMount{{Name: "mount", HostPath: "/foo", MountPath: "bar", ReadOnly: true, PathType: "File"}},
	}

	scheduler := before.DeepCopy()
	scheduler.Spec.KubeadmConfigSpec.ClusterConfiguration.Scheduler = bootstrapv1.ControlPlaneComponent{
		ExtraArgs:    map[string]string{"scheduler field": "scheduler value"},
		ExtraVolumes: []bootstrapv1.HostPathMount{{Name: "mount", HostPath: "/foo", MountPath: "bar", ReadOnly: true, PathType: "File"}},
	}

	dns := before.DeepCopy()
	dns.Spec.KubeadmConfigSpec.ClusterConfiguration.DNS = bootstrapv1.DNS{
		ImageMeta: bootstrapv1.ImageMeta{
			ImageRepository: "gcr.io/capi-test",
			ImageTag:        "v1.6.6_foobar.1",
		},
	}

	dnsBuildTag := before.DeepCopy()
	dnsBuildTag.Spec.KubeadmConfigSpec.ClusterConfiguration.DNS = bootstrapv1.DNS{
		ImageMeta: bootstrapv1.ImageMeta{
			ImageRepository: "gcr.io/capi-test",
			ImageTag:        "1.6.7",
		},
	}

	dnsInvalidTag := before.DeepCopy()
	dnsInvalidTag.Spec.KubeadmConfigSpec.ClusterConfiguration.DNS = bootstrapv1.DNS{
		ImageMeta: bootstrapv1.ImageMeta{
			ImageRepository: "gcr.io/capi-test",
			ImageTag:        "v0.20.0+invalidBuild1",
		},
	}

	dnsInvalidCoreDNSToVersion := dns.DeepCopy()
	dnsInvalidCoreDNSToVersion.Spec.KubeadmConfigSpec.ClusterConfiguration.DNS = bootstrapv1.DNS{
		ImageMeta: bootstrapv1.ImageMeta{
			ImageRepository: "gcr.io/capi-test",
			ImageTag:        "1.6.5",
		},
	}

	validCoreDNSCustomToVersion := dns.DeepCopy()
	validCoreDNSCustomToVersion.Spec.KubeadmConfigSpec.ClusterConfiguration.DNS = bootstrapv1.DNS{
		ImageMeta: bootstrapv1.ImageMeta{
			ImageRepository: "gcr.io/capi-test",
			ImageTag:        "v1.6.6_foobar.2",
		},
	}

	unsetCoreDNSToVersion := dns.DeepCopy()
	unsetCoreDNSToVersion.Spec.KubeadmConfigSpec.ClusterConfiguration.DNS = bootstrapv1.DNS{
		ImageMeta: bootstrapv1.ImageMeta{
			ImageRepository: "",
			ImageTag:        "",
		},
	}

	certificatesDir := before.DeepCopy()
	certificatesDir.Spec.KubeadmConfigSpec.ClusterConfiguration.CertificatesDir = "a new certificates directory"

	imageRepository := before.DeepCopy()
	imageRepository.Spec.KubeadmConfigSpec.ClusterConfiguration.ImageRepository = "a new image repository"

	featureGates := before.DeepCopy()
	featureGates.Spec.KubeadmConfigSpec.ClusterConfiguration.FeatureGates = map[string]bool{"a feature gate": true}

	externalEtcd := before.DeepCopy()
	externalEtcd.Spec.KubeadmConfigSpec.ClusterConfiguration.Etcd.External = &bootstrapv1.ExternalEtcd{
		KeyFile: "some key file",
	}

	localDataDir := before.DeepCopy()
	localDataDir.Spec.KubeadmConfigSpec.ClusterConfiguration.Etcd.Local = &bootstrapv1.LocalEtcd{
		DataDir: "some local data dir",
	}
	modifyLocalDataDir := localDataDir.DeepCopy()
	modifyLocalDataDir.Spec.KubeadmConfigSpec.ClusterConfiguration.Etcd.Local.DataDir = "a different local data dir"

	localPeerCertSANs := before.DeepCopy()
	localPeerCertSANs.Spec.KubeadmConfigSpec.ClusterConfiguration.Etcd.Local = &bootstrapv1.LocalEtcd{
		PeerCertSANs: []string{"a cert"},
	}

	localServerCertSANs := before.DeepCopy()
	localServerCertSANs.Spec.KubeadmConfigSpec.ClusterConfiguration.Etcd.Local = &bootstrapv1.LocalEtcd{
		ServerCertSANs: []string{"a cert"},
	}

	localExtraArgs := before.DeepCopy()
	localExtraArgs.Spec.KubeadmConfigSpec.ClusterConfiguration.Etcd.Local = &bootstrapv1.LocalEtcd{
		ExtraArgs: map[string]string{"an arg": "a value"},
	}

	beforeExternalEtcdCluster := before.DeepCopy()
	beforeExternalEtcdCluster.Spec.KubeadmConfigSpec.ClusterConfiguration = &bootstrapv1.ClusterConfiguration{
		Etcd: bootstrapv1.Etcd{
			External: &bootstrapv1.ExternalEtcd{
				Endpoints: []string{"127.0.0.1"},
			},
		},
	}
	scaleToEvenExternalEtcdCluster := beforeExternalEtcdCluster.DeepCopy()
	scaleToEvenExternalEtcdCluster.Spec.Replicas = pointer.Int32Ptr(2)

	beforeInvalidEtcdCluster := before.DeepCopy()
	beforeInvalidEtcdCluster.Spec.KubeadmConfigSpec.ClusterConfiguration.Etcd = bootstrapv1.Etcd{
		Local: &bootstrapv1.LocalEtcd{
			ImageMeta: bootstrapv1.ImageMeta{
				ImageRepository: "image-repository",
				ImageTag:        "latest",
			},
		},
	}

	afterInvalidEtcdCluster := beforeInvalidEtcdCluster.DeepCopy()
	afterInvalidEtcdCluster.Spec.KubeadmConfigSpec.ClusterConfiguration.Etcd = bootstrapv1.Etcd{
		External: &bootstrapv1.ExternalEtcd{
			Endpoints: []string{"127.0.0.1"},
		},
	}

	withoutClusterConfiguration := before.DeepCopy()
	withoutClusterConfiguration.Spec.KubeadmConfigSpec.ClusterConfiguration = nil

	afterEtcdLocalDirAddition := before.DeepCopy()
	afterEtcdLocalDirAddition.Spec.KubeadmConfigSpec.ClusterConfiguration.Etcd.Local = &bootstrapv1.LocalEtcd{
		DataDir: "/data",
	}

	disallowedUpgrade118Prev := prevKCPWithVersion("v1.18.8")
	disallowedUpgrade119Version := before.DeepCopy()
	disallowedUpgrade119Version.Spec.Version = "v1.19.0"

	updateNTPServers := before.DeepCopy()
	updateNTPServers.Spec.KubeadmConfigSpec.NTP.Servers = []string{"new-server"}

	disableNTPServers := before.DeepCopy()
	disableNTPServers.Spec.KubeadmConfigSpec.NTP.Enabled = pointer.BoolPtr(false)

	withInvalidIgnitionConfiguration := before.DeepCopy()
	withInvalidIgnitionConfiguration.Spec.KubeadmConfigSpec.Format = bootstrapv1.Ignition

	tests := []struct {
		name      string
		expectErr bool
		before    *KubeadmControlPlane
		kcp       *KubeadmControlPlane
	}{
		{
			name:      "should succeed when given a valid config",
			expectErr: false,
			before:    before,
			kcp:       validUpdate,
		},
		{
			name:      "should return error when trying to mutate the kubeadmconfigspec initconfiguration",
			expectErr: true,
			before:    before,
			kcp:       invalidUpdateKubeadmConfigInit,
		},
		{
			name:      "should not return an error when trying to mutate the kubeadmconfigspec initconfiguration noderegistration",
			expectErr: false,
			before:    before,
			kcp:       validUpdateKubeadmConfigInit,
		},
		{
			name:      "should return error when trying to mutate the kubeadmconfigspec clusterconfiguration",
			expectErr: true,
			before:    before,
			kcp:       invalidUpdateKubeadmConfigCluster,
		},
		{
			name:      "should return error when trying to mutate the kubeadmconfigspec joinconfiguration",
			expectErr: true,
			before:    before,
			kcp:       invalidUpdateKubeadmConfigJoin,
		},
		{
			name:      "should not return an error when trying to mutate the kubeadmconfigspec joinconfiguration noderegistration",
			expectErr: false,
			before:    before,
			kcp:       validUpdateKubeadmConfigJoin,
		},
		{
			name:      "should return error when trying to scale to zero",
			expectErr: true,
			before:    before,
			kcp:       scaleToZero,
		},
		{
			name:      "should return error when trying to scale to an even number",
			expectErr: true,
			before:    before,
			kcp:       scaleToEven,
		},
		{
			name:      "should return error when trying to reference cross namespace",
			expectErr: true,
			before:    before,
			kcp:       invalidNamespace,
		},
		{
			name:      "should return error when trying to scale to nil",
			expectErr: true,
			before:    before,
			kcp:       missingReplicas,
		},
		{
			name:      "should succeed when trying to scale to an even number with external etcd defined in ClusterConfiguration",
			expectErr: false,
			before:    beforeExternalEtcdCluster,
			kcp:       scaleToEvenExternalEtcdCluster,
		},
		{
			name:      "should succeed when making a change to the local etcd image tag",
			expectErr: false,
			before:    before,
			kcp:       etcdLocalImageTag,
		},
		{
			name:      "should succeed when making a change to the local etcd image tag",
			expectErr: false,
			before:    before,
			kcp:       etcdLocalImageBuildTag,
		},
		{
			name:      "should fail when using an invalid etcd image tag",
			expectErr: true,
			before:    before,
			kcp:       etcdLocalImageInvalidTag,
		},
		{
			name:      "should fail when making a change to the cluster config's networking struct",
			expectErr: true,
			before:    before,
			kcp:       networking,
		},
		{
			name:      "should fail when making a change to the cluster config's kubernetes version",
			expectErr: true,
			before:    before,
			kcp:       kubernetesVersion,
		},
		{
			name:      "should fail when making a change to the cluster config's controlPlaneEndpoint",
			expectErr: true,
			before:    before,
			kcp:       controlPlaneEndpoint,
		},
		{
			name:      "should allow changes to the cluster config's apiServer",
			expectErr: false,
			before:    before,
			kcp:       apiServer,
		},
		{
			name:      "should allow changes to the cluster config's controllerManager",
			expectErr: false,
			before:    before,
			kcp:       controllerManager,
		},
		{
			name:      "should allow changes to the cluster config's scheduler",
			expectErr: false,
			before:    before,
			kcp:       scheduler,
		},
		{
			name:      "should succeed when making a change to the cluster config's dns",
			expectErr: false,
			before:    before,
			kcp:       dns,
		},
		{
			name:      "should succeed when changing to a valid custom CoreDNS version",
			expectErr: false,
			before:    dns,
			kcp:       validCoreDNSCustomToVersion,
		},
		{
			name:      "should succeed when CoreDNS ImageTag is unset",
			expectErr: false,
			before:    dns,
			kcp:       unsetCoreDNSToVersion,
		},
		{
			name:      "should succeed when using an valid DNS build",
			expectErr: false,
			before:    before,
			kcp:       dnsBuildTag,
		},
		{
			name:      "should fail when using an invalid DNS build",
			expectErr: true,
			before:    before,
			kcp:       dnsInvalidTag,
		},
		{
			name:      "should fail when using an invalid CoreDNS version",
			expectErr: true,
			before:    dns,
			kcp:       dnsInvalidCoreDNSToVersion,
		},
		{
			name:      "should fail when making a change to the cluster config's certificatesDir",
			expectErr: true,
			before:    before,
			kcp:       certificatesDir,
		},
		{
			name:      "should fail when making a change to the cluster config's imageRepository",
			expectErr: false,
			before:    before,
			kcp:       imageRepository,
		},
		{
			name:      "should fail when making a change to the cluster config's featureGates",
			expectErr: true,
			before:    before,
			kcp:       featureGates,
		},
		{
			name:      "should fail when making a change to the cluster config's local etcd's configuration localDataDir field",
			expectErr: true,
			before:    before,
			kcp:       localDataDir,
		},
		{
			name:      "should fail when making a change to the cluster config's local etcd's configuration localPeerCertSANs field",
			expectErr: true,
			before:    before,
			kcp:       localPeerCertSANs,
		},
		{
			name:      "should fail when making a change to the cluster config's local etcd's configuration localServerCertSANs field",
			expectErr: true,
			before:    before,
			kcp:       localServerCertSANs,
		},
		{
			name:      "should fail when making a change to the cluster config's local etcd's configuration localExtraArgs field",
			expectErr: true,
			before:    before,
			kcp:       localExtraArgs,
		},
		{
			name:      "should fail when making a change to the cluster config's external etcd's configuration",
			expectErr: true,
			before:    before,
			kcp:       externalEtcd,
		},
		{
			name:      "should fail when attempting to unset the etcd local object",
			expectErr: true,
			before:    etcdLocalImageTag,
			kcp:       unsetEtcd,
		},
		{
			name:      "should fail when modifying a field that is not the local etcd image metadata",
			expectErr: true,
			before:    localDataDir,
			kcp:       modifyLocalDataDir,
		},
		{
			name:      "should fail if both local and external etcd are set",
			expectErr: true,
			before:    beforeInvalidEtcdCluster,
			kcp:       afterInvalidEtcdCluster,
		},
		{
			name:      "should pass if ClusterConfiguration is nil",
			expectErr: false,
			before:    withoutClusterConfiguration,
			kcp:       withoutClusterConfiguration,
		},
		{
			name:      "should fail if etcd local dir is changed from missing ClusterConfiguration",
			expectErr: true,
			before:    withoutClusterConfiguration,
			kcp:       afterEtcdLocalDirAddition,
		},
		{
			name:      "should fail when skipping control plane minor versions",
			expectErr: true,
			before:    before,
			kcp:       skipMinorControlPlaneVersion,
		},
		{
			name:      "should fail when no control plane version is passed",
			expectErr: true,
			before:    before,
			kcp:       emptyControlPlaneVersion,
		},
		{
			name:      "should pass if control plane version is the same",
			expectErr: false,
			before:    before,
			kcp:       before.DeepCopy(),
		},
		{
			name:      "should return error when trying to upgrade to v1.19.0",
			expectErr: true,
			before:    disallowedUpgrade118Prev,
			kcp:       disallowedUpgrade119Version,
		},
		{
			name:      "should not return an error when maxSurge value is updated to 0",
			expectErr: false,
			before:    before,
			kcp:       updateMaxSurgeVal,
		},
		{
			name:      "should return an error when maxSurge value is updated to 0, but replica count is < 3",
			expectErr: true,
			before:    before,
			kcp:       wrongReplicaCountForScaleIn,
		},
		{
			name:      "should pass if NTP servers are updated",
			expectErr: false,
			before:    before,
			kcp:       updateNTPServers,
		},
		{
			name:      "should pass if NTP servers is disabled during update",
			expectErr: false,
			before:    before,
			kcp:       disableNTPServers,
		},
		{
			name:      "validates Ignition configuration",
			expectErr: true,
			before:    withInvalidIgnitionConfiguration,
			kcp:       withInvalidIgnitionConfiguration,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			err := tt.kcp.ValidateUpdate(tt.before.DeepCopy())
			if tt.expectErr {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).To(Succeed())
			}
		})
	}
}

func TestKubeadmControlPlaneValidateUpdateAfterDefaulting(t *testing.T) {
	before := &KubeadmControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "foo",
		},
		Spec: KubeadmControlPlaneSpec{
			Version: "v1.19.0",
			MachineTemplate: KubeadmControlPlaneMachineTemplate{
				InfrastructureRef: corev1.ObjectReference{
					APIVersion: "test/v1alpha1",
					Kind:       "UnknownInfraMachine",
					Namespace:  "foo",
					Name:       "infraTemplate",
				},
			},
		},
	}

	afterDefault := before.DeepCopy()
	afterDefault.Default()

	tests := []struct {
		name      string
		expectErr bool
		before    *KubeadmControlPlane
		kcp       *KubeadmControlPlane
	}{
		{
			name:      "update should succeed after defaulting",
			expectErr: false,
			before:    before,
			kcp:       afterDefault,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			err := tt.kcp.ValidateUpdate(tt.before.DeepCopy())
			if tt.expectErr {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).To(Succeed())
				g.Expect(tt.kcp.Spec.MachineTemplate.InfrastructureRef.Namespace).To(Equal(tt.before.Namespace))
				g.Expect(tt.kcp.Spec.Version).To(Equal("v1.19.0"))
				g.Expect(tt.kcp.Spec.RolloutStrategy.Type).To(Equal(RollingUpdateStrategyType))
				g.Expect(tt.kcp.Spec.RolloutStrategy.RollingUpdate.MaxSurge.IntVal).To(Equal(int32(1)))
				g.Expect(tt.kcp.Spec.Replicas).To(Equal(pointer.Int32Ptr(1)))
			}
		})
	}
}

func TestPathsMatch(t *testing.T) {
	tests := []struct {
		name          string
		allowed, path []string
		match         bool
	}{
		{
			name:    "a simple match case",
			allowed: []string{"a", "b", "c"},
			path:    []string{"a", "b", "c"},
			match:   true,
		},
		{
			name:    "a case can't match",
			allowed: []string{"a", "b", "c"},
			path:    []string{"a"},
			match:   false,
		},
		{
			name:    "an empty path for whatever reason",
			allowed: []string{"a"},
			path:    []string{""},
			match:   false,
		},
		{
			name:    "empty allowed matches nothing",
			allowed: []string{},
			path:    []string{"a"},
			match:   false,
		},
		{
			name:    "wildcard match",
			allowed: []string{"a", "b", "c", "d", "*"},
			path:    []string{"a", "b", "c", "d", "e", "f", "g"},
			match:   true,
		},
		{
			name:    "long path",
			allowed: []string{"a"},
			path:    []string{"a", "b", "c", "d", "e", "f", "g"},
			match:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(pathsMatch(tt.allowed, tt.path)).To(Equal(tt.match))
		})
	}
}

func TestAllowed(t *testing.T) {
	tests := []struct {
		name      string
		allowList [][]string
		path      []string
		match     bool
	}{
		{
			name: "matches the first and none of the others",
			allowList: [][]string{
				{"a", "b", "c"},
				{"b", "d", "x"},
			},
			path:  []string{"a", "b", "c"},
			match: true,
		},
		{
			name: "matches none in the allow list",
			allowList: [][]string{
				{"a", "b", "c"},
				{"b", "c", "d"},
				{"e", "*"},
			},
			path:  []string{"a"},
			match: false,
		},
		{
			name: "an empty path matches nothing",
			allowList: [][]string{
				{"a", "b", "c"},
				{"*"},
				{"b", "c"},
			},
			path:  []string{},
			match: false,
		},
		{
			name:      "empty allowList matches nothing",
			allowList: [][]string{},
			path:      []string{"a"},
			match:     false,
		},
		{
			name: "length test check",
			allowList: [][]string{
				{"a", "b", "c", "d", "e", "f"},
				{"a", "b", "c", "d", "e", "f", "g", "h"},
			},
			path:  []string{"a", "b", "c", "d", "e", "f", "g"},
			match: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(allowed(tt.allowList, tt.path)).To(Equal(tt.match))
		})
	}
}

func TestPaths(t *testing.T) {
	tests := []struct {
		name     string
		path     []string
		diff     map[string]interface{}
		expected [][]string
	}{
		{
			name: "basic check",
			diff: map[string]interface{}{
				"spec": map[string]interface{}{
					"replicas": 4,
					"version":  "1.17.3",
					"kubeadmConfigSpec": map[string]interface{}{
						"clusterConfiguration": map[string]interface{}{
							"version": "v2.0.1",
						},
						"initConfiguration": map[string]interface{}{
							"bootstrapToken": []string{"abcd", "defg"},
						},
						"joinConfiguration": nil,
					},
				},
			},
			expected: [][]string{
				{"spec", "replicas"},
				{"spec", "version"},
				{"spec", "kubeadmConfigSpec", "joinConfiguration"},
				{"spec", "kubeadmConfigSpec", "clusterConfiguration", "version"},
				{"spec", "kubeadmConfigSpec", "initConfiguration", "bootstrapToken"},
			},
		},
		{
			name:     "empty input makes for empty output",
			path:     []string{"a"},
			diff:     map[string]interface{}{},
			expected: [][]string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(paths(tt.path, tt.diff)).To(ConsistOf(tt.expected))
		})
	}
}
