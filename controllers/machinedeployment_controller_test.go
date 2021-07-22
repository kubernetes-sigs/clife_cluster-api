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

package controllers

import (
	"testing"

	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	"sigs.k8s.io/cluster-api/controllers/external"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/conditions"
)

const (
	machineDeploymentNamespace = "md-test"
)

var _ reconcile.Reconciler = &MachineDeploymentReconciler{}

func TestMachineDeploymentReconciler(t *testing.T) {
	setup := func(t *testing.T, g *WithT) (*corev1.Namespace, *clusterv1.Cluster) {
		t.Log("Creating the namespace")
		ns, err := env.CreateNamespace(ctx, machineDeploymentNamespace)
		g.Expect(err).To(BeNil())

		t.Log("Creating the Cluster")
		cluster := &clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Namespace: ns.Name, Name: "test-cluster"}}
		g.Expect(env.Create(ctx, cluster)).To(Succeed())

		t.Log("Creating the Cluster Kubeconfig Secret")
		g.Expect(env.CreateKubeconfigSecret(ctx, cluster)).To(Succeed())

		return ns, cluster
	}

	teardown := func(t *testing.T, g *WithT, ns *corev1.Namespace, cluster *clusterv1.Cluster) {
		t.Log("Deleting the Cluster")
		g.Expect(env.Delete(ctx, cluster)).To(Succeed())
		t.Log("Deleting the namespace")
		g.Expect(env.Delete(ctx, ns)).To(Succeed())
	}

	t.Run("Should reconcile a MachineDeployment", func(t *testing.T) {
		g := NewWithT(t)
		namespace, testCluster := setup(t, g)
		defer teardown(t, g, namespace, testCluster)

		labels := map[string]string{
			"foo":                      "bar",
			clusterv1.ClusterLabelName: testCluster.Name,
		}
		version := "v1.10.3"
		deployment := &clusterv1.MachineDeployment{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "md-",
				Namespace:    namespace.Name,
				Labels: map[string]string{
					clusterv1.ClusterLabelName: testCluster.Name,
				},
			},
			Spec: clusterv1.MachineDeploymentSpec{
				ClusterName:          testCluster.Name,
				MinReadySeconds:      pointer.Int32Ptr(0),
				Replicas:             pointer.Int32Ptr(2),
				RevisionHistoryLimit: pointer.Int32Ptr(0),
				Selector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						clusterv1.ClusterLabelName: testCluster.Name,
					},
				},
				Strategy: &clusterv1.MachineDeploymentStrategy{
					Type: clusterv1.RollingUpdateMachineDeploymentStrategyType,
					RollingUpdate: &clusterv1.MachineRollingUpdateDeployment{
						MaxUnavailable: intOrStrPtr(0),
						MaxSurge:       intOrStrPtr(1),
						DeletePolicy:   pointer.StringPtr("Oldest"),
					},
				},
				Template: clusterv1.MachineTemplateSpec{
					ObjectMeta: clusterv1.ObjectMeta{
						Labels: labels,
					},
					Spec: clusterv1.MachineSpec{
						ClusterName: testCluster.Name,
						Version:     &version,
						InfrastructureRef: corev1.ObjectReference{
							APIVersion: "infrastructure.cluster.x-k8s.io/v1alpha4",
							Kind:       "InfrastructureMachineTemplate",
							Name:       "md-template",
						},
						Bootstrap: clusterv1.Bootstrap{
							DataSecretName: pointer.StringPtr("data-secret-name"),
						},
					},
				},
			},
		}
		msListOpts := []client.ListOption{
			client.InNamespace(namespace.Name),
			client.MatchingLabels(labels),
		}

		// Create infrastructure template resource.
		infraResource := map[string]interface{}{
			"kind":       "InfrastructureMachine",
			"apiVersion": "infrastructure.cluster.x-k8s.io/v1alpha4",
			"metadata":   map[string]interface{}{},
			"spec": map[string]interface{}{
				"size": "3xlarge",
			},
		}
		infraTmpl := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"spec": map[string]interface{}{
					"template": infraResource,
				},
			},
		}
		infraTmpl.SetKind("InfrastructureMachineTemplate")
		infraTmpl.SetAPIVersion("infrastructure.cluster.x-k8s.io/v1alpha4")
		infraTmpl.SetName("md-template")
		infraTmpl.SetNamespace(namespace.Name)
		t.Log("Creating the infrastructure template")
		g.Expect(env.Create(ctx, infraTmpl)).To(Succeed())

		// Create the MachineDeployment object and expect Reconcile to be called.
		t.Log("Creating the MachineDeployment")
		g.Expect(env.Create(ctx, deployment)).To(Succeed())
		defer func() {
			t.Log("Deleting the MachineDeployment")
			g.Expect(env.Delete(ctx, deployment)).To(Succeed())
		}()

		t.Log("Verifying the MachineDeployment has a cluster label and ownerRef")
		g.Eventually(func() bool {
			key := client.ObjectKey{Name: deployment.Name, Namespace: deployment.Namespace}
			if err := env.Get(ctx, key, deployment); err != nil {
				return false
			}
			if len(deployment.Labels) == 0 || deployment.Labels[clusterv1.ClusterLabelName] != testCluster.Name {
				return false
			}
			if len(deployment.OwnerReferences) == 0 || deployment.OwnerReferences[0].Name != testCluster.Name {
				return false
			}
			return true
		}, timeout).Should(BeTrue())

		// Verify that the MachineSet was created.
		t.Log("Verifying the MachineSet was created")
		machineSets := &clusterv1.MachineSetList{}
		g.Eventually(func() int {
			if err := env.List(ctx, machineSets, msListOpts...); err != nil {
				return -1
			}
			return len(machineSets.Items)
		}, timeout).Should(BeEquivalentTo(1))

		t.Log("Verifying that the deployment's deletePolicy was propagated to the machineset")
		g.Expect(machineSets.Items[0].Spec.DeletePolicy).To(Equal("Oldest"))

		t.Log("Verifying the linked infrastructure template has a cluster owner reference")
		g.Eventually(func() bool {
			obj, err := external.Get(ctx, env, &deployment.Spec.Template.Spec.InfrastructureRef, deployment.Namespace)
			if err != nil {
				return false
			}

			return util.HasOwnerRef(obj.GetOwnerReferences(), metav1.OwnerReference{
				APIVersion: clusterv1.GroupVersion.String(),
				Kind:       "Cluster",
				Name:       testCluster.Name,
				UID:        testCluster.UID,
			})
		}, timeout).Should(BeTrue())

		// Verify that expected number of machines are created
		t.Log("Verify expected number of machines are created")
		machines := &clusterv1.MachineList{}
		g.Eventually(func() int {
			if err := env.List(ctx, machines, client.InNamespace(namespace.Name)); err != nil {
				return -1
			}
			return len(machines.Items)
		}, timeout).Should(BeEquivalentTo(*deployment.Spec.Replicas))

		// Verify that machines has MachineSetLabelName and MachineDeploymentLabelName labels
		t.Log("Verify machines have expected MachineSetLabelName and MachineDeploymentLabelName")
		for _, m := range machines.Items {
			g.Expect(m.Labels[clusterv1.ClusterLabelName]).To(Equal(testCluster.Name))
		}

		firstMachineSet := machineSets.Items[0]
		g.Expect(*firstMachineSet.Spec.Replicas).To(BeEquivalentTo(2))
		g.Expect(*firstMachineSet.Spec.Template.Spec.Version).To(BeEquivalentTo("v1.10.3"))

		//
		// Delete firstMachineSet and expect Reconcile to be called to replace it.
		//
		t.Log("Deleting the initial MachineSet")
		g.Expect(env.Delete(ctx, &firstMachineSet)).To(Succeed())
		g.Eventually(func() bool {
			if err := env.List(ctx, machineSets, msListOpts...); err != nil {
				return false
			}
			for _, ms := range machineSets.Items {
				if ms.UID == firstMachineSet.UID {
					return false
				}
			}
			return len(machineSets.Items) > 0
		}, timeout).Should(BeTrue())

		//
		// Scale the MachineDeployment and expect Reconcile to be called.
		//
		secondMachineSet := machineSets.Items[0]
		t.Log("Scaling the MachineDeployment to 3 replicas")
		modifyFunc := func(d *clusterv1.MachineDeployment) { d.Spec.Replicas = pointer.Int32Ptr(3) }
		g.Expect(updateMachineDeployment(ctx, env, deployment, modifyFunc)).To(Succeed())
		g.Eventually(func() int {
			key := client.ObjectKey{Name: secondMachineSet.Name, Namespace: secondMachineSet.Namespace}
			if err := env.Get(ctx, key, &secondMachineSet); err != nil {
				return -1
			}
			return int(*secondMachineSet.Spec.Replicas)
		}, timeout).Should(BeEquivalentTo(3))

		//
		// Update a MachineDeployment, expect Reconcile to be called and a new MachineSet to appear.
		//
		t.Log("Setting a label on the MachineDeployment")
		modifyFunc = func(d *clusterv1.MachineDeployment) { d.Spec.Template.Labels["updated"] = "true" }
		g.Expect(updateMachineDeployment(ctx, env, deployment, modifyFunc)).To(Succeed())
		g.Eventually(func() int {
			if err := env.List(ctx, machineSets, msListOpts...); err != nil {
				return -1
			}
			return len(machineSets.Items)
		}, timeout).Should(BeEquivalentTo(2))

		t.Log("Updating deletePolicy on the MachineDeployment")
		modifyFunc = func(d *clusterv1.MachineDeployment) {
			d.Spec.Strategy.RollingUpdate.DeletePolicy = pointer.StringPtr("Newest")
		}
		g.Expect(updateMachineDeployment(ctx, env, deployment, modifyFunc)).To(Succeed())
		g.Eventually(func() string {
			if err := env.List(ctx, machineSets, msListOpts...); err != nil {
				return ""
			}
			return machineSets.Items[0].Spec.DeletePolicy
		}, timeout).Should(Equal("Newest"))

		// Verify that the old machine set retains its delete policy
		g.Expect(machineSets.Items[1].Spec.DeletePolicy).To(Equal("Oldest"))

		// Verify that all the MachineSets have the expected OwnerRef.
		t.Log("Verifying MachineSet owner references")
		g.Eventually(func() bool {
			if err := env.List(ctx, machineSets, msListOpts...); err != nil {
				return false
			}
			for i := 0; i < len(machineSets.Items); i++ {
				ms := machineSets.Items[0]
				if !metav1.IsControlledBy(&ms, deployment) || metav1.GetControllerOf(&ms).Kind != "MachineDeployment" {
					return false
				}
			}
			return true
		}, timeout).Should(BeTrue())

		t.Log("Locating the newest MachineSet")
		var thirdMachineSet *clusterv1.MachineSet
		for i := range machineSets.Items {
			ms := &machineSets.Items[i]
			if ms.UID != secondMachineSet.UID {
				thirdMachineSet = ms
				break
			}
		}
		g.Expect(thirdMachineSet).NotTo(BeNil())

		t.Log("Verifying the initial MachineSet is deleted")
		g.Eventually(func() int {
			// Set the all non-deleted machines as ready with a NodeRef, so the MachineSet controller can proceed
			// to properly set AvailableReplicas.
			foundMachines := &clusterv1.MachineList{}
			g.Expect(env.List(ctx, foundMachines, client.InNamespace(namespace.Name))).To(Succeed())
			for i := 0; i < len(foundMachines.Items); i++ {
				m := foundMachines.Items[i]
				// Skip over deleted Machines
				if !m.DeletionTimestamp.IsZero() {
					continue
				}
				// Skip over Machines controlled by other (previous) MachineSets
				if !metav1.IsControlledBy(&m, thirdMachineSet) {
					continue
				}
				providerID := fakeInfrastructureRefReady(m.Spec.InfrastructureRef, infraResource, g)
				fakeMachineNodeRef(&m, providerID, g)
			}

			if err := env.List(ctx, machineSets, msListOpts...); err != nil {
				return -1
			}
			return len(machineSets.Items)
		}, timeout*3).Should(BeEquivalentTo(1))

		//
		// Update a MachineDeployment spec.Selector.Matchlabels spec.Template.Labels
		// expect Reconcile to be called and a new MachineSet to appear
		// expect old MachineSets with old labels to be deleted
		//
		oldLabels := deployment.Spec.Selector.MatchLabels
		oldLabels[clusterv1.MachineDeploymentLabelName] = deployment.Name

		newLabels := map[string]string{
			"new-key":                  "new-value",
			clusterv1.ClusterLabelName: testCluster.Name,
		}

		t.Log("Updating MachineDeployment label")
		modifyFunc = func(d *clusterv1.MachineDeployment) {
			d.Spec.Selector.MatchLabels = newLabels
			d.Spec.Template.Labels = newLabels
		}
		g.Expect(updateMachineDeployment(ctx, env, deployment, modifyFunc)).To(Succeed())

		t.Log("Verifying if a new MachineSet with updated labels are created")
		g.Eventually(func() int {
			listOpts := client.MatchingLabels(newLabels)
			if err := env.List(ctx, machineSets, listOpts); err != nil {
				return -1
			}
			return len(machineSets.Items)
		}, timeout).Should(BeEquivalentTo(1))
		newms := machineSets.Items[0]

		t.Log("Verifying new MachineSet has desired number of replicas")
		g.Eventually(func() bool {
			// Set the all non-deleted machines as ready with a NodeRef, so the MachineSet controller can proceed
			// to properly set AvailableReplicas.
			foundMachines := &clusterv1.MachineList{}
			g.Expect(env.List(ctx, foundMachines, client.InNamespace(namespace.Name))).To(Succeed())
			for i := 0; i < len(foundMachines.Items); i++ {
				m := foundMachines.Items[i]
				if !m.DeletionTimestamp.IsZero() {
					continue
				}
				// Skip over Machines controlled by other (previous) MachineSets
				if !metav1.IsControlledBy(&m, &newms) {
					continue
				}
				providerID := fakeInfrastructureRefReady(m.Spec.InfrastructureRef, infraResource, g)
				fakeMachineNodeRef(&m, providerID, g)
			}

			listOpts := client.MatchingLabels(newLabels)
			if err := env.List(ctx, machineSets, listOpts); err != nil {
				return false
			}
			return machineSets.Items[0].Status.Replicas == *deployment.Spec.Replicas
		}, timeout*5).Should(BeTrue())

		t.Log("Verifying MachineSets with old labels are deleted")
		g.Eventually(func() int {
			listOpts := client.MatchingLabels(oldLabels)
			if err := env.List(ctx, machineSets, listOpts); err != nil {
				return -1
			}

			return len(machineSets.Items)
		}, timeout*5).Should(BeEquivalentTo(0))

		t.Log("Verifying MachineDeployment has correct Conditions")
		g.Eventually(func() bool {
			key := client.ObjectKey{Name: deployment.Name, Namespace: deployment.Namespace}
			g.Expect(env.Get(ctx, key, deployment)).To(Succeed())
			return conditions.IsTrue(deployment, clusterv1.MachineDeploymentAvailableCondition)
		}, timeout).Should(BeTrue())

		// Validate that the controller set the cluster name label in selector.
		g.Expect(deployment.Status.Selector).To(ContainSubstring(testCluster.Name))
	})
}

func TestMachineSetToDeployments(t *testing.T) {
	g := NewWithT(t)

	machineDeployment := &clusterv1.MachineDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "withMatchingLabels",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: clusterv1.MachineDeploymentSpec{
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"foo":                      "bar",
					clusterv1.ClusterLabelName: "test-cluster",
				},
			},
		},
	}

	machineDeplopymentList := []client.Object{machineDeployment}

	ms1 := clusterv1.MachineSet{
		TypeMeta: metav1.TypeMeta{
			Kind: "MachineSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "withOwnerRef",
			Namespace: metav1.NamespaceDefault,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(machineDeployment, machineDeploymentKind),
			},
			Labels: map[string]string{
				clusterv1.ClusterLabelName: "test-cluster",
			},
		},
	}
	ms2 := clusterv1.MachineSet{
		TypeMeta: metav1.TypeMeta{
			Kind: "MachineSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "noOwnerRefNoLabels",
			Namespace: metav1.NamespaceDefault,
			Labels: map[string]string{
				clusterv1.ClusterLabelName: "test-cluster",
			},
		},
	}
	ms3 := clusterv1.MachineSet{
		TypeMeta: metav1.TypeMeta{
			Kind: "MachineSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "withMatchingLabels",
			Namespace: metav1.NamespaceDefault,
			Labels: map[string]string{
				"foo":                      "bar",
				clusterv1.ClusterLabelName: "test-cluster",
			},
		},
	}

	testsCases := []struct {
		machineSet clusterv1.MachineSet
		mapObject  client.Object
		expected   []reconcile.Request
	}{
		{
			machineSet: ms1,
			mapObject:  &ms1,
			expected:   []reconcile.Request{},
		},
		{
			machineSet: ms2,
			mapObject:  &ms2,
			expected:   nil,
		},
		{
			machineSet: ms3,
			mapObject:  &ms3,
			expected: []reconcile.Request{
				{NamespacedName: client.ObjectKey{Namespace: metav1.NamespaceDefault, Name: "withMatchingLabels"}},
			},
		},
	}

	r := &MachineDeploymentReconciler{
		Client:   fake.NewClientBuilder().WithObjects(machineDeplopymentList...).Build(),
		recorder: record.NewFakeRecorder(32),
	}

	for _, tc := range testsCases {
		got := r.MachineSetToDeployments(tc.mapObject)
		g.Expect(got).To(Equal(tc.expected))
	}
}

func TestGetMachineDeploymentsForMachineSet(t *testing.T) {
	g := NewWithT(t)

	machineDeployment := &clusterv1.MachineDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "withLabels",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: clusterv1.MachineDeploymentSpec{
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"foo": "bar",
				},
			},
		},
	}
	machineDeploymentList := []client.Object{machineDeployment}

	ms1 := clusterv1.MachineSet{
		TypeMeta: metav1.TypeMeta{
			Kind: "MachineSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "NoMatchingLabels",
			Namespace: metav1.NamespaceDefault,
		},
	}
	ms2 := clusterv1.MachineSet{
		TypeMeta: metav1.TypeMeta{
			Kind: "MachineSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "withMatchingLabels",
			Namespace: metav1.NamespaceDefault,
			Labels: map[string]string{
				"foo": "bar",
			},
		},
	}

	testCases := []struct {
		machineSet clusterv1.MachineSet
		expected   []client.Object
	}{
		{
			machineSet: ms1,
			expected:   nil,
		},
		{
			machineSet: ms2,
			expected:   []client.Object{machineDeployment},
		},
	}

	r := &MachineDeploymentReconciler{
		Client:   fake.NewClientBuilder().WithObjects(append(machineDeploymentList, &ms1, &ms2)...).Build(),
		recorder: record.NewFakeRecorder(32),
	}

	for _, tc := range testCases {
		var got []client.Object
		for _, x := range r.getMachineDeploymentsForMachineSet(ctx, &tc.machineSet) {
			got = append(got, x)
		}
		g.Expect(got).To(Equal(tc.expected))
	}
}

func TestGetMachineSetsForDeployment(t *testing.T) {
	machineDeployment1 := clusterv1.MachineDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "withMatchingOwnerRefAndLabels",
			Namespace: metav1.NamespaceDefault,
			UID:       "UID",
		},
		Spec: clusterv1.MachineDeploymentSpec{
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"foo": "bar",
				},
			},
		},
	}
	machineDeployment2 := clusterv1.MachineDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "withNoMatchingOwnerRef",
			Namespace: metav1.NamespaceDefault,
			UID:       "unMatchingUID",
		},
		Spec: clusterv1.MachineDeploymentSpec{
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"foo": "bar2",
				},
			},
		},
	}
	machineDeployment3 := clusterv1.MachineDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "withMatchingOwnerRefAndNoMatchingLabels",
			Namespace: metav1.NamespaceDefault,
			UID:       "UID3",
		},
		Spec: clusterv1.MachineDeploymentSpec{
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"foo": "bar",
				},
			},
		},
	}

	ms1 := clusterv1.MachineSet{
		TypeMeta: metav1.TypeMeta{
			Kind: "MachineSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "withNoOwnerRefShouldBeAdopted2",
			Namespace: metav1.NamespaceDefault,
			Labels: map[string]string{
				"foo": "bar2",
			},
		},
	}
	ms2 := clusterv1.MachineSet{
		TypeMeta: metav1.TypeMeta{
			Kind: "MachineSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "withOwnerRefAndLabels",
			Namespace: metav1.NamespaceDefault,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(&machineDeployment1, machineDeploymentKind),
			},
			Labels: map[string]string{
				"foo": "bar",
			},
		},
	}
	ms3 := clusterv1.MachineSet{
		TypeMeta: metav1.TypeMeta{
			Kind: "MachineSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "withNoOwnerRefShouldBeAdopted1",
			Namespace: metav1.NamespaceDefault,
			Labels: map[string]string{
				"foo": "bar",
			},
		},
	}
	ms4 := clusterv1.MachineSet{
		TypeMeta: metav1.TypeMeta{
			Kind: "MachineSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "withNoOwnerRefNoMatch",
			Namespace: metav1.NamespaceDefault,
			Labels: map[string]string{
				"foo": "nomatch",
			},
		},
	}
	ms5 := clusterv1.MachineSet{
		TypeMeta: metav1.TypeMeta{
			Kind: "MachineSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "withOwnerRefAndNoMatchLabels",
			Namespace: metav1.NamespaceDefault,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(&machineDeployment3, machineDeploymentKind),
			},
			Labels: map[string]string{
				"foo": "nomatch",
			},
		},
	}
	machineSetList := []client.Object{
		&ms1,
		&ms2,
		&ms3,
		&ms4,
		&ms5,
	}

	testCases := []struct {
		name              string
		machineDeployment clusterv1.MachineDeployment
		expected          []*clusterv1.MachineSet
	}{
		{
			name:              "matching ownerRef and labels",
			machineDeployment: machineDeployment1,
			expected:          []*clusterv1.MachineSet{&ms3, &ms2},
		},
		{
			name:              "no matching ownerRef, matching labels",
			machineDeployment: machineDeployment2,
			expected:          []*clusterv1.MachineSet{&ms1},
		},
		{
			name:              "matching ownerRef, mismatch labels",
			machineDeployment: machineDeployment3,
			expected:          []*clusterv1.MachineSet{&ms3, &ms5},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			r := &MachineDeploymentReconciler{
				Client:   fake.NewClientBuilder().WithObjects(machineSetList...).Build(),
				recorder: record.NewFakeRecorder(32),
			}

			got, err := r.getMachineSetsForDeployment(ctx, &tc.machineDeployment)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(got).To(HaveLen(len(tc.expected)))

			for idx, res := range got {
				g.Expect(res.Name).To(Equal(tc.expected[idx].Name))
				g.Expect(res.Namespace).To(Equal(tc.expected[idx].Namespace))
			}
		})
	}
}
