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
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/pointer"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	expv1 "sigs.k8s.io/cluster-api/exp/api/v1alpha4"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/conditions"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestMachinePoolFinalizer(t *testing.T) {
	bootstrapData := "some valid machinepool bootstrap data"
	clusterCorrectMeta := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: metav1.NamespaceDefault,
			Name:      "valid-cluster",
		},
	}

	machinePoolValidCluster := &expv1.MachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machinePool1",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: expv1.MachinePoolSpec{
			Replicas: pointer.Int32Ptr(1),
			Template: clusterv1.MachineTemplateSpec{
				Spec: clusterv1.MachineSpec{
					Bootstrap: clusterv1.Bootstrap{
						DataSecretName: &bootstrapData,
					},
				},
			},
			ClusterName: "valid-cluster",
		},
	}

	machinePoolWithFinalizer := &expv1.MachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "machinePool2",
			Namespace:  metav1.NamespaceDefault,
			Finalizers: []string{"some-other-finalizer"},
		},
		Spec: expv1.MachinePoolSpec{
			Replicas: pointer.Int32Ptr(1),
			Template: clusterv1.MachineTemplateSpec{
				Spec: clusterv1.MachineSpec{
					Bootstrap: clusterv1.Bootstrap{
						DataSecretName: &bootstrapData,
					},
				},
			},
			ClusterName: "valid-cluster",
		},
	}

	testCases := []struct {
		name               string
		request            reconcile.Request
		m                  *expv1.MachinePool
		expectedFinalizers []string
	}{
		{
			name: "should add a machinePool finalizer to the machinePool if it doesn't have one",
			request: reconcile.Request{
				NamespacedName: util.ObjectKey(machinePoolValidCluster),
			},
			m:                  machinePoolValidCluster,
			expectedFinalizers: []string{expv1.MachinePoolFinalizer},
		},
		{
			name: "should append the machinePool finalizer to the machinePool if it already has a finalizer",
			request: reconcile.Request{
				NamespacedName: util.ObjectKey(machinePoolWithFinalizer),
			},
			m:                  machinePoolWithFinalizer,
			expectedFinalizers: []string{"some-other-finalizer", expv1.MachinePoolFinalizer},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			mr := &MachinePoolReconciler{
				Client: fake.NewClientBuilder().WithObjects(
					clusterCorrectMeta,
					machinePoolValidCluster,
					machinePoolWithFinalizer,
				).Build(),
			}

			_, _ = mr.Reconcile(ctx, tc.request)

			key := client.ObjectKey{Namespace: tc.m.Namespace, Name: tc.m.Name}
			var actual expv1.MachinePool
			if len(tc.expectedFinalizers) > 0 {
				g.Expect(mr.Client.Get(ctx, key, &actual)).To(Succeed())
				g.Expect(actual.Finalizers).ToNot(BeEmpty())
				g.Expect(actual.Finalizers).To(Equal(tc.expectedFinalizers))
			} else {
				g.Expect(actual.Finalizers).To(BeEmpty())
			}
		})
	}
}

func TestMachinePoolOwnerReference(t *testing.T) {
	bootstrapData := "some valid machinepool bootstrap data"
	testCluster := &clusterv1.Cluster{
		TypeMeta:   metav1.TypeMeta{Kind: "Cluster", APIVersion: clusterv1.GroupVersion.String()},
		ObjectMeta: metav1.ObjectMeta{Namespace: metav1.NamespaceDefault, Name: "test-cluster"},
	}

	machinePoolInvalidCluster := &expv1.MachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machinePool1",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: expv1.MachinePoolSpec{
			Replicas:    pointer.Int32Ptr(1),
			ClusterName: "invalid",
		},
	}

	machinePoolValidCluster := &expv1.MachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machinePool2",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: expv1.MachinePoolSpec{
			Replicas: pointer.Int32Ptr(1),
			Template: clusterv1.MachineTemplateSpec{
				Spec: clusterv1.MachineSpec{
					Bootstrap: clusterv1.Bootstrap{
						DataSecretName: &bootstrapData,
					},
				},
			},
			ClusterName: "test-cluster",
		},
	}

	machinePoolValidMachinePool := &expv1.MachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machinePool3",
			Namespace: metav1.NamespaceDefault,
			Labels: map[string]string{
				clusterv1.ClusterLabelName: "valid-cluster",
			},
		},
		Spec: expv1.MachinePoolSpec{
			Replicas: pointer.Int32Ptr(1),
			Template: clusterv1.MachineTemplateSpec{
				Spec: clusterv1.MachineSpec{
					Bootstrap: clusterv1.Bootstrap{
						DataSecretName: &bootstrapData,
					},
				},
			},
			ClusterName: "test-cluster",
		},
	}

	testCases := []struct {
		name       string
		request    reconcile.Request
		m          *expv1.MachinePool
		expectedOR []metav1.OwnerReference
	}{
		{
			name: "should add owner reference to machinePool referencing a cluster with correct type meta",
			request: reconcile.Request{
				NamespacedName: util.ObjectKey(machinePoolValidCluster),
			},
			m: machinePoolValidCluster,
			expectedOR: []metav1.OwnerReference{
				{
					APIVersion: testCluster.APIVersion,
					Kind:       testCluster.Kind,
					Name:       testCluster.Name,
					UID:        testCluster.UID,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			mr := &MachinePoolReconciler{
				Client: fake.NewClientBuilder().WithObjects(
					testCluster,
					machinePoolInvalidCluster,
					machinePoolValidCluster,
					machinePoolValidMachinePool,
				).Build(),
			}

			key := client.ObjectKey{Namespace: tc.m.Namespace, Name: tc.m.Name}
			var actual expv1.MachinePool

			// this first requeue is to add finalizer
			result, err := mr.Reconcile(ctx, tc.request)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(result).To(Equal(ctrl.Result{}))
			g.Expect(mr.Client.Get(ctx, key, &actual)).To(Succeed())
			g.Expect(actual.Finalizers).To(ContainElement(expv1.MachinePoolFinalizer))

			_, _ = mr.Reconcile(ctx, tc.request)

			if len(tc.expectedOR) > 0 {
				g.Expect(mr.Client.Get(ctx, key, &actual)).To(Succeed())
				g.Expect(actual.OwnerReferences).To(Equal(tc.expectedOR))
			} else {
				g.Expect(actual.OwnerReferences).To(BeEmpty())
			}
		})
	}
}

func TestReconcileMachinePoolRequest(t *testing.T) {
	infraConfig := unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "InfrastructureConfig",
			"apiVersion": "infrastructure.cluster.x-k8s.io/v1alpha4",
			"metadata": map[string]interface{}{
				"name":      "infra-config1",
				"namespace": metav1.NamespaceDefault,
			},
			"spec": map[string]interface{}{
				"providerIDList": []interface{}{
					"test://id-1",
				},
			},
			"status": map[string]interface{}{
				"ready": true,
				"addresses": []interface{}{
					map[string]interface{}{
						"type":    "InternalIP",
						"address": "10.0.0.1",
					},
				},
			},
		},
	}

	time := metav1.Now()

	testCluster := clusterv1.Cluster{
		TypeMeta:   metav1.TypeMeta{Kind: "Cluster", APIVersion: clusterv1.GroupVersion.String()},
		ObjectMeta: metav1.ObjectMeta{Namespace: metav1.NamespaceDefault, Name: "test-cluster"},
	}

	bootstrapConfig := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "BootstrapConfig",
			"apiVersion": "bootstrap.cluster.x-k8s.io/v1alpha4",
			"metadata": map[string]interface{}{
				"name":      "test-bootstrap",
				"namespace": metav1.NamespaceDefault,
			},
		},
	}

	type expected struct {
		result reconcile.Result
		err    bool
	}
	testCases := []struct {
		machinePool expv1.MachinePool
		expected    expected
	}{
		{
			machinePool: expv1.MachinePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "created",
					Namespace:  metav1.NamespaceDefault,
					Finalizers: []string{expv1.MachinePoolFinalizer},
				},
				Spec: expv1.MachinePoolSpec{
					ClusterName:    "test-cluster",
					ProviderIDList: []string{"test://id-1"},
					Replicas:       pointer.Int32Ptr(1),
					Template: clusterv1.MachineTemplateSpec{
						Spec: clusterv1.MachineSpec{

							InfrastructureRef: corev1.ObjectReference{
								APIVersion: "infrastructure.cluster.x-k8s.io/v1alpha4",
								Kind:       "InfrastructureConfig",
								Name:       "infra-config1",
							},
							Bootstrap: clusterv1.Bootstrap{DataSecretName: pointer.StringPtr("data")},
						},
					},
				},
				Status: expv1.MachinePoolStatus{
					Replicas:      1,
					ReadyReplicas: 1,
					NodeRefs: []corev1.ObjectReference{
						{Name: "test"},
					},
					ObservedGeneration: 1,
				},
			},
			expected: expected{
				result: reconcile.Result{},
				err:    false,
			},
		},
		{
			machinePool: expv1.MachinePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "updated",
					Namespace:  metav1.NamespaceDefault,
					Finalizers: []string{expv1.MachinePoolFinalizer},
				},
				Spec: expv1.MachinePoolSpec{
					ClusterName:    "test-cluster",
					ProviderIDList: []string{"test://id-1"},
					Replicas:       pointer.Int32Ptr(1),
					Template: clusterv1.MachineTemplateSpec{
						Spec: clusterv1.MachineSpec{
							InfrastructureRef: corev1.ObjectReference{
								APIVersion: "infrastructure.cluster.x-k8s.io/v1alpha4",
								Kind:       "InfrastructureConfig",
								Name:       "infra-config1",
							},
							Bootstrap: clusterv1.Bootstrap{DataSecretName: pointer.StringPtr("data")},
						},
					},
				},
				Status: expv1.MachinePoolStatus{
					Replicas:      1,
					ReadyReplicas: 1,
					NodeRefs: []corev1.ObjectReference{
						{Name: "test"},
					},
					ObservedGeneration: 1,
				},
			},
			expected: expected{
				result: reconcile.Result{},
				err:    false,
			},
		},
		{
			machinePool: expv1.MachinePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "deleted",
					Namespace: metav1.NamespaceDefault,
					Labels: map[string]string{
						clusterv1.MachineControlPlaneLabelName: "",
					},
					Finalizers:        []string{expv1.MachinePoolFinalizer},
					DeletionTimestamp: &time,
				},
				Spec: expv1.MachinePoolSpec{
					ClusterName: "test-cluster",
					Replicas:    pointer.Int32Ptr(1),
					Template: clusterv1.MachineTemplateSpec{
						Spec: clusterv1.MachineSpec{
							InfrastructureRef: corev1.ObjectReference{
								APIVersion: "infrastructure.cluster.x-k8s.io/v1alpha4",
								Kind:       "InfrastructureConfig",
								Name:       "infra-config1",
							},
							Bootstrap: clusterv1.Bootstrap{DataSecretName: pointer.StringPtr("data")},
						},
					},
				},
			},
			expected: expected{
				result: reconcile.Result{},
				err:    false,
			},
		},
	}

	for _, tc := range testCases {
		t.Run("machinePool should be "+tc.machinePool.Name, func(t *testing.T) {
			g := NewWithT(t)

			clientFake := fake.NewClientBuilder().WithObjects(
				&testCluster,
				&tc.machinePool,
				&infraConfig,
				bootstrapConfig,
			).Build()

			r := &MachinePoolReconciler{
				Client: clientFake,
			}

			result, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: util.ObjectKey(&tc.machinePool)})
			if tc.expected.err {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}

			g.Expect(result).To(Equal(tc.expected.result))
		})
	}
}

func TestReconcileMachinePoolDeleteExternal(t *testing.T) {
	testCluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Namespace: metav1.NamespaceDefault, Name: "test-cluster"},
	}

	bootstrapConfig := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "BootstrapConfig",
			"apiVersion": "bootstrap.cluster.x-k8s.io/v1alpha4",
			"metadata": map[string]interface{}{
				"name":      "delete-bootstrap",
				"namespace": metav1.NamespaceDefault,
			},
		},
	}

	infraConfig := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "InfrastructureConfig",
			"apiVersion": "infrastructure.cluster.x-k8s.io/v1alpha4",
			"metadata": map[string]interface{}{
				"name":      "delete-infra",
				"namespace": metav1.NamespaceDefault,
			},
		},
	}

	machinePool := &expv1.MachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "delete",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: expv1.MachinePoolSpec{
			ClusterName: "test-cluster",
			Replicas:    pointer.Int32Ptr(1),
			Template: clusterv1.MachineTemplateSpec{
				Spec: clusterv1.MachineSpec{
					InfrastructureRef: corev1.ObjectReference{
						APIVersion: "infrastructure.cluster.x-k8s.io/v1alpha4",
						Kind:       "InfrastructureConfig",
						Name:       "delete-infra",
					},
					Bootstrap: clusterv1.Bootstrap{
						ConfigRef: &corev1.ObjectReference{
							APIVersion: "bootstrap.cluster.x-k8s.io/v1alpha4",
							Kind:       "BootstrapConfig",
							Name:       "delete-bootstrap",
						},
					},
				},
			},
		},
	}

	testCases := []struct {
		name            string
		bootstrapExists bool
		infraExists     bool
		expected        bool
		expectError     bool
	}{
		{
			name:            "should continue to reconcile delete of external refs since both refs exists",
			bootstrapExists: true,
			infraExists:     true,
			expected:        false,
			expectError:     false,
		},
		{
			name:            "should continue to reconcile delete of external refs since infra ref exist",
			bootstrapExists: false,
			infraExists:     true,
			expected:        false,
			expectError:     false,
		},
		{
			name:            "should continue to reconcile delete of external refs since bootstrap ref exist",
			bootstrapExists: true,
			infraExists:     false,
			expected:        false,
			expectError:     false,
		},
		{
			name:            "should no longer reconcile deletion of external refs since both don't exist",
			bootstrapExists: false,
			infraExists:     false,
			expected:        true,
			expectError:     false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			objs := []client.Object{testCluster, machinePool}

			if tc.bootstrapExists {
				objs = append(objs, bootstrapConfig)
			}

			if tc.infraExists {
				objs = append(objs, infraConfig)
			}

			r := &MachinePoolReconciler{
				Client: fake.NewClientBuilder().WithObjects(objs...).Build(),
			}

			ok, err := r.reconcileDeleteExternal(ctx, machinePool)
			g.Expect(ok).To(Equal(tc.expected))
			if tc.expectError {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}

func TestRemoveMachinePoolFinalizerAfterDeleteReconcile(t *testing.T) {
	g := NewWithT(t)

	dt := metav1.Now()

	testCluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Namespace: metav1.NamespaceDefault, Name: "test-cluster"},
	}

	m := &expv1.MachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "delete123",
			Namespace:         metav1.NamespaceDefault,
			Finalizers:        []string{expv1.MachinePoolFinalizer, "test"},
			DeletionTimestamp: &dt,
		},
		Spec: expv1.MachinePoolSpec{
			ClusterName: "test-cluster",
			Replicas:    pointer.Int32Ptr(1),
			Template: clusterv1.MachineTemplateSpec{
				Spec: clusterv1.MachineSpec{
					InfrastructureRef: corev1.ObjectReference{
						APIVersion: "infrastructure.cluster.x-k8s.io/v1alpha4",
						Kind:       "InfrastructureConfig",
						Name:       "infra-config1",
					},
					Bootstrap: clusterv1.Bootstrap{DataSecretName: pointer.StringPtr("data")},
				},
			},
		},
	}
	key := client.ObjectKey{Namespace: m.Namespace, Name: m.Name}
	mr := &MachinePoolReconciler{
		Client: fake.NewClientBuilder().WithObjects(testCluster, m).Build(),
	}
	_, err := mr.Reconcile(ctx, reconcile.Request{NamespacedName: key})
	g.Expect(err).ToNot(HaveOccurred())

	var actual expv1.MachinePool
	g.Expect(mr.Client.Get(ctx, key, &actual)).To(Succeed())
	g.Expect(actual.ObjectMeta.Finalizers).To(Equal([]string{"test"}))
}

func TestMachinePoolConditions(t *testing.T) {
	testCluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Namespace: metav1.NamespaceDefault, Name: "test-cluster"},
	}

	bootstrapConfig := func(ready bool) *unstructured.Unstructured {
		return &unstructured.Unstructured{
			Object: map[string]interface{}{
				"kind":       "BootstrapConfig",
				"apiVersion": "bootstrap.cluster.x-k8s.io/v1alpha4",
				"metadata": map[string]interface{}{
					"name":      "bootstrap1",
					"namespace": metav1.NamespaceDefault,
				},
				"status": map[string]interface{}{
					"ready":          ready,
					"dataSecretName": "data",
				},
			},
		}
	}

	infraConfig := func(ready bool) *unstructured.Unstructured {
		return &unstructured.Unstructured{
			Object: map[string]interface{}{
				"kind":       "InfrastructureConfig",
				"apiVersion": "infrastructure.cluster.x-k8s.io/v1alpha4",
				"metadata": map[string]interface{}{
					"name":      "infra1",
					"namespace": metav1.NamespaceDefault,
				},
				"status": map[string]interface{}{
					"ready": ready,
				},
				"spec": map[string]interface{}{
					"providerIDList": []interface{}{
						"azure://westus2/id-node-4",
						"aws://us-east-1/id-node-1",
					},
				},
			},
		}
	}

	machinePool := &expv1.MachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "blah",
			Namespace:  metav1.NamespaceDefault,
			Finalizers: []string{expv1.MachinePoolFinalizer},
		},
		Spec: expv1.MachinePoolSpec{
			ClusterName: "test-cluster",
			Replicas:    pointer.Int32Ptr(2),
			Template: clusterv1.MachineTemplateSpec{
				Spec: clusterv1.MachineSpec{
					InfrastructureRef: corev1.ObjectReference{
						APIVersion: "infrastructure.cluster.x-k8s.io/v1alpha4",
						Kind:       "InfrastructureConfig",
						Name:       "infra1",
					},
					Bootstrap: clusterv1.Bootstrap{
						ConfigRef: &corev1.ObjectReference{
							APIVersion: "bootstrap.cluster.x-k8s.io/v1alpha4",
							Kind:       "BootstrapConfig",
							Name:       "bootstrap1",
						},
					},
				},
			},
		},
	}

	nodeList := corev1.NodeList{
		Items: []corev1.Node{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node-1",
				},
				Spec: corev1.NodeSpec{
					ProviderID: "aws://us-east-1/id-node-1",
				},
				Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady}}},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "azure-node-4",
				},
				Spec: corev1.NodeSpec{
					ProviderID: "azure://westus2/id-node-4",
				},
				Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady}}},
			},
		},
	}

	testcases := []struct {
		name                string
		bootstrapReady      bool
		infrastructureReady bool
		expectError         bool
		beforeFunc          func(bootstrap, infra *unstructured.Unstructured, mp *expv1.MachinePool, nodeList *corev1.NodeList)
		conditionAssertFunc func(t *testing.T, getter conditions.Getter)
	}{
		{
			name:                "all conditions true",
			bootstrapReady:      true,
			infrastructureReady: true,
			beforeFunc: func(bootstrap, infra *unstructured.Unstructured, mp *expv1.MachinePool, nodeList *corev1.NodeList) {
				mp.Spec.ProviderIDList = []string{"azure://westus2/id-node-4", "aws://us-east-1/id-node-1"}
				mp.Status = expv1.MachinePoolStatus{
					NodeRefs: []corev1.ObjectReference{
						{Name: "node-1"},
						{Name: "azure-node-4"},
					},
					Replicas:      2,
					ReadyReplicas: 2,
				}
			},
			conditionAssertFunc: func(t *testing.T, getter conditions.Getter) {
				g := NewWithT(t)
				g.Expect(getter.GetConditions()).NotTo(HaveLen(0))
				for _, c := range getter.GetConditions() {
					g.Expect(c.Status).To(Equal(corev1.ConditionTrue))
				}
			},
		},
		{
			name:                "boostrap not ready",
			bootstrapReady:      false,
			infrastructureReady: true,
			beforeFunc: func(bootstrap, infra *unstructured.Unstructured, mp *expv1.MachinePool, nodeList *corev1.NodeList) {
				addConditionsToExternal(bootstrap, clusterv1.Conditions{
					{
						Type:     clusterv1.ReadyCondition,
						Status:   corev1.ConditionFalse,
						Severity: clusterv1.ConditionSeverityInfo,
						Reason:   "Custom reason",
					},
				})
			},
			conditionAssertFunc: func(t *testing.T, getter conditions.Getter) {
				g := NewWithT(t)

				g.Expect(conditions.Has(getter, clusterv1.BootstrapReadyCondition)).To(BeTrue())
				infraReadyCondition := conditions.Get(getter, clusterv1.BootstrapReadyCondition)
				g.Expect(infraReadyCondition.Status).To(Equal(corev1.ConditionFalse))
				g.Expect(infraReadyCondition.Reason).To(Equal("Custom reason"))
			},
		},
		{
			name:                "bootstrap not ready with fallback condition",
			bootstrapReady:      false,
			infrastructureReady: true,
			conditionAssertFunc: func(t *testing.T, getter conditions.Getter) {
				g := NewWithT(t)

				g.Expect(conditions.Has(getter, clusterv1.BootstrapReadyCondition)).To(BeTrue())
				bootstrapReadyCondition := conditions.Get(getter, clusterv1.BootstrapReadyCondition)
				g.Expect(bootstrapReadyCondition.Status).To(Equal(corev1.ConditionFalse))

				g.Expect(conditions.Has(getter, clusterv1.ReadyCondition)).To(BeTrue())
				readyCondition := conditions.Get(getter, clusterv1.ReadyCondition)
				g.Expect(readyCondition.Status).To(Equal(corev1.ConditionFalse))
			},
		},
		{
			name:                "infrastructure not ready",
			bootstrapReady:      true,
			infrastructureReady: false,
			beforeFunc: func(bootstrap, infra *unstructured.Unstructured, mp *expv1.MachinePool, nodeList *corev1.NodeList) {
				addConditionsToExternal(infra, clusterv1.Conditions{
					{
						Type:     clusterv1.ReadyCondition,
						Status:   corev1.ConditionFalse,
						Severity: clusterv1.ConditionSeverityInfo,
						Reason:   "Custom reason",
					},
				})
			},
			conditionAssertFunc: func(t *testing.T, getter conditions.Getter) {
				g := NewWithT(t)

				g.Expect(conditions.Has(getter, clusterv1.InfrastructureReadyCondition)).To(BeTrue())
				infraReadyCondition := conditions.Get(getter, clusterv1.InfrastructureReadyCondition)
				g.Expect(infraReadyCondition.Status).To(Equal(corev1.ConditionFalse))
				g.Expect(infraReadyCondition.Reason).To(Equal("Custom reason"))
			},
		},
		{
			name:                "infrastructure not ready with fallback condition",
			bootstrapReady:      true,
			infrastructureReady: false,
			conditionAssertFunc: func(t *testing.T, getter conditions.Getter) {
				g := NewWithT(t)

				g.Expect(conditions.Has(getter, clusterv1.InfrastructureReadyCondition)).To(BeTrue())
				infraReadyCondition := conditions.Get(getter, clusterv1.InfrastructureReadyCondition)
				g.Expect(infraReadyCondition.Status).To(Equal(corev1.ConditionFalse))

				g.Expect(conditions.Has(getter, clusterv1.ReadyCondition)).To(BeTrue())
				readyCondition := conditions.Get(getter, clusterv1.ReadyCondition)
				g.Expect(readyCondition.Status).To(Equal(corev1.ConditionFalse))
			},
		},
		{
			name:           "incorrect infrastructure reference",
			bootstrapReady: true,
			expectError:    true,
			beforeFunc: func(bootstrap, infra *unstructured.Unstructured, mp *expv1.MachinePool, nodeList *corev1.NodeList) {
				mp.Spec.Template.Spec.InfrastructureRef = corev1.ObjectReference{
					APIVersion: "infrastructure.cluster.x-k8s.io/v1alpha4",
					Kind:       "InfrastructureConfig",
					Name:       "does-not-exist",
				}
			},
			conditionAssertFunc: func(t *testing.T, getter conditions.Getter) {
				g := NewWithT(t)

				g.Expect(conditions.Has(getter, clusterv1.InfrastructureReadyCondition)).To(BeTrue())
				infraReadyCondition := conditions.Get(getter, clusterv1.InfrastructureReadyCondition)
				g.Expect(infraReadyCondition.Status).To(Equal(corev1.ConditionFalse))
			},
		},
	}

	for _, tt := range testcases {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			// setup objects
			bootstrap := bootstrapConfig(tt.bootstrapReady)
			infra := infraConfig(tt.infrastructureReady)
			mp := machinePool.DeepCopy()
			nodes := nodeList.DeepCopy()
			if tt.beforeFunc != nil {
				tt.beforeFunc(bootstrap, infra, mp, nodes)
			}

			g.Expect(clusterv1.AddToScheme(scheme.Scheme)).To(Succeed())

			clientFake := fake.NewClientBuilder().WithObjects(
				testCluster,
				mp,
				infra,
				bootstrap,
				&nodes.Items[0],
				&nodes.Items[1],
			).Build()

			r := &MachinePoolReconciler{
				Client: clientFake,
			}

			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: util.ObjectKey(machinePool)})
			if !tt.expectError {
				g.Expect(err).NotTo(HaveOccurred())
			}

			m := &expv1.MachinePool{}
			machinePoolKey := client.ObjectKeyFromObject(machinePool)
			g.Expect(r.Client.Get(ctx, machinePoolKey, m)).NotTo(HaveOccurred())

			tt.conditionAssertFunc(t, m)
		})
	}
}

// adds a condition list to an external object.
func addConditionsToExternal(u *unstructured.Unstructured, newConditions clusterv1.Conditions) {
	existingConditions := clusterv1.Conditions{}
	if cs := conditions.UnstructuredGetter(u).GetConditions(); len(cs) != 0 {
		existingConditions = cs
	}
	existingConditions = append(existingConditions, newConditions...)
	conditions.UnstructuredSetter(u).SetConditions(existingConditions)
}
