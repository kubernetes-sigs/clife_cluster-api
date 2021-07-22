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

package v1beta1

import (
	apimachineryconversion "k8s.io/apimachinery/pkg/conversion"
	bootstrapv1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1alpha4"
	v1alpha4 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1alpha4"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

// ConvertTo converts this ClusterConfiguration to the Hub version (v1alpha4).
func (src *ClusterConfiguration) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*bootstrapv1.ClusterConfiguration)
	return Convert_v1beta1_ClusterConfiguration_To_v1alpha4_ClusterConfiguration(src, dst, nil)
}

// ConvertFrom converts from the ClusterConfiguration Hub version (v1alpha4) to this version.
func (dst *ClusterConfiguration) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*bootstrapv1.ClusterConfiguration)
	return Convert_v1alpha4_ClusterConfiguration_To_v1beta1_ClusterConfiguration(src, dst, nil)
}

// ConvertTo converts this ClusterStatus to the Hub version (v1alpha4).
func (src *ClusterStatus) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*bootstrapv1.ClusterStatus)
	return Convert_v1beta1_ClusterStatus_To_v1alpha4_ClusterStatus(src, dst, nil)
}

// ConvertFrom converts from the ClusterStatus Hub version (v1alpha4) to this version.
func (dst *ClusterStatus) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*bootstrapv1.ClusterStatus)
	return Convert_v1alpha4_ClusterStatus_To_v1beta1_ClusterStatus(src, dst, nil)
}

// ConvertTo converts this InitConfiguration to the Hub version (v1alpha4).
func (src *InitConfiguration) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*bootstrapv1.InitConfiguration)
	return Convert_v1beta1_InitConfiguration_To_v1alpha4_InitConfiguration(src, dst, nil)
}

// ConvertFrom converts from the InitConfiguration Hub version (v1alpha4) to this version.
func (dst *InitConfiguration) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*bootstrapv1.InitConfiguration)
	return Convert_v1alpha4_InitConfiguration_To_v1beta1_InitConfiguration(src, dst, nil)
}

// ConvertTo converts this JoinConfiguration to the Hub version (v1alpha4).
func (src *JoinConfiguration) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*bootstrapv1.JoinConfiguration)
	return Convert_v1beta1_JoinConfiguration_To_v1alpha4_JoinConfiguration(src, dst, nil)
}

// ConvertFrom converts from the JoinConfiguration Hub version (v1alpha4) to this version.
func (dst *JoinConfiguration) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*bootstrapv1.JoinConfiguration)
	return Convert_v1alpha4_JoinConfiguration_To_v1beta1_JoinConfiguration(src, dst, nil)
}

func Convert_v1beta1_DNS_To_v1alpha4_DNS(in *DNS, out *bootstrapv1.DNS, s apimachineryconversion.Scope) error {
	// DNS.Type was removed in v1alpha4 because only CoreDNS is supported, dropping this info.
	return autoConvert_v1beta1_DNS_To_v1alpha4_DNS(in, out, s)
}

func Convert_v1beta1_ClusterConfiguration_To_v1alpha4_ClusterConfiguration(in *ClusterConfiguration, out *bootstrapv1.ClusterConfiguration, s apimachineryconversion.Scope) error {
	// ClusterConfiguration.UseHyperKubeImage was removed in kubeadm v1alpha4 API
	return autoConvert_v1beta1_ClusterConfiguration_To_v1alpha4_ClusterConfiguration(in, out, s)
}

func Convert_v1alpha4_NodeRegistrationOptions_To_v1beta1_NodeRegistrationOptions(in *v1alpha4.NodeRegistrationOptions, out *NodeRegistrationOptions, s apimachineryconversion.Scope) error {
	// NodeRegistrationOptions.IgnorePreflightErrors does not exist in kubeadm v1beta1 API
	return autoConvert_v1alpha4_NodeRegistrationOptions_To_v1beta1_NodeRegistrationOptions(in, out, s)
}
