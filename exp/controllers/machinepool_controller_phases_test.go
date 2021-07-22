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
	"time"

	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	expv1 "sigs.k8s.io/cluster-api/exp/api/v1alpha4"
	"sigs.k8s.io/cluster-api/util/kubeconfig"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	clusterName    = "test-cluster"
	wrongNamespace = "wrong-namespace"
)

func init() {
	externalReadyWait = 1 * time.Second
}

func TestReconcileMachinePoolPhases(t *testing.T) {
	deletionTimestamp := metav1.Now()

	var defaultKubeconfigSecret *corev1.Secret
	defaultCluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: metav1.NamespaceDefault,
		},
	}

	defaultMachinePool := expv1.MachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machinepool-test",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: expv1.MachinePoolSpec{
			ClusterName: defaultCluster.Name,
			Replicas:    pointer.Int32Ptr(1),
			Template: clusterv1.MachineTemplateSpec{
				Spec: clusterv1.MachineSpec{
					Bootstrap: clusterv1.Bootstrap{
						ConfigRef: &corev1.ObjectReference{
							APIVersion: "bootstrap.cluster.x-k8s.io/v1alpha4",
							Kind:       "BootstrapConfig",
							Name:       "bootstrap-config1",
						},
					},
					InfrastructureRef: corev1.ObjectReference{
						APIVersion: "infrastructure.cluster.x-k8s.io/v1alpha4",
						Kind:       "InfrastructureConfig",
						Name:       "infra-config1",
					},
				},
			},
		},
	}

	defaultBootstrap := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "BootstrapConfig",
			"apiVersion": "bootstrap.cluster.x-k8s.io/v1alpha4",
			"metadata": map[string]interface{}{
				"name":      "bootstrap-config1",
				"namespace": metav1.NamespaceDefault,
			},
			"spec":   map[string]interface{}{},
			"status": map[string]interface{}{},
		},
	}

	defaultInfra := &unstructured.Unstructured{
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
			"status": map[string]interface{}{},
		},
	}

	t.Run("Should set OwnerReference and cluster name label on external objects", func(t *testing.T) {
		g := NewWithT(t)

		defaultKubeconfigSecret = kubeconfig.GenerateSecret(defaultCluster, kubeconfig.FromEnvTestConfig(env.Config, defaultCluster))
		machinepool := defaultMachinePool.DeepCopy()
		bootstrapConfig := defaultBootstrap.DeepCopy()
		infraConfig := defaultInfra.DeepCopy()

		r := &MachinePoolReconciler{
			Client: fake.NewClientBuilder().WithObjects(defaultCluster, defaultKubeconfigSecret, machinepool, bootstrapConfig, infraConfig).Build(),
		}

		res, err := r.reconcile(ctx, defaultCluster, machinepool)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(res.Requeue).To(BeFalse())

		r.reconcilePhase(machinepool)

		g.Expect(r.Client.Get(ctx, types.NamespacedName{Name: bootstrapConfig.GetName(), Namespace: bootstrapConfig.GetNamespace()}, bootstrapConfig)).To(Succeed())

		g.Expect(bootstrapConfig.GetOwnerReferences()).To(HaveLen(1))
		g.Expect(bootstrapConfig.GetLabels()[clusterv1.ClusterLabelName]).To(BeEquivalentTo(clusterName))

		g.Expect(r.Client.Get(ctx, types.NamespacedName{Name: infraConfig.GetName(), Namespace: infraConfig.GetNamespace()}, infraConfig)).To(Succeed())

		g.Expect(infraConfig.GetOwnerReferences()).To(HaveLen(1))
		g.Expect(infraConfig.GetLabels()[clusterv1.ClusterLabelName]).To(BeEquivalentTo(clusterName))
	})

	t.Run("Should set `Pending` with a new MachinePool", func(t *testing.T) {
		g := NewWithT(t)

		defaultKubeconfigSecret = kubeconfig.GenerateSecret(defaultCluster, kubeconfig.FromEnvTestConfig(env.Config, defaultCluster))
		machinepool := defaultMachinePool.DeepCopy()
		bootstrapConfig := defaultBootstrap.DeepCopy()
		infraConfig := defaultInfra.DeepCopy()

		r := &MachinePoolReconciler{
			Client: fake.NewClientBuilder().WithObjects(defaultCluster, defaultKubeconfigSecret, machinepool, bootstrapConfig, infraConfig).Build(),
		}

		res, err := r.reconcile(ctx, defaultCluster, machinepool)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(res.Requeue).To(BeFalse())

		r.reconcilePhase(machinepool)
		g.Expect(machinepool.Status.GetTypedPhase()).To(Equal(expv1.MachinePoolPhasePending))
	})

	t.Run("Should set `Provisioning` when bootstrap is ready", func(t *testing.T) {
		g := NewWithT(t)

		defaultKubeconfigSecret = kubeconfig.GenerateSecret(defaultCluster, kubeconfig.FromEnvTestConfig(env.Config, defaultCluster))
		machinepool := defaultMachinePool.DeepCopy()
		bootstrapConfig := defaultBootstrap.DeepCopy()
		infraConfig := defaultInfra.DeepCopy()

		// Set bootstrap ready.
		err := unstructured.SetNestedField(bootstrapConfig.Object, true, "status", "ready")
		g.Expect(err).NotTo(HaveOccurred())

		err = unstructured.SetNestedField(bootstrapConfig.Object, "secret-data", "status", "dataSecretName")
		g.Expect(err).NotTo(HaveOccurred())

		r := &MachinePoolReconciler{
			Client: fake.NewClientBuilder().WithObjects(defaultCluster, defaultKubeconfigSecret, machinepool, bootstrapConfig, infraConfig).Build(),
		}

		res, err := r.reconcile(ctx, defaultCluster, machinepool)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(res.Requeue).To(BeFalse())

		r.reconcilePhase(machinepool)
		g.Expect(machinepool.Status.GetTypedPhase()).To(Equal(expv1.MachinePoolPhaseProvisioning))
	})

	t.Run("Should set `Running` when bootstrap and infra is ready", func(t *testing.T) {
		g := NewWithT(t)

		defaultKubeconfigSecret = kubeconfig.GenerateSecret(defaultCluster, kubeconfig.FromEnvTestConfig(env.Config, defaultCluster))
		machinepool := defaultMachinePool.DeepCopy()
		bootstrapConfig := defaultBootstrap.DeepCopy()
		infraConfig := defaultInfra.DeepCopy()

		// Set bootstrap ready.
		err := unstructured.SetNestedField(bootstrapConfig.Object, true, "status", "ready")
		g.Expect(err).NotTo(HaveOccurred())

		err = unstructured.SetNestedField(bootstrapConfig.Object, "secret-data", "status", "dataSecretName")
		g.Expect(err).NotTo(HaveOccurred())

		// Set infra ready.
		err = unstructured.SetNestedField(infraConfig.Object, true, "status", "ready")
		g.Expect(err).NotTo(HaveOccurred())

		err = unstructured.SetNestedField(infraConfig.Object, int64(1), "status", "replicas")
		g.Expect(err).NotTo(HaveOccurred())

		err = unstructured.SetNestedStringSlice(infraConfig.Object, []string{"test://machinepool-test-node"}, "spec", "providerIDList")
		g.Expect(err).NotTo(HaveOccurred())

		err = unstructured.SetNestedField(infraConfig.Object, "us-east-2a", "spec", "failureDomain")
		g.Expect(err).NotTo(HaveOccurred())

		// Set NodeRef.
		machinepool.Status.NodeRefs = []corev1.ObjectReference{{Kind: "Node", Name: "machinepool-test-node"}}

		r := &MachinePoolReconciler{
			Client: fake.NewClientBuilder().WithObjects(defaultCluster, defaultKubeconfigSecret, machinepool, bootstrapConfig, infraConfig).Build(),
		}

		res, err := r.reconcile(ctx, defaultCluster, machinepool)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(res.Requeue).To(BeFalse())

		// Set ReadyReplicas
		machinepool.Status.ReadyReplicas = 1

		r.reconcilePhase(machinepool)
		g.Expect(machinepool.Status.GetTypedPhase()).To(Equal(expv1.MachinePoolPhaseRunning))
	})

	t.Run("Should set `Running` when bootstrap, infra, and ready replicas equals spec replicas", func(t *testing.T) {
		g := NewWithT(t)

		defaultKubeconfigSecret = kubeconfig.GenerateSecret(defaultCluster, kubeconfig.FromEnvTestConfig(env.Config, defaultCluster))
		machinepool := defaultMachinePool.DeepCopy()
		bootstrapConfig := defaultBootstrap.DeepCopy()
		infraConfig := defaultInfra.DeepCopy()

		// Set bootstrap ready.
		err := unstructured.SetNestedField(bootstrapConfig.Object, true, "status", "ready")
		g.Expect(err).NotTo(HaveOccurred())

		err = unstructured.SetNestedField(bootstrapConfig.Object, "secret-data", "status", "dataSecretName")
		g.Expect(err).NotTo(HaveOccurred())

		// Set infra ready.
		err = unstructured.SetNestedStringSlice(infraConfig.Object, []string{"test://id-1"}, "spec", "providerIDList")
		g.Expect(err).NotTo(HaveOccurred())

		err = unstructured.SetNestedField(infraConfig.Object, true, "status", "ready")
		g.Expect(err).NotTo(HaveOccurred())

		err = unstructured.SetNestedField(infraConfig.Object, int64(1), "status", "replicas")
		g.Expect(err).NotTo(HaveOccurred())

		err = unstructured.SetNestedField(infraConfig.Object, []interface{}{
			map[string]interface{}{
				"type":    "InternalIP",
				"address": "10.0.0.1",
			},
			map[string]interface{}{
				"type":    "InternalIP",
				"address": "10.0.0.2",
			},
		}, "addresses")
		g.Expect(err).NotTo(HaveOccurred())

		// Set NodeRef.
		machinepool.Status.NodeRefs = []corev1.ObjectReference{{Kind: "Node", Name: "machinepool-test-node"}}

		r := &MachinePoolReconciler{
			Client: fake.NewClientBuilder().WithObjects(defaultCluster, defaultKubeconfigSecret, machinepool, bootstrapConfig, infraConfig).Build(),
		}

		res, err := r.reconcile(ctx, defaultCluster, machinepool)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(res.Requeue).To(BeFalse())

		// Set ReadyReplicas
		machinepool.Status.ReadyReplicas = 1

		r.reconcilePhase(machinepool)
		g.Expect(machinepool.Status.GetTypedPhase()).To(Equal(expv1.MachinePoolPhaseRunning))
	})

	t.Run("Should set `Provisioned` when there is a NodeRef but infra is not ready ", func(t *testing.T) {
		g := NewWithT(t)

		defaultKubeconfigSecret = kubeconfig.GenerateSecret(defaultCluster, kubeconfig.FromEnvTestConfig(env.Config, defaultCluster))
		machinepool := defaultMachinePool.DeepCopy()
		bootstrapConfig := defaultBootstrap.DeepCopy()
		infraConfig := defaultInfra.DeepCopy()

		// Set bootstrap ready.
		err := unstructured.SetNestedField(bootstrapConfig.Object, true, "status", "ready")
		g.Expect(err).NotTo(HaveOccurred())

		err = unstructured.SetNestedField(bootstrapConfig.Object, "secret-data", "status", "dataSecretName")
		g.Expect(err).NotTo(HaveOccurred())

		// Set NodeRef.
		machinepool.Status.NodeRefs = []corev1.ObjectReference{{Kind: "Node", Name: "machinepool-test-node"}}

		r := &MachinePoolReconciler{
			Client: fake.NewClientBuilder().WithObjects(defaultCluster, defaultKubeconfigSecret, machinepool, bootstrapConfig, infraConfig).Build(),
		}

		res, err := r.reconcile(ctx, defaultCluster, machinepool)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(res.Requeue).To(BeFalse())

		r.reconcilePhase(machinepool)
		g.Expect(machinepool.Status.GetTypedPhase()).To(Equal(expv1.MachinePoolPhaseProvisioned))
	})

	t.Run("Should set `ScalingUp` when infra is scaling up", func(t *testing.T) {
		g := NewWithT(t)

		defaultKubeconfigSecret = kubeconfig.GenerateSecret(defaultCluster, kubeconfig.FromEnvTestConfig(env.Config, defaultCluster))
		machinepool := defaultMachinePool.DeepCopy()
		bootstrapConfig := defaultBootstrap.DeepCopy()
		infraConfig := defaultInfra.DeepCopy()

		// Set bootstrap ready.
		err := unstructured.SetNestedField(bootstrapConfig.Object, true, "status", "ready")
		g.Expect(err).NotTo(HaveOccurred())

		err = unstructured.SetNestedField(bootstrapConfig.Object, "secret-data", "status", "dataSecretName")
		g.Expect(err).NotTo(HaveOccurred())

		// Set infra ready.
		err = unstructured.SetNestedStringSlice(infraConfig.Object, []string{"test://id-1"}, "spec", "providerIDList")
		g.Expect(err).NotTo(HaveOccurred())

		err = unstructured.SetNestedField(infraConfig.Object, true, "status", "ready")
		g.Expect(err).NotTo(HaveOccurred())

		err = unstructured.SetNestedField(infraConfig.Object, int64(1), "status", "replicas")
		g.Expect(err).NotTo(HaveOccurred())

		// Set NodeRef.
		machinepool.Status.NodeRefs = []corev1.ObjectReference{{Kind: "Node", Name: "machinepool-test-node"}}

		r := &MachinePoolReconciler{
			Client: fake.NewClientBuilder().WithObjects(defaultCluster, defaultKubeconfigSecret, machinepool, bootstrapConfig, infraConfig).Build(),
		}

		res, err := r.reconcile(ctx, defaultCluster, machinepool)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(res.Requeue).To(BeFalse())

		// Set ReadyReplicas
		machinepool.Status.ReadyReplicas = 1

		// Scale up
		machinepool.Spec.Replicas = pointer.Int32Ptr(5)

		r.reconcilePhase(machinepool)
		g.Expect(machinepool.Status.GetTypedPhase()).To(Equal(expv1.MachinePoolPhaseScalingUp))
	})

	t.Run("Should set `ScalingDown` when infra is scaling down", func(t *testing.T) {
		g := NewWithT(t)

		defaultKubeconfigSecret = kubeconfig.GenerateSecret(defaultCluster, kubeconfig.FromEnvTestConfig(env.Config, defaultCluster))
		machinepool := defaultMachinePool.DeepCopy()
		bootstrapConfig := defaultBootstrap.DeepCopy()
		infraConfig := defaultInfra.DeepCopy()

		// Set bootstrap ready.
		err := unstructured.SetNestedField(bootstrapConfig.Object, true, "status", "ready")
		g.Expect(err).NotTo(HaveOccurred())

		err = unstructured.SetNestedField(bootstrapConfig.Object, "secret-data", "status", "dataSecretName")
		g.Expect(err).NotTo(HaveOccurred())

		// Set infra ready.
		err = unstructured.SetNestedStringSlice(infraConfig.Object, []string{"test://id-1"}, "spec", "providerIDList")
		g.Expect(err).NotTo(HaveOccurred())

		err = unstructured.SetNestedField(infraConfig.Object, true, "status", "ready")
		g.Expect(err).NotTo(HaveOccurred())

		err = unstructured.SetNestedField(infraConfig.Object, int64(4), "status", "replicas")
		g.Expect(err).NotTo(HaveOccurred())

		machinepool.Spec.Replicas = pointer.Int32Ptr(4)

		// Set NodeRef.
		machinepool.Status.NodeRefs = []corev1.ObjectReference{
			{Kind: "Node", Name: "machinepool-test-node-0"},
			{Kind: "Node", Name: "machinepool-test-node-1"},
			{Kind: "Node", Name: "machinepool-test-node-2"},
			{Kind: "Node", Name: "machinepool-test-node-3"},
		}

		r := &MachinePoolReconciler{
			Client: fake.NewClientBuilder().WithObjects(defaultCluster, defaultKubeconfigSecret, machinepool, bootstrapConfig, infraConfig).Build(),
		}

		res, err := r.reconcile(ctx, defaultCluster, machinepool)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(res.Requeue).To(BeFalse())

		// Set ReadyReplicas
		machinepool.Status.ReadyReplicas = 4

		// Scale down
		machinepool.Spec.Replicas = pointer.Int32Ptr(1)

		r.reconcilePhase(machinepool)
		g.Expect(machinepool.Status.GetTypedPhase()).To(Equal(expv1.MachinePoolPhaseScalingDown))
	})

	t.Run("Should set `Deleting` when MachinePool is being deleted", func(t *testing.T) {
		g := NewWithT(t)

		defaultKubeconfigSecret = kubeconfig.GenerateSecret(defaultCluster, kubeconfig.FromEnvTestConfig(env.Config, defaultCluster))
		machinepool := defaultMachinePool.DeepCopy()
		bootstrapConfig := defaultBootstrap.DeepCopy()
		infraConfig := defaultInfra.DeepCopy()

		// Set bootstrap ready.
		err := unstructured.SetNestedField(bootstrapConfig.Object, true, "status", "ready")
		g.Expect(err).NotTo(HaveOccurred())

		err = unstructured.SetNestedField(bootstrapConfig.Object, "secret-data", "status", "dataSecretName")
		g.Expect(err).NotTo(HaveOccurred())

		// Set infra ready.
		err = unstructured.SetNestedStringSlice(infraConfig.Object, []string{"test://id-1"}, "spec", "providerIDList")
		g.Expect(err).NotTo(HaveOccurred())

		err = unstructured.SetNestedField(infraConfig.Object, true, "status", "ready")
		g.Expect(err).NotTo(HaveOccurred())

		err = unstructured.SetNestedField(infraConfig.Object, []interface{}{
			map[string]interface{}{
				"type":    "InternalIP",
				"address": "10.0.0.1",
			},
			map[string]interface{}{
				"type":    "InternalIP",
				"address": "10.0.0.2",
			},
		}, "addresses")
		g.Expect(err).NotTo(HaveOccurred())

		// Set NodeRef.
		machinepool.Status.NodeRefs = []corev1.ObjectReference{{Kind: "Node", Name: "machinepool-test-node"}}

		// Set Deletion Timestamp.
		machinepool.SetDeletionTimestamp(&deletionTimestamp)

		r := &MachinePoolReconciler{
			Client: fake.NewClientBuilder().WithObjects(defaultCluster, defaultKubeconfigSecret, machinepool, bootstrapConfig, infraConfig).Build(),
		}

		res, err := r.reconcile(ctx, defaultCluster, machinepool)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(res.Requeue).To(BeFalse())

		r.reconcilePhase(machinepool)
		g.Expect(machinepool.Status.GetTypedPhase()).To(Equal(expv1.MachinePoolPhaseDeleting))
	})
}

func TestReconcileMachinePoolBootstrap(t *testing.T) {
	defaultMachinePool := expv1.MachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machinepool-test",
			Namespace: metav1.NamespaceDefault,
			Labels: map[string]string{
				clusterv1.ClusterLabelName: clusterName,
			},
		},
		Spec: expv1.MachinePoolSpec{
			Template: clusterv1.MachineTemplateSpec{
				Spec: clusterv1.MachineSpec{
					Bootstrap: clusterv1.Bootstrap{
						ConfigRef: &corev1.ObjectReference{
							APIVersion: "bootstrap.cluster.x-k8s.io/v1alpha4",
							Kind:       "BootstrapConfig",
							Name:       "bootstrap-config1",
						},
					},
				},
			},
		},
	}

	defaultCluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: metav1.NamespaceDefault,
		},
	}

	testCases := []struct {
		name            string
		bootstrapConfig map[string]interface{}
		machinepool     *expv1.MachinePool
		expectError     bool
		expectResult    ctrl.Result
		expected        func(g *WithT, m *expv1.MachinePool)
	}{
		{
			name: "new machinepool, bootstrap config ready with data",
			bootstrapConfig: map[string]interface{}{
				"kind":       "BootstrapConfig",
				"apiVersion": "bootstrap.cluster.x-k8s.io/v1alpha4",
				"metadata": map[string]interface{}{
					"name":      "bootstrap-config1",
					"namespace": metav1.NamespaceDefault,
				},
				"spec": map[string]interface{}{},
				"status": map[string]interface{}{
					"ready":          true,
					"dataSecretName": "secret-data",
				},
			},
			expectError: false,
			expected: func(g *WithT, m *expv1.MachinePool) {
				g.Expect(m.Status.BootstrapReady).To(BeTrue())
				g.Expect(m.Spec.Template.Spec.Bootstrap.DataSecretName).ToNot(BeNil())
				g.Expect(*m.Spec.Template.Spec.Bootstrap.DataSecretName).To(ContainSubstring("secret-data"))
			},
		},
		{
			name: "new machinepool, bootstrap config ready with no data",
			bootstrapConfig: map[string]interface{}{
				"kind":       "BootstrapConfig",
				"apiVersion": "bootstrap.cluster.x-k8s.io/v1alpha4",
				"metadata": map[string]interface{}{
					"name":      "bootstrap-config1",
					"namespace": metav1.NamespaceDefault,
				},
				"spec": map[string]interface{}{},
				"status": map[string]interface{}{
					"ready": true,
				},
			},
			expectError: true,
			expected: func(g *WithT, m *expv1.MachinePool) {
				g.Expect(m.Status.BootstrapReady).To(BeFalse())
				g.Expect(m.Spec.Template.Spec.Bootstrap.DataSecretName).To(BeNil())
			},
		},
		{
			name: "new machinepool, bootstrap config not ready",
			bootstrapConfig: map[string]interface{}{
				"kind":       "BootstrapConfig",
				"apiVersion": "bootstrap.cluster.x-k8s.io/v1alpha4",
				"metadata": map[string]interface{}{
					"name":      "bootstrap-config1",
					"namespace": metav1.NamespaceDefault,
				},
				"spec":   map[string]interface{}{},
				"status": map[string]interface{}{},
			},
			expectError:  false,
			expectResult: ctrl.Result{RequeueAfter: externalReadyWait},
			expected: func(g *WithT, m *expv1.MachinePool) {
				g.Expect(m.Status.BootstrapReady).To(BeFalse())
			},
		},
		{
			name: "new machinepool, bootstrap config is not found",
			bootstrapConfig: map[string]interface{}{
				"kind":       "BootstrapConfig",
				"apiVersion": "bootstrap.cluster.x-k8s.io/v1alpha4",
				"metadata": map[string]interface{}{
					"name":      "bootstrap-config1",
					"namespace": wrongNamespace,
				},
				"spec":   map[string]interface{}{},
				"status": map[string]interface{}{},
			},
			expectError: true,
			expected: func(g *WithT, m *expv1.MachinePool) {
				g.Expect(m.Status.BootstrapReady).To(BeFalse())
			},
		},
		{
			name: "new machinepool, no bootstrap config or data",
			bootstrapConfig: map[string]interface{}{
				"kind":       "BootstrapConfig",
				"apiVersion": "bootstrap.cluster.x-k8s.io/v1alpha4",
				"metadata": map[string]interface{}{
					"name":      "bootstrap-config1",
					"namespace": wrongNamespace,
				},
				"spec":   map[string]interface{}{},
				"status": map[string]interface{}{},
			},
			expectError: true,
		},
		{
			name: "existing machinepool, bootstrap data should not change",
			bootstrapConfig: map[string]interface{}{
				"kind":       "BootstrapConfig",
				"apiVersion": "bootstrap.cluster.x-k8s.io/v1alpha4",
				"metadata": map[string]interface{}{
					"name":      "bootstrap-config1",
					"namespace": metav1.NamespaceDefault,
				},
				"spec": map[string]interface{}{},
				"status": map[string]interface{}{
					"ready":          true,
					"dataSecretName": "secret-data",
				},
			},
			machinepool: &expv1.MachinePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bootstrap-test-existing",
					Namespace: metav1.NamespaceDefault,
				},
				Spec: expv1.MachinePoolSpec{
					Template: clusterv1.MachineTemplateSpec{
						Spec: clusterv1.MachineSpec{
							Bootstrap: clusterv1.Bootstrap{
								ConfigRef: &corev1.ObjectReference{
									APIVersion: "bootstrap.cluster.x-k8s.io/v1alpha4",
									Kind:       "BootstrapConfig",
									Name:       "bootstrap-config1",
								},
								DataSecretName: pointer.StringPtr("data"),
							},
						},
					},
				},
				Status: expv1.MachinePoolStatus{
					BootstrapReady: true,
				},
			},
			expectError: false,
			expected: func(g *WithT, m *expv1.MachinePool) {
				g.Expect(m.Status.BootstrapReady).To(BeTrue())
				g.Expect(*m.Spec.Template.Spec.Bootstrap.DataSecretName).To(Equal("data"))
			},
		},
		{
			name: "existing machinepool, bootstrap provider is to not ready",
			bootstrapConfig: map[string]interface{}{
				"kind":       "BootstrapConfig",
				"apiVersion": "bootstrap.cluster.x-k8s.io/v1alpha4",
				"metadata": map[string]interface{}{
					"name":      "bootstrap-config1",
					"namespace": metav1.NamespaceDefault,
				},
				"spec": map[string]interface{}{},
				"status": map[string]interface{}{
					"ready": false,
					"data":  "#!/bin/bash ... data",
				},
			},
			machinepool: &expv1.MachinePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bootstrap-test-existing",
					Namespace: metav1.NamespaceDefault,
				},
				Spec: expv1.MachinePoolSpec{
					Template: clusterv1.MachineTemplateSpec{
						Spec: clusterv1.MachineSpec{
							Bootstrap: clusterv1.Bootstrap{
								ConfigRef: &corev1.ObjectReference{
									APIVersion: "bootstrap.cluster.x-k8s.io/v1alpha4",
									Kind:       "BootstrapConfig",
									Name:       "bootstrap-config1",
								},
								DataSecretName: pointer.StringPtr("data"),
							},
						},
					},
				},
				Status: expv1.MachinePoolStatus{
					BootstrapReady: true,
				},
			},
			expectError: false,
			expected: func(g *WithT, m *expv1.MachinePool) {
				g.Expect(m.Status.BootstrapReady).To(BeTrue())
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			if tc.machinepool == nil {
				tc.machinepool = defaultMachinePool.DeepCopy()
			}

			bootstrapConfig := &unstructured.Unstructured{Object: tc.bootstrapConfig}
			r := &MachinePoolReconciler{
				Client: fake.NewClientBuilder().WithObjects(tc.machinepool, bootstrapConfig).Build(),
			}

			res, err := r.reconcileBootstrap(ctx, defaultCluster, tc.machinepool)
			g.Expect(res).To(Equal(tc.expectResult))
			if tc.expectError {
				g.Expect(err).ToNot(BeNil())
			} else {
				g.Expect(err).To(BeNil())
			}

			if tc.expected != nil {
				tc.expected(g, tc.machinepool)
			}
		})
	}
}

func TestReconcileMachinePoolInfrastructure(t *testing.T) {
	defaultMachinePool := expv1.MachinePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machinepool-test",
			Namespace: metav1.NamespaceDefault,
			Labels: map[string]string{
				clusterv1.ClusterLabelName: clusterName,
			},
		},
		Spec: expv1.MachinePoolSpec{
			Replicas: pointer.Int32Ptr(1),
			Template: clusterv1.MachineTemplateSpec{
				Spec: clusterv1.MachineSpec{
					Bootstrap: clusterv1.Bootstrap{
						ConfigRef: &corev1.ObjectReference{
							APIVersion: "bootstrap.cluster.x-k8s.io/v1alpha4",
							Kind:       "BootstrapConfig",
							Name:       "bootstrap-config1",
						},
					},
					InfrastructureRef: corev1.ObjectReference{
						APIVersion: "infrastructure.cluster.x-k8s.io/v1alpha4",
						Kind:       "InfrastructureConfig",
						Name:       "infra-config1",
					},
				},
			},
		},
	}

	defaultCluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: metav1.NamespaceDefault,
		},
	}

	testCases := []struct {
		name               string
		bootstrapConfig    map[string]interface{}
		infraConfig        map[string]interface{}
		machinepool        *expv1.MachinePool
		expectError        bool
		expectChanged      bool
		expectRequeueAfter bool
		expected           func(g *WithT, m *expv1.MachinePool)
	}{
		{
			name: "new machinepool, infrastructure config ready",
			infraConfig: map[string]interface{}{
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
						map[string]interface{}{
							"type":    "InternalIP",
							"address": "10.0.0.2",
						},
					},
				},
			},
			expectError:   false,
			expectChanged: true,
			expected: func(g *WithT, m *expv1.MachinePool) {
				g.Expect(m.Status.InfrastructureReady).To(BeTrue())
			},
		},
		{
			name: "ready bootstrap, infra, and nodeRef, machinepool is running, infra object is deleted, expect failed",
			machinepool: &expv1.MachinePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "machinepool-test",
					Namespace: metav1.NamespaceDefault,
				},
				Spec: expv1.MachinePoolSpec{
					Replicas: pointer.Int32Ptr(1),
					Template: clusterv1.MachineTemplateSpec{
						Spec: clusterv1.MachineSpec{
							Bootstrap: clusterv1.Bootstrap{
								ConfigRef: &corev1.ObjectReference{
									APIVersion: "bootstrap.cluster.x-k8s.io/v1alpha4",
									Kind:       "BootstrapConfig",
									Name:       "bootstrap-config1",
								},
							},
							InfrastructureRef: corev1.ObjectReference{
								APIVersion: "infrastructure.cluster.x-k8s.io/v1alpha4",
								Kind:       "InfrastructureConfig",
								Name:       "infra-config1",
							},
						},
					},
				},
				Status: expv1.MachinePoolStatus{
					BootstrapReady:      true,
					InfrastructureReady: true,
					NodeRefs:            []corev1.ObjectReference{{Kind: "Node", Name: "machinepool-test-node"}},
				},
			},
			bootstrapConfig: map[string]interface{}{
				"kind":       "BootstrapConfig",
				"apiVersion": "bootstrap.cluster.x-k8s.io/v1alpha4",
				"metadata": map[string]interface{}{
					"name":      "bootstrap-config1",
					"namespace": metav1.NamespaceDefault,
				},
				"spec": map[string]interface{}{},
				"status": map[string]interface{}{
					"ready":          true,
					"dataSecretName": "secret-data",
				},
			},
			infraConfig: map[string]interface{}{
				"kind":       "InfrastructureConfig",
				"apiVersion": "infrastructure.cluster.x-k8s.io/v1alpha4",
				"metadata":   map[string]interface{}{},
			},
			expectError:        true,
			expectRequeueAfter: false,
			expected: func(g *WithT, m *expv1.MachinePool) {
				g.Expect(m.Status.InfrastructureReady).To(BeTrue())
				g.Expect(m.Status.FailureMessage).ToNot(BeNil())
				g.Expect(m.Status.FailureReason).ToNot(BeNil())
				g.Expect(m.Status.GetTypedPhase()).To(Equal(expv1.MachinePoolPhaseFailed))
			},
		},
		{
			name: "infrastructure ref is paused",
			infraConfig: map[string]interface{}{
				"kind":       "InfrastructureConfig",
				"apiVersion": "infrastructure.cluster.x-k8s.io/v1alpha4",
				"metadata": map[string]interface{}{
					"name":      "infra-config1",
					"namespace": metav1.NamespaceDefault,
					"annotations": map[string]interface{}{
						"cluster.x-k8s.io/paused": "true",
					},
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
						map[string]interface{}{
							"type":    "InternalIP",
							"address": "10.0.0.2",
						},
					},
				},
			},
			expectError:   false,
			expectChanged: false,
			expected: func(g *WithT, m *expv1.MachinePool) {
				g.Expect(m.Status.InfrastructureReady).To(BeFalse())
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			if tc.machinepool == nil {
				tc.machinepool = defaultMachinePool.DeepCopy()
			}

			infraConfig := &unstructured.Unstructured{Object: tc.infraConfig}
			r := &MachinePoolReconciler{
				Client: fake.NewClientBuilder().WithObjects(tc.machinepool, infraConfig).Build(),
			}

			res, err := r.reconcileInfrastructure(ctx, defaultCluster, tc.machinepool)
			if tc.expectRequeueAfter {
				g.Expect(res.RequeueAfter).To(BeNumerically(">=", 0))
			}
			r.reconcilePhase(tc.machinepool)
			if tc.expectError {
				g.Expect(err).ToNot(BeNil())
			} else {
				g.Expect(err).To(BeNil())
			}

			if tc.expected != nil {
				tc.expected(g, tc.machinepool)
			}
		})
	}
}
