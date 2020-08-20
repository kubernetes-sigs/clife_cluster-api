/*
Copyright 2020 The Kubernetes Authors.

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
	"bytes"
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	bootstrapapi "k8s.io/cluster-bootstrap/token/api"
	"k8s.io/klog/klogr"
	"k8s.io/utils/pointer"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	fakeremote "sigs.k8s.io/cluster-api/controllers/remote/fake"
	expv1 "sigs.k8s.io/cluster-api/exp/api/v1alpha3"
	bootstrapv1 "sigs.k8s.io/cluster-api/exp/kubeadm-ignition/api/v1alpha3"
	kubeadmv1beta1 "sigs.k8s.io/cluster-api/exp/kubeadm-ignition/types/v1beta1"
	"sigs.k8s.io/cluster-api/feature"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/secret"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func setupScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	if err := clusterv1.AddToScheme(scheme); err != nil {
		panic(err)
	}
	if err := expv1.AddToScheme(scheme); err != nil {
		panic(err)
	}
	if err := bootstrapv1.AddToScheme(scheme); err != nil {
		panic(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		panic(err)
	}
	return scheme
}

// MachineToBootstrapMapFunc return kubeadm bootstrap configref name when configref exists
func TestKubeadmIgnitionConfigReconciler_MachineToBootstrapMapFuncReturn(t *testing.T) {
	g := NewWithT(t)

	cluster := newCluster("my-cluster")
	objs := []runtime.Object{cluster}
	machineObjs := []runtime.Object{}
	var expectedConfigName string
	for i := 0; i < 3; i++ {
		m := newMachine(cluster, fmt.Sprintf("my-machine-%d", i))
		configName := fmt.Sprintf("my-config-%d", i)
		if i == 1 {
			c := newKubeadmIgnitionConfig(m, configName)
			objs = append(objs, m, c)
			expectedConfigName = configName
		} else {
			objs = append(objs, m)
		}
		machineObjs = append(machineObjs, m)
	}
	fakeClient := fake.NewFakeClientWithScheme(setupScheme(), objs...)
	reconciler := &KubeadmIgnitionConfigReconciler{
		Log:    log.Log,
		Client: fakeClient,
	}
	for i := 0; i < 3; i++ {
		o := handler.MapObject{
			Object: machineObjs[i],
		}
		configs := reconciler.MachineToBootstrapMapFunc(o)
		if i == 1 {
			g.Expect(configs[0].Name).To(Equal(expectedConfigName))
		} else {
			g.Expect(configs[0].Name).To(Equal(""))
		}
	}
}

// Reconcile returns early if the kubeadm config is ready because it should never re-generate bootstrap data.
func TestKubeadmIgnitionConfigReconciler_Reconcile_ReturnEarlyIfKubeadmIgnitionConfigIsReady(t *testing.T) {
	g := NewWithT(t)

	config := newKubeadmIgnitionConfig(nil, "cfg")
	config.Status.Ready = true

	objects := []runtime.Object{
		config,
	}
	myclient := fake.NewFakeClientWithScheme(setupScheme(), objects...)

	k := &KubeadmIgnitionConfigReconciler{
		Log:    log.Log,
		Client: myclient,
	}

	request := ctrl.Request{
		NamespacedName: client.ObjectKey{
			Name:      "default",
			Namespace: "cfg",
		},
	}
	result, err := k.Reconcile(request)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Requeue).To(BeFalse())
	g.Expect(result.RequeueAfter).To(Equal(time.Duration(0)))
}

// Reconcile returns an error in this case because the owning machine should not go away before the things it owns.
func TestKubeadmIgnitionConfigReconciler_Reconcile_ReturnErrorIfReferencedMachineIsNotFound(t *testing.T) {
	g := NewWithT(t)

	machine := newMachine(nil, "machine")
	config := newKubeadmIgnitionConfig(machine, "cfg")

	objects := []runtime.Object{
		// intentionally omitting machine
		config,
	}
	myclient := fake.NewFakeClientWithScheme(setupScheme(), objects...)

	k := &KubeadmIgnitionConfigReconciler{
		Log:    log.Log,
		Client: myclient,
	}

	request := ctrl.Request{
		NamespacedName: client.ObjectKey{
			Namespace: "default",
			Name:      "cfg",
		},
	}
	_, err := k.Reconcile(request)
	g.Expect(err).To(HaveOccurred())
}

// If the machine has bootstrap data secret reference, there is no need to generate more bootstrap data.
func TestKubeadmIgnitionConfigReconciler_Reconcile_ReturnEarlyIfMachineHasDataSecretName(t *testing.T) {
	g := NewWithT(t)

	machine := newMachine(nil, "machine")
	machine.Spec.Bootstrap.DataSecretName = pointer.StringPtr("something")

	config := newKubeadmIgnitionConfig(machine, "cfg")
	objects := []runtime.Object{
		machine,
		config,
	}
	myclient := fake.NewFakeClientWithScheme(setupScheme(), objects...)

	k := &KubeadmIgnitionConfigReconciler{
		Log:    log.Log,
		Client: myclient,
	}

	request := ctrl.Request{
		NamespacedName: client.ObjectKey{
			Namespace: "default",
			Name:      "cfg",
		},
	}
	result, err := k.Reconcile(request)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Requeue).To(BeFalse())
	g.Expect(result.RequeueAfter).To(Equal(time.Duration(0)))
}

// Test the logic to migrate plaintext bootstrap data to a field.
func TestKubeadmIgnitionConfigReconciler_Reconcile_MigrateToSecret(t *testing.T) {
	g := NewWithT(t)

	cluster := newCluster("cluster")
	cluster.Status.InfrastructureReady = true
	machine := newMachine(cluster, "machine")
	config := newKubeadmIgnitionConfig(machine, "cfg")
	config.Status.Ready = true
	config.Status.BootstrapData = []byte("test")
	objects := []runtime.Object{
		cluster,
		machine,
		config,
	}
	myclient := fake.NewFakeClientWithScheme(setupScheme(), objects...)

	k := &KubeadmIgnitionConfigReconciler{
		Log:    log.Log,
		Client: myclient,
	}

	request := ctrl.Request{
		NamespacedName: client.ObjectKey{
			Namespace: "default",
			Name:      "cfg",
		},
	}

	result, err := k.Reconcile(request)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Requeue).To(BeFalse())
	g.Expect(result.RequeueAfter).To(Equal(time.Duration(0)))

	g.Expect(k.Client.Get(context.Background(), client.ObjectKey{Name: config.Name, Namespace: config.Namespace}, config)).To(Succeed())
	g.Expect(config.Status.DataSecretName).NotTo(BeNil())

	secret := &corev1.Secret{}
	g.Expect(k.Client.Get(context.Background(), client.ObjectKey{Namespace: config.Namespace, Name: *config.Status.DataSecretName}, secret)).To(Succeed())
	g.Expect(secret.Data["value"]).NotTo(Equal("test"))
	g.Expect(secret.Type).To(Equal(clusterv1.ClusterSecretType))
	clusterName := secret.Labels[clusterv1.ClusterLabelName]
	g.Expect(clusterName).To(Equal("cluster"))
}

func TestKubeadmIgnitionConfigReconciler_ReturnEarlyIfClusterInfraNotReady(t *testing.T) {
	g := NewWithT(t)

	cluster := newCluster("cluster")
	machine := newMachine(cluster, "machine")
	config := newKubeadmIgnitionConfig(machine, "cfg")

	//cluster infra not ready
	cluster.Status = clusterv1.ClusterStatus{
		InfrastructureReady: false,
	}

	objects := []runtime.Object{
		cluster,
		machine,
		config,
	}
	myclient := fake.NewFakeClientWithScheme(setupScheme(), objects...)

	k := &KubeadmIgnitionConfigReconciler{
		Log:    log.Log,
		Client: myclient,
	}

	request := ctrl.Request{
		NamespacedName: client.ObjectKey{
			Namespace: "default",
			Name:      "cfg",
		},
	}

	expectedResult := reconcile.Result{}
	actualResult, actualError := k.Reconcile(request)
	g.Expect(actualResult).To(Equal(expectedResult))
	g.Expect(actualError).NotTo(HaveOccurred())
}

// Return early If the owning machine does not have an associated cluster
func TestKubeadmIgnitionConfigReconciler_Reconcile_ReturnEarlyIfMachineHasNoCluster(t *testing.T) {
	g := NewWithT(t)

	machine := newMachine(nil, "machine") // Machine without a cluster
	config := newKubeadmIgnitionConfig(machine, "cfg")

	objects := []runtime.Object{
		machine,
		config,
	}
	myclient := fake.NewFakeClientWithScheme(setupScheme(), objects...)

	k := &KubeadmIgnitionConfigReconciler{
		Log:    log.Log,
		Client: myclient,
	}

	request := ctrl.Request{
		NamespacedName: client.ObjectKey{
			Namespace: "default",
			Name:      "cfg",
		},
	}
	_, err := k.Reconcile(request)
	g.Expect(err).NotTo(HaveOccurred())
}

// This does not expect an error, hoping the machine gets updated with a cluster
func TestKubeadmIgnitionConfigReconciler_Reconcile_ReturnNilIfMachineDoesNotHaveAssociatedCluster(t *testing.T) {
	g := NewWithT(t)

	machine := newMachine(nil, "machine") // intentionally omitting cluster
	config := newKubeadmIgnitionConfig(machine, "cfg")

	objects := []runtime.Object{
		machine,
		config,
	}
	myclient := fake.NewFakeClientWithScheme(setupScheme(), objects...)

	k := &KubeadmIgnitionConfigReconciler{
		Log:    log.Log,
		Client: myclient,
	}

	request := ctrl.Request{
		NamespacedName: client.ObjectKey{
			Namespace: "default",
			Name:      "cfg",
		},
	}
	_, err := k.Reconcile(request)
	g.Expect(err).NotTo(HaveOccurred())
}

// This does not expect an error, hoping that the associated cluster will be created
func TestKubeadmIgnitionConfigReconciler_Reconcile_ReturnNilIfAssociatedClusterIsNotFound(t *testing.T) {
	g := NewWithT(t)

	cluster := newCluster("cluster")
	machine := newMachine(cluster, "machine")
	config := newKubeadmIgnitionConfig(machine, "cfg")

	objects := []runtime.Object{
		// intentionally omitting cluster
		machine,
		config,
	}
	myclient := fake.NewFakeClientWithScheme(setupScheme(), objects...)

	k := &KubeadmIgnitionConfigReconciler{
		Log:    log.Log,
		Client: myclient,
	}

	request := ctrl.Request{
		NamespacedName: client.ObjectKey{
			Namespace: "default",
			Name:      "cfg",
		},
	}
	_, err := k.Reconcile(request)
	g.Expect(err).NotTo(HaveOccurred())
}

// If the control plane isn't initialized then there is no cluster for either a worker or control plane node to join.
func TestKubeadmIgnitionConfigReconciler_Reconcile_RequeueJoiningNodesIfControlPlaneNotInitialized(t *testing.T) {
	g := NewWithT(t)

	cluster := newCluster("cluster")
	cluster.Status.InfrastructureReady = true

	workerMachine := newWorkerMachine(cluster)
	workerJoinConfig := newWorkerJoinKubeadmIgnitionConfig(workerMachine)

	controlPlaneJoinMachine := newControlPlaneMachine(cluster, "control-plane-join-machine")
	controlPlaneJoinConfig := newControlPlaneJoinKubeadmIgnitionConfig(controlPlaneJoinMachine, "control-plane-join-cfg")

	testcases := []struct {
		name    string
		request ctrl.Request
		objects []runtime.Object
	}{
		{
			name: "requeue worker when control plane is not yet initialiezd",
			request: ctrl.Request{
				NamespacedName: client.ObjectKey{
					Namespace: workerJoinConfig.Namespace,
					Name:      workerJoinConfig.Name,
				},
			},
			objects: []runtime.Object{
				cluster,
				workerMachine,
				workerJoinConfig,
			},
		},
		{
			name: "requeue a secondary control plane when the control plane is not yet initialized",
			request: ctrl.Request{
				NamespacedName: client.ObjectKey{
					Namespace: controlPlaneJoinConfig.Namespace,
					Name:      controlPlaneJoinConfig.Name,
				},
			},
			objects: []runtime.Object{
				cluster,
				controlPlaneJoinMachine,
				controlPlaneJoinConfig,
			},
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			myclient := fake.NewFakeClientWithScheme(setupScheme(), tc.objects...)

			k := &KubeadmIgnitionConfigReconciler{
				Log:             log.Log,
				Client:          myclient,
				KubeadmInitLock: &myInitLocker{},
			}

			result, err := k.Reconcile(tc.request)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(result.Requeue).To(BeFalse())
			g.Expect(result.RequeueAfter).To(Equal(30 * time.Second))
		})
	}
}

// This generates cloud-config data but does not test the validity of it.
func TestKubeadmIgnitionConfigReconciler_Reconcile_GenerateCloudConfigData(t *testing.T) {
	g := NewWithT(t)

	cluster := newCluster("cluster")
	cluster.Status.InfrastructureReady = true

	controlPlaneInitMachine := newControlPlaneMachine(cluster, "control-plane-init-machine")
	controlPlaneInitConfig := newControlPlaneInitKubeadmIgnitionConfig(controlPlaneInitMachine, "control-plane-init-cfg")

	objects := []runtime.Object{
		cluster,
		controlPlaneInitMachine,
		controlPlaneInitConfig,
	}
	objects = append(objects, createSecrets(t, cluster, controlPlaneInitConfig)...)

	myclient := fake.NewFakeClientWithScheme(setupScheme(), objects...)

	k := &KubeadmIgnitionConfigReconciler{
		Log:             log.Log,
		Client:          myclient,
		KubeadmInitLock: &myInitLocker{},
	}

	request := ctrl.Request{
		NamespacedName: client.ObjectKey{
			Namespace: "default",
			Name:      "control-plane-init-cfg",
		},
	}
	result, err := k.Reconcile(request)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Requeue).To(BeFalse())
	g.Expect(result.RequeueAfter).To(Equal(time.Duration(0)))

	cfg, err := getKubeadmIgnitionConfig(myclient, "control-plane-init-cfg")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(cfg.Status.Ready).To(BeTrue())
	g.Expect(cfg.Status.DataSecretName).NotTo(BeNil())

	// Ensure that we don't fail trying to refresh any bootstrap tokens
	_, err = k.Reconcile(request)
	g.Expect(err).NotTo(HaveOccurred())
}

// If a control plane has no JoinConfiguration, then we will create a default and no error will occur
func TestKubeadmIgnitionConfigReconciler_Reconcile_ErrorIfJoiningControlPlaneHasInvalidConfiguration(t *testing.T) {
	g := NewWithT(t)
	// TODO: extract this kind of code into a setup function that puts the state of objects into an initialized controlplane (implies secrets exist)
	cluster := newCluster("cluster")
	cluster.Status.InfrastructureReady = true
	cluster.Status.ControlPlaneInitialized = true
	cluster.Spec.ControlPlaneEndpoint = clusterv1.APIEndpoint{Host: "100.105.150.1", Port: 6443}
	controlPlaneInitMachine := newControlPlaneMachine(cluster, "control-plane-init-machine")
	controlPlaneInitConfig := newControlPlaneInitKubeadmIgnitionConfig(controlPlaneInitMachine, "control-plane-init-cfg")

	controlPlaneJoinMachine := newControlPlaneMachine(cluster, "control-plane-join-machine")
	controlPlaneJoinConfig := newControlPlaneJoinKubeadmIgnitionConfig(controlPlaneJoinMachine, "control-plane-join-cfg")
	controlPlaneJoinConfig.Spec.JoinConfiguration.ControlPlane = nil // Makes controlPlaneJoinConfig invalid for a control plane machine

	objects := []runtime.Object{
		cluster,
		controlPlaneJoinMachine,
		controlPlaneJoinConfig,
	}
	objects = append(objects, createSecrets(t, cluster, controlPlaneInitConfig)...)
	myclient := fake.NewFakeClientWithScheme(setupScheme(), objects...)

	k := &KubeadmIgnitionConfigReconciler{
		Log:                log.Log,
		Client:             myclient,
		KubeadmInitLock:    &myInitLocker{},
		remoteClientGetter: fakeremote.NewClusterClient,
	}

	request := ctrl.Request{
		NamespacedName: client.ObjectKey{
			Namespace: "default",
			Name:      "control-plane-join-cfg",
		},
	}
	_, err := k.Reconcile(request)
	g.Expect(err).NotTo(HaveOccurred())
}

// If there is no APIEndpoint but everything is ready then requeue in hopes of a new APIEndpoint showing up eventually.
func TestKubeadmIgnitionConfigReconciler_Reconcile_RequeueIfControlPlaneIsMissingAPIEndpoints(t *testing.T) {
	g := NewWithT(t)

	cluster := newCluster("cluster")
	cluster.Status.InfrastructureReady = true
	cluster.Status.ControlPlaneInitialized = true
	controlPlaneInitMachine := newControlPlaneMachine(cluster, "control-plane-init-machine")
	controlPlaneInitConfig := newControlPlaneInitKubeadmIgnitionConfig(controlPlaneInitMachine, "control-plane-init-cfg")

	workerMachine := newWorkerMachine(cluster)
	workerJoinConfig := newWorkerJoinKubeadmIgnitionConfig(workerMachine)

	objects := []runtime.Object{
		cluster,
		workerMachine,
		workerJoinConfig,
	}
	objects = append(objects, createSecrets(t, cluster, controlPlaneInitConfig)...)

	myclient := fake.NewFakeClientWithScheme(setupScheme(), objects...)

	k := &KubeadmIgnitionConfigReconciler{
		Log:             log.Log,
		Client:          myclient,
		KubeadmInitLock: &myInitLocker{},
	}

	request := ctrl.Request{
		NamespacedName: client.ObjectKey{
			Namespace: "default",
			Name:      "worker-join-cfg",
		},
	}
	result, err := k.Reconcile(request)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Requeue).To(BeFalse())
	g.Expect(result.RequeueAfter).To(Equal(10 * time.Second))
}

func TestReconcileIfJoinNodesAndControlPlaneIsReady(t *testing.T) {
	g := NewWithT(t)

	cluster := newCluster("cluster")
	cluster.Status.InfrastructureReady = true
	cluster.Status.ControlPlaneInitialized = true
	cluster.Spec.ControlPlaneEndpoint = clusterv1.APIEndpoint{Host: "100.105.150.1", Port: 6443}

	var useCases = []struct {
		name          string
		machine       *clusterv1.Machine
		configName    string
		configBuilder func(*clusterv1.Machine, string) *bootstrapv1.KubeadmIgnitionConfig
	}{
		{
			name:       "Join a worker node with a fully compiled kubeadm config object",
			machine:    newWorkerMachine(cluster),
			configName: "worker-join-cfg",
			configBuilder: func(machine *clusterv1.Machine, name string) *bootstrapv1.KubeadmIgnitionConfig {
				return newWorkerJoinKubeadmIgnitionConfig(machine)
			},
		},
		{
			name:          "Join a worker node  with an empty kubeadm config object (defaults apply)",
			machine:       newWorkerMachine(cluster),
			configName:    "worker-join-cfg",
			configBuilder: newKubeadmIgnitionConfig,
		},
		{
			name:          "Join a control plane node with a fully compiled kubeadm config object",
			machine:       newControlPlaneMachine(cluster, "control-plane-join-machine"),
			configName:    "control-plane-join-cfg",
			configBuilder: newControlPlaneJoinKubeadmIgnitionConfig,
		},
		{
			name:          "Join a control plane node with an empty kubeadm config object (defaults apply)",
			machine:       newControlPlaneMachine(cluster, "control-plane-join-machine"),
			configName:    "control-plane-join-cfg",
			configBuilder: newKubeadmIgnitionConfig,
		},
	}

	for _, rt := range useCases {
		rt := rt // pin!
		t.Run(rt.name, func(t *testing.T) {
			config := rt.configBuilder(rt.machine, rt.configName)

			objects := []runtime.Object{
				cluster,
				rt.machine,
				config,
			}
			objects = append(objects, createSecrets(t, cluster, config)...)
			myclient := fake.NewFakeClientWithScheme(setupScheme(), objects...)
			k := &KubeadmIgnitionConfigReconciler{
				Log:                log.Log,
				Client:             myclient,
				KubeadmInitLock:    &myInitLocker{},
				remoteClientGetter: fakeremote.NewClusterClient,
			}

			request := ctrl.Request{
				NamespacedName: client.ObjectKey{
					Namespace: config.GetNamespace(),
					Name:      rt.configName,
				},
			}
			result, err := k.Reconcile(request)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(result.Requeue).To(BeFalse())
			g.Expect(result.RequeueAfter).To(Equal(time.Duration(0)))

			cfg, err := getKubeadmIgnitionConfig(myclient, rt.configName)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(cfg.Status.Ready).To(BeTrue())
			g.Expect(cfg.Status.DataSecretName).NotTo(BeNil())

			l := &corev1.SecretList{}
			err = myclient.List(context.Background(), l, client.ListOption(client.InNamespace(metav1.NamespaceSystem)))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(len(l.Items)).To(Equal(1))
		})

	}
}

func TestReconcileIfJoinNodePoolsAndControlPlaneIsReady(t *testing.T) {
	g := NewWithT(t)

	_ = feature.MutableGates.Set("MachinePool=true")

	cluster := newCluster("cluster")
	cluster.Status.InfrastructureReady = true
	cluster.Status.ControlPlaneInitialized = true
	cluster.Spec.ControlPlaneEndpoint = clusterv1.APIEndpoint{Host: "100.105.150.1", Port: 6443}

	var useCases = []struct {
		name          string
		machinePool   *expv1.MachinePool
		configName    string
		configBuilder func(*expv1.MachinePool, string) *bootstrapv1.KubeadmIgnitionConfig
	}{
		{
			name:        "Join a worker node with a fully compiled kubeadm config object",
			machinePool: newWorkerMachinePool(cluster),
			configName:  "workerpool-join-cfg",
			configBuilder: func(machinePool *expv1.MachinePool, name string) *bootstrapv1.KubeadmIgnitionConfig {
				return newWorkerPoolJoinKubeadmIgnitionConfig(machinePool)
			},
		},
		{
			name:          "Join a worker node  with an empty kubeadm config object (defaults apply)",
			machinePool:   newWorkerMachinePool(cluster),
			configName:    "workerpool-join-cfg",
			configBuilder: newMachinePoolKubeadmIgnitionConfig,
		},
	}

	for _, rt := range useCases {
		rt := rt // pin!
		t.Run(rt.name, func(t *testing.T) {
			config := rt.configBuilder(rt.machinePool, rt.configName)

			objects := []runtime.Object{
				cluster,
				rt.machinePool,
				config,
			}
			objects = append(objects, createSecrets(t, cluster, config)...)
			myclient := fake.NewFakeClientWithScheme(setupScheme(), objects...)
			k := &KubeadmIgnitionConfigReconciler{
				Log:                log.Log,
				Client:             myclient,
				KubeadmInitLock:    &myInitLocker{},
				remoteClientGetter: fakeremote.NewClusterClient,
			}

			request := ctrl.Request{
				NamespacedName: client.ObjectKey{
					Namespace: config.GetNamespace(),
					Name:      rt.configName,
				},
			}
			result, err := k.Reconcile(request)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(result.Requeue).To(BeFalse())
			g.Expect(result.RequeueAfter).To(Equal(time.Duration(0)))

			cfg, err := getKubeadmIgnitionConfig(myclient, rt.configName)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(cfg.Status.Ready).To(BeTrue())
			g.Expect(cfg.Status.DataSecretName).NotTo(BeNil())

			l := &corev1.SecretList{}
			err = myclient.List(context.Background(), l, client.ListOption(client.InNamespace(metav1.NamespaceSystem)))
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(len(l.Items)).To(Equal(1))
		})

	}
}

func TestBootstrapTokenTTLExtension(t *testing.T) {
	g := NewWithT(t)

	cluster := newCluster("cluster")
	cluster.Status.InfrastructureReady = true
	cluster.Status.ControlPlaneInitialized = true
	cluster.Spec.ControlPlaneEndpoint = clusterv1.APIEndpoint{Host: "100.105.150.1", Port: 6443}

	controlPlaneInitMachine := newControlPlaneMachine(cluster, "control-plane-init-machine")
	initConfig := newControlPlaneInitKubeadmIgnitionConfig(controlPlaneInitMachine, "control-plane-init-config")
	workerMachine := newWorkerMachine(cluster)
	workerJoinConfig := newWorkerJoinKubeadmIgnitionConfig(workerMachine)
	controlPlaneJoinMachine := newControlPlaneMachine(cluster, "control-plane-join-machine")
	controlPlaneJoinConfig := newControlPlaneJoinKubeadmIgnitionConfig(controlPlaneJoinMachine, "control-plane-join-cfg")
	objects := []runtime.Object{
		cluster,
		workerMachine,
		workerJoinConfig,
		controlPlaneJoinMachine,
		controlPlaneJoinConfig,
	}

	objects = append(objects, createSecrets(t, cluster, initConfig)...)
	myclient := fake.NewFakeClientWithScheme(setupScheme(), objects...)
	k := &KubeadmIgnitionConfigReconciler{
		Log:                log.Log,
		Client:             myclient,
		KubeadmInitLock:    &myInitLocker{},
		remoteClientGetter: fakeremote.NewClusterClient,
	}
	request := ctrl.Request{
		NamespacedName: client.ObjectKey{
			Namespace: "default",
			Name:      "worker-join-cfg",
		},
	}
	result, err := k.Reconcile(request)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Requeue).To(BeFalse())
	g.Expect(result.RequeueAfter).To(Equal(time.Duration(0)))

	cfg, err := getKubeadmIgnitionConfig(myclient, "worker-join-cfg")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(cfg.Status.Ready).To(BeTrue())
	g.Expect(cfg.Status.DataSecretName).NotTo(BeNil())

	request = ctrl.Request{
		NamespacedName: client.ObjectKey{
			Namespace: "default",
			Name:      "control-plane-join-cfg",
		},
	}
	result, err = k.Reconcile(request)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Requeue).To(BeFalse())
	g.Expect(result.RequeueAfter).To(Equal(time.Duration(0)))

	cfg, err = getKubeadmIgnitionConfig(myclient, "control-plane-join-cfg")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(cfg.Status.Ready).To(BeTrue())
	g.Expect(cfg.Status.DataSecretName).NotTo(BeNil())

	l := &corev1.SecretList{}
	err = myclient.List(context.Background(), l, client.ListOption(client.InNamespace(metav1.NamespaceSystem)))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(len(l.Items)).To(Equal(2))

	// ensure that the token is refreshed...
	tokenExpires := make([][]byte, len(l.Items))

	for i, item := range l.Items {
		tokenExpires[i] = item.Data[bootstrapapi.BootstrapTokenExpirationKey]
	}

	<-time.After(1 * time.Second)

	for _, req := range []ctrl.Request{
		{
			NamespacedName: client.ObjectKey{
				Namespace: "default",
				Name:      "worker-join-cfg",
			},
		},
		{
			NamespacedName: client.ObjectKey{
				Namespace: "default",
				Name:      "control-plane-join-cfg",
			},
		},
	} {

		result, err := k.Reconcile(req)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result.RequeueAfter).NotTo(BeNumerically(">=", DefaultTokenTTL))
	}

	l = &corev1.SecretList{}
	err = myclient.List(context.Background(), l, client.ListOption(client.InNamespace(metav1.NamespaceSystem)))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(len(l.Items)).To(Equal(2))

	for i, item := range l.Items {
		g.Expect(bytes.Equal(tokenExpires[i], item.Data[bootstrapapi.BootstrapTokenExpirationKey])).To(BeFalse())
		tokenExpires[i] = item.Data[bootstrapapi.BootstrapTokenExpirationKey]
	}

	// ...until the infrastructure is marked "ready"
	workerMachine.Status.InfrastructureReady = true
	err = myclient.Update(context.Background(), workerMachine)
	g.Expect(err).NotTo(HaveOccurred())

	controlPlaneJoinMachine.Status.InfrastructureReady = true
	err = myclient.Update(context.Background(), controlPlaneJoinMachine)
	g.Expect(err).NotTo(HaveOccurred())

	<-time.After(1 * time.Second)

	for _, req := range []ctrl.Request{
		{
			NamespacedName: client.ObjectKey{
				Namespace: "default",
				Name:      "worker-join-cfg",
			},
		},
		{
			NamespacedName: client.ObjectKey{
				Namespace: "default",
				Name:      "control-plane-join-cfg",
			},
		},
	} {

		result, err := k.Reconcile(req)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result.Requeue).To(BeFalse())
		g.Expect(result.RequeueAfter).To(Equal(time.Duration(0)))
	}

	l = &corev1.SecretList{}
	err = myclient.List(context.Background(), l, client.ListOption(client.InNamespace(metav1.NamespaceSystem)))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(len(l.Items)).To(Equal(2))

	for i, item := range l.Items {
		g.Expect(bytes.Equal(tokenExpires[i], item.Data[bootstrapapi.BootstrapTokenExpirationKey])).To(BeTrue())
	}
}

// Ensure the discovery portion of the JoinConfiguration gets generated correctly.
func TestKubeadmIgnitionConfigReconciler_Reconcile_DisocveryReconcileBehaviors(t *testing.T) {
	g := NewWithT(t)

	k := &KubeadmIgnitionConfigReconciler{
		Log:                log.Log,
		Client:             fake.NewFakeClientWithScheme(setupScheme()),
		KubeadmInitLock:    &myInitLocker{},
		remoteClientGetter: fakeremote.NewClusterClient,
	}

	dummyCAHash := []string{"...."}
	bootstrapToken := kubeadmv1beta1.Discovery{
		BootstrapToken: &kubeadmv1beta1.BootstrapTokenDiscovery{
			CACertHashes: dummyCAHash,
		},
	}
	goodcluster := &clusterv1.Cluster{
		Spec: clusterv1.ClusterSpec{
			ControlPlaneEndpoint: clusterv1.APIEndpoint{
				Host: "example.com",
				Port: 6443,
			},
		},
	}
	testcases := []struct {
		name              string
		cluster           *clusterv1.Cluster
		config            *bootstrapv1.KubeadmIgnitionConfig
		validateDiscovery func(*bootstrapv1.KubeadmIgnitionConfig) error
	}{
		{
			name:    "Automatically generate token if discovery not specified",
			cluster: goodcluster,
			config: &bootstrapv1.KubeadmIgnitionConfig{
				Spec: bootstrapv1.KubeadmIgnitionConfigSpec{
					JoinConfiguration: &kubeadmv1beta1.JoinConfiguration{
						Discovery: bootstrapToken,
					},
				},
			},
			validateDiscovery: func(c *bootstrapv1.KubeadmIgnitionConfig) error {
				d := c.Spec.JoinConfiguration.Discovery
				g.Expect(d.BootstrapToken).NotTo(BeNil())
				g.Expect(d.BootstrapToken.Token).NotTo(Equal(""))
				g.Expect(d.BootstrapToken.APIServerEndpoint).To(Equal("example.com:6443"))
				g.Expect(d.BootstrapToken.UnsafeSkipCAVerification).To(BeFalse())
				return nil
			},
		},
		{
			name:    "Respect discoveryConfiguration.File",
			cluster: goodcluster,
			config: &bootstrapv1.KubeadmIgnitionConfig{
				Spec: bootstrapv1.KubeadmIgnitionConfigSpec{
					JoinConfiguration: &kubeadmv1beta1.JoinConfiguration{
						Discovery: kubeadmv1beta1.Discovery{
							File: &kubeadmv1beta1.FileDiscovery{},
						},
					},
				},
			},
			validateDiscovery: func(c *bootstrapv1.KubeadmIgnitionConfig) error {
				d := c.Spec.JoinConfiguration.Discovery
				g.Expect(d.BootstrapToken).To(BeNil())
				return nil
			},
		},
		{
			name:    "Respect discoveryConfiguration.BootstrapToken.APIServerEndpoint",
			cluster: goodcluster,
			config: &bootstrapv1.KubeadmIgnitionConfig{
				Spec: bootstrapv1.KubeadmIgnitionConfigSpec{
					JoinConfiguration: &kubeadmv1beta1.JoinConfiguration{
						Discovery: kubeadmv1beta1.Discovery{
							BootstrapToken: &kubeadmv1beta1.BootstrapTokenDiscovery{
								CACertHashes:      dummyCAHash,
								APIServerEndpoint: "bar.com:6443",
							},
						},
					},
				},
			},
			validateDiscovery: func(c *bootstrapv1.KubeadmIgnitionConfig) error {
				d := c.Spec.JoinConfiguration.Discovery
				g.Expect(d.BootstrapToken.APIServerEndpoint).To(Equal("bar.com:6443"))
				return nil
			},
		},
		{
			name:    "Respect discoveryConfiguration.BootstrapToken.Token",
			cluster: goodcluster,
			config: &bootstrapv1.KubeadmIgnitionConfig{
				Spec: bootstrapv1.KubeadmIgnitionConfigSpec{
					JoinConfiguration: &kubeadmv1beta1.JoinConfiguration{
						Discovery: kubeadmv1beta1.Discovery{
							BootstrapToken: &kubeadmv1beta1.BootstrapTokenDiscovery{
								CACertHashes: dummyCAHash,
								Token:        "abcdef.0123456789abcdef",
							},
						},
					},
				},
			},
			validateDiscovery: func(c *bootstrapv1.KubeadmIgnitionConfig) error {
				d := c.Spec.JoinConfiguration.Discovery
				g.Expect(d.BootstrapToken.Token).To(Equal("abcdef.0123456789abcdef"))
				return nil
			},
		},
		{
			name:    "Respect discoveryConfiguration.BootstrapToken.CACertHashes",
			cluster: goodcluster,
			config: &bootstrapv1.KubeadmIgnitionConfig{
				Spec: bootstrapv1.KubeadmIgnitionConfigSpec{
					JoinConfiguration: &kubeadmv1beta1.JoinConfiguration{
						Discovery: kubeadmv1beta1.Discovery{
							BootstrapToken: &kubeadmv1beta1.BootstrapTokenDiscovery{
								CACertHashes: dummyCAHash,
							},
						},
					},
				},
			},
			validateDiscovery: func(c *bootstrapv1.KubeadmIgnitionConfig) error {
				d := c.Spec.JoinConfiguration.Discovery
				g.Expect(reflect.DeepEqual(d.BootstrapToken.CACertHashes, dummyCAHash)).To(BeTrue())
				return nil
			},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			err := k.reconcileDiscovery(context.Background(), tc.cluster, tc.config, secret.Certificates{})
			g.Expect(err).NotTo(HaveOccurred())

			err = tc.validateDiscovery(tc.config)
			g.Expect(err).NotTo(HaveOccurred())
		})
	}
}

// Test failure cases for the discovery reconcile function.
func TestKubeadmIgnitionConfigReconciler_Reconcile_DisocveryReconcileFailureBehaviors(t *testing.T) {
	g := NewWithT(t)

	k := &KubeadmIgnitionConfigReconciler{
		Log:    log.Log,
		Client: nil,
	}

	testcases := []struct {
		name    string
		cluster *clusterv1.Cluster
		config  *bootstrapv1.KubeadmIgnitionConfig
	}{
		{
			name:    "Fail if cluster has not ControlPlaneEndpoint",
			cluster: &clusterv1.Cluster{}, // cluster without endpoints
			config: &bootstrapv1.KubeadmIgnitionConfig{
				Spec: bootstrapv1.KubeadmIgnitionConfigSpec{
					JoinConfiguration: &kubeadmv1beta1.JoinConfiguration{
						Discovery: kubeadmv1beta1.Discovery{
							BootstrapToken: &kubeadmv1beta1.BootstrapTokenDiscovery{
								CACertHashes: []string{"item"},
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			err := k.reconcileDiscovery(context.Background(), tc.cluster, tc.config, secret.Certificates{})
			g.Expect(err).To(HaveOccurred())
		})
	}
}

// Set cluster configuration defaults based on dynamic values from the cluster object.
func TestKubeadmIgnitionConfigReconciler_Reconcile_DynamicDefaultsForClusterConfiguration(t *testing.T) {
	g := NewWithT(t)

	k := &KubeadmIgnitionConfigReconciler{
		Log:    log.Log,
		Client: nil,
	}

	testcases := []struct {
		name    string
		cluster *clusterv1.Cluster
		machine *clusterv1.Machine
		config  *bootstrapv1.KubeadmIgnitionConfig
	}{
		{
			name: "Config settings have precedence",
			config: &bootstrapv1.KubeadmIgnitionConfig{
				Spec: bootstrapv1.KubeadmIgnitionConfigSpec{
					ClusterConfiguration: &kubeadmv1beta1.ClusterConfiguration{
						ClusterName:       "mycluster",
						KubernetesVersion: "myversion",
						Networking: kubeadmv1beta1.Networking{
							PodSubnet:     "myPodSubnet",
							ServiceSubnet: "myServiceSubnet",
							DNSDomain:     "myDNSDomain",
						},
						ControlPlaneEndpoint: "myControlPlaneEndpoint:6443",
					},
				},
			},
			cluster: &clusterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "OtherName",
				},
				Spec: clusterv1.ClusterSpec{
					ClusterNetwork: &clusterv1.ClusterNetwork{
						Services:      &clusterv1.NetworkRanges{CIDRBlocks: []string{"otherServicesCidr"}},
						Pods:          &clusterv1.NetworkRanges{CIDRBlocks: []string{"otherPodsCidr"}},
						ServiceDomain: "otherServiceDomain",
					},
					ControlPlaneEndpoint: clusterv1.APIEndpoint{Host: "otherVersion", Port: 0},
				},
			},
			machine: &clusterv1.Machine{
				Spec: clusterv1.MachineSpec{
					Version: pointer.StringPtr("otherVersion"),
				},
			},
		},
		{
			name: "Top level object settings are used in case config settings are missing",
			config: &bootstrapv1.KubeadmIgnitionConfig{
				Spec: bootstrapv1.KubeadmIgnitionConfigSpec{
					ClusterConfiguration: &kubeadmv1beta1.ClusterConfiguration{},
				},
			},
			cluster: &clusterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "mycluster",
				},
				Spec: clusterv1.ClusterSpec{
					ClusterNetwork: &clusterv1.ClusterNetwork{
						Services:      &clusterv1.NetworkRanges{CIDRBlocks: []string{"myServiceSubnet"}},
						Pods:          &clusterv1.NetworkRanges{CIDRBlocks: []string{"myPodSubnet"}},
						ServiceDomain: "myDNSDomain",
					},
					ControlPlaneEndpoint: clusterv1.APIEndpoint{Host: "myControlPlaneEndpoint", Port: 6443},
				},
			},
			machine: &clusterv1.Machine{
				Spec: clusterv1.MachineSpec{
					Version: pointer.StringPtr("myversion"),
				},
			},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			k.reconcileTopLevelObjectSettings(tc.cluster, tc.machine, tc.config)

			g.Expect(tc.config.Spec.ClusterConfiguration.ControlPlaneEndpoint).To(Equal("myControlPlaneEndpoint:6443"))
			g.Expect(tc.config.Spec.ClusterConfiguration.ClusterName).To(Equal("mycluster"))
			g.Expect(tc.config.Spec.ClusterConfiguration.Networking.PodSubnet).To(Equal("myPodSubnet"))
			g.Expect(tc.config.Spec.ClusterConfiguration.Networking.ServiceSubnet).To(Equal("myServiceSubnet"))
			g.Expect(tc.config.Spec.ClusterConfiguration.Networking.DNSDomain).To(Equal("myDNSDomain"))
			g.Expect(tc.config.Spec.ClusterConfiguration.KubernetesVersion).To(Equal("myversion"))
		})
	}
}

// Allow users to skip CA Verification if they *really* want to.
func TestKubeadmIgnitionConfigReconciler_Reconcile_AlwaysCheckCAVerificationUnlessRequestedToSkip(t *testing.T) {
	g := NewWithT(t)

	// Setup work for an initialized cluster
	clusterName := "my-cluster"
	cluster := newCluster(clusterName)
	cluster.Status.ControlPlaneInitialized = true
	cluster.Status.InfrastructureReady = true
	cluster.Spec.ControlPlaneEndpoint = clusterv1.APIEndpoint{
		Host: "example.com",
		Port: 6443,
	}
	controlPlaneInitMachine := newControlPlaneMachine(cluster, "my-control-plane-init-machine")
	initConfig := newControlPlaneInitKubeadmIgnitionConfig(controlPlaneInitMachine, "my-control-plane-init-config")

	controlPlaneMachineName := "my-machine"
	machine := newMachine(cluster, controlPlaneMachineName)

	workerMachineName := "my-worker"
	workerMachine := newMachine(cluster, workerMachineName)

	controlPlaneConfigName := "my-config"
	config := newKubeadmIgnitionConfig(machine, controlPlaneConfigName)

	objects := []runtime.Object{
		cluster, machine, workerMachine, config,
	}
	objects = append(objects, createSecrets(t, cluster, initConfig)...)

	testcases := []struct {
		name               string
		discovery          *kubeadmv1beta1.BootstrapTokenDiscovery
		skipCAVerification bool
	}{
		{
			name:               "Do not skip CA verification by default",
			discovery:          &kubeadmv1beta1.BootstrapTokenDiscovery{},
			skipCAVerification: false,
		},
		{
			name: "Skip CA verification if requested by the user",
			discovery: &kubeadmv1beta1.BootstrapTokenDiscovery{
				UnsafeSkipCAVerification: true,
			},
			skipCAVerification: true,
		},
		{
			// skipCAVerification should be true since no Cert Hashes are provided, but reconcile will *always* get or create certs.
			// TODO: Certificate get/create behavior needs to be mocked to enable this test.
			name: "cannot test for defaulting behavior through the reconcile function",
			discovery: &kubeadmv1beta1.BootstrapTokenDiscovery{
				CACertHashes: []string{""},
			},
			skipCAVerification: false,
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			myclient := fake.NewFakeClientWithScheme(setupScheme(), objects...)
			reconciler := KubeadmIgnitionConfigReconciler{
				Client:             myclient,
				KubeadmInitLock:    &myInitLocker{},
				Log:                klogr.New(),
				remoteClientGetter: fakeremote.NewClusterClient,
			}

			wc := newWorkerJoinKubeadmIgnitionConfig(workerMachine)
			wc.Spec.JoinConfiguration.Discovery.BootstrapToken = tc.discovery
			key := client.ObjectKey{Namespace: wc.Namespace, Name: wc.Name}
			err := myclient.Create(context.Background(), wc)
			g.Expect(err).NotTo(HaveOccurred())

			req := ctrl.Request{NamespacedName: key}
			_, err = reconciler.Reconcile(req)
			g.Expect(err).NotTo(HaveOccurred())

			cfg := &bootstrapv1.KubeadmIgnitionConfig{}
			err = myclient.Get(context.Background(), key, cfg)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(cfg.Spec.JoinConfiguration.Discovery.BootstrapToken.UnsafeSkipCAVerification).To(Equal(tc.skipCAVerification))
		})
	}
}

// If a cluster object changes then all associated KubeadmIgnitionConfigs should be re-reconciled.
// This allows us to not requeue a kubeadm config while we wait for InfrastructureReady.
func TestKubeadmIgnitionConfigReconciler_ClusterToKubeadmIgnitionConfigs(t *testing.T) {
	g := NewWithT(t)

	cluster := newCluster("my-cluster")
	objs := []runtime.Object{cluster}
	expectedNames := []string{}
	for i := 0; i < 3; i++ {
		m := newMachine(cluster, fmt.Sprintf("my-machine-%d", i))
		configName := fmt.Sprintf("my-config-%d", i)
		c := newKubeadmIgnitionConfig(m, configName)
		expectedNames = append(expectedNames, configName)
		objs = append(objs, m, c)
	}
	fakeClient := fake.NewFakeClientWithScheme(setupScheme(), objs...)
	reconciler := &KubeadmIgnitionConfigReconciler{
		Log:    log.Log,
		Client: fakeClient,
	}
	o := handler.MapObject{
		Object: cluster,
	}
	configs := reconciler.ClusterToKubeadmIgnitionConfigs(o)
	names := make([]string, 3)
	for i := range configs {
		names[i] = configs[i].Name
	}
	for _, name := range expectedNames {
		found := false
		for _, foundName := range names {
			if foundName == name {
				found = true
			}
		}
		g.Expect(found).To(BeTrue())
	}
}

// Reconcile should not fail if the Etcd CA Secret already exists
func TestKubeadmIgnitionConfigReconciler_Reconcile_DoesNotFailIfCASecretsAlreadyExist(t *testing.T) {
	g := NewWithT(t)

	cluster := newCluster("my-cluster")
	cluster.Status.InfrastructureReady = true
	cluster.Status.ControlPlaneInitialized = false
	m := newControlPlaneMachine(cluster, "control-plane-machine")
	configName := "my-config"
	c := newControlPlaneInitKubeadmIgnitionConfig(m, configName)
	scrt := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", cluster.Name, secret.EtcdCA),
			Namespace: "default",
		},
		Data: map[string][]byte{
			"tls.crt": []byte("hello world"),
			"tls.key": []byte("hello world"),
		},
	}
	fakec := fake.NewFakeClientWithScheme(setupScheme(), []runtime.Object{cluster, m, c, scrt}...)
	reconciler := &KubeadmIgnitionConfigReconciler{
		Log:             log.Log,
		Client:          fakec,
		KubeadmInitLock: &myInitLocker{},
	}
	req := ctrl.Request{
		NamespacedName: client.ObjectKey{Namespace: "default", Name: configName},
	}
	_, err := reconciler.Reconcile(req)
	g.Expect(err).NotTo(HaveOccurred())
}

// Exactly one control plane machine initializes if there are multiple control plane machines defined
func TestKubeadmIgnitionConfigReconciler_Reconcile_ExactlyOneControlPlaneMachineInitializes(t *testing.T) {
	g := NewWithT(t)

	cluster := newCluster("cluster")
	cluster.Status.InfrastructureReady = true

	controlPlaneInitMachineFirst := newControlPlaneMachine(cluster, "control-plane-init-machine-first")
	controlPlaneInitConfigFirst := newControlPlaneInitKubeadmIgnitionConfig(controlPlaneInitMachineFirst, "control-plane-init-cfg-first")

	controlPlaneInitMachineSecond := newControlPlaneMachine(cluster, "control-plane-init-machine-second")
	controlPlaneInitConfigSecond := newControlPlaneInitKubeadmIgnitionConfig(controlPlaneInitMachineSecond, "control-plane-init-cfg-second")

	objects := []runtime.Object{
		cluster,
		controlPlaneInitMachineFirst,
		controlPlaneInitConfigFirst,
		controlPlaneInitMachineSecond,
		controlPlaneInitConfigSecond,
	}
	myclient := fake.NewFakeClientWithScheme(setupScheme(), objects...)
	k := &KubeadmIgnitionConfigReconciler{
		Log:             log.Log,
		Client:          myclient,
		KubeadmInitLock: &myInitLocker{},
	}

	request := ctrl.Request{
		NamespacedName: client.ObjectKey{
			Namespace: "default",
			Name:      "control-plane-init-cfg-first",
		},
	}
	result, err := k.Reconcile(request)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Requeue).To(BeFalse())
	g.Expect(result.RequeueAfter).To(Equal(time.Duration(0)))

	request = ctrl.Request{
		NamespacedName: client.ObjectKey{
			Namespace: "default",
			Name:      "control-plane-init-cfg-second",
		},
	}
	result, err = k.Reconcile(request)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Requeue).To(BeFalse())
	g.Expect(result.RequeueAfter).To(Equal(30 * time.Second))
}

// No patch should be applied if there is an error in reconcile
func TestKubeadmIgnitionConfigReconciler_Reconcile_DoNotPatchWhenErrorOccurred(t *testing.T) {
	g := NewWithT(t)

	cluster := newCluster("cluster")
	cluster.Status.InfrastructureReady = true

	controlPlaneInitMachine := newControlPlaneMachine(cluster, "control-plane-init-machine")
	controlPlaneInitConfig := newControlPlaneInitKubeadmIgnitionConfig(controlPlaneInitMachine, "control-plane-init-cfg")

	// set InitConfiguration as nil, we will check this to determine if the kubeadm config has been patched
	controlPlaneInitConfig.Spec.InitConfiguration = nil

	objects := []runtime.Object{
		cluster,
		controlPlaneInitMachine,
		controlPlaneInitConfig,
	}

	secrets := createSecrets(t, cluster, controlPlaneInitConfig)
	for _, obj := range secrets {
		s := obj.(*corev1.Secret)
		delete(s.Data, secret.TLSCrtDataName) // destroy the secrets, which will cause Reconcile to fail
		objects = append(objects, s)
	}

	myclient := fake.NewFakeClientWithScheme(setupScheme(), objects...)
	k := &KubeadmIgnitionConfigReconciler{
		Log:             log.Log,
		Client:          myclient,
		KubeadmInitLock: &myInitLocker{},
	}

	request := ctrl.Request{
		NamespacedName: client.ObjectKey{
			Namespace: "default",
			Name:      "control-plane-init-cfg",
		},
	}

	result, err := k.Reconcile(request)
	g.Expect(err).To(HaveOccurred())
	g.Expect(result.Requeue).To(BeFalse())
	g.Expect(result.RequeueAfter).To(Equal(time.Duration(0)))

	cfg, err := getKubeadmIgnitionConfig(myclient, "control-plane-init-cfg")
	g.Expect(err).NotTo(HaveOccurred())
	// check if the kubeadm config has been patched
	g.Expect(cfg.Spec.InitConfiguration).To(BeNil())
}

// test utils

// newCluster return a CAPI cluster object
func newCluster(name string) *clusterv1.Cluster {
	return &clusterv1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Cluster",
			APIVersion: clusterv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      name,
		},
	}
}

// newMachine return a CAPI machine object; if cluster is not nil, the machine is linked to the cluster as well
func newMachine(cluster *clusterv1.Cluster, name string) *clusterv1.Machine {
	machine := &clusterv1.Machine{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Machine",
			APIVersion: clusterv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      name,
		},
		Spec: clusterv1.MachineSpec{
			Bootstrap: clusterv1.Bootstrap{
				ConfigRef: &corev1.ObjectReference{
					Kind:       "KubeadmIgnitionConfig",
					APIVersion: bootstrapv1.GroupVersion.String(),
				},
			},
		},
	}
	if cluster != nil {
		machine.Spec.ClusterName = cluster.Name
		machine.ObjectMeta.Labels = map[string]string{
			clusterv1.ClusterLabelName: cluster.Name,
		}
	}
	return machine
}

func newWorkerMachine(cluster *clusterv1.Cluster) *clusterv1.Machine {
	return newMachine(cluster, "worker-machine") // machine by default is a worker node (not the bootstrapNode)
}

func newControlPlaneMachine(cluster *clusterv1.Cluster, name string) *clusterv1.Machine {
	m := newMachine(cluster, name)
	m.Labels[clusterv1.MachineControlPlaneLabelName] = ""
	return m
}

// newMachinePool return a CAPI machine pool object; if cluster is not nil, the machine pool is linked to the cluster as well
func newMachinePool(cluster *clusterv1.Cluster, name string) *expv1.MachinePool {
	machine := &expv1.MachinePool{
		TypeMeta: metav1.TypeMeta{
			Kind:       "MachinePool",
			APIVersion: expv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      name,
		},
		Spec: expv1.MachinePoolSpec{
			Template: clusterv1.MachineTemplateSpec{
				Spec: clusterv1.MachineSpec{
					Bootstrap: clusterv1.Bootstrap{
						ConfigRef: &corev1.ObjectReference{
							Kind:       "KubeadmIgnitionConfig",
							APIVersion: bootstrapv1.GroupVersion.String(),
						},
					},
				},
			},
		},
	}
	if cluster != nil {
		machine.Spec.ClusterName = cluster.Name
		machine.ObjectMeta.Labels = map[string]string{
			clusterv1.ClusterLabelName: cluster.Name,
		}
	}
	return machine
}

func newWorkerMachinePool(cluster *clusterv1.Cluster) *expv1.MachinePool {
	return newMachinePool(cluster, "worker-machinepool")
}

// newKubeadmIgnitionConfig return a CABPK KubeadmIgnitionConfig object; if machine is not nil, the KubeadmIgnitionConfig is linked to the machine as well
func newKubeadmIgnitionConfig(machine *clusterv1.Machine, name string) *bootstrapv1.KubeadmIgnitionConfig {
	config := &bootstrapv1.KubeadmIgnitionConfig{
		TypeMeta: metav1.TypeMeta{
			Kind:       "KubeadmIgnitionConfig",
			APIVersion: bootstrapv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      name,
		},
	}
	if machine != nil {
		config.ObjectMeta.OwnerReferences = []metav1.OwnerReference{
			{
				Kind:       "Machine",
				APIVersion: clusterv1.GroupVersion.String(),
				Name:       machine.Name,
				UID:        types.UID(fmt.Sprintf("%s uid", machine.Name)),
			},
		}
		machine.Spec.Bootstrap.ConfigRef.Name = config.Name
		machine.Spec.Bootstrap.ConfigRef.Namespace = config.Namespace
	}
	return config
}

func newWorkerJoinKubeadmIgnitionConfig(machine *clusterv1.Machine) *bootstrapv1.KubeadmIgnitionConfig {
	c := newKubeadmIgnitionConfig(machine, "worker-join-cfg")
	c.Spec.JoinConfiguration = &kubeadmv1beta1.JoinConfiguration{
		ControlPlane: nil,
	}
	return c
}

func newControlPlaneJoinKubeadmIgnitionConfig(machine *clusterv1.Machine, name string) *bootstrapv1.KubeadmIgnitionConfig {
	c := newKubeadmIgnitionConfig(machine, name)
	c.Spec.JoinConfiguration = &kubeadmv1beta1.JoinConfiguration{
		ControlPlane: &kubeadmv1beta1.JoinControlPlane{},
	}
	return c
}

func newControlPlaneInitKubeadmIgnitionConfig(machine *clusterv1.Machine, name string) *bootstrapv1.KubeadmIgnitionConfig {
	c := newKubeadmIgnitionConfig(machine, name)
	c.Spec.ClusterConfiguration = &kubeadmv1beta1.ClusterConfiguration{}
	c.Spec.InitConfiguration = &kubeadmv1beta1.InitConfiguration{}
	return c
}

// newMachinePoolKubeadmIgnitionConfig return a CABPK KubeadmIgnitionConfig object; if machine pool is not nil,
// the KubeadmIgnitionConfig is linked to the machine pool as well
func newMachinePoolKubeadmIgnitionConfig(machinePool *expv1.MachinePool, name string) *bootstrapv1.KubeadmIgnitionConfig {
	config := &bootstrapv1.KubeadmIgnitionConfig{
		TypeMeta: metav1.TypeMeta{
			Kind:       "KubeadmIgnitionConfig",
			APIVersion: bootstrapv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      name,
		},
	}
	if machinePool != nil {
		config.ObjectMeta.OwnerReferences = []metav1.OwnerReference{
			{
				Kind:       "MachinePool",
				APIVersion: expv1.GroupVersion.String(),
				Name:       machinePool.Name,
				UID:        types.UID(fmt.Sprintf("%s uid", machinePool.Name)),
			},
		}
		machinePool.Spec.Template.Spec.Bootstrap.ConfigRef.Name = config.Name
		machinePool.Spec.Template.Spec.Bootstrap.ConfigRef.Namespace = config.Namespace
	}
	return config
}

func newWorkerPoolJoinKubeadmIgnitionConfig(machinePool *expv1.MachinePool) *bootstrapv1.KubeadmIgnitionConfig {
	c := newMachinePoolKubeadmIgnitionConfig(machinePool, "workerpool-join-cfg")
	c.Spec.JoinConfiguration = &kubeadmv1beta1.JoinConfiguration{
		ControlPlane: nil,
	}
	return c
}

func createSecrets(t *testing.T, cluster *clusterv1.Cluster, config *bootstrapv1.KubeadmIgnitionConfig) []runtime.Object {
	out := []runtime.Object{}
	if config.Spec.ClusterConfiguration == nil {
		config.Spec.ClusterConfiguration = &kubeadmv1beta1.ClusterConfiguration{}
	}
	certificates := secret.NewCertificatesForInitialControlPlane(config.Spec.ClusterConfiguration)
	if err := certificates.Generate(); err != nil {
		t.Fatal(err)
	}
	for _, certificate := range certificates {
		out = append(out, certificate.AsSecret(util.ObjectKey(cluster), *metav1.NewControllerRef(config, bootstrapv1.GroupVersion.WithKind("KubeadmIgnitionConfig"))))
	}
	return out
}

type myInitLocker struct {
	locked bool
}

func (m *myInitLocker) Lock(_ context.Context, _ *clusterv1.Cluster, _ *clusterv1.Machine) bool {
	if !m.locked {
		m.locked = true
		return true
	}
	return false
}

func (m *myInitLocker) Unlock(_ context.Context, _ *clusterv1.Cluster) bool {
	if m.locked {
		m.locked = false
	}
	return true
}
