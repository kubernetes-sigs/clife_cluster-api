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

package mdutil

import (
	"fmt"
	"math/rand"
	"strconv"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apiserver/pkg/storage/names"
	"k8s.io/klog/v2/klogr"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
)

func newDControllerRef(d *clusterv1.MachineDeployment) *metav1.OwnerReference {
	isController := true
	return &metav1.OwnerReference{
		APIVersion: "clusters/v1alpha",
		Kind:       "MachineDeployment",
		Name:       d.GetName(),
		UID:        d.GetUID(),
		Controller: &isController,
	}
}

// generateMS creates a machine set, with the input deployment's template as its template.
func generateMS(deployment clusterv1.MachineDeployment) clusterv1.MachineSet {
	template := deployment.Spec.Template.DeepCopy()
	return clusterv1.MachineSet{
		ObjectMeta: metav1.ObjectMeta{
			UID:             randomUID(),
			Name:            names.SimpleNameGenerator.GenerateName("machineset"),
			Labels:          template.Labels,
			OwnerReferences: []metav1.OwnerReference{*newDControllerRef(&deployment)},
		},
		Spec: clusterv1.MachineSetSpec{
			Replicas: new(int32),
			Template: *template,
			Selector: metav1.LabelSelector{MatchLabels: template.Labels},
		},
	}
}

func randomUID() types.UID {
	return types.UID(strconv.FormatInt(rand.Int63(), 10)) //nolint:gosec
}

// generateDeployment creates a deployment, with the input image as its template.
func generateDeployment(image string) clusterv1.MachineDeployment {
	machineLabels := map[string]string{"name": image}
	return clusterv1.MachineDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        image,
			Annotations: make(map[string]string),
		},
		Spec: clusterv1.MachineDeploymentSpec{
			Replicas: func(i int32) *int32 { return &i }(1),
			Selector: metav1.LabelSelector{MatchLabels: machineLabels},
			Template: clusterv1.MachineTemplateSpec{
				ObjectMeta: clusterv1.ObjectMeta{
					Labels: machineLabels,
				},
				Spec: clusterv1.MachineSpec{},
			},
		},
	}
}

func generateMachineTemplateSpec(annotations, labels map[string]string) clusterv1.MachineTemplateSpec {
	return clusterv1.MachineTemplateSpec{
		ObjectMeta: clusterv1.ObjectMeta{
			Annotations: annotations,
			Labels:      labels,
		},
		Spec: clusterv1.MachineSpec{},
	}
}

func TestEqualMachineTemplate(t *testing.T) {
	tests := []struct {
		Name           string
		Former, Latter clusterv1.MachineTemplateSpec
		Expected       bool
	}{
		{
			Name:     "Same spec, same labels",
			Former:   generateMachineTemplateSpec(map[string]string{}, map[string]string{DefaultMachineDeploymentUniqueLabelKey: "value-1", "something": "else"}),
			Latter:   generateMachineTemplateSpec(map[string]string{}, map[string]string{DefaultMachineDeploymentUniqueLabelKey: "value-1", "something": "else"}),
			Expected: true,
		},
		{
			Name:     "Same spec, only machine-template-hash label value is different",
			Former:   generateMachineTemplateSpec(map[string]string{}, map[string]string{DefaultMachineDeploymentUniqueLabelKey: "value-1", "something": "else"}),
			Latter:   generateMachineTemplateSpec(map[string]string{}, map[string]string{DefaultMachineDeploymentUniqueLabelKey: "value-2", "something": "else"}),
			Expected: true,
		},
		{
			Name:     "Same spec, the former doesn't have machine-template-hash label",
			Former:   generateMachineTemplateSpec(map[string]string{}, map[string]string{"something": "else"}),
			Latter:   generateMachineTemplateSpec(map[string]string{}, map[string]string{DefaultMachineDeploymentUniqueLabelKey: "value-2", "something": "else"}),
			Expected: true,
		},
		{
			Name:     "Same spec, the former doesn't have machine-template-hash label",
			Former:   generateMachineTemplateSpec(map[string]string{}, map[string]string{"something": "else"}),
			Latter:   generateMachineTemplateSpec(map[string]string{}, map[string]string{DefaultMachineDeploymentUniqueLabelKey: "value-2", "something": "else"}),
			Expected: true,
		},
		{
			Name:     "Same spec, the label is different, the former doesn't have machine-template-hash label, same number of labels",
			Former:   generateMachineTemplateSpec(map[string]string{}, map[string]string{"something": "else"}),
			Latter:   generateMachineTemplateSpec(map[string]string{}, map[string]string{DefaultMachineDeploymentUniqueLabelKey: "value-2"}),
			Expected: false,
		},
		{
			Name:     "Same spec, the label is different, the latter doesn't have machine-template-hash label, same number of labels",
			Former:   generateMachineTemplateSpec(map[string]string{}, map[string]string{DefaultMachineDeploymentUniqueLabelKey: "value-1"}),
			Latter:   generateMachineTemplateSpec(map[string]string{}, map[string]string{"something": "else"}),
			Expected: false,
		},
		{
			Name:     "Same spec, the label is different, and the machine-template-hash label value is the same",
			Former:   generateMachineTemplateSpec(map[string]string{}, map[string]string{DefaultMachineDeploymentUniqueLabelKey: "value-1"}),
			Latter:   generateMachineTemplateSpec(map[string]string{}, map[string]string{DefaultMachineDeploymentUniqueLabelKey: "value-1", "something": "else"}),
			Expected: false,
		},
		{
			Name:     "Different spec, same labels",
			Former:   generateMachineTemplateSpec(map[string]string{"former": "value"}, map[string]string{DefaultMachineDeploymentUniqueLabelKey: "value-1", "something": "else"}),
			Latter:   generateMachineTemplateSpec(map[string]string{"latter": "value"}, map[string]string{DefaultMachineDeploymentUniqueLabelKey: "value-1", "something": "else"}),
			Expected: false,
		},
		{
			Name:     "Different spec, different machine-template-hash label value",
			Former:   generateMachineTemplateSpec(map[string]string{"x": ""}, map[string]string{DefaultMachineDeploymentUniqueLabelKey: "value-1", "something": "else"}),
			Latter:   generateMachineTemplateSpec(map[string]string{"x": "1"}, map[string]string{DefaultMachineDeploymentUniqueLabelKey: "value-2", "something": "else"}),
			Expected: false,
		},
		{
			Name:     "Different spec, the former doesn't have machine-template-hash label",
			Former:   generateMachineTemplateSpec(map[string]string{"x": ""}, map[string]string{"something": "else"}),
			Latter:   generateMachineTemplateSpec(map[string]string{"x": "1"}, map[string]string{DefaultMachineDeploymentUniqueLabelKey: "value-2", "something": "else"}),
			Expected: false,
		},
		{
			Name:     "Different spec, different labels",
			Former:   generateMachineTemplateSpec(map[string]string{}, map[string]string{"something": "else"}),
			Latter:   generateMachineTemplateSpec(map[string]string{}, map[string]string{"nothing": "else"}),
			Expected: false,
		},
		{
			Name: "Same spec, except for references versions",
			Former: clusterv1.MachineTemplateSpec{
				ObjectMeta: clusterv1.ObjectMeta{
					Labels: map[string]string{},
				},
				Spec: clusterv1.MachineSpec{
					Bootstrap: clusterv1.Bootstrap{
						ConfigRef: &corev1.ObjectReference{
							APIVersion: "bootstrap.cluster.x-k8s.io/v1alpha2",
							Kind:       "MachineBootstrap",
						},
					},
					InfrastructureRef: corev1.ObjectReference{
						APIVersion: "infrastructure.cluster.x-k8s.io/v1alpha2",
						Kind:       "MachineInfrastructure",
					},
				},
			},
			Latter: clusterv1.MachineTemplateSpec{
				ObjectMeta: clusterv1.ObjectMeta{
					Labels: map[string]string{},
				},
				Spec: clusterv1.MachineSpec{
					Bootstrap: clusterv1.Bootstrap{
						ConfigRef: &corev1.ObjectReference{
							APIVersion: "bootstrap.cluster.x-k8s.io/v1alpha4",
							Kind:       "MachineBootstrap",
						},
					},
					InfrastructureRef: corev1.ObjectReference{
						APIVersion: "infrastructure.cluster.x-k8s.io/v1alpha4",
						Kind:       "MachineInfrastructure",
					},
				},
			},
			Expected: true,
		},
		{
			Name: "Same spec, bootstrap references are different kinds",
			Former: clusterv1.MachineTemplateSpec{
				ObjectMeta: clusterv1.ObjectMeta{
					Labels: map[string]string{},
				},
				Spec: clusterv1.MachineSpec{
					Bootstrap: clusterv1.Bootstrap{
						ConfigRef: &corev1.ObjectReference{
							APIVersion: "bootstrap.cluster.x-k8s.io/v1alpha2",
							Kind:       "MachineBootstrap1",
						},
					},
					InfrastructureRef: corev1.ObjectReference{
						APIVersion: "infrastructure.cluster.x-k8s.io/v1alpha2",
						Kind:       "MachineInfrastructure",
					},
				},
			},
			Latter: clusterv1.MachineTemplateSpec{
				ObjectMeta: clusterv1.ObjectMeta{
					Labels: map[string]string{},
				},
				Spec: clusterv1.MachineSpec{
					Bootstrap: clusterv1.Bootstrap{
						ConfigRef: &corev1.ObjectReference{
							APIVersion: "bootstrap.cluster.x-k8s.io/v1alpha4",
							Kind:       "MachineBootstrap2",
						},
					},
					InfrastructureRef: corev1.ObjectReference{
						APIVersion: "infrastructure.cluster.x-k8s.io/v1alpha4",
						Kind:       "MachineInfrastructure",
					},
				},
			},
			Expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			g := NewWithT(t)

			runTest := func(t1, t2 *clusterv1.MachineTemplateSpec) {
				// Run
				equal := EqualMachineTemplate(t1, t2)
				g.Expect(equal).To(Equal(test.Expected))
				g.Expect(t1.Labels).NotTo(BeNil())
				g.Expect(t2.Labels).NotTo(BeNil())
			}

			runTest(&test.Former, &test.Latter)
			// Test the same case in reverse order
			runTest(&test.Latter, &test.Former)
		})
	}
}

func TestFindNewMachineSet(t *testing.T) {
	now := metav1.Now()
	later := metav1.Time{Time: now.Add(time.Minute)}

	deployment := generateDeployment("nginx")
	newMS := generateMS(deployment)
	newMS.Labels[DefaultMachineDeploymentUniqueLabelKey] = "hash"
	newMS.CreationTimestamp = later

	newMSDup := generateMS(deployment)
	newMSDup.Labels[DefaultMachineDeploymentUniqueLabelKey] = "different-hash"
	newMSDup.CreationTimestamp = now

	oldDeployment := generateDeployment("nginx")
	oldMS := generateMS(oldDeployment)
	oldMS.Spec.Template.Annotations = map[string]string{
		"old": "true",
	}
	oldMS.Status.FullyLabeledReplicas = *(oldMS.Spec.Replicas)

	tests := []struct {
		Name       string
		deployment clusterv1.MachineDeployment
		msList     []*clusterv1.MachineSet
		expected   *clusterv1.MachineSet
	}{
		{
			Name:       "Get new MachineSet with the same template as Deployment spec but different machine-template-hash value",
			deployment: deployment,
			msList:     []*clusterv1.MachineSet{&newMS, &oldMS},
			expected:   &newMS,
		},
		{
			Name:       "Get the oldest new MachineSet when there are more than one MachineSet with the same template",
			deployment: deployment,
			msList:     []*clusterv1.MachineSet{&newMS, &oldMS, &newMSDup},
			expected:   &newMSDup,
		},
		{
			Name:       "Get nil new MachineSet",
			deployment: deployment,
			msList:     []*clusterv1.MachineSet{&oldMS},
			expected:   nil,
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			g := NewWithT(t)

			ms := FindNewMachineSet(&test.deployment, test.msList)
			g.Expect(ms).To(Equal(test.expected))
		})
	}
}

func TestFindOldMachineSets(t *testing.T) {
	now := metav1.Now()
	later := metav1.Time{Time: now.Add(time.Minute)}
	before := metav1.Time{Time: now.Add(-time.Minute)}

	deployment := generateDeployment("nginx")
	newMS := generateMS(deployment)
	*(newMS.Spec.Replicas) = 1
	newMS.Labels[DefaultMachineDeploymentUniqueLabelKey] = "hash"
	newMS.CreationTimestamp = later

	newMSDup := generateMS(deployment)
	newMSDup.Labels[DefaultMachineDeploymentUniqueLabelKey] = "different-hash"
	newMSDup.CreationTimestamp = now

	oldDeployment := generateDeployment("nginx")
	oldMS := generateMS(oldDeployment)
	oldMS.Spec.Template.Annotations = map[string]string{
		"old": "true",
	}
	oldMS.Status.FullyLabeledReplicas = *(oldMS.Spec.Replicas)
	oldMS.CreationTimestamp = before

	oldDeployment = generateDeployment("nginx")
	oldDeployment.Spec.Selector.MatchLabels["old-label"] = "old-value"
	oldDeployment.Spec.Template.Labels["old-label"] = "old-value"
	oldMSwithOldLabel := generateMS(oldDeployment)
	oldMSwithOldLabel.Status.FullyLabeledReplicas = *(oldMSwithOldLabel.Spec.Replicas)
	oldMSwithOldLabel.CreationTimestamp = before

	tests := []struct {
		Name            string
		deployment      clusterv1.MachineDeployment
		msList          []*clusterv1.MachineSet
		expected        []*clusterv1.MachineSet
		expectedRequire []*clusterv1.MachineSet
	}{
		{
			Name:            "Get old MachineSets",
			deployment:      deployment,
			msList:          []*clusterv1.MachineSet{&newMS, &oldMS},
			expected:        []*clusterv1.MachineSet{&oldMS},
			expectedRequire: nil,
		},
		{
			Name:            "Get old MachineSets with no new MachineSet",
			deployment:      deployment,
			msList:          []*clusterv1.MachineSet{&oldMS},
			expected:        []*clusterv1.MachineSet{&oldMS},
			expectedRequire: nil,
		},
		{
			Name:            "Get old MachineSets with two new MachineSets, only the oldest new MachineSet is seen as new MachineSet",
			deployment:      deployment,
			msList:          []*clusterv1.MachineSet{&oldMS, &newMS, &newMSDup},
			expected:        []*clusterv1.MachineSet{&oldMS, &newMS},
			expectedRequire: []*clusterv1.MachineSet{&newMS},
		},
		{
			Name:            "Get empty old MachineSets",
			deployment:      deployment,
			msList:          []*clusterv1.MachineSet{&newMS},
			expected:        []*clusterv1.MachineSet{},
			expectedRequire: nil,
		},
		{
			Name:            "Get old MachineSets after label changed in MachineDeployments",
			deployment:      deployment,
			msList:          []*clusterv1.MachineSet{&newMS, &oldMSwithOldLabel},
			expected:        []*clusterv1.MachineSet{&oldMSwithOldLabel},
			expectedRequire: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			g := NewWithT(t)

			requireMS, allMS := FindOldMachineSets(&test.deployment, test.msList)
			g.Expect(allMS).To(ConsistOf(test.expected))
			// MSs are getting filtered correctly by ms.spec.replicas
			g.Expect(requireMS).To(ConsistOf(test.expectedRequire))
		})
	}
}

func TestGetReplicaCountForMachineSets(t *testing.T) {
	ms1 := generateMS(generateDeployment("foo"))
	*(ms1.Spec.Replicas) = 1
	ms1.Status.Replicas = 2
	ms2 := generateMS(generateDeployment("bar"))
	*(ms2.Spec.Replicas) = 5
	ms2.Status.Replicas = 3

	tests := []struct {
		Name           string
		Sets           []*clusterv1.MachineSet
		ExpectedCount  int32
		ExpectedActual int32
		ExpectedTotal  int32
	}{
		{
			Name:           "1:2 Replicas",
			Sets:           []*clusterv1.MachineSet{&ms1},
			ExpectedCount:  1,
			ExpectedActual: 2,
			ExpectedTotal:  2,
		},
		{
			Name:           "6:5 Replicas",
			Sets:           []*clusterv1.MachineSet{&ms1, &ms2},
			ExpectedCount:  6,
			ExpectedActual: 5,
			ExpectedTotal:  7,
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			g := NewWithT(t)

			g.Expect(GetReplicaCountForMachineSets(test.Sets)).To(Equal(test.ExpectedCount))
			g.Expect(GetActualReplicaCountForMachineSets(test.Sets)).To(Equal(test.ExpectedActual))
			g.Expect(TotalMachineSetsReplicaSum(test.Sets)).To(Equal(test.ExpectedTotal))
		})
	}
}

func TestResolveFenceposts(t *testing.T) {
	tests := []struct {
		maxSurge          string
		maxUnavailable    string
		desired           int32
		expectSurge       int32
		expectUnavailable int32
		expectError       bool
	}{
		{
			maxSurge:          "0%",
			maxUnavailable:    "0%",
			desired:           0,
			expectSurge:       0,
			expectUnavailable: 1,
			expectError:       false,
		},
		{
			maxSurge:          "39%",
			maxUnavailable:    "39%",
			desired:           10,
			expectSurge:       4,
			expectUnavailable: 3,
			expectError:       false,
		},
		{
			maxSurge:          "oops",
			maxUnavailable:    "39%",
			desired:           10,
			expectSurge:       0,
			expectUnavailable: 0,
			expectError:       true,
		},
		{
			maxSurge:          "55%",
			maxUnavailable:    "urg",
			desired:           10,
			expectSurge:       0,
			expectUnavailable: 0,
			expectError:       true,
		},
		{
			maxSurge:          "5",
			maxUnavailable:    "1",
			desired:           7,
			expectSurge:       0,
			expectUnavailable: 0,
			expectError:       true,
		},
	}

	for _, test := range tests {
		t.Run("maxSurge="+test.maxSurge, func(t *testing.T) {
			g := NewWithT(t)

			maxSurge := intstr.FromString(test.maxSurge)
			maxUnavail := intstr.FromString(test.maxUnavailable)
			surge, unavail, err := ResolveFenceposts(&maxSurge, &maxUnavail, test.desired)
			if test.expectError {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
			g.Expect(surge).To(Equal(test.expectSurge))
			g.Expect(unavail).To(Equal(test.expectUnavailable))
		})
	}
}

func TestNewMSNewReplicas(t *testing.T) {
	tests := []struct {
		Name          string
		strategyType  clusterv1.MachineDeploymentStrategyType
		depReplicas   int32
		newMSReplicas int32
		maxSurge      int
		expected      int32
	}{
		{
			"can not scale up - to newMSReplicas",
			clusterv1.RollingUpdateMachineDeploymentStrategyType,
			1, 5, 1, 5,
		},
		{
			"scale up - to depReplicas",
			clusterv1.RollingUpdateMachineDeploymentStrategyType,
			6, 2, 10, 6,
		},
	}
	newDeployment := generateDeployment("nginx")
	newRC := generateMS(newDeployment)
	rs5 := generateMS(newDeployment)
	*(rs5.Spec.Replicas) = 5

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			g := NewWithT(t)

			*(newDeployment.Spec.Replicas) = test.depReplicas
			newDeployment.Spec.Strategy = &clusterv1.MachineDeploymentStrategy{Type: test.strategyType}
			newDeployment.Spec.Strategy.RollingUpdate = &clusterv1.MachineRollingUpdateDeployment{
				MaxUnavailable: func(i int) *intstr.IntOrString {
					x := intstr.FromInt(i)
					return &x
				}(1),
				MaxSurge: func(i int) *intstr.IntOrString {
					x := intstr.FromInt(i)
					return &x
				}(test.maxSurge),
			}
			*(newRC.Spec.Replicas) = test.newMSReplicas
			ms, err := NewMSNewReplicas(&newDeployment, []*clusterv1.MachineSet{&rs5}, &newRC)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(ms).To(Equal(test.expected))
		})
	}
}

func TestDeploymentComplete(t *testing.T) {
	deployment := func(desired, current, updated, available, maxUnavailable, maxSurge int32) *clusterv1.MachineDeployment {
		return &clusterv1.MachineDeployment{
			Spec: clusterv1.MachineDeploymentSpec{
				Replicas: &desired,
				Strategy: &clusterv1.MachineDeploymentStrategy{
					RollingUpdate: &clusterv1.MachineRollingUpdateDeployment{
						MaxUnavailable: func(i int) *intstr.IntOrString { x := intstr.FromInt(i); return &x }(int(maxUnavailable)),
						MaxSurge:       func(i int) *intstr.IntOrString { x := intstr.FromInt(i); return &x }(int(maxSurge)),
					},
					Type: clusterv1.RollingUpdateMachineDeploymentStrategyType,
				},
			},
			Status: clusterv1.MachineDeploymentStatus{
				Replicas:          current,
				UpdatedReplicas:   updated,
				AvailableReplicas: available,
			},
		}
	}

	tests := []struct {
		name string

		d *clusterv1.MachineDeployment

		expected bool
	}{
		{
			name: "not complete: min but not all machines become available",

			d:        deployment(5, 5, 5, 4, 1, 0),
			expected: false,
		},
		{
			name: "not complete: min availability is not honored",

			d:        deployment(5, 5, 5, 3, 1, 0),
			expected: false,
		},
		{
			name: "complete",

			d:        deployment(5, 5, 5, 5, 0, 0),
			expected: true,
		},
		{
			name: "not complete: all machines are available but not updated",

			d:        deployment(5, 5, 4, 5, 0, 0),
			expected: false,
		},
		{
			name: "not complete: still running old machines",

			// old machine set: spec.replicas=1, status.replicas=1, status.availableReplicas=1
			// new machine set: spec.replicas=1, status.replicas=1, status.availableReplicas=0
			d:        deployment(1, 2, 1, 1, 0, 1),
			expected: false,
		},
		{
			name: "not complete: one replica deployment never comes up",

			d:        deployment(1, 1, 1, 0, 1, 1),
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)

			g.Expect(DeploymentComplete(test.d, &test.d.Status)).To(Equal(test.expected))
		})
	}
}

func TestMaxUnavailable(t *testing.T) {
	deployment := func(replicas int32, maxUnavailable intstr.IntOrString) clusterv1.MachineDeployment {
		return clusterv1.MachineDeployment{
			Spec: clusterv1.MachineDeploymentSpec{
				Replicas: func(i int32) *int32 { return &i }(replicas),
				Strategy: &clusterv1.MachineDeploymentStrategy{
					RollingUpdate: &clusterv1.MachineRollingUpdateDeployment{
						MaxSurge:       func(i int) *intstr.IntOrString { x := intstr.FromInt(i); return &x }(int(1)),
						MaxUnavailable: &maxUnavailable,
					},
					Type: clusterv1.RollingUpdateMachineDeploymentStrategyType,
				},
			},
		}
	}
	tests := []struct {
		name       string
		deployment clusterv1.MachineDeployment
		expected   int32
	}{
		{
			name:       "maxUnavailable less than replicas",
			deployment: deployment(10, intstr.FromInt(5)),
			expected:   int32(5),
		},
		{
			name:       "maxUnavailable equal replicas",
			deployment: deployment(10, intstr.FromInt(10)),
			expected:   int32(10),
		},
		{
			name:       "maxUnavailable greater than replicas",
			deployment: deployment(5, intstr.FromInt(10)),
			expected:   int32(5),
		},
		{
			name:       "maxUnavailable with replicas is 0",
			deployment: deployment(0, intstr.FromInt(10)),
			expected:   int32(0),
		},
		{
			name:       "maxUnavailable less than replicas with percents",
			deployment: deployment(10, intstr.FromString("50%")),
			expected:   int32(5),
		},
		{
			name:       "maxUnavailable equal replicas with percents",
			deployment: deployment(10, intstr.FromString("100%")),
			expected:   int32(10),
		},
		{
			name:       "maxUnavailable greater than replicas with percents",
			deployment: deployment(5, intstr.FromString("100%")),
			expected:   int32(5),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)

			g.Expect(MaxUnavailable(test.deployment)).To(Equal(test.expected))
		})
	}
}

// TestAnnotationUtils is a set of simple tests for annotation related util functions.
func TestAnnotationUtils(t *testing.T) {
	// Setup
	tDeployment := generateDeployment("nginx")
	tMS := generateMS(tDeployment)
	tDeployment.Annotations[clusterv1.RevisionAnnotation] = "999"
	logger := klogr.New()

	// Test Case 1: Check if anotations are copied properly from deployment to MS
	t.Run("SetNewMachineSetAnnotations", func(t *testing.T) {
		g := NewWithT(t)

		// Try to set the increment revision from 1 through 20
		for i := 0; i < 20; i++ {
			nextRevision := fmt.Sprintf("%d", i+1)
			SetNewMachineSetAnnotations(&tDeployment, &tMS, nextRevision, true, logger)
			// Now the MachineSets Revision Annotation should be i+1
			g.Expect(tMS.Annotations).To(HaveKeyWithValue(clusterv1.RevisionAnnotation, nextRevision))
		}
	})

	// Test Case 2:  Check if annotations are set properly
	t.Run("SetReplicasAnnotations", func(t *testing.T) {
		g := NewWithT(t)

		g.Expect(SetReplicasAnnotations(&tMS, 10, 11)).To(BeTrue())
		g.Expect(tMS.Annotations).To(HaveKeyWithValue(clusterv1.DesiredReplicasAnnotation, "10"))
		g.Expect(tMS.Annotations).To(HaveKeyWithValue(clusterv1.MaxReplicasAnnotation, "11"))
	})

	// Test Case 3:  Check if annotations reflect deployments state
	tMS.Annotations[clusterv1.DesiredReplicasAnnotation] = "1"
	tMS.Status.AvailableReplicas = 1
	tMS.Spec.Replicas = new(int32)
	*tMS.Spec.Replicas = 1

	t.Run("IsSaturated", func(t *testing.T) {
		g := NewWithT(t)

		g.Expect(IsSaturated(&tDeployment, &tMS)).To(BeTrue())
	})
}

func TestReplicasAnnotationsNeedUpdate(t *testing.T) {
	desiredReplicas := fmt.Sprintf("%d", int32(10))
	maxReplicas := fmt.Sprintf("%d", int32(20))

	tests := []struct {
		name       string
		machineSet *clusterv1.MachineSet
		expected   bool
	}{
		{
			name: "test Annotations nil",
			machineSet: &clusterv1.MachineSet{
				ObjectMeta: metav1.ObjectMeta{Name: "hello", Namespace: metav1.NamespaceDefault},
				Spec: clusterv1.MachineSetSpec{
					Selector: metav1.LabelSelector{MatchLabels: map[string]string{"foo": "bar"}},
				},
			},
			expected: true,
		},
		{
			name: "test desiredReplicas update",
			machineSet: &clusterv1.MachineSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "hello",
					Namespace:   metav1.NamespaceDefault,
					Annotations: map[string]string{clusterv1.DesiredReplicasAnnotation: "8", clusterv1.MaxReplicasAnnotation: maxReplicas},
				},
				Spec: clusterv1.MachineSetSpec{
					Selector: metav1.LabelSelector{MatchLabels: map[string]string{"foo": "bar"}},
				},
			},
			expected: true,
		},
		{
			name: "test maxReplicas update",
			machineSet: &clusterv1.MachineSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "hello",
					Namespace:   metav1.NamespaceDefault,
					Annotations: map[string]string{clusterv1.DesiredReplicasAnnotation: desiredReplicas, clusterv1.MaxReplicasAnnotation: "16"},
				},
				Spec: clusterv1.MachineSetSpec{
					Selector: metav1.LabelSelector{MatchLabels: map[string]string{"foo": "bar"}},
				},
			},
			expected: true,
		},
		{
			name: "test needn't update",
			machineSet: &clusterv1.MachineSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "hello",
					Namespace:   metav1.NamespaceDefault,
					Annotations: map[string]string{clusterv1.DesiredReplicasAnnotation: desiredReplicas, clusterv1.MaxReplicasAnnotation: maxReplicas},
				},
				Spec: clusterv1.MachineSetSpec{
					Selector: metav1.LabelSelector{MatchLabels: map[string]string{"foo": "bar"}},
				},
			},
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)

			g.Expect(ReplicasAnnotationsNeedUpdate(test.machineSet, 10, 20)).To(Equal(test.expected))
		})
	}
}
