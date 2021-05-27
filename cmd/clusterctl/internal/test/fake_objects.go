/*
Copyright 2019 The Kubernetes Authors.

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

package test

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apiextensionslv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	clusterctlv1 "sigs.k8s.io/cluster-api/cmd/clusterctl/api/v1alpha3"
	fakebootstrap "sigs.k8s.io/cluster-api/cmd/clusterctl/internal/test/providers/bootstrap"
	fakecontrolplane "sigs.k8s.io/cluster-api/cmd/clusterctl/internal/test/providers/controlplane"
	fakeexternal "sigs.k8s.io/cluster-api/cmd/clusterctl/internal/test/providers/external"
	fakeinfrastructure "sigs.k8s.io/cluster-api/cmd/clusterctl/internal/test/providers/infrastructure"
	addonsv1 "sigs.k8s.io/cluster-api/exp/addons/api/v1alpha4"
	expv1 "sigs.k8s.io/cluster-api/exp/api/v1alpha4"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type FakeCluster struct {
	namespace             string
	name                  string
	controlPlane          *FakeControlPlane
	machinePools          []*FakeMachinePool
	machineDeployments    []*FakeMachineDeployment
	machineSets           []*FakeMachineSet
	machines              []*FakeMachine
	withCloudConfigSecret bool
}

// NewFakeCluster return a FakeCluster that can generate a cluster object, all its own ancillary objects:
// - the clusterInfrastructure object
// - the kubeconfig secret object (if there is no a control plane object)
// - a user defined ca secret
// and all the objects for the defined FakeControlPlane, FakeMachinePools, FakeMachineDeployments, FakeMachineSets, FakeMachines
// Nb. if there is no a control plane object, the first FakeMachine gets a generated sa secret.
func NewFakeCluster(namespace, name string) *FakeCluster {
	return &FakeCluster{
		namespace: namespace,
		name:      name,
	}
}

func (f *FakeCluster) WithControlPlane(fakeControlPlane *FakeControlPlane) *FakeCluster {
	f.controlPlane = fakeControlPlane
	return f
}

func (f *FakeCluster) WithMachinePools(fakeMachinePool ...*FakeMachinePool) *FakeCluster {
	f.machinePools = append(f.machinePools, fakeMachinePool...)
	return f
}

func (f *FakeCluster) WithCloudConfigSecret() *FakeCluster {
	f.withCloudConfigSecret = true
	return f
}

func (f *FakeCluster) WithMachineDeployments(fakeMachineDeployment ...*FakeMachineDeployment) *FakeCluster {
	f.machineDeployments = append(f.machineDeployments, fakeMachineDeployment...)
	return f
}

func (f *FakeCluster) WithMachineSets(fakeMachineSet ...*FakeMachineSet) *FakeCluster {
	f.machineSets = append(f.machineSets, fakeMachineSet...)
	return f
}

func (f *FakeCluster) WithMachines(fakeMachine ...*FakeMachine) *FakeCluster {
	f.machines = append(f.machines, fakeMachine...)
	return f
}

func (f *FakeCluster) Objs() []client.Object {
	clusterInfrastructure := &fakeinfrastructure.GenericInfrastructureCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: fakeinfrastructure.GroupVersion.String(),
			Kind:       "GenericInfrastructureCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      f.name,
			Namespace: f.namespace,
			// OwnerReferences: cluster, Added by the cluster controller (see below) -- RECONCILED
			Labels: map[string]string{clusterv1.ClusterLabelName: f.name},
		},
	}

	cluster := &clusterv1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Cluster",
			APIVersion: clusterv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      f.name,
			Namespace: f.namespace,
			// Labels: cluster.x-k8s.io/cluster-name=cluster MISSING??
		},
		Spec: clusterv1.ClusterSpec{
			InfrastructureRef: &corev1.ObjectReference{
				APIVersion: clusterInfrastructure.APIVersion,
				Kind:       clusterInfrastructure.Kind,
				Name:       clusterInfrastructure.Name,
				Namespace:  clusterInfrastructure.Namespace,
			},
		},
	}

	// Ensure the cluster gets a UID to be used by dependant objects for creating OwnerReferences.
	setUID(cluster)

	clusterInfrastructure.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion: cluster.APIVersion,
			Kind:       cluster.Kind,
			Name:       cluster.Name,
			UID:        cluster.UID,
		},
	})
	clusterInfrastructure.SetLabels(map[string]string{
		clusterv1.ClusterLabelName: cluster.Name,
	})

	caSecret := &corev1.Secret{ // provided by the user -- ** NOT RECONCILED **
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      f.name + "-ca",
			Namespace: f.namespace,
			Labels:    map[string]string{clusterv1.ClusterLabelName: f.name},
		},
	}

	objs := []client.Object{
		cluster,
		clusterInfrastructure,
		caSecret,
	}

	if f.withCloudConfigSecret {
		cloudSecret := &corev1.Secret{ // provided by the user -- ** NOT RECONCILED **
			TypeMeta: metav1.TypeMeta{
				Kind:       "Secret",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      f.name + "-cloud-config",
				Namespace: f.namespace,
				Labels:    map[string]string{clusterv1.ClusterLabelName: f.name},
			},
		}

		cloudSecret.SetLabels(map[string]string{
			clusterctlv1.ClusterctlMoveLabelName: "",
		})
		objs = append(objs, cloudSecret)
	}

	// if the cluster has a control plane object
	if f.controlPlane != nil {
		// Adds the objects for the controlPlane
		objs = append(objs, f.controlPlane.Objs(cluster)...)
	} else {
		// Adds the kubeconfig object generated by the cluster controller -- ** NOT RECONCILED **
		kubeconfigSecret := &corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Secret",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      f.name + "-kubeconfig",
				Namespace: f.namespace,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: cluster.APIVersion,
						Kind:       cluster.Kind,
						Name:       cluster.Name,
						UID:        cluster.UID,
					},
				},
				Labels: map[string]string{clusterv1.ClusterLabelName: f.name},
			},
		}
		objs = append(objs, kubeconfigSecret)
	}

	// Adds the objects for the machinePools
	for _, machinePool := range f.machinePools {
		objs = append(objs, machinePool.Objs(cluster)...)
	}

	// Adds the objects for the machineDeployments
	for _, machineDeployment := range f.machineDeployments {
		objs = append(objs, machineDeployment.Objs(cluster)...)
	}

	// Adds the objects for the machineSets directly attached to the cluster
	for _, machineSet := range f.machineSets {
		objs = append(objs, machineSet.Objs(cluster, nil)...)
	}

	// Adds the objects for the machines directly attached to the cluster
	// Nb. In case there is no control plane, the first machine is arbitrarily used to simulate the generation of certificates Secrets implemented by the bootstrap controller.
	for i, machine := range f.machines {
		generateCerts := false
		if f.controlPlane == nil && i == 0 {
			generateCerts = true
		}
		objs = append(objs, machine.Objs(cluster, generateCerts, nil, nil)...)
	}

	// Ensure all the objects gets UID.
	// Nb. This adds UID to all the objects; it does not change the UID explicitly sets in advance for the objects involved in the object graphs.
	for _, o := range objs {
		setUID(o)
	}

	return objs
}

type FakeControlPlane struct {
	name     string
	machines []*FakeMachine
}

// NewFakeControlPlane return a FakeControlPlane that can generate a controlPlane object, all its own ancillary objects:
// - the controlPlaneInfrastructure template object
// - the kubeconfig secret object
// - a generated sa secret
// and all the objects for the defined FakeMachines.
func NewFakeControlPlane(name string) *FakeControlPlane {
	return &FakeControlPlane{
		name: name,
	}
}

func (f *FakeControlPlane) WithMachines(fakeMachine ...*FakeMachine) *FakeControlPlane {
	f.machines = append(f.machines, fakeMachine...)
	return f
}

func (f *FakeControlPlane) Objs(cluster *clusterv1.Cluster) []client.Object {
	controlPlaneInfrastructure := &fakeinfrastructure.GenericInfrastructureMachineTemplate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: fakeinfrastructure.GroupVersion.String(),
			Kind:       "GenericInfrastructureMachineTemplate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      f.name,
			Namespace: cluster.Namespace,
			OwnerReferences: []metav1.OwnerReference{ // Added by the control plane  controller (see below) -- RECONCILED
				{
					APIVersion: clusterv1.GroupVersion.String(),
					Kind:       "Cluster",
					Name:       cluster.Name,
					UID:        cluster.UID,
				},
			},
			// Labels: MISSING
		},
	}

	controlPlane := &fakecontrolplane.GenericControlPlane{
		TypeMeta: metav1.TypeMeta{
			APIVersion: fakecontrolplane.GroupVersion.String(),
			Kind:       "GenericControlPlane",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      f.name,
			Namespace: cluster.Namespace,
			OwnerReferences: []metav1.OwnerReference{ // Added by the control plane  controller (see below) -- RECONCILED
				{
					APIVersion: clusterv1.GroupVersion.String(),
					Kind:       "Cluster",
					Name:       cluster.Name,
					UID:        cluster.UID,
				},
			},
			Labels: map[string]string{ // cluster.x-k8s.io/cluster-name=cluster, Added by the control plane controller (see below) -- RECONCILED
				clusterv1.ClusterLabelName: cluster.Name,
			},
		},
		Spec: fakecontrolplane.GenericControlPlaneSpec{
			InfrastructureTemplate: corev1.ObjectReference{
				APIVersion: controlPlaneInfrastructure.APIVersion,
				Kind:       controlPlaneInfrastructure.Kind,
				Namespace:  controlPlaneInfrastructure.Namespace,
				Name:       controlPlaneInfrastructure.Name,
			},
		},
	}

	// Ensure the controlPlane gets a UID to be used by dependant objects for creating OwnerReferences.
	setUID(controlPlane)

	// sets the reference from the cluster to the plane object
	cluster.Spec.ControlPlaneRef = &corev1.ObjectReference{
		APIVersion: controlPlane.APIVersion,
		Kind:       controlPlane.Kind,
		Namespace:  controlPlane.Namespace,
		Name:       controlPlane.Name,
	}

	// Adds the kubeconfig object generated by the control plane  controller -- ** NOT RECONCILED **
	kubeconfigSecret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.GetName() + "-kubeconfig",
			Namespace: cluster.GetNamespace(),
			// Labels: cluster.x-k8s.io/cluster-name=cluster MISSING??
		},
	}
	kubeconfigSecret.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(controlPlane, controlPlane.GroupVersionKind())})

	// Adds one of the certificate secret object generated by the control plane controller -- ** NOT RECONCILED **
	saSecret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.GetName() + "-sa",
			Namespace: cluster.GetNamespace(),
			// Labels: cluster.x-k8s.io/cluster-name=cluster MISSING??
		},
	}
	saSecret.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(controlPlane, controlPlane.GroupVersionKind())})

	objs := []client.Object{
		controlPlane,
		controlPlaneInfrastructure,
		kubeconfigSecret,
		saSecret,
	}

	// Adds the objects for the machines controlled by the controlPlane
	for _, machine := range f.machines {
		objs = append(objs, machine.Objs(cluster, false, nil, controlPlane)...)
	}

	return objs
}

type FakeMachinePool struct {
	name string
}

// NewFakeMachinePool return a FakeMachinePool that can generate a MachinePool object, all its own ancillary objects:
// - the machinePoolInfrastructure object
// - the machinePoolBootstrap object.
func NewFakeMachinePool(name string) *FakeMachinePool {
	return &FakeMachinePool{
		name: name,
	}
}

func (f *FakeMachinePool) Objs(cluster *clusterv1.Cluster) []client.Object {
	machinePoolInfrastructure := &fakeinfrastructure.GenericInfrastructureMachineTemplate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: fakeinfrastructure.GroupVersion.String(),
			Kind:       "GenericInfrastructureMachineTemplate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      f.name,
			Namespace: cluster.Namespace,
			OwnerReferences: []metav1.OwnerReference{ // Added by the machinePool controller (mirrors machinePool.spec.ClusterName) -- RECONCILED
				{
					APIVersion: clusterv1.GroupVersion.String(),
					Kind:       "Cluster",
					Name:       cluster.Name,
					UID:        cluster.UID,
				},
			},
			Labels: map[string]string{clusterv1.ClusterLabelName: cluster.Name},
		},
	}

	machinePoolBootstrap := &fakebootstrap.GenericBootstrapConfigTemplate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: fakebootstrap.GroupVersion.String(),
			Kind:       "GenericBootstrapConfigTemplate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      f.name,
			Namespace: cluster.Namespace,
			OwnerReferences: []metav1.OwnerReference{ // Added by the machinePool controller (mirrors machinePool.spec.ClusterName) -- RECONCILED
				{
					APIVersion: clusterv1.GroupVersion.String(),
					Kind:       "Cluster",
					Name:       cluster.Name,
					UID:        cluster.UID,
				},
			},
			Labels: map[string]string{clusterv1.ClusterLabelName: cluster.Name},
		},
	}

	machinePool := &expv1.MachinePool{
		TypeMeta: metav1.TypeMeta{
			Kind:       "MachinePool",
			APIVersion: expv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      f.name,
			Namespace: cluster.Namespace,
			OwnerReferences: []metav1.OwnerReference{ // Added by the machinePool controller (mirrors machinePool.spec.ClusterName) -- RECONCILED
				{
					APIVersion: clusterv1.GroupVersion.String(),
					Kind:       "Cluster",
					Name:       cluster.Name,
					UID:        cluster.UID,
				},
			},
			Labels: map[string]string{
				clusterv1.ClusterLabelName: cluster.Name, // Added by the machinePool controller (mirrors machinePoolt.spec.ClusterName) -- RECONCILED
			},
		},
		Spec: expv1.MachinePoolSpec{
			Template: clusterv1.MachineTemplateSpec{
				Spec: clusterv1.MachineSpec{
					InfrastructureRef: corev1.ObjectReference{
						APIVersion: machinePoolInfrastructure.APIVersion,
						Kind:       machinePoolInfrastructure.Kind,
						Name:       machinePoolInfrastructure.Name,
						Namespace:  machinePoolInfrastructure.Namespace,
					},
					Bootstrap: clusterv1.Bootstrap{
						ConfigRef: &corev1.ObjectReference{
							APIVersion: machinePoolBootstrap.APIVersion,
							Kind:       machinePoolBootstrap.Kind,
							Name:       machinePoolBootstrap.Name,
							Namespace:  machinePoolBootstrap.Namespace,
						},
					},
				},
			},
			ClusterName: cluster.Name,
		},
	}

	// Ensure the machinePool gets a UID to be used by dependant objects for creating OwnerReferences.
	setUID(machinePool)

	objs := []client.Object{
		machinePool,
		machinePoolInfrastructure,
		machinePoolBootstrap,
	}

	return objs
}

func NewFakeInfrastructureTemplate(name string) *fakeinfrastructure.GenericInfrastructureMachineTemplate {
	return &fakeinfrastructure.GenericInfrastructureMachineTemplate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: fakeinfrastructure.GroupVersion.String(),
			Kind:       "GenericInfrastructureMachineTemplate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			// OwnerReference Added by the machine set controller -- RECONCILED
			// Labels: MISSING
		},
	}
}

type FakeMachineDeployment struct {
	name                         string
	machineSets                  []*FakeMachineSet
	sharedInfrastructureTemplate *fakeinfrastructure.GenericInfrastructureMachineTemplate
}

// NewFakeMachineDeployment return a FakeMachineDeployment that can generate a MachineDeployment object, all its own ancillary objects:
// - the machineDeploymentInfrastructure template object
// - the machineDeploymentBootstrap template object
// and all the objects for the defined FakeMachineSet.
func NewFakeMachineDeployment(name string) *FakeMachineDeployment {
	return &FakeMachineDeployment{
		name: name,
	}
}

func (f *FakeMachineDeployment) WithMachineSets(fakeMachineSet ...*FakeMachineSet) *FakeMachineDeployment {
	f.machineSets = append(f.machineSets, fakeMachineSet...)
	return f
}

func (f *FakeMachineDeployment) WithInfrastructureTemplate(infrastructureTemplate *fakeinfrastructure.GenericInfrastructureMachineTemplate) *FakeMachineDeployment {
	f.sharedInfrastructureTemplate = infrastructureTemplate
	return f
}

func (f *FakeMachineDeployment) Objs(cluster *clusterv1.Cluster) []client.Object {
	// infra template can be either shared or specific to the machine deployment
	machineDeploymentInfrastructure := f.sharedInfrastructureTemplate
	if machineDeploymentInfrastructure == nil {
		machineDeploymentInfrastructure = NewFakeInfrastructureTemplate(f.name)
	}
	machineDeploymentInfrastructure.Namespace = cluster.Namespace
	machineDeploymentInfrastructure.OwnerReferences = append(machineDeploymentInfrastructure.OwnerReferences, // Added by the machine set controller -- RECONCILED
		metav1.OwnerReference{
			APIVersion: clusterv1.GroupVersion.String(),
			Kind:       "Cluster",
			Name:       cluster.Name,
			UID:        cluster.UID,
		},
	)
	setUID(machineDeploymentInfrastructure)

	machineDeploymentBootstrap := &fakebootstrap.GenericBootstrapConfigTemplate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: fakebootstrap.GroupVersion.String(),
			Kind:       "GenericBootstrapConfigTemplate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      f.name,
			Namespace: cluster.Namespace,
			OwnerReferences: []metav1.OwnerReference{ // Added by the machine set controller -- RECONCILED
				{
					APIVersion: clusterv1.GroupVersion.String(),
					Kind:       "Cluster",
					Name:       cluster.Name,
					UID:        cluster.UID,
				},
			},
			Labels: map[string]string{clusterv1.ClusterLabelName: cluster.Name},
		},
	}

	machineDeployment := &clusterv1.MachineDeployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "MachineDeployment",
			APIVersion: clusterv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      f.name,
			Namespace: cluster.Namespace,
			OwnerReferences: []metav1.OwnerReference{ // Added by the machineDeployment controller (mirrors machineDeployment.spec.ClusterName) -- RECONCILED
				{
					APIVersion: clusterv1.GroupVersion.String(),
					Kind:       "Cluster",
					Name:       cluster.Name,
					UID:        cluster.UID,
				},
			},
			Labels: map[string]string{
				clusterv1.ClusterLabelName: cluster.Name, // Added by the machineDeployment controller (mirrors machineDeployment.spec.ClusterName) -- RECONCILED
			},
		},
		Spec: clusterv1.MachineDeploymentSpec{
			Template: clusterv1.MachineTemplateSpec{
				Spec: clusterv1.MachineSpec{
					InfrastructureRef: corev1.ObjectReference{
						APIVersion: machineDeploymentInfrastructure.APIVersion,
						Kind:       machineDeploymentInfrastructure.Kind,
						Name:       machineDeploymentInfrastructure.Name,
						Namespace:  machineDeploymentInfrastructure.Namespace,
					},
					Bootstrap: clusterv1.Bootstrap{
						ConfigRef: &corev1.ObjectReference{
							APIVersion: machineDeploymentBootstrap.APIVersion,
							Kind:       machineDeploymentBootstrap.Kind,
							Name:       machineDeploymentBootstrap.Name,
							Namespace:  machineDeploymentBootstrap.Namespace,
						},
					},
				},
			},
			ClusterName: cluster.Name,
		},
	}

	// Ensure the machineDeployment gets a UID to be used by dependant objects for creating OwnerReferences.
	setUID(machineDeployment)

	objs := []client.Object{
		machineDeployment,
		machineDeploymentBootstrap,
	}
	// if the infra template is specific to the machine deployment, add it to the object list
	if f.sharedInfrastructureTemplate == nil {
		objs = append(objs, machineDeploymentInfrastructure)
	}

	// Adds the objects for the machineSets
	for _, machineSet := range f.machineSets {
		objs = append(objs, machineSet.Objs(cluster, machineDeployment)...)
	}

	return objs
}

type FakeMachineSet struct {
	name                         string
	machines                     []*FakeMachine
	sharedInfrastructureTemplate *fakeinfrastructure.GenericInfrastructureMachineTemplate
}

// NewFakeMachineSet return a FakeMachineSet that can generate a MachineSet object, all its own ancillary objects:
// - the machineSetInfrastructure template object (only if not controlled by a MachineDeployment)
// - the machineSetBootstrap template object (only if not controlled by a MachineDeployment)
// and all the objects for the defined FakeMachine.
func NewFakeMachineSet(name string) *FakeMachineSet {
	return &FakeMachineSet{
		name: name,
	}
}

func (f *FakeMachineSet) WithMachines(fakeMachine ...*FakeMachine) *FakeMachineSet {
	f.machines = append(f.machines, fakeMachine...)
	return f
}

func (f *FakeMachineSet) WithInfrastructureTemplate(infrastructureTemplate *fakeinfrastructure.GenericInfrastructureMachineTemplate) *FakeMachineSet {
	f.sharedInfrastructureTemplate = infrastructureTemplate
	return f
}

func (f *FakeMachineSet) Objs(cluster *clusterv1.Cluster, machineDeployment *clusterv1.MachineDeployment) []client.Object {
	machineSet := &clusterv1.MachineSet{ // Created by machineDeployment controller
		TypeMeta: metav1.TypeMeta{
			Kind:       "MachineSet",
			APIVersion: clusterv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      f.name,
			Namespace: cluster.Namespace,
			// Owner reference set by machineSet controller or by machineDeployment controller (see below)
			Labels: map[string]string{
				clusterv1.ClusterLabelName: cluster.Name, // Added by the machineSet controller (mirrors machineSet.spec.ClusterName) -- RECONCILED
			},
		},
		Spec: clusterv1.MachineSetSpec{
			ClusterName: cluster.Name,
		},
	}

	// Ensure the machineSet gets a UID to be used by dependant objects for creating OwnerReferences.
	setUID(machineSet)

	objs := make([]client.Object, 0)

	if machineDeployment != nil {
		// If this machineSet belong to a machineDeployment, it is controlled by it / ownership set by the machineDeployment controller  -- ** NOT RECONCILED **
		machineSet.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(machineDeployment, machineDeployment.GroupVersionKind())})

		// additionally the machine has ref to the same infra and bootstrap templates defined in the MachineDeployment
		machineSet.Spec.Template.Spec.InfrastructureRef = *machineDeployment.Spec.Template.Spec.InfrastructureRef.DeepCopy()
		machineSet.Spec.Template.Spec.Bootstrap.ConfigRef = machineDeployment.Spec.Template.Spec.Bootstrap.ConfigRef.DeepCopy()

		objs = append(objs, machineSet)
	} else {
		// If this machineSet does not belong to a machineDeployment, it is owned by the cluster / ownership set by the machineSet controller -- RECONCILED
		machineSet.SetOwnerReferences([]metav1.OwnerReference{{
			APIVersion: cluster.APIVersion,
			Kind:       cluster.Kind,
			Name:       cluster.Name,
			UID:        cluster.UID,
		}})

		// additionally the machineSet has ref to dedicated infra and bootstrap templates

		// infra template can be either shared or specific to the machine set
		machineSetInfrastructure := f.sharedInfrastructureTemplate
		if machineSetInfrastructure == nil {
			machineSetInfrastructure = NewFakeInfrastructureTemplate(f.name)
		}
		machineSetInfrastructure.Namespace = cluster.Namespace
		machineSetInfrastructure.OwnerReferences = append(machineSetInfrastructure.OwnerReferences, // Added by the machine set controller -- RECONCILED
			metav1.OwnerReference{
				APIVersion: clusterv1.GroupVersion.String(),
				Kind:       "Cluster",
				Name:       cluster.Name,
				UID:        cluster.UID,
			},
		)
		setUID(machineSetInfrastructure)

		machineSet.Spec.Template.Spec.InfrastructureRef = corev1.ObjectReference{
			APIVersion: machineSetInfrastructure.APIVersion,
			Kind:       machineSetInfrastructure.Kind,
			Name:       machineSetInfrastructure.Name,
			Namespace:  machineSetInfrastructure.Namespace,
		}

		machineSetBootstrap := &fakebootstrap.GenericBootstrapConfigTemplate{
			TypeMeta: metav1.TypeMeta{
				APIVersion: fakebootstrap.GroupVersion.String(),
				Kind:       "GenericBootstrapConfigTemplate",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      f.name,
				Namespace: cluster.Namespace,
				OwnerReferences: []metav1.OwnerReference{ // Added by the machine set controller -- RECONCILED
					{
						APIVersion: clusterv1.GroupVersion.String(),
						Kind:       "Cluster",
						Name:       cluster.Name,
						UID:        cluster.UID,
					},
				},
				Labels: map[string]string{clusterv1.ClusterLabelName: cluster.Name},
			},
		}

		machineSet.Spec.Template.Spec.Bootstrap = clusterv1.Bootstrap{
			ConfigRef: &corev1.ObjectReference{
				APIVersion: machineSetBootstrap.APIVersion,
				Kind:       machineSetBootstrap.Kind,
				Name:       machineSetBootstrap.Name,
				Namespace:  machineSetBootstrap.Namespace,
			},
		}

		objs = append(objs, machineSet, machineSetBootstrap)
		// if the infra template is specific to the machine set, add it to the object list
		if f.sharedInfrastructureTemplate == nil {
			objs = append(objs, machineSetInfrastructure)
		}
	}

	// Adds the objects for the machines controlled by the machineSet
	for _, machine := range f.machines {
		objs = append(objs, machine.Objs(cluster, false, machineSet, nil)...)
	}

	return objs
}

type FakeMachine struct {
	name string
}

// NewFakeMachine return a FakeMachine that can generate a Machine object, all its own ancillary objects:
// - the machineInfrastructure object
// - the machineBootstrap object and the related bootstrapDataSecret
// If there is no a control plane object in the cluster, the first FakeMachine gets a generated sa secret.
func NewFakeMachine(name string) *FakeMachine {
	return &FakeMachine{
		name: name,
	}
}

func (f *FakeMachine) Objs(cluster *clusterv1.Cluster, generateCerts bool, machineSet *clusterv1.MachineSet, controlPlane *fakecontrolplane.GenericControlPlane) []client.Object {
	machineInfrastructure := &fakeinfrastructure.GenericInfrastructureMachine{
		TypeMeta: metav1.TypeMeta{
			APIVersion: fakeinfrastructure.GroupVersion.String(),
			Kind:       "GenericInfrastructureMachine",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      f.name,
			Namespace: cluster.Namespace,
			// OwnerReferences: machine, Added by the machine controller (see below) -- RECONCILED
			Labels: map[string]string{clusterv1.ClusterLabelName: cluster.Name},
		},
	}

	bootstrapDataSecretName := f.name

	machineBootstrap := &fakebootstrap.GenericBootstrapConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: fakebootstrap.GroupVersion.String(),
			Kind:       "GenericBootstrapConfig",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      f.name,
			Namespace: cluster.Namespace,
			// OwnerReferences: machine, Added by the machine controller (see below) -- RECONCILED
			Labels: map[string]string{clusterv1.ClusterLabelName: cluster.Name},
		},
		Status: fakebootstrap.GenericBootstrapConfigStatus{
			DataSecretName: &bootstrapDataSecretName,
		},
	}

	// Ensure the machineBootstrap gets a UID to be used by dependant objects for creating OwnerReferences.
	setUID(machineBootstrap)

	bootstrapDataSecret := &corev1.Secret{ // generated by the bootstrap controller -- ** NOT RECONCILED **
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      bootstrapDataSecretName,
			Namespace: cluster.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(machineBootstrap, machineBootstrap.GroupVersionKind()),
			},
			Labels: map[string]string{
				clusterv1.ClusterLabelName: cluster.Name, // derives from Config -(ownerRef)-> machine.spec.ClusterName
			},
		},
	}

	machine := &clusterv1.Machine{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Machine",
			APIVersion: clusterv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      f.name,
			Namespace: cluster.Namespace,
			// Owner reference set by machine controller or by machineSet controller (see below)
			Labels: map[string]string{
				clusterv1.ClusterLabelName: cluster.Name, // Added by the machine controller (mirrors machine.spec.ClusterName) -- RECONCILED
			},
		},
		Spec: clusterv1.MachineSpec{
			InfrastructureRef: corev1.ObjectReference{
				APIVersion: machineInfrastructure.APIVersion,
				Kind:       machineInfrastructure.Kind,
				Name:       machineInfrastructure.Name,
				Namespace:  cluster.Namespace,
			},
			Bootstrap: clusterv1.Bootstrap{
				ConfigRef: &corev1.ObjectReference{
					APIVersion: machineBootstrap.APIVersion,
					Kind:       machineBootstrap.Kind,
					Name:       machineBootstrap.Name,
					Namespace:  cluster.Namespace,
				},
				DataSecretName: &bootstrapDataSecretName,
			},
			ClusterName: cluster.Name,
		},
	}

	// Ensure the machine gets a UID to be used by dependant objects for creating OwnerReferences.
	setUID(machine)

	var additionalObjs []client.Object

	switch {
	case machineSet != nil:
		// If this machine belong to a machineSet, it is controlled by it / ownership set by the machineSet controller -- ** NOT RECONCILED ?? **
		machine.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(machineSet, machineSet.GroupVersionKind())})
	case controlPlane != nil:
		// If this machine belong to a controlPlane, it is controlled by it / ownership set by the controlPlane controller -- ** NOT RECONCILED ?? **
		machine.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(controlPlane, controlPlane.GroupVersionKind())})
		// Sets the MachineControlPlane Label
		machine.Labels[clusterv1.MachineControlPlaneLabelName] = ""
	default:
		// If this machine does not belong to a machineSet or to a control plane, it is owned by the cluster / ownership set by the machine controller -- RECONCILED
		machine.SetOwnerReferences([]metav1.OwnerReference{{
			APIVersion: cluster.APIVersion,
			Kind:       cluster.Kind,
			Name:       cluster.Name,
			UID:        cluster.UID,
		}})

		// Adds one of the certificate secret object generated by the bootstrap config controller -- ** NOT RECONCILED **
		if generateCerts {
			saSecret := &corev1.Secret{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Secret",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      cluster.GetName() + "-sa",
					Namespace: cluster.GetNamespace(),
					Labels:    map[string]string{clusterv1.ClusterLabelName: cluster.Name},
				},
			}
			// Set controlled by the machineBootstrap / ownership set by the bootstrap config controller -- ** NOT RECONCILED ?? **
			saSecret.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(machineBootstrap, machineBootstrap.GroupVersionKind())})

			additionalObjs = append(additionalObjs, saSecret)
		}
	}

	machineInfrastructure.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion: machine.APIVersion,
			Kind:       machine.Kind,
			Name:       machine.Name,
			UID:        machine.UID,
		},
	})
	machineInfrastructure.SetLabels(map[string]string{
		clusterv1.ClusterLabelName: machine.Spec.ClusterName,
	})

	machineBootstrap.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion: machine.APIVersion,
			Kind:       machine.Kind,
			Name:       machine.Name,
			UID:        machine.UID,
		},
	})
	machineBootstrap.SetLabels(map[string]string{
		clusterv1.ClusterLabelName: machine.Spec.ClusterName,
	})

	objs := []client.Object{
		machine,
		machineInfrastructure,
		machineBootstrap,
		bootstrapDataSecret,
	}

	objs = append(objs, additionalObjs...)

	return objs
}

type FakeClusterResourceSet struct {
	name       string
	namespace  string
	secrets    []*corev1.Secret
	configMaps []*corev1.ConfigMap
	clusters   []*clusterv1.Cluster
}

// NewFakeClusterResourceSet return a FakeClusterResourceSet that can generate a ClusterResourceSet object, all its own ancillary objects:
// - the Secret/ConfigMap defining resources
// - the bindings that are created when a ClusterResourceSet is applied to a cluster.
func NewFakeClusterResourceSet(namespace, name string) *FakeClusterResourceSet {
	return &FakeClusterResourceSet{
		name:      name,
		namespace: namespace,
	}
}

func (f *FakeClusterResourceSet) WithSecret(name string) *FakeClusterResourceSet {
	f.secrets = append(f.secrets, &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: f.namespace,
		},
		// No data are required for the sake of move tests
	})
	return f
}

func (f *FakeClusterResourceSet) WithConfigMap(name string) *FakeClusterResourceSet {
	f.configMaps = append(f.configMaps, &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: f.namespace,
		},
		// No data are required for the sake of move tests
	})
	return f
}

func (f *FakeClusterResourceSet) ApplyToCluster(cluster *clusterv1.Cluster) *FakeClusterResourceSet {
	if f.namespace != cluster.Namespace {
		panic("A ClusterResourceSet can only be applied to a cluster in the same namespace")
	}
	f.clusters = append(f.clusters, cluster)
	return f
}

func (f *FakeClusterResourceSet) Objs() []client.Object {
	crs := &addonsv1.ClusterResourceSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterResourceSet",
			APIVersion: addonsv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      f.name,
			Namespace: f.namespace,
		},
		Spec: addonsv1.ClusterResourceSetSpec{
			Resources: []addonsv1.ResourceRef{},
		},
	}

	// Ensure the ClusterResourceSet gets a UID to be used by dependant objects for creating OwnerReferences.
	setUID(crs)

	objs := []client.Object{crs}

	// Ensures all the resources of type Secret are created and listed as a ClusterResourceSet resources
	for i := range f.secrets {
		secret := f.secrets[i]

		// secrets are owned by the ClusterResourceSet / ownership set by the ClusterResourceSet controller
		secret.SetOwnerReferences([]metav1.OwnerReference{{
			APIVersion: crs.APIVersion,
			Kind:       crs.Kind,
			Name:       crs.Name,
			UID:        crs.UID,
		}})

		crs.Spec.Resources = append(crs.Spec.Resources, addonsv1.ResourceRef{
			Name: secret.Name,
			Kind: secret.Kind,
		})

		objs = append(objs, secret)
	}

	// Ensures all the resources of type ConfigMap are created and listed as a ClusterResourceSet resources
	for i := range f.configMaps {
		configMap := f.configMaps[i]

		// configMap are owned by the ClusterResourceSet / ownership set by the ClusterResourceSet controller
		configMap.SetOwnerReferences([]metav1.OwnerReference{{
			APIVersion: crs.APIVersion,
			Kind:       crs.Kind,
			Name:       crs.Name,
			UID:        crs.UID,
		}})

		crs.Spec.Resources = append(crs.Spec.Resources, addonsv1.ResourceRef{
			Name: configMap.Name,
			Kind: configMap.Kind,
		})

		objs = append(objs, configMap)
	}

	// Ensures all the binding with the clusters where resources are applied.
	for _, cluster := range f.clusters {
		binding := &addonsv1.ClusterResourceSetBinding{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ClusterResourceSetBinding",
				APIVersion: addonsv1.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      cluster.Name,
				Namespace: cluster.Namespace,
			},
			Spec: addonsv1.ClusterResourceSetBindingSpec{
				Bindings: []*addonsv1.ResourceSetBinding{
					{
						ClusterResourceSetName: crs.Name,
					},
				},
			},
		}

		binding.SetOwnerReferences([]metav1.OwnerReference{
			// binding are owned by the ClusterResourceSet / ownership set by the ClusterResourceSet controller
			{
				APIVersion: crs.APIVersion,
				Kind:       crs.Kind,
				Name:       crs.Name,
				UID:        crs.UID,
			},
		})

		objs = append(objs, binding)

		// binding are owned by the Cluster / ownership set by the ClusterResourceSet controller
		binding.SetOwnerReferences(append(binding.OwnerReferences, metav1.OwnerReference{
			APIVersion: cluster.APIVersion,
			Kind:       cluster.Kind,
			Name:       cluster.Name,
			UID:        cluster.UID,
		}))

		resourceSetBinding := addonsv1.ResourceSetBinding{
			ClusterResourceSetName: crs.Name,
			Resources:              []addonsv1.ResourceBinding{},
		}
		binding.Spec.Bindings = append(binding.Spec.Bindings, &resourceSetBinding)

		// creates map entries for each cluster/resource of type Secret
		for _, secret := range f.secrets {
			resourceSetBinding.Resources = append(resourceSetBinding.Resources, addonsv1.ResourceBinding{ResourceRef: addonsv1.ResourceRef{
				Name: secret.Name,
				Kind: "Secret",
			}})
		}

		// creates map entries for each cluster/resource of type ConfigMap
		for _, configMap := range f.configMaps {
			resourceSetBinding.Resources = append(resourceSetBinding.Resources, addonsv1.ResourceBinding{ResourceRef: addonsv1.ResourceRef{
				Name: configMap.Name,
				Kind: "ConfigMap",
			}})
		}
	}

	// Ensure all the objects gets UID.
	// Nb. This adds UID to all the objects; it does not change the UID explicitly sets in advance for the objects involved in the object graphs.
	for _, o := range objs {
		setUID(o)
	}

	return objs
}

type FakeExternalObject struct {
	name      string
	namespace string
}

func NewFakeExternalObject(namespace, name string) *FakeExternalObject {
	return &FakeExternalObject{
		name:      name,
		namespace: namespace,
	}
}

func (f *FakeExternalObject) Objs() []client.Object {
	externalObj := &fakeexternal.GenericExternalObject{
		TypeMeta: metav1.TypeMeta{
			APIVersion: fakeexternal.GroupVersion.String(),
			Kind:       "GenericExternalObject",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      f.name,
			Namespace: f.namespace,
		},
	}

	setUID(externalObj)

	return []client.Object{externalObj}
}

// SelectClusterObj finds and returns a Cluster with the given name and namespace, if any.
func SelectClusterObj(objs []client.Object, namespace, name string) *clusterv1.Cluster {
	for _, o := range objs {
		if o.GetObjectKind().GroupVersionKind().GroupKind() != clusterv1.GroupVersion.WithKind("Cluster").GroupKind() {
			continue
		}

		if o.GetName() == name && o.GetNamespace() == namespace {
			// Converts the object to cluster
			// NB. Convert returns an object without version/kind, so we are enforcing those values back.
			cluster := &clusterv1.Cluster{}
			if err := FakeScheme.Convert(o, cluster, nil); err != nil {
				panic(fmt.Sprintf("failed to convert %s to cluster: %v", o.GetObjectKind(), err))
			}
			cluster.APIVersion = o.GetObjectKind().GroupVersionKind().GroupVersion().String()
			cluster.Kind = o.GetObjectKind().GroupVersionKind().Kind
			return cluster
		}
	}
	return nil
}

// setUID assigns a UID to the object, so test objects are uniquely identified.
// NB. In order to make debugging easier we are using a human readable, deterministic string (instead of a random UID).
func setUID(obj client.Object) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		panic(fmt.Sprintf("failde to get accessor for test object: %v", err))
	}
	uid := fmt.Sprintf("%s, %s/%s", obj.GetObjectKind().GroupVersionKind().String(), accessor.GetNamespace(), accessor.GetName())
	accessor.SetUID(types.UID(uid))
}

// FakeCustomResourceDefinition returns a fake CRD object for the given group/versions/kind.
func FakeCustomResourceDefinition(group string, kind string, versions ...string) *apiextensionslv1.CustomResourceDefinition {
	crd := &apiextensionslv1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			Kind:       apiextensionslv1.SchemeGroupVersion.String(),
			APIVersion: "CustomResourceDefinition",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s.%s", strings.ToLower(kind), group), // NB. this technically should use plural(kind), but for the sake of test what really matters is to generate a unique name
			Labels: map[string]string{
				clusterctlv1.ClusterctlLabelName: "",
			},
		},
		Spec: apiextensionslv1.CustomResourceDefinitionSpec{ // NB. the spec contains only what is strictly required by the move test
			Group: group,
			Names: apiextensionslv1.CustomResourceDefinitionNames{
				Kind: kind,
			},
		},
	}

	for i, version := range versions {
		// set the first version as a storage version
		versionObj := apiextensionslv1.CustomResourceDefinitionVersion{Name: version}
		if i == 0 {
			versionObj.Storage = true
		}
		crd.Spec.Versions = append(crd.Spec.Versions, versionObj)
	}

	return crd
}

// FakeCRDList returns FakeCustomResourceDefinitions for all the Types used in the test object graph.
func FakeCRDList() []*apiextensionslv1.CustomResourceDefinition {
	version := "v1alpha4"

	// Ensure external objects are of a CRD type with the "force move" label
	externalCRD := FakeCustomResourceDefinition(fakeexternal.GroupVersion.Group, "GenericExternalObject", version)
	externalCRD.Labels[clusterctlv1.ClusterctlMoveLabelName] = ""

	return []*apiextensionslv1.CustomResourceDefinition{
		FakeCustomResourceDefinition(clusterv1.GroupVersion.Group, "Cluster", version),
		FakeCustomResourceDefinition(clusterv1.GroupVersion.Group, "Machine", version),
		FakeCustomResourceDefinition(clusterv1.GroupVersion.Group, "MachineDeployment", version),
		FakeCustomResourceDefinition(clusterv1.GroupVersion.Group, "MachineSet", version),
		FakeCustomResourceDefinition(expv1.GroupVersion.Group, "MachinePool", version),
		FakeCustomResourceDefinition(addonsv1.GroupVersion.Group, "ClusterResourceSet", version),
		FakeCustomResourceDefinition(addonsv1.GroupVersion.Group, "ClusterResourceSetBinding", version),
		FakeCustomResourceDefinition(fakecontrolplane.GroupVersion.Group, "GenericControlPlane", version),
		FakeCustomResourceDefinition(fakeinfrastructure.GroupVersion.Group, "GenericInfrastructureCluster", version),
		FakeCustomResourceDefinition(fakeinfrastructure.GroupVersion.Group, "GenericInfrastructureMachine", version),
		FakeCustomResourceDefinition(fakeinfrastructure.GroupVersion.Group, "GenericInfrastructureMachineTemplate", version),
		FakeCustomResourceDefinition(fakebootstrap.GroupVersion.Group, "GenericBootstrapConfig", version),
		FakeCustomResourceDefinition(fakebootstrap.GroupVersion.Group, "GenericBootstrapConfigTemplate", version),
		externalCRD,
	}
}
