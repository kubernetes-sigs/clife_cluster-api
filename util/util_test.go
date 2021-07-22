/*
Copyright 2018 The Kubernetes Authors.

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

package util

import (
	"fmt"
	"testing"

	"github.com/blang/semver"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestMachineToInfrastructureMapFunc(t *testing.T) {
	g := NewWithT(t)

	var testcases = []struct {
		name    string
		input   schema.GroupVersionKind
		request client.Object
		output  []reconcile.Request
	}{
		{
			name: "should reconcile infra-1",
			input: schema.GroupVersionKind{
				Group:   "foo.cluster.x-k8s.io",
				Version: "v1alpha4",
				Kind:    "TestMachine",
			},
			request: &clusterv1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: metav1.NamespaceDefault,
					Name:      "test-1",
				},
				Spec: clusterv1.MachineSpec{
					InfrastructureRef: corev1.ObjectReference{
						APIVersion: "foo.cluster.x-k8s.io/v1alpha4",
						Kind:       "TestMachine",
						Name:       "infra-1",
					},
				},
			},
			output: []reconcile.Request{
				{
					NamespacedName: client.ObjectKey{
						Namespace: metav1.NamespaceDefault,
						Name:      "infra-1",
					},
				},
			},
		},
		{
			name: "should return no matching reconcile requests",
			input: schema.GroupVersionKind{
				Group:   "foo.cluster.x-k8s.io",
				Version: "v1alpha4",
				Kind:    "TestMachine",
			},
			request: &clusterv1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: metav1.NamespaceDefault,
					Name:      "test-1",
				},
				Spec: clusterv1.MachineSpec{
					InfrastructureRef: corev1.ObjectReference{
						APIVersion: "bar.cluster.x-k8s.io/v1alpha4",
						Kind:       "TestMachine",
						Name:       "bar-1",
					},
				},
			},
			output: nil,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			fn := MachineToInfrastructureMapFunc(tc.input)
			out := fn(tc.request)
			g.Expect(out).To(Equal(tc.output))
		})
	}
}

func TestClusterToInfrastructureMapFunc(t *testing.T) {
	g := NewWithT(t)

	var testcases = []struct {
		name    string
		input   schema.GroupVersionKind
		request client.Object
		output  []reconcile.Request
	}{
		{
			name: "should reconcile infra-1",
			input: schema.GroupVersionKind{
				Group:   "foo.cluster.x-k8s.io",
				Version: "v1alpha4",
				Kind:    "TestCluster",
			},
			request: &clusterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: metav1.NamespaceDefault,
					Name:      "test-1",
				},
				Spec: clusterv1.ClusterSpec{
					InfrastructureRef: &corev1.ObjectReference{
						APIVersion: "foo.cluster.x-k8s.io/v1alpha4",
						Kind:       "TestCluster",
						Name:       "infra-1",
					},
				},
			},
			output: []reconcile.Request{
				{
					NamespacedName: client.ObjectKey{
						Namespace: metav1.NamespaceDefault,
						Name:      "infra-1",
					},
				},
			},
		},
		{
			name: "should return no matching reconcile requests",
			input: schema.GroupVersionKind{
				Group:   "foo.cluster.x-k8s.io",
				Version: "v1alpha4",
				Kind:    "TestCluster",
			},
			request: &clusterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: metav1.NamespaceDefault,
					Name:      "test-1",
				},
				Spec: clusterv1.ClusterSpec{
					InfrastructureRef: &corev1.ObjectReference{
						APIVersion: "bar.cluster.x-k8s.io/v1alpha4",
						Kind:       "TestCluster",
						Name:       "bar-1",
					},
				},
			},
			output: nil,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			fn := ClusterToInfrastructureMapFunc(tc.input)
			out := fn(tc.request)
			g.Expect(out).To(Equal(tc.output))
		})
	}
}

func TestHasOwner(t *testing.T) {
	g := NewWithT(t)

	tests := []struct {
		name     string
		refList  []metav1.OwnerReference
		expected bool
	}{
		{
			name: "no ownership",
		},
		{
			name: "owned by cluster",
			refList: []metav1.OwnerReference{
				{
					Kind:       "Cluster",
					APIVersion: clusterv1.GroupVersion.String(),
				},
			},
			expected: true,
		},
		{
			name: "owned by cluster from older version",
			refList: []metav1.OwnerReference{
				{
					Kind:       "Cluster",
					APIVersion: "cluster.x-k8s.io/v1alpha2",
				},
			},
			expected: true,
		},
		{
			name: "owned by a MachineDeployment from older version",
			refList: []metav1.OwnerReference{
				{
					Kind:       "MachineDeployment",
					APIVersion: "cluster.x-k8s.io/v1alpha2",
				},
			},
			expected: true,
		},
		{
			name: "owned by something else",
			refList: []metav1.OwnerReference{
				{
					Kind:       "Pod",
					APIVersion: "v1",
				},
				{
					Kind:       "Deployment",
					APIVersion: "apps/v1",
				},
			},
		},
		{
			name: "owner by a deployment",
			refList: []metav1.OwnerReference{
				{
					Kind:       "MachineDeployment",
					APIVersion: clusterv1.GroupVersion.String(),
				},
			},
			expected: true,
		},
		{
			name: "right kind, wrong apiversion",
			refList: []metav1.OwnerReference{
				{
					Kind:       "MachineDeployment",
					APIVersion: "wrong/v2",
				},
			},
		},
		{
			name: "right apiversion, wrong kind",
			refList: []metav1.OwnerReference{
				{
					Kind:       "Machine",
					APIVersion: clusterv1.GroupVersion.String(),
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := HasOwner(
				test.refList,
				clusterv1.GroupVersion.String(),
				[]string{"MachineDeployment", "Cluster"},
			)
			g.Expect(result).To(Equal(test.expected))
		})
	}
}

type fakeMeta struct {
	metav1.ObjectMeta
	metav1.TypeMeta
}

var _ runtime.Object = &fakeMeta{}

func (*fakeMeta) DeepCopyObject() runtime.Object {
	panic("not implemented")
}

func TestIsOwnedByObject(t *testing.T) {
	g := NewWithT(t)

	targetGroup := "ponies.info"
	targetKind := "Rainbow"
	targetName := "fri3ndsh1p"

	meta := fakeMeta{
		metav1.ObjectMeta{
			Name: targetName,
		},
		metav1.TypeMeta{
			APIVersion: "ponies.info/v1",
			Kind:       targetKind,
		},
	}

	tests := []struct {
		name     string
		refs     []metav1.OwnerReference
		expected bool
	}{
		{
			name: "empty owner list",
		},
		{
			name: "single wrong name owner ref",
			refs: []metav1.OwnerReference{{
				APIVersion: targetGroup + "/v1",
				Kind:       targetKind,
				Name:       "m4g1c",
			}},
		},
		{
			name: "single wrong group owner ref",
			refs: []metav1.OwnerReference{{
				APIVersion: "dazzlings.info/v1",
				Kind:       "Twilight",
				Name:       "m4g1c",
			}},
		},
		{
			name: "single wrong kind owner ref",
			refs: []metav1.OwnerReference{{
				APIVersion: targetGroup + "/v1",
				Kind:       "Twilight",
				Name:       "m4g1c",
			}},
		},
		{
			name: "single right owner ref",
			refs: []metav1.OwnerReference{{
				APIVersion: targetGroup + "/v1",
				Kind:       targetKind,
				Name:       targetName,
			}},
			expected: true,
		},
		{
			name: "single right owner ref (different version)",
			refs: []metav1.OwnerReference{{
				APIVersion: targetGroup + "/v2alpha2",
				Kind:       targetKind,
				Name:       targetName,
			}},
			expected: true,
		},
		{
			name: "multiple wrong refs",
			refs: []metav1.OwnerReference{{
				APIVersion: targetGroup + "/v1",
				Kind:       targetKind,
				Name:       "m4g1c",
			}, {
				APIVersion: targetGroup + "/v1",
				Kind:       targetKind,
				Name:       "h4rm0ny",
			}},
		},
		{
			name: "multiple refs one right",
			refs: []metav1.OwnerReference{{
				APIVersion: targetGroup + "/v1",
				Kind:       targetKind,
				Name:       "m4g1c",
			}, {
				APIVersion: targetGroup + "/v1",
				Kind:       targetKind,
				Name:       targetName,
			}},
			expected: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pointer := &metav1.ObjectMeta{
				OwnerReferences: test.refs,
			}

			g.Expect(IsOwnedByObject(pointer, &meta)).To(Equal(test.expected), "Could not find a ref to %+v in %+v", meta, test.refs)
		})
	}
}

func TestGetOwnerClusterSuccessByName(t *testing.T) {
	g := NewWithT(t)

	myCluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: metav1.NamespaceDefault,
		},
	}

	c := fake.NewClientBuilder().
		WithObjects(myCluster).
		Build()

	objm := metav1.ObjectMeta{
		OwnerReferences: []metav1.OwnerReference{
			{
				Kind:       "Cluster",
				APIVersion: clusterv1.GroupVersion.String(),
				Name:       "my-cluster",
			},
		},
		Namespace: metav1.NamespaceDefault,
		Name:      "my-resource-owned-by-cluster",
	}
	cluster, err := GetOwnerCluster(ctx, c, objm)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(cluster).NotTo(BeNil())

	// Make sure API version does not matter
	objm.OwnerReferences[0].APIVersion = "cluster.x-k8s.io/v1alpha1234"
	cluster, err = GetOwnerCluster(ctx, c, objm)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(cluster).NotTo(BeNil())
}

func TestGetOwnerMachineSuccessByName(t *testing.T) {
	g := NewWithT(t)

	myMachine := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-machine",
			Namespace: metav1.NamespaceDefault,
		},
	}

	c := fake.NewClientBuilder().
		WithObjects(myMachine).
		Build()

	objm := metav1.ObjectMeta{
		OwnerReferences: []metav1.OwnerReference{
			{
				Kind:       "Machine",
				APIVersion: clusterv1.GroupVersion.String(),
				Name:       "my-machine",
			},
		},
		Namespace: metav1.NamespaceDefault,
		Name:      "my-resource-owned-by-machine",
	}
	machine, err := GetOwnerMachine(ctx, c, objm)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(machine).NotTo(BeNil())
}

func TestGetOwnerMachineSuccessByNameFromDifferentVersion(t *testing.T) {
	g := NewWithT(t)

	myMachine := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-machine",
			Namespace: metav1.NamespaceDefault,
		},
	}

	c := fake.NewClientBuilder().
		WithObjects(myMachine).
		Build()

	objm := metav1.ObjectMeta{
		OwnerReferences: []metav1.OwnerReference{
			{
				Kind:       "Machine",
				APIVersion: clusterv1.GroupVersion.Group + "/v1alpha2",
				Name:       "my-machine",
			},
		},
		Namespace: metav1.NamespaceDefault,
		Name:      "my-resource-owned-by-machine",
	}
	machine, err := GetOwnerMachine(ctx, c, objm)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(machine).NotTo(BeNil())
}

func TestGetMachinesForCluster(t *testing.T) {
	g := NewWithT(t)

	cluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: metav1.NamespaceDefault,
		},
	}

	machine := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-machine",
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				clusterv1.ClusterLabelName: cluster.Name,
			},
		},
	}

	machineDifferentClusterNameSameNamespace := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "other-machine",
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				clusterv1.ClusterLabelName: "other-cluster",
			},
		},
	}

	machineSameClusterNameDifferentNamespace := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "other-machine",
			Namespace: "other-ns",
			Labels: map[string]string{
				clusterv1.ClusterLabelName: cluster.Name,
			},
		},
	}

	c := fake.NewClientBuilder().WithObjects(
		machine,
		machineDifferentClusterNameSameNamespace,
		machineSameClusterNameDifferentNamespace,
	).Build()

	machines, err := GetMachinesForCluster(ctx, c, cluster)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(machines.Items).To(HaveLen(1))
	g.Expect(machines.Items[0].Labels[clusterv1.ClusterLabelName]).To(Equal(cluster.Name))
}

func TestIsExternalManagedControlPlane(t *testing.T) {
	g := NewWithT(t)

	t.Run("should return true if control plane status externalManagedControlPlane is true", func(t *testing.T) {
		controlPlane := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"status": map[string]interface{}{
					"externalManagedControlPlane": true,
				},
			},
		}
		result := IsExternalManagedControlPlane(controlPlane)
		g.Expect(result).Should(Equal(true))
	})

	t.Run("should return false if control plane status externalManagedControlPlane is false", func(t *testing.T) {
		controlPlane := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"status": map[string]interface{}{
					"externalManagedControlPlane": false,
				},
			},
		}
		result := IsExternalManagedControlPlane(controlPlane)
		g.Expect(result).Should(Equal(false))
	})

	t.Run("should return false if control plane status externalManagedControlPlane is not set", func(t *testing.T) {
		controlPlane := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"status": map[string]interface{}{
					"someOtherStatusField": "someValue",
				},
			},
		}
		result := IsExternalManagedControlPlane(controlPlane)
		g.Expect(result).Should(Equal(false))
	})
}

func TestEnsureOwnerRef(t *testing.T) {
	g := NewWithT(t)

	t.Run("should set ownerRef on an empty list", func(t *testing.T) {
		obj := &clusterv1.Machine{}
		ref := metav1.OwnerReference{
			APIVersion: clusterv1.GroupVersion.String(),
			Kind:       "Cluster",
			Name:       "test-cluster",
		}
		obj.OwnerReferences = EnsureOwnerRef(obj.OwnerReferences, ref)
		g.Expect(obj.OwnerReferences).Should(ContainElement(ref))
	})

	t.Run("should not duplicate owner references", func(t *testing.T) {
		obj := &clusterv1.Machine{
			ObjectMeta: metav1.ObjectMeta{
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: clusterv1.GroupVersion.String(),
						Kind:       "Cluster",
						Name:       "test-cluster",
					},
				},
			},
		}
		ref := metav1.OwnerReference{
			APIVersion: clusterv1.GroupVersion.String(),
			Kind:       "Cluster",
			Name:       "test-cluster",
		}
		obj.OwnerReferences = EnsureOwnerRef(obj.OwnerReferences, ref)
		g.Expect(obj.OwnerReferences).Should(ContainElement(ref))
		g.Expect(obj.OwnerReferences).Should(HaveLen(1))
	})

	t.Run("should update the APIVersion if duplicate", func(t *testing.T) {
		oldgvk := schema.GroupVersion{
			Group:   clusterv1.GroupVersion.Group,
			Version: "v1alpha2",
		}
		obj := &clusterv1.Machine{
			ObjectMeta: metav1.ObjectMeta{
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: oldgvk.String(),
						Kind:       "Cluster",
						Name:       "test-cluster",
					},
				},
			},
		}
		ref := metav1.OwnerReference{
			APIVersion: clusterv1.GroupVersion.String(),
			Kind:       "Cluster",
			Name:       "test-cluster",
		}
		obj.OwnerReferences = EnsureOwnerRef(obj.OwnerReferences, ref)
		g.Expect(obj.OwnerReferences).Should(ContainElement(ref))
		g.Expect(obj.OwnerReferences).Should(HaveLen(1))
	})
}

func TestClusterToObjectsMapper(t *testing.T) {
	g := NewWithT(t)

	cluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test1",
		},
	}

	table := []struct {
		name        string
		objects     []client.Object
		input       runtime.Object
		output      []ctrl.Request
		expectError bool
	}{
		{
			name:  "should return a list of requests with labelled machines",
			input: &clusterv1.MachineList{},
			objects: []client.Object{
				&clusterv1.Machine{
					ObjectMeta: metav1.ObjectMeta{
						Name: "machine1",
						Labels: map[string]string{
							clusterv1.ClusterLabelName: "test1",
						},
					},
				},
				&clusterv1.Machine{
					ObjectMeta: metav1.ObjectMeta{
						Name: "machine2",
						Labels: map[string]string{
							clusterv1.ClusterLabelName: "test1",
						},
					},
				},
			},
			output: []ctrl.Request{
				{NamespacedName: client.ObjectKey{Name: "machine1"}},
				{NamespacedName: client.ObjectKey{Name: "machine2"}},
			},
		},
		{
			name:  "should return a list of requests with labelled MachineDeployments",
			input: &clusterv1.MachineDeploymentList{},
			objects: []client.Object{
				&clusterv1.MachineDeployment{
					ObjectMeta: metav1.ObjectMeta{
						Name: "md1",
						Labels: map[string]string{
							clusterv1.ClusterLabelName: "test1",
						},
					},
				},
				&clusterv1.MachineDeployment{
					ObjectMeta: metav1.ObjectMeta{
						Name: "md2",
						Labels: map[string]string{
							clusterv1.ClusterLabelName: "test2",
						},
					},
				},
				&clusterv1.MachineDeployment{
					ObjectMeta: metav1.ObjectMeta{
						Name: "md3",
						Labels: map[string]string{
							clusterv1.ClusterLabelName: "test1",
						},
					},
				},
				&clusterv1.MachineDeployment{
					ObjectMeta: metav1.ObjectMeta{
						Name: "md4",
					},
				},
			},
			output: []ctrl.Request{
				{NamespacedName: client.ObjectKey{Name: "md1"}},
				{NamespacedName: client.ObjectKey{Name: "md3"}},
			},
		},
	}

	for _, tc := range table {
		tc.objects = append(tc.objects, cluster)
		client := fake.NewClientBuilder().WithObjects(tc.objects...).Build()
		f, err := ClusterToObjectsMapper(client, tc.input, scheme.Scheme)
		g.Expect(err != nil, err).To(Equal(tc.expectError))
		g.Expect(f(cluster)).To(ConsistOf(tc.output))
	}
}

func TestOrdinalize(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, "0th"},
		{1, "1st"},
		{2, "2nd"},
		{43, "43rd"},
		{5, "5th"},
		{6, "6th"},
		{207, "207th"},
		{1008, "1008th"},
		{-109, "-109th"},
		{-0, "0th"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("ordinalize %d", tt.input), func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(Ordinalize(tt.input)).To(Equal(tt.expected))
		})
	}
}

func TestIsSupportedVersionSkew(t *testing.T) {
	type args struct {
		a semver.Version
		b semver.Version
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "same version",
			args: args{
				a: semver.MustParse("1.10.0"),
				b: semver.MustParse("1.10.0"),
			},
			want: true,
		},
		{
			name: "different patch version",
			args: args{
				a: semver.MustParse("1.10.0"),
				b: semver.MustParse("1.10.2"),
			},
			want: true,
		},
		{
			name: "a + 1 minor version",
			args: args{
				a: semver.MustParse("1.11.0"),
				b: semver.MustParse("1.10.2"),
			},
			want: true,
		},
		{
			name: "b + 1 minor version",
			args: args{
				a: semver.MustParse("1.10.0"),
				b: semver.MustParse("1.11.2"),
			},
			want: true,
		},
		{
			name: "a + 2 minor versions",
			args: args{
				a: semver.MustParse("1.12.0"),
				b: semver.MustParse("1.10.0"),
			},
			want: false,
		},
		{
			name: "b + 2 minor versions",
			args: args{
				a: semver.MustParse("1.10.0"),
				b: semver.MustParse("1.12.0"),
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSupportedVersionSkew(tt.args.a, tt.args.b); got != tt.want {
				t.Errorf("IsSupportedVersionSkew() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRemoveOwnerRef(t *testing.T) {
	g := NewWithT(t)
	ownerRefs := []metav1.OwnerReference{
		{
			APIVersion: "dazzlings.info/v1",
			Kind:       "Twilight",
			Name:       "m4g1c",
		},
		{
			APIVersion: "bar.cluster.x-k8s.io/v1alpha4",
			Kind:       "TestCluster",
			Name:       "bar-1",
		},
	}

	tests := []struct {
		name        string
		toBeRemoved metav1.OwnerReference
	}{
		{
			name: "owner reference present",
			toBeRemoved: metav1.OwnerReference{
				APIVersion: "dazzlings.info/v1",
				Kind:       "Twilight",
				Name:       "m4g1c",
			},
		},
		{
			name: "owner reference not present",
			toBeRemoved: metav1.OwnerReference{
				APIVersion: "dazzlings.info/v1",
				Kind:       "Twilight",
				Name:       "abcdef",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newOwnerRefs := RemoveOwnerRef(ownerRefs, tt.toBeRemoved)
			g.Expect(HasOwnerRef(newOwnerRefs, tt.toBeRemoved)).NotTo(BeTrue())
		})
	}
}
