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

package controllers

import (
	"testing"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
)

func TestGetNestedRef(t *testing.T) {
	t.Run("Gets a nested ref if defined", func(t *testing.T) {
		g := NewWithT(t)

		infrastructureMachineTemplate := newFakeInfrastructureMachineTemplate(metav1.NamespaceDefault, "control-plane-machine-infrastructure-template1").Obj()
		controlPlaneTemplate := newFakeControlPlane(metav1.NamespaceDefault, "control-plane-template").
			WithInfrastructureMachineTemplate(infrastructureMachineTemplate).
			Obj()

		ref := getNestedRef(controlPlaneTemplate, "spec", "machineTemplate", "infrastructureRef")
		g.Expect(ref).ToNot(BeNil())
		g.Expect(ref.APIVersion).To(Equal(infrastructureMachineTemplate.GetAPIVersion()))
		g.Expect(ref.Kind).To(Equal(infrastructureMachineTemplate.GetKind()))
		g.Expect(ref.Name).To(Equal(infrastructureMachineTemplate.GetName()))
		g.Expect(ref.Namespace).To(Equal(infrastructureMachineTemplate.GetNamespace()))
	})
	t.Run("Gets nin if the nested ref does not exists", func(t *testing.T) {
		g := NewWithT(t)

		controlPlaneTemplate := newFakeControlPlane(metav1.NamespaceDefault, "control-plane-template").Obj()

		ref := getNestedRef(controlPlaneTemplate, "spec", "machineTemplate", "infrastructureRef")
		g.Expect(ref).To(BeNil())
	})
}

func TestSetNestedRef(t *testing.T) {
	t.Run("Sets a nested ref", func(t *testing.T) {
		g := NewWithT(t)
		infrastructureMachineTemplate := newFakeInfrastructureMachineTemplate(metav1.NamespaceDefault, "control-plane-machine-infrastructure-template1").Obj()
		controlPlaneTemplate := newFakeControlPlane(metav1.NamespaceDefault, "control-plane-template").Obj()

		err := setNestedRef(controlPlaneTemplate, infrastructureMachineTemplate, "spec", "machineTemplate", "infrastructureRef")
		g.Expect(err).To(BeNil())

		ref := getNestedRef(controlPlaneTemplate, "spec", "machineTemplate", "infrastructureRef")
		g.Expect(ref).ToNot(BeNil())
		g.Expect(ref.APIVersion).To(Equal(infrastructureMachineTemplate.GetAPIVersion()))
		g.Expect(ref.Kind).To(Equal(infrastructureMachineTemplate.GetKind()))
		g.Expect(ref.Name).To(Equal(infrastructureMachineTemplate.GetName()))
		g.Expect(ref.Namespace).To(Equal(infrastructureMachineTemplate.GetNamespace()))
	})
}

func TestObjToRef(t *testing.T) {
	t.Run("Gets a ref from an obj", func(t *testing.T) {
		g := NewWithT(t)
		infrastructureMachineTemplate := newFakeInfrastructureMachineTemplate(metav1.NamespaceDefault, "control-plane-machine-infrastructure-template1").Obj()
		ref := objToRef(infrastructureMachineTemplate)

		g.Expect(ref).ToNot(BeNil())
		g.Expect(ref.APIVersion).To(Equal(infrastructureMachineTemplate.GetAPIVersion()))
		g.Expect(ref.Kind).To(Equal(infrastructureMachineTemplate.GetKind()))
		g.Expect(ref.Name).To(Equal(infrastructureMachineTemplate.GetName()))
		g.Expect(ref.Namespace).To(Equal(infrastructureMachineTemplate.GetNamespace()))
	})
}

var (
	fakeInfrastructureProviderGroupVersion = schema.GroupVersion{Group: "infrastructure.cluster.x-k8s.io", Version: "v1alpha4"}
	fakeControlPlaneProviderGroupVersion   = schema.GroupVersion{Group: "controlplane.cluster.x-k8s.io", Version: "v1alpha4"}
)

type fakeClusterClass struct {
	namespace                     string
	name                          string
	infrastructureClusterTemplate *unstructured.Unstructured
	controlPlaneTemplate          *unstructured.Unstructured
}

func newFakeClusterClass(namespace, name string) *fakeClusterClass { //nolint:deadcode
	return &fakeClusterClass{
		namespace: namespace,
		name:      name,
	}
}

func (f *fakeClusterClass) WithInfrastructureClusterTemplate(t *unstructured.Unstructured) *fakeClusterClass {
	f.infrastructureClusterTemplate = t
	return f
}

func (f *fakeClusterClass) WithControlPlaneTemplate(t *unstructured.Unstructured) *fakeClusterClass {
	f.controlPlaneTemplate = t
	return f
}

func (f *fakeClusterClass) Obj() *clusterv1.ClusterClass {
	obj := &clusterv1.ClusterClass{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterClass",
			APIVersion: clusterv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      f.name,
			Namespace: f.namespace,
		},
		Spec: clusterv1.ClusterClassSpec{},
	}
	if f.infrastructureClusterTemplate != nil {
		obj.Spec.Infrastructure = clusterv1.LocalObjectTemplate{
			Ref: objToRef(f.infrastructureClusterTemplate),
		}
	}
	if f.controlPlaneTemplate != nil {
		obj.Spec.ControlPlane = clusterv1.LocalObjectTemplate{
			Ref: objToRef(f.controlPlaneTemplate),
		}
	}
	return obj
}

type fakeInfrastructureClusterTemplate struct {
	namespace string
	name      string
}

func newFakeInfrastructureClusterTemplate(namespace, name string) *fakeInfrastructureClusterTemplate { //nolint:deadcode
	return &fakeInfrastructureClusterTemplate{
		namespace: namespace,
		name:      name,
	}
}

func (f *fakeInfrastructureClusterTemplate) Obj() *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion(fakeInfrastructureProviderGroupVersion.String())
	obj.SetKind("FakeInfrastructureClusterTemplate")
	obj.SetNamespace(f.namespace)
	obj.SetName(f.name)

	if err := unstructured.SetNestedField(obj.UnstructuredContent(), true, "spec", "template", "spec", "fakeSetting"); err != nil {
		panic(err)
	}

	return obj
}

type fakeInfrastructureMachineTemplate struct {
	namespace string
	name      string
}

func newFakeInfrastructureMachineTemplate(namespace, name string) *fakeInfrastructureMachineTemplate {
	return &fakeInfrastructureMachineTemplate{
		namespace: namespace,
		name:      name,
	}
}

func (f *fakeInfrastructureMachineTemplate) Obj() *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion(fakeInfrastructureProviderGroupVersion.String())
	obj.SetKind("FakeInfrastructureMachineTemplate")
	obj.SetNamespace(f.namespace)
	obj.SetName(f.name)

	if err := unstructured.SetNestedField(obj.UnstructuredContent(), true, "spec", "template", "spec", "fakeSetting"); err != nil {
		panic(err)
	}

	return obj
}

type fakeControlPlaneTemplate struct {
	namespace                     string
	name                          string
	infrastructureMachineTemplate *unstructured.Unstructured
}

func newFakeControlPlaneTemplate(namespace, name string) *fakeControlPlaneTemplate { //nolint:deadcode
	return &fakeControlPlaneTemplate{
		namespace: namespace,
		name:      name,
	}
}

func (f *fakeControlPlaneTemplate) WithInfrastructureMachineTemplate(t *unstructured.Unstructured) *fakeControlPlaneTemplate {
	f.infrastructureMachineTemplate = t
	return f
}

func (f *fakeControlPlaneTemplate) Obj() *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion(fakeControlPlaneProviderGroupVersion.String())
	obj.SetKind("FakeControlPlaneTemplate")
	obj.SetNamespace(f.namespace)
	obj.SetName(f.name)

	if err := unstructured.SetNestedField(obj.UnstructuredContent(), true, "spec", "template", "spec", "fakeSetting"); err != nil {
		panic(err)
	}

	if f.infrastructureMachineTemplate != nil {
		if err := setNestedRef(obj, f.infrastructureMachineTemplate, "spec", "template", "spec", "machineTemplate", "infrastructureRef"); err != nil {
			panic(err)
		}
	}
	return obj
}

type fakeInfrastructureCluster struct {
	namespace string
	name      string
}

func newFakeInfrastructureCluster(namespace, name string) *fakeInfrastructureCluster { //nolint:deadcode
	return &fakeInfrastructureCluster{
		namespace: namespace,
		name:      name,
	}
}

func (f *fakeInfrastructureCluster) Obj() *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion(fakeControlPlaneProviderGroupVersion.String())
	obj.SetKind("FakeInfrastructureCluster")
	obj.SetNamespace(f.namespace)
	obj.SetName(f.name)

	if err := unstructured.SetNestedField(obj.UnstructuredContent(), true, "spec", "fakeSetting"); err != nil {
		panic(err)
	}

	return obj
}

type fakeControlPlane struct {
	namespace                     string
	name                          string
	infrastructureMachineTemplate *unstructured.Unstructured
}

func newFakeControlPlane(namespace, name string) *fakeControlPlane {
	return &fakeControlPlane{
		namespace: namespace,
		name:      name,
	}
}

func (f *fakeControlPlane) WithInfrastructureMachineTemplate(t *unstructured.Unstructured) *fakeControlPlane {
	f.infrastructureMachineTemplate = t
	return f
}

func (f *fakeControlPlane) Obj() *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion(fakeControlPlaneProviderGroupVersion.String())
	obj.SetKind("FakeControlPlane")
	obj.SetNamespace(f.namespace)
	obj.SetName(f.name)

	if err := unstructured.SetNestedField(obj.UnstructuredContent(), true, "spec", "fakeSetting"); err != nil {
		panic(err)
	}

	if f.infrastructureMachineTemplate != nil {
		if err := setNestedRef(obj, f.infrastructureMachineTemplate, "spec", "machineTemplate", "infrastructureRef"); err != nil {
			panic(err)
		}
	}
	return obj
}
