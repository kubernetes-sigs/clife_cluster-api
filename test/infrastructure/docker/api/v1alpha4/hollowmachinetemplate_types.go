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

// HollowMachineTemplateSpec defines the desired state of HollowMachineTemplate.
type HollowMachineTemplateSpec struct {
	Template HollowMachineTemplateResource `json:"template"`
}

// HollowMachineTemplateResource describes the data needed to create a HollowMachine from a template.
type HollowMachineTemplateResource struct {
	// Spec is the specification of the desired behavior of the machine.
	Spec HollowMachineSpec `json:"spec"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=hollowmachinetemplates,scope=Namespaced,categories=cluster-api
// +kubebuilder:storageversion

// HollowMachineTemplate is the Schema for the hollowmachinetemplates API.
type HollowMachineTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec HollowMachineTemplateSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// HollowMachineTemplateList contains a list of HollowMachineTemplate.
type HollowMachineTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []HollowMachineTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&HollowMachineTemplate{}, &HollowMachineTemplateList{})
}
