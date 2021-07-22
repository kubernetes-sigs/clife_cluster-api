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

package v1alpha4

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DockerClusterTemplateSpec defines the desired state of DockerClusterTemplate.
type DockerClusterTemplateSpec struct {
	Template DockerClusterTemplateResource `json:"template"`
}

// +kubebuilder:object:root=true

// DockerClusterTemplate is the Schema for the dockerclustertemplates API.
type DockerClusterTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec DockerClusterTemplateSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=dockerclustertemplates,scope=Namespaced,categories=cluster-api

// DockerClusterTemplateList contains a list of DockerClusterTemplate.
type DockerClusterTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DockerClusterTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DockerClusterTemplate{}, &DockerClusterTemplateList{})
}

// DockerClusterTemplateResource describes the data needed to create a DockerCluster from a template.
type DockerClusterTemplateResource struct {
	Spec DockerClusterSpec `json:"spec"`
}
