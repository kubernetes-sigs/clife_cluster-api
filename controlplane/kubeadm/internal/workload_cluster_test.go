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
	"context"
	"errors"
	"testing"

	. "github.com/onsi/gomega"

	"github.com/blang/semver"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	cabpkv1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1alpha4"
	kubeadmv1beta1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/types/v1beta1"
	"sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1alpha4"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestUpdateKubeProxyImageInfo(t *testing.T) {
	g := NewWithT(t)

	scheme := runtime.NewScheme()
	g.Expect(appsv1.AddToScheme(scheme)).To(Succeed())

	tests := []struct {
		name        string
		ds          appsv1.DaemonSet
		expectErr   bool
		expectImage string
		clientGet   map[string]interface{}
		patchErr    error
		KCP         *v1alpha4.KubeadmControlPlane
	}{
		{
			name:        "succeeds if patch correctly",
			ds:          newKubeProxyDS(),
			expectErr:   false,
			expectImage: "k8s.gcr.io/kube-proxy:v1.16.3",
			KCP:         &v1alpha4.KubeadmControlPlane{Spec: v1alpha4.KubeadmControlPlaneSpec{Version: "v1.16.3"}},
		},
		{
			name:        "returns error if image in kube-proxy ds was in digest format",
			ds:          newKubeProxyDSWithImage("k8s.gcr.io/kube-proxy@sha256:47bfd"),
			expectErr:   true,
			expectImage: "k8s.gcr.io/kube-proxy@sha256:47bfd",
			KCP:         &v1alpha4.KubeadmControlPlane{Spec: v1alpha4.KubeadmControlPlaneSpec{Version: "v1.16.3"}},
		},
		{
			name:        "expects OCI compatible format of tag",
			ds:          newKubeProxyDS(),
			expectErr:   false,
			expectImage: "k8s.gcr.io/kube-proxy:v1.16.3_build1",
			KCP:         &v1alpha4.KubeadmControlPlane{Spec: v1alpha4.KubeadmControlPlaneSpec{Version: "v1.16.3+build1"}},
		},
		{
			name:      "returns error if image in kube-proxy ds was in wrong format",
			ds:        newKubeProxyDSWithImage(""),
			expectErr: true,
			KCP:       &v1alpha4.KubeadmControlPlane{Spec: v1alpha4.KubeadmControlPlaneSpec{Version: "v1.16.3"}},
		},
		{
			name:        "updates image repository if one has been set on the control plane",
			ds:          newKubeProxyDS(),
			expectErr:   false,
			expectImage: "foo.bar.example/baz/qux/kube-proxy:v1.16.3",
			KCP: &v1alpha4.KubeadmControlPlane{
				Spec: v1alpha4.KubeadmControlPlaneSpec{
					Version: "v1.16.3",
					KubeadmConfigSpec: cabpkv1.KubeadmConfigSpec{
						ClusterConfiguration: &kubeadmv1beta1.ClusterConfiguration{
							ImageRepository: "foo.bar.example/baz/qux",
						},
					},
				}},
		},
		{
			name:        "does not update image repository if it is blank",
			ds:          newKubeProxyDS(),
			expectErr:   false,
			expectImage: "k8s.gcr.io/kube-proxy:v1.16.3",
			KCP: &v1alpha4.KubeadmControlPlane{
				Spec: v1alpha4.KubeadmControlPlaneSpec{
					Version: "v1.16.3",
					KubeadmConfigSpec: cabpkv1.KubeadmConfigSpec{
						ClusterConfiguration: &kubeadmv1beta1.ClusterConfiguration{
							ImageRepository: "",
						},
					},
				}},
		},
		{
			name:      "returns error if image repository is invalid",
			ds:        newKubeProxyDS(),
			expectErr: true,
			KCP: &v1alpha4.KubeadmControlPlane{
				Spec: v1alpha4.KubeadmControlPlaneSpec{
					Version: "v1.16.3",
					KubeadmConfigSpec: cabpkv1.KubeadmConfigSpec{
						ClusterConfiguration: &kubeadmv1beta1.ClusterConfiguration{
							ImageRepository: "%%%",
						},
					},
				}},
		},
		{
			name:        "does not update image repository when no kube-proxy update is requested",
			ds:          newKubeProxyDSWithImage(""), // Using the same image name that would otherwise lead to an error
			expectErr:   false,
			expectImage: "",
			KCP: &v1alpha4.KubeadmControlPlane{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						v1alpha4.SkipKubeProxyAnnotation: "",
					},
				},
				Spec: v1alpha4.KubeadmControlPlaneSpec{
					Version: "v1.16.3",
				}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gs := NewWithT(t)

			objects := []client.Object{
				&tt.ds,
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
			w := &Workload{
				Client: fakeClient,
			}
			err := w.UpdateKubeProxyImageInfo(ctx, tt.KCP)
			if tt.expectErr {
				gs.Expect(err).To(HaveOccurred())
			} else {
				gs.Expect(err).NotTo(HaveOccurred())
			}

			proxyImage, err := getProxyImageInfo(ctx, w.Client)
			gs.Expect(err).NotTo(HaveOccurred())
			if tt.expectImage != "" {
				gs.Expect(proxyImage).To(Equal(tt.expectImage))
			}
		})
	}
}

func TestRemoveMachineFromKubeadmConfigMap(t *testing.T) {
	machine := &clusterv1.Machine{
		Status: clusterv1.MachineStatus{
			NodeRef: &corev1.ObjectReference{
				Name: "ip-10-0-0-1.ec2.internal",
			},
		},
	}
	kubeadmConfig := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kubeadmConfigKey,
			Namespace: metav1.NamespaceSystem,
		},
		Data: map[string]string{
			clusterStatusKey: `apiEndpoints:
  ip-10-0-0-1.ec2.internal:
    advertiseAddress: 10.0.0.1
    bindPort: 6443
  ip-10-0-0-2.ec2.internal:
    advertiseAddress: 10.0.0.2
    bindPort: 6443
    someFieldThatIsAddedInTheFuture: bar
apiVersion: kubeadm.k8s.io/vNbetaM
kind: ClusterStatus`,
		},
		BinaryData: map[string][]byte{
			"": nil,
		},
	}
	kconfWithoutKey := kubeadmConfig.DeepCopy()
	delete(kconfWithoutKey.Data, clusterStatusKey)

	g := NewWithT(t)
	scheme := runtime.NewScheme()
	g.Expect(corev1.AddToScheme(scheme)).To(Succeed())
	tests := []struct {
		name              string
		machine           *clusterv1.Machine
		objs              []client.Object
		expectErr         bool
		expectedEndpoints string
	}{
		{
			name:      "does not panic if machine is nil",
			expectErr: false,
		},
		{
			name: "does not panic if machine noderef is nil",
			machine: &clusterv1.Machine{
				Status: clusterv1.MachineStatus{
					NodeRef: nil,
				},
			},
			expectErr: false,
		},
		{
			name:      "returns error if unable to find kubeadm-config",
			machine:   machine,
			expectErr: true,
		},
		{
			name:      "returns error if unable to remove api endpoint",
			machine:   machine,
			objs:      []client.Object{kconfWithoutKey},
			expectErr: true,
		},
		{
			name:      "removes the machine node ref from kubeadm config",
			machine:   machine,
			objs:      []client.Object{kubeadmConfig},
			expectErr: false,
			expectedEndpoints: `apiEndpoints:
  ip-10-0-0-2.ec2.internal:
    advertiseAddress: 10.0.0.2
    bindPort: 6443
    someFieldThatIsAddedInTheFuture: bar
apiVersion: kubeadm.k8s.io/vNbetaM
kind: ClusterStatus
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.objs...).Build()
			w := &Workload{
				Client: fakeClient,
			}
			err := w.RemoveMachineFromKubeadmConfigMap(ctx, tt.machine)
			if tt.expectErr {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).ToNot(HaveOccurred())
			if tt.expectedEndpoints != "" {
				var actualConfig corev1.ConfigMap
				g.Expect(w.Client.Get(
					ctx,
					client.ObjectKey{Name: kubeadmConfigKey, Namespace: metav1.NamespaceSystem},
					&actualConfig,
				)).To(Succeed())
				g.Expect(actualConfig.Data[clusterStatusKey]).To(Equal(tt.expectedEndpoints))
			}
		})
	}
}

func TestUpdateKubeletConfigMap(t *testing.T) {
	kubeletConfig := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "kubelet-config-1.1",
			Namespace:       metav1.NamespaceSystem,
			ResourceVersion: "some-resource-version",
		},
	}

	g := NewWithT(t)
	scheme := runtime.NewScheme()
	g.Expect(corev1.AddToScheme(scheme)).To(Succeed())
	tests := []struct {
		name      string
		version   semver.Version
		objs      []client.Object
		expectErr bool
	}{
		{
			name:      "create new config map",
			version:   semver.Version{Major: 1, Minor: 2},
			objs:      []client.Object{kubeletConfig},
			expectErr: false,
		},
		{
			name:      "returns error if cannot find previous config map",
			version:   semver.Version{Major: 1, Minor: 2},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.objs...).Build()
			w := &Workload{
				Client: fakeClient,
			}
			err := w.UpdateKubeletConfigMap(ctx, tt.version)
			if tt.expectErr {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).ToNot(HaveOccurred())
			var actualConfig corev1.ConfigMap
			g.Expect(w.Client.Get(
				ctx,
				client.ObjectKey{Name: "kubelet-config-1.2", Namespace: metav1.NamespaceSystem},
				&actualConfig,
			)).To(Succeed())
			g.Expect(actualConfig.ResourceVersion).ToNot(Equal(kubeletConfig.ResourceVersion))
		})
	}
}

func TestClusterStatus(t *testing.T) {
	node1 := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node1",
			Labels: map[string]string{
				labelNodeRoleControlPlane: "",
			},
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{{
				Type:   corev1.NodeReady,
				Status: corev1.ConditionTrue,
			}},
		},
	}
	node2 := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node2",
			Labels: map[string]string{
				labelNodeRoleControlPlane: "",
			},
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{{
				Type:   corev1.NodeReady,
				Status: corev1.ConditionFalse,
			}},
		},
	}
	kconf := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kubeadmConfigKey,
			Namespace: metav1.NamespaceSystem,
		},
	}
	tests := []struct {
		name          string
		objs          []client.Object
		expectErr     bool
		expectHasConf bool
	}{
		{
			name:          "returns cluster status",
			objs:          []client.Object{node1, node2},
			expectErr:     false,
			expectHasConf: false,
		},
		{
			name:          "returns cluster status with kubeadm config",
			objs:          []client.Object{node1, node2, kconf},
			expectErr:     false,
			expectHasConf: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			scheme := runtime.NewScheme()
			g.Expect(corev1.AddToScheme(scheme)).To(Succeed())
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.objs...).Build()
			w := &Workload{
				Client: fakeClient,
			}
			status, err := w.ClusterStatus(ctx)
			if tt.expectErr {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(status.Nodes).To(BeEquivalentTo(2))
			g.Expect(status.ReadyNodes).To(BeEquivalentTo(1))
			if tt.expectHasConf {
				g.Expect(status.HasKubeadmConfig).To(BeTrue())
				return
			}
			g.Expect(status.HasKubeadmConfig).To(BeFalse())
		})
	}
}

func getProxyImageInfo(ctx context.Context, c client.Client) (string, error) {
	ds := &appsv1.DaemonSet{}

	if err := c.Get(ctx, client.ObjectKey{Name: kubeProxyKey, Namespace: metav1.NamespaceSystem}, ds); err != nil {
		if apierrors.IsNotFound(err) {
			return "", errors.New("no image found")
		}
		return "", errors.New("failed to determine if daemonset already exists")
	}
	container := findKubeProxyContainer(ds)
	if container == nil {
		return "", errors.New("unable to find container")
	}
	return container.Image, nil
}

func newKubeProxyDS() appsv1.DaemonSet {
	return appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kubeProxyKey,
			Namespace: metav1.NamespaceSystem,
		},
		Spec: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Image: "k8s.gcr.io/kube-proxy:v1.16.2",
							Name:  "kube-proxy",
						},
					},
				},
			},
		},
	}
}

func newKubeProxyDSWithImage(image string) appsv1.DaemonSet {
	ds := newKubeProxyDS()
	ds.Spec.Template.Spec.Containers[0].Image = image
	return ds
}
