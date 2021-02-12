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
	"fmt"
	"reflect"
	"strings"

	"github.com/blang/semver"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	kubeadmv1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/types/v1beta1"
	"sigs.k8s.io/yaml"
)

const (
	clusterStatusKey         = "ClusterStatus"
	clusterConfigurationKey  = "ClusterConfiguration"
	statusAPIEndpointsKey    = "apiEndpoints"
	configVersionKey         = "kubernetesVersion"
	dnsKey                   = "dns"
	dnsTypeKey               = "type"
	dnsImageRepositoryKey    = "imageRepository"
	dnsImageTagKey           = "imageTag"
	configImageRepositoryKey = "imageRepository"
	apiServerKey             = "apiServer"
	controllerManagerKey     = "controllerManager"
	schedulerKey             = "scheduler"
)

// KubeadmConfig wraps up interactions necessary for modifying the kubeadm config during an upgrade.
type KubeadmConfig interface {
	GetConfigMap() *corev1.ConfigMap
	RemoveAPIEndpoint(endpoint string) error
	ReconcileKubernetesVersion(version semver.Version) error
	ReconcileImageRepository(imageRepository string) error
	ReconcileEtcdImageMeta(imageMeta kubeadmv1.ImageMeta) error
	ReconcileCoreDNSImageInfo(repository, tag string) error
	ReconcileAPIServer(apiServer kubeadmv1.APIServer) error
	ReconcileControllerManager(controllerManager kubeadmv1.ControlPlaneComponent) error
	ReconcileScheduler(scheduler kubeadmv1.ControlPlaneComponent) error
}

var _ KubeadmConfig = (*kubeadmConfig)(nil)

type kubeadmConfig struct {
	ConfigMap *corev1.ConfigMap
}

func NewKubeadmConfig(configMap *corev1.ConfigMap) KubeadmConfig {
	return &kubeadmConfig{ConfigMap: configMap}
}

func (k *kubeadmConfig) GetConfigMap() *corev1.ConfigMap {
	return k.ConfigMap
}

// RemoveAPIEndpoint removes an APIEndpoint fromt he kubeadm config cluster status config map
func (k *kubeadmConfig) RemoveAPIEndpoint(endpoint string) error {
	data, ok := k.ConfigMap.Data[clusterStatusKey]
	if !ok {
		return errors.Errorf("unable to find %q key in kubeadm ConfigMap", clusterStatusKey)
	}
	status, err := yamlToUnstructured([]byte(data))
	if err != nil {
		return errors.Wrapf(err, "unable to decode kubeadm ConfigMap's %q to Unstructured object", clusterStatusKey)
	}
	endpoints, _, err := unstructured.NestedMap(status.UnstructuredContent(), statusAPIEndpointsKey)
	if err != nil {
		return errors.Wrapf(err, "unable to extract %q from kubeadm ConfigMap's %q", statusAPIEndpointsKey, clusterStatusKey)
	}
	delete(endpoints, endpoint)
	if err := unstructured.SetNestedMap(status.UnstructuredContent(), endpoints, statusAPIEndpointsKey); err != nil {
		return errors.Wrapf(err, "unable to update %q on kubeadm ConfigMap's %q", statusAPIEndpointsKey, clusterStatusKey)
	}
	updated, err := yaml.Marshal(status)
	if err != nil {
		return errors.Wrapf(err, "unable to encode kubeadm ConfigMap's %q to YAML", clusterStatusKey)
	}
	k.ConfigMap.Data[clusterStatusKey] = string(updated)
	return nil
}

// ReconcileKubernetesVersion changes the kubernetes version found in the kubeadm config map
func (k *kubeadmConfig) ReconcileKubernetesVersion(version semver.Version) error {
	if k.ConfigMap == nil {
		return errors.New("unable to operate on a nil config map")
	}
	data, ok := k.ConfigMap.Data[clusterConfigurationKey]
	if !ok {
		return errors.Errorf("unable to find %q key in kubeadm ConfigMap", clusterConfigurationKey)
	}
	configuration, err := yamlToUnstructured([]byte(data))
	if err != nil {
		return errors.Wrapf(err, "unable to decode kubeadm ConfigMap's %q to Unstructured object", clusterConfigurationKey)
	}
	versionStr := fmt.Sprintf("v%s", version)
	if err := unstructured.SetNestedField(configuration.UnstructuredContent(), versionStr, configVersionKey); err != nil {
		return errors.Wrapf(err, "unable to update %q on kubeadm ConfigMap's %q", configVersionKey, clusterConfigurationKey)
	}
	updated, err := yaml.Marshal(configuration)
	if err != nil {
		return errors.Wrapf(err, "unable to encode kubeadm ConfigMap's %q to YAML", clusterConfigurationKey)
	}
	k.ConfigMap.Data[clusterConfigurationKey] = string(updated)
	return nil
}

// ReconcileImageRepository changes the image repository found in the kubeadm config map
func (k *kubeadmConfig) ReconcileImageRepository(imageRepository string) error {
	if imageRepository == "" {
		return nil
	}
	data, ok := k.ConfigMap.Data[clusterConfigurationKey]
	if !ok {
		return errors.Errorf("unable to find %q key in kubeadm ConfigMap", clusterConfigurationKey)
	}
	configuration, err := yamlToUnstructured([]byte(data))
	if err != nil {
		return errors.Wrapf(err, "unable to decode kubeadm ConfigMap's %q to Unstructured object", clusterConfigurationKey)
	}
	if err := unstructured.SetNestedField(configuration.UnstructuredContent(), imageRepository, configImageRepositoryKey); err != nil {
		return errors.Wrapf(err, "unable to update %q on kubeadm ConfigMap's %q", imageRepository, clusterConfigurationKey)
	}
	updated, err := yaml.Marshal(configuration)
	if err != nil {
		return errors.Wrapf(err, "unable to encode kubeadm ConfigMap's %q to YAML", clusterConfigurationKey)
	}
	k.ConfigMap.Data[clusterConfigurationKey] = string(updated)
	return nil
}

// ReconcileEtcdImageMeta updates the local etcd's configuration's image repository and image tag
func (k *kubeadmConfig) ReconcileEtcdImageMeta(imageMeta kubeadmv1.ImageMeta) error {
	data, ok := k.ConfigMap.Data[clusterConfigurationKey]
	if !ok {
		return errors.Errorf("unable to find %q in kubeadm ConfigMap", clusterConfigurationKey)
	}
	configuration, err := yamlToUnstructured([]byte(data))
	if err != nil {
		return errors.Wrapf(err, "unable to decode kubeadm ConfigMap's %q to Unstructured object", clusterConfigurationKey)
	}

	var changed bool

	// Handle etcd.local.imageRepository.
	imageRepository := imageMeta.ImageRepository
	imageRepositoryPath := []string{"etcd", "local", "imageRepository"}
	currentImageRepository, _, err := unstructured.NestedString(configuration.UnstructuredContent(), imageRepositoryPath...)
	if err != nil {
		return errors.Wrapf(err, "unable to retrieve %q from kubeadm ConfigMap", strings.Join(imageRepositoryPath, "."))
	}
	if currentImageRepository != imageRepository {
		if err := unstructured.SetNestedField(configuration.UnstructuredContent(), imageRepository, imageRepositoryPath...); err != nil {
			return errors.Wrapf(err, "unable to update %q on kubeadm ConfigMap", strings.Join(imageRepositoryPath, "."))
		}
		changed = true
	}

	// Handle etcd.local.imageTag.
	imageTag := imageMeta.ImageTag
	imageTagPath := []string{"etcd", "local", "imageTag"}
	currentImageTag, _, err := unstructured.NestedString(configuration.UnstructuredContent(), imageTagPath...)
	if err != nil {
		return errors.Wrapf(err, "unable to retrieve %q from kubeadm ConfigMap", strings.Join(imageTagPath, "."))
	}
	if currentImageTag != imageTag {
		if err := unstructured.SetNestedField(configuration.UnstructuredContent(), imageTag, imageTagPath...); err != nil {
			return errors.Wrapf(err, "unable to update %q on kubeadm ConfigMap", strings.Join(imageTagPath, "."))
		}
		changed = true
	}

	// update only if changes have been performed.
	if changed {
		updated, err := yaml.Marshal(configuration)
		if err != nil {
			return errors.Wrapf(err, "unable to encode kubeadm ConfigMap's %q to YAML", clusterConfigurationKey)
		}
		k.ConfigMap.Data[clusterConfigurationKey] = string(updated)
	}

	return nil
}

// ReconcileCoreDNSImageInfo changes the dns.ImageTag and dns.ImageRepository
// found in the kubeadm config map
func (k *kubeadmConfig) ReconcileCoreDNSImageInfo(repository, tag string) error {
	dnsMap := map[string]string{
		dnsTypeKey:            string(kubeadmv1.CoreDNS),
		dnsImageRepositoryKey: repository,
		dnsImageTagKey:        tag,
	}
	err := k.reconcileClusterConfiguration(dnsMap, dnsKey)
	if err != nil {
		err = errors.Wrapf(err, "unable to update core DNS image info in kubeadm config map")
	}
	return err
}

// ReconcileAPIServer sets the api server configuration to values set in `apiServer` in kubeadm config map.
func (k *kubeadmConfig) ReconcileAPIServer(apiServer kubeadmv1.APIServer) error {
	err := k.reconcileClusterConfiguration(apiServer, apiServerKey)
	if err != nil {
		return errors.Wrap(err, "unable to update api server configuration in kubeadm config map")
	}
	return nil
}

// ReconcileControllerManager sets the controller manager configuration to values set in `controllerManager` in kubeadm config map.
func (k *kubeadmConfig) ReconcileControllerManager(controllerManager kubeadmv1.ControlPlaneComponent) error {
	err := k.reconcileClusterConfiguration(controllerManager, controllerManagerKey)
	if err != nil {
		return errors.Wrap(err, "unable to update controller manager configuration in kubeadm config map")
	}
	return nil
}

// ReconcileScheduler sets the scheduler configuration to values set in `scheduler` in kubeadm config map.
func (k *kubeadmConfig) ReconcileScheduler(scheduler kubeadmv1.ControlPlaneComponent) error {
	err := k.reconcileClusterConfiguration(scheduler, schedulerKey)
	if err != nil {
		return errors.Wrap(err, "unable to update scheduler configuration in kubeadm config map")
	}
	return nil
}

// reconcileClusterConfiguration is a generic method to update any kubeadm ClusterConfiguration spec with custom types in the specified path.
func (k *kubeadmConfig) reconcileClusterConfiguration(config interface{}, path ...string) error {
	data, ok := k.ConfigMap.Data[clusterConfigurationKey]
	if !ok {
		return errors.Errorf("unable to find %q in kubeadm ConfigMap", clusterConfigurationKey)
	}

	configuration, err := yamlToUnstructured([]byte(data))
	if err != nil {
		return errors.Wrapf(err, "unable to decode kubeadm ConfigMap's %q to Unstructured object", clusterConfigurationKey)
	}

	currentConfig, _, err := unstructured.NestedFieldCopy(configuration.UnstructuredContent(), path...)
	if err != nil {
		return errors.Wrapf(err, "unable to retrieve %q from kubeadm ConfigMap", strings.Join(path, "."))
	}

	// convert config to map[string]interface because unstructured.SetNestedField does not accept custom structs.
	newConfig, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&config)
	if err != nil {
		return errors.Wrap(err, "unable to convert config to unstructured")
	}

	// if there are no changes, return early.
	if reflect.DeepEqual(newConfig, currentConfig) {
		return nil
	}

	if err := unstructured.SetNestedField(configuration.UnstructuredContent(), newConfig, path...); err != nil {
		return errors.Wrapf(err, "unable to update %q on kubeadm ConfigMap", strings.Join(path, "."))
	}

	updated, err := yaml.Marshal(configuration)
	if err != nil {
		return errors.Wrapf(err, "unable to encode kubeadm ConfigMap's %q to YAML", clusterConfigurationKey)
	}

	k.ConfigMap.Data[clusterConfigurationKey] = string(updated)
	return nil
}

// yamlToUnstructured looks inside a config map for a specific key and extracts the embedded YAML into an
// *unstructured.Unstructured.
func yamlToUnstructured(rawYAML []byte) (*unstructured.Unstructured, error) {
	unst := &unstructured.Unstructured{}
	err := yaml.Unmarshal(rawYAML, unst)
	return unst, err
}
