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

// Package ignition aggregates all Ignition flavors into a single package to be consumed
// by the bootstrap provider by exposing an API similar to 'internal/cloudinit' package.
package ignition

import (
	"fmt"

	bootstrapv1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1alpha4"
	"sigs.k8s.io/cluster-api/bootstrap/kubeadm/internal/cloudinit"
	"sigs.k8s.io/cluster-api/bootstrap/kubeadm/internal/ignition/clc"
)

const (
	joinSubcommand         = "join"
	initSubcommand         = "init"
	kubeadmCommandTemplate = "kubeadm %s --config /etc/kubeadm.yml %s"
)

// NodeInput defines the context to generate a node user data.
type NodeInput struct {
	*cloudinit.NodeInput

	Ignition *bootstrapv1.IgnitionSpec
}

// ControlPlaneJoinInput defines context to generate controlplane instance user data for control plane node join.
type ControlPlaneJoinInput struct {
	*cloudinit.ControlPlaneJoinInput

	Ignition *bootstrapv1.IgnitionSpec
}

// ControlPlaneInput defines the context to generate a controlplane instance user data.
type ControlPlaneInput struct {
	*cloudinit.ControlPlaneInput

	Ignition *bootstrapv1.IgnitionSpec
}

// NewNode returns Ignition configuration for new worker node joining the cluster.
func NewNode(input *NodeInput) ([]byte, string, error) {
	if input == nil {
		return nil, "", fmt.Errorf("input can't be nil")
	}

	if input.NodeInput == nil {
		return nil, "", fmt.Errorf("node input can't be nil")
	}

	input.WriteFiles = append(input.WriteFiles, input.AdditionalFiles...)
	input.KubeadmCommand = fmt.Sprintf(kubeadmCommandTemplate, joinSubcommand, input.KubeadmVerbosity)

	return render(&input.BaseUserData, input.Ignition, input.JoinConfiguration)
}

// NewJoinControlPlane returns Ignition configuration for new controlplane node joining the cluster.
func NewJoinControlPlane(input *ControlPlaneJoinInput) ([]byte, string, error) {
	if input == nil {
		return nil, "", fmt.Errorf("input can't be nil")
	}

	if input.ControlPlaneJoinInput == nil {
		return nil, "", fmt.Errorf("controlplane join input can't be nil")
	}

	input.WriteFiles = input.Certificates.AsFiles()
	input.WriteFiles = append(input.WriteFiles, input.AdditionalFiles...)
	input.KubeadmCommand = fmt.Sprintf(kubeadmCommandTemplate, joinSubcommand, input.KubeadmVerbosity)

	return render(&input.BaseUserData, input.Ignition, input.JoinConfiguration)
}

// NewInitControlPlane returns Ignition configuration for bootstrapping new cluster.
func NewInitControlPlane(input *ControlPlaneInput) ([]byte, string, error) {
	if input == nil {
		return nil, "", fmt.Errorf("input can't be nil")
	}

	if input.ControlPlaneInput == nil {
		return nil, "", fmt.Errorf("controlplane input can't be nil")
	}

	input.WriteFiles = input.Certificates.AsFiles()
	input.WriteFiles = append(input.WriteFiles, input.AdditionalFiles...)
	input.KubeadmCommand = fmt.Sprintf(kubeadmCommandTemplate, initSubcommand, input.KubeadmVerbosity)

	kubeadmConfig := fmt.Sprintf("%s\n---\n%s", input.ClusterConfiguration, input.InitConfiguration)

	return render(&input.BaseUserData, input.Ignition, kubeadmConfig)
}

func render(input *cloudinit.BaseUserData, ignitionConfig *bootstrapv1.IgnitionSpec, kubeadmConfig string) ([]byte, string, error) {
	clcConfig := &bootstrapv1.ContainerLinuxConfig{}
	if ignitionConfig != nil && ignitionConfig.ContainerLinuxConfig != nil {
		clcConfig = ignitionConfig.ContainerLinuxConfig
	}

	return clc.Render(input, clcConfig, kubeadmConfig)
}
