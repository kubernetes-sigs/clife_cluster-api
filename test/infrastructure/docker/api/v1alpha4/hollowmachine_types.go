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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// HollowMachineFinalizer allows reconcile to clean up resources associated with HollowMachine before
	// removing it from the apiserver.
	HollowMachineFinalizer = "hollowmachine.infrastructure.cluster.x-k8s.io"
)

// HollowMachineSpec defines the desired state of HollowMachine.
type HollowMachineSpec struct {
	// TODO: should we allow to specify a kubeconfig (a secret) so the target cluster could be
	//  any Kubernetes cluster (instead of requiring a CAPI cluster)
	//  Cons: this requires to specify many kubeconfig files (one for each component).

	// TODO: heapster, cluster-autoscaler
	//  Note: Those components are not linked to a specific machines, but they are kubemark specific,
	//  so there are two options: install using ClusterResourceSets, or make all the HollowMachines to reconcile them (create if missing).
	//  Also: Are they required in the CAPI E2E scenario?

	// TODO: dns; this component seems generic; what about using ClusterResourceSets?

	// ExternalCluster define the name of the cluster where Pods hosting kubemark should be executed.
	// If empty, the machine's cluster will be used.
	// +optional
	ExternalCluster *string `json:"externalCluster,omitempty"`

	// Kubemark allows to tune some details of how we run the kubemark instance backing this HollowMachine.
	// +optional
	Kubemark KubemarkSpec `json:"kubemark,omitempty"`

	// ProviderID reports the value assigned by kubemark on the hollow Kubelet.
	// NOTE: this is required by the cluster API contract.
	// +optional
	ProviderID *string `json:"providerID,omitempty"`
}

// KubemarkSpec allows to tune some details of the Pods hosting kubemark.
type KubemarkSpec struct {
	// TODO: consider if support logging to local host

	// Image defines the docker image for kubemark.
	// If not set gcr.io/k8s-staging-cluster-api/kubemark-amd64:<kubernetes version> will be used
	// +optional
	Image *string `json:"image,omitempty"`

	// Pod allows to tune some details of the Pods hosting kubemark.
	// +optional
	Pod KubemarkPodSpec `json:"pod,omitempty"`
}

// KubemarkPodSpec allows to tune some details of the Pods running kubemark.
type KubemarkPodSpec struct {
	// TODO: consider if to add node.kubernetes.io/unreachable by default as per https://github.com/kubernetes/kubernetes/blob/ea0764452222146c47ec826977f49d7001b0ea8c/test/kubemark/resources/hollow-node_template.yaml#L130-L138

	// Containers allows to tune some details of the containers.
	// +optional
	Containers HollowContainersSpec `json:"containers,omitempty"`

	// If specified, the pod's toleration.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
}

// HollowContainersSpec allows to tune some details of the containers in the Pod hosting hollow Kubelet.
type HollowContainersSpec struct {
	// HollowKubelet allows to tune some details of the container running kubemark morphed into kubelet.
	// +optional
	HollowKubelet HollowKubeletContainersSpec `json:"hollowKubelet,omitempty"`

	// HollowProxy allows to tune some details of the container running kubemark morphed into kube-proxy.
	// +optional
	HollowProxy HollowProxyContainersSpec `json:"hollowProxy,omitempty"`

	// TODO: node-problem-detector
	//  Is this required in the CAPI E2E scenario?
}

// HollowKubeletContainersSpec allows to tune some details of the kubemark container in the Pod hosting hollow kubelet.
type HollowKubeletContainersSpec struct {
	// Arguments to the kubemark entrypoint.
	// Please note that --morph=kubelet, --kubeconfig=<kubeconfig mount> and --name=<node name> will be always added to the list.
	// +optional
	Args []string `json:"args,omitempty" protobuf:"bytes,4,rep,name=args"`

	// Compute Resources required by hollow kubelet.
	// if not specified cpu and memory requests, defaults of 40m and 10240Ki will be applied.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// HollowProxyContainersSpec allows to tune some details of the kubemark container in the Pod hosting hollow kube-proxy.
type HollowProxyContainersSpec struct {
	// TODO: consider if to logic for computing proxy resources by the number of replicas as per
	//   https://github.com/kubernetes/kubernetes/blob/ea0764452222146c47ec826977f49d7001b0ea8c/test/kubemark/start-kubemark.sh#L140-L147
	//   Cons; this won't adapt on scale.

	// Arguments to the kubemark entrypoint.
	// Please note that --morph=proxy, --kubeconfig=<kubeconfig mount> and --name=<node name> will be always added to the list.
	// +optional
	Args []string `json:"args,omitempty" protobuf:"bytes,4,rep,name=args"`

	// Compute Resources required by hollow kubelet.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// HollowMachineStatus defines the observed state of HollowMachine.
type HollowMachineStatus struct {
	// Ready denotes that the machine (docker container) is ready
	// +optional
	Ready bool `json:"ready"`
}

// +kubebuilder:resource:path=hollowmachines,scope=Namespaced,categories=cluster-api
// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:subresource:status

// HollowMachine is the Schema for the hollowmachines API.
type HollowMachine struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HollowMachineSpec   `json:"spec,omitempty"`
	Status HollowMachineStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// HollowMachineList contains a list of HollowMachine.
type HollowMachineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []HollowMachine `json:"items"`
}

func init() {
	SchemeBuilder.Register(&HollowMachine{}, &HollowMachineList{})
}
