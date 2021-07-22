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

package kubemark

import (
	"context"
	"fmt"

	"github.com/blang/semver"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	"sigs.k8s.io/cluster-api/controllers/remote"
	infrav1 "sigs.k8s.io/cluster-api/test/infrastructure/docker/api/v1alpha4"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type kubemark struct {
	externalClusterClient client.Client
	externalCluster       *clusterv1.Cluster

	managementClusterClient client.Client
	cluster                 *clusterv1.Cluster
	machine                 *clusterv1.Machine
	hollowMachine           *infrav1.HollowMachine
}

func ReconcilePod(ctx context.Context, managementClusterClient client.Client, tracker *remote.ClusterCacheTracker, externalCluster, cluster *clusterv1.Cluster, machine *clusterv1.Machine, hollowMachine *infrav1.HollowMachine) error {
	externalClient, err := tracker.GetClient(ctx, client.ObjectKeyFromObject(externalCluster))
	if err != nil {
		return errors.Wrapf(err, "failed to create remote client for kubemark")
	}

	kubemark := &kubemark{
		externalClusterClient: externalClient,
		externalCluster:       externalCluster,

		managementClusterClient: managementClusterClient,
		cluster:                 cluster,
		machine:                 machine,
		hollowMachine:           hollowMachine,
	}

	// NOTE: the secret with kube-proxy identity is shared across all the machines, however, it is reconciled for every machine for sake of simplicity.
	if err := kubemark.createSharedSecret(ctx); err != nil {
		return err
	}

	if err := kubemark.createPodSecret(ctx); err != nil {
		return err
	}

	return kubemark.createPod(ctx)
}

func DeletePod(ctx context.Context, managementClusterClient client.Client, tracker *remote.ClusterCacheTracker, externalCluster *clusterv1.Cluster, cluster *clusterv1.Cluster, machine *clusterv1.Machine, hollowMachine *infrav1.HollowMachine) error {
	externalClient, err := tracker.GetClient(ctx, client.ObjectKeyFromObject(externalCluster))
	if err != nil {
		return errors.Wrapf(err, "failed to create remote client for kubemark")
	}

	hollowKubelet := &kubemark{
		externalClusterClient: externalClient,
		externalCluster:       externalCluster,

		managementClusterClient: managementClusterClient,
		cluster:                 cluster,
		machine:                 machine,
		hollowMachine:           hollowMachine,
	}

	if err := hollowKubelet.deletePod(ctx); err != nil {
		return err
	}

	if err := hollowKubelet.deletePodSecret(ctx); err != nil {
		return err
	}

	// TODO: delete shared secret if there are no more machines

	return nil
}

func (k *kubemark) createPod(ctx context.Context) error {
	key := podKey(k.hollowMachine)
	pod := &corev1.Pod{}
	if err := k.externalClusterClient.Get(ctx, key, pod); err != nil {
		if !apierrors.IsNotFound(err) {
			return errors.Wrap(err, "failed to get kubemark Pod")
		}
	}
	if pod.Name != "" {
		return nil
	}

	kubemarkImage, err := k.getKubemarkImage()
	if err != nil {
		return err
	}

	pod = &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: key.Namespace,
			Name:      key.Name,
			// TODO: POD Labels (down from machine??)
			//  name: hollow-node
			//  autoscaling.k8s.io/nodegroup: ${KUBEMARK_AUTOSCALER_MIG_NAME} >> CAPI autoscaler
		},
		Spec: corev1.PodSpec{
			// TODO: investigate why this is required
			InitContainers: []corev1.Container{
				k.getInitINotifyLimitContainer(),
			},
			Containers: []corev1.Container{
				k.getKubeletContainer(kubemarkImage),
				k.getKubeProxyContainer(kubemarkImage),
			},
			Tolerations: k.hollowMachine.Spec.Kubemark.Pod.Tolerations,
			Volumes: []corev1.Volume{
				{
					Name: "pod-secret",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: podSecretKey(k.hollowMachine).Name,
						},
					},
				},
				{
					Name: "shared-secret",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: sharedSecretKey(k.hollowMachine).Name,
						},
					},
				},
				{
					Name: "containerd",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/run/containerd",
						},
					},
				},
			},
		},
	}

	if err := k.externalClusterClient.Create(ctx, pod); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return errors.Wrap(err, "failed to create Pod for HollowMachine")
		}
	}
	return nil
}

func (k *kubemark) getKubemarkImage() (string, error) {
	if k.hollowMachine.Spec.Kubemark.Image != nil {
		return *k.hollowMachine.Spec.Kubemark.Image, nil
	}

	if k.machine.Spec.Version == nil {
		return "", errors.New("unable to create kubemark Pod for Machines without the Spec.Version value")
	}
	version, err := semver.ParseTolerant(*k.machine.Spec.Version)
	if err != nil {
		return "", errors.Wrapf(err, "failed to parse kubemark.Spec.Version (%s)", *k.machine.Spec.Version)
	}

	kubemarkImage := fmt.Sprintf("gcr.io/k8s-staging-cluster-api/kubemark:v%s", version.String())
	if k.hollowMachine.Spec.Kubemark.Image != nil {
		kubemarkImage = *k.hollowMachine.Spec.Kubemark.Image
	}
	return kubemarkImage, nil
}

func (k *kubemark) getInitINotifyLimitContainer() corev1.Container {
	initInotifyLimitContainer := corev1.Container{
		Name:    "init-inotify-limit",
		Image:   "busybox:1.32",
		Command: []string{"sysctl", "-w", "fs.inotify.max_user_instances=1000"},
		SecurityContext: &corev1.SecurityContext{
			Privileged: pointer.BoolPtr(true),
		},
	}
	return initInotifyLimitContainer
}

func (k *kubemark) getKubeletContainer(kubemarkImage string) corev1.Container {
	kubeletArgs := k.hollowMachine.Spec.Kubemark.Pod.Containers.HollowKubelet.Args
	kubeletArgs = append(kubeletArgs, "--morph=kubelet")
	kubeletArgs = append(kubeletArgs, "--kubeconfig=/kubeconfig/kubelet.kubeconfig")
	kubeletArgs = append(kubeletArgs, fmt.Sprintf("--name=%s", HollowNodeName(k.hollowMachine)))

	kubeletResources := k.hollowMachine.Spec.Kubemark.Pod.Containers.HollowKubelet.Resources
	if kubeletResources.Requests == nil {
		kubeletResources.Requests = make(map[corev1.ResourceName]resource.Quantity)
	}
	if kubeletResources.Requests.Cpu().IsZero() {
		kubeletResources.Requests[corev1.ResourceCPU] = resource.MustParse("40m")
	}
	if kubeletResources.Requests.Memory().IsZero() {
		kubeletResources.Requests[corev1.ResourceMemory] = resource.MustParse("10240Ki")
	}

	kubeletContainer := corev1.Container{
		Name:    "hollow-kubelet",
		Image:   kubemarkImage,
		Args:    kubeletArgs,
		Command: []string{"/kubemark"},
		Ports: []corev1.ContainerPort{
			{ContainerPort: 4194},
			{ContainerPort: 10250},
			{ContainerPort: 10255},
		},
		SecurityContext: &corev1.SecurityContext{
			Privileged: pointer.BoolPtr(true),
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				MountPath: "/kubeconfig",
				Name:      "pod-secret",
				ReadOnly:  true,
			},
			{
				// TODO: investigate why this is required
				MountPath: "/run/containerd",
				Name:      "containerd",
			},
		},
		Resources: kubeletResources,
	}
	return kubeletContainer
}

func (k *kubemark) getKubeProxyContainer(kubemarkImage string) corev1.Container {
	proxyResources := k.hollowMachine.Spec.Kubemark.Pod.Containers.HollowProxy.Resources
	if proxyResources.Requests == nil {
		proxyResources.Requests = make(map[corev1.ResourceName]resource.Quantity)
	}
	if proxyResources.Requests.Cpu().IsZero() {
		proxyResources.Requests[corev1.ResourceCPU] = resource.MustParse("40m")
	}
	if proxyResources.Requests.Memory().IsZero() {
		proxyResources.Requests[corev1.ResourceMemory] = resource.MustParse("10240Ki")
	}

	proxyArgs := k.hollowMachine.Spec.Kubemark.Pod.Containers.HollowProxy.Args
	proxyArgs = append(proxyArgs, "--morph=proxy")
	proxyArgs = append(proxyArgs, "--kubeconfig=/kubeconfig/kubeproxy.kubeconfig")
	proxyArgs = append(proxyArgs, fmt.Sprintf("--name=%s", HollowNodeName(k.hollowMachine)))

	proxyContainer := corev1.Container{
		Name:    "hollow-proxy",
		Image:   kubemarkImage,
		Args:    proxyArgs,
		Command: []string{"/kubemark"},
		VolumeMounts: []corev1.VolumeMount{
			{
				MountPath: "/kubeconfig",
				Name:      "shared-secret",
				ReadOnly:  true,
			},
		},
		Resources: proxyResources,
	}
	return proxyContainer
}

func (k *kubemark) deletePod(ctx context.Context) error {
	key := podKey(k.hollowMachine)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: key.Namespace,
			Name:      key.Name,
		},
	}
	if err := k.externalClusterClient.Delete(ctx, pod); err != nil {
		if !apierrors.IsNotFound(err) {
			return errors.Wrap(err, "failed to delete kubemark Pod")
		}
	}
	return nil
}

func podKey(hollowMachine *infrav1.HollowMachine) client.ObjectKey {
	return client.ObjectKey{
		Namespace: hollowMachine.Namespace,
		Name:      hollowMachine.Name,
	}
}

func HollowNodeName(hollowMachine *infrav1.HollowMachine) string {
	return fmt.Sprintf("%s.%s", hollowMachine.Namespace, hollowMachine.Name)
}
