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
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	expv1 "sigs.k8s.io/cluster-api/exp/api/v1alpha4"
	"sigs.k8s.io/cluster-api/feature"

	"github.com/blang/semver"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2/klogr"
	"k8s.io/utils/pointer"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	bootstrapv1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1alpha4"
	"sigs.k8s.io/cluster-api/controllers/external"
	controlplanev1 "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1alpha4"
	"sigs.k8s.io/cluster-api/controlplane/kubeadm/internal"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/collections"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/kubeconfig"
	"sigs.k8s.io/cluster-api/util/secret"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func TestClusterToKubeadmControlPlane(t *testing.T) {
	g := NewWithT(t)
	fakeClient := newFakeClient()

	cluster := newCluster(&types.NamespacedName{Name: "foo", Namespace: metav1.NamespaceDefault})
	cluster.Spec = clusterv1.ClusterSpec{
		ControlPlaneRef: &corev1.ObjectReference{
			Kind:       "KubeadmControlPlane",
			Namespace:  metav1.NamespaceDefault,
			Name:       "kcp-foo",
			APIVersion: controlplanev1.GroupVersion.String(),
		},
	}

	expectedResult := []ctrl.Request{
		{
			NamespacedName: client.ObjectKey{
				Namespace: cluster.Spec.ControlPlaneRef.Namespace,
				Name:      cluster.Spec.ControlPlaneRef.Name},
		},
	}

	r := &KubeadmControlPlaneReconciler{
		Client:   fakeClient,
		recorder: record.NewFakeRecorder(32),
	}

	got := r.ClusterToKubeadmControlPlane(cluster)
	g.Expect(got).To(Equal(expectedResult))
}

func TestClusterToKubeadmControlPlaneNoControlPlane(t *testing.T) {
	g := NewWithT(t)
	fakeClient := newFakeClient()

	cluster := newCluster(&types.NamespacedName{Name: "foo", Namespace: metav1.NamespaceDefault})

	r := &KubeadmControlPlaneReconciler{
		Client:   fakeClient,
		recorder: record.NewFakeRecorder(32),
	}

	got := r.ClusterToKubeadmControlPlane(cluster)
	g.Expect(got).To(BeNil())
}

func TestClusterToKubeadmControlPlaneOtherControlPlane(t *testing.T) {
	g := NewWithT(t)
	fakeClient := newFakeClient()

	cluster := newCluster(&types.NamespacedName{Name: "foo", Namespace: metav1.NamespaceDefault})
	cluster.Spec = clusterv1.ClusterSpec{
		ControlPlaneRef: &corev1.ObjectReference{
			Kind:       "OtherControlPlane",
			Namespace:  metav1.NamespaceDefault,
			Name:       "other-foo",
			APIVersion: controlplanev1.GroupVersion.String(),
		},
	}

	r := &KubeadmControlPlaneReconciler{
		Client:   fakeClient,
		recorder: record.NewFakeRecorder(32),
	}

	got := r.ClusterToKubeadmControlPlane(cluster)
	g.Expect(got).To(BeNil())
}

func TestReconcileReturnErrorWhenOwnerClusterIsMissing(t *testing.T) {
	g := NewWithT(t)

	ns, err := env.CreateNamespace(ctx, "test-reconcile-return-error")
	g.Expect(err).ToNot(HaveOccurred())

	cluster, kcp, _ := createClusterWithControlPlane(ns.Name)
	g.Expect(env.Create(ctx, cluster)).To(Succeed())
	g.Expect(env.Create(ctx, kcp)).To(Succeed())
	defer func(do ...client.Object) {
		g.Expect(env.Cleanup(ctx, do...)).To(Succeed())
	}(kcp, ns)

	r := &KubeadmControlPlaneReconciler{
		Client:   env,
		recorder: record.NewFakeRecorder(32),
	}

	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(kcp)})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result).To(Equal(ctrl.Result{}))

	// calling reconcile should return error
	g.Expect(env.Delete(ctx, cluster)).To(Succeed())

	g.Eventually(func() error {
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(kcp)})
		return err
	}, 10*time.Second).Should(HaveOccurred())
}

func TestReconcileUpdateObservedGeneration(t *testing.T) {
	t.Skip("Disabling this test temporarily until we can get a fix for https://github.com/kubernetes/kubernetes/issues/80609 in controller runtime + switch to a live client in test env.")

	g := NewWithT(t)
	r := &KubeadmControlPlaneReconciler{
		Client:            env,
		recorder:          record.NewFakeRecorder(32),
		managementCluster: &internal.Management{Client: env.Client, Tracker: nil},
	}

	ns, err := env.CreateNamespace(ctx, "test-reconcile-upd-og")
	g.Expect(err).ToNot(HaveOccurred())

	cluster, kcp, _ := createClusterWithControlPlane(ns.Name)
	g.Expect(env.Create(ctx, cluster)).To(Succeed())
	g.Expect(env.Create(ctx, kcp)).To(Succeed())
	defer func(do ...client.Object) {
		g.Expect(env.Cleanup(ctx, do...)).To(Succeed())
	}(cluster, kcp, ns)

	// read kcp.Generation after create
	errGettingObject := env.Get(ctx, util.ObjectKey(kcp), kcp)
	g.Expect(errGettingObject).NotTo(HaveOccurred())
	generation := kcp.Generation

	// Set cluster.status.InfrastructureReady so we actually enter in the reconcile loop
	patch := client.RawPatch(types.MergePatchType, []byte(fmt.Sprintf("{\"status\":{\"infrastructureReady\":%t}}", true)))
	g.Expect(env.Status().Patch(ctx, cluster, patch)).To(Succeed())

	// call reconcile the first time, so we can check if observedGeneration is set when adding a finalizer
	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(kcp)})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result).To(Equal(ctrl.Result{}))

	g.Eventually(func() int64 {
		errGettingObject = env.Get(ctx, util.ObjectKey(kcp), kcp)
		g.Expect(errGettingObject).NotTo(HaveOccurred())
		return kcp.Status.ObservedGeneration
	}, 10*time.Second).Should(Equal(generation))

	// triggers a generation change by changing the spec
	kcp.Spec.Replicas = pointer.Int32Ptr(*kcp.Spec.Replicas + 2)
	g.Expect(env.Update(ctx, kcp)).To(Succeed())

	// read kcp.Generation after the update
	errGettingObject = env.Get(ctx, util.ObjectKey(kcp), kcp)
	g.Expect(errGettingObject).NotTo(HaveOccurred())
	generation = kcp.Generation

	// call reconcile the second time, so we can check if observedGeneration is set when calling defer patch
	// NB. The call to reconcile fails because KCP is not properly setup (e.g. missing InfrastructureTemplate)
	// but this is not important because what we want is KCP to be patched
	_, _ = r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(kcp)})

	g.Eventually(func() int64 {
		errGettingObject = env.Get(ctx, util.ObjectKey(kcp), kcp)
		g.Expect(errGettingObject).NotTo(HaveOccurred())
		return kcp.Status.ObservedGeneration
	}, 10*time.Second).Should(Equal(generation))
}

func TestReconcileNoClusterOwnerRef(t *testing.T) {
	g := NewWithT(t)

	kcp := &controlplanev1.KubeadmControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: metav1.NamespaceDefault,
			Name:      "foo",
		},
		Spec: controlplanev1.KubeadmControlPlaneSpec{
			Version: "v1.16.6",
			MachineTemplate: controlplanev1.KubeadmControlPlaneMachineTemplate{
				InfrastructureRef: corev1.ObjectReference{
					Kind:       "UnknownInfraMachine",
					APIVersion: "test/v1alpha1",
					Name:       "foo",
					Namespace:  metav1.NamespaceDefault,
				},
			},
		},
	}
	kcp.Default()
	g.Expect(kcp.ValidateCreate()).To(Succeed())

	fakeClient := newFakeClient(kcp.DeepCopy())
	r := &KubeadmControlPlaneReconciler{
		Client:   fakeClient,
		recorder: record.NewFakeRecorder(32),
	}

	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(kcp)})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result).To(Equal(ctrl.Result{}))

	machineList := &clusterv1.MachineList{}
	g.Expect(fakeClient.List(ctx, machineList, client.InNamespace(metav1.NamespaceDefault))).To(Succeed())
	g.Expect(machineList.Items).To(BeEmpty())
}

func TestReconcileNoKCP(t *testing.T) {
	g := NewWithT(t)

	kcp := &controlplanev1.KubeadmControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: metav1.NamespaceDefault,
			Name:      "foo",
		},
		Spec: controlplanev1.KubeadmControlPlaneSpec{
			Version: "v1.16.6",
			MachineTemplate: controlplanev1.KubeadmControlPlaneMachineTemplate{
				InfrastructureRef: corev1.ObjectReference{
					Kind:       "UnknownInfraMachine",
					APIVersion: "test/v1alpha1",
					Name:       "foo",
					Namespace:  metav1.NamespaceDefault,
				},
			},
		},
	}

	fakeClient := newFakeClient()
	r := &KubeadmControlPlaneReconciler{
		Client:   fakeClient,
		recorder: record.NewFakeRecorder(32),
	}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(kcp)})
	g.Expect(err).NotTo(HaveOccurred())
}

func TestReconcileNoCluster(t *testing.T) {
	g := NewWithT(t)

	kcp := &controlplanev1.KubeadmControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: metav1.NamespaceDefault,
			Name:      "foo",
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind:       "Cluster",
					APIVersion: clusterv1.GroupVersion.String(),
					Name:       "foo",
				},
			},
		},
		Spec: controlplanev1.KubeadmControlPlaneSpec{
			Version: "v1.16.6",
			MachineTemplate: controlplanev1.KubeadmControlPlaneMachineTemplate{
				InfrastructureRef: corev1.ObjectReference{
					Kind:       "UnknownInfraMachine",
					APIVersion: "test/v1alpha1",
					Name:       "foo",
					Namespace:  metav1.NamespaceDefault,
				},
			},
		},
	}
	kcp.Default()
	g.Expect(kcp.ValidateCreate()).To(Succeed())

	fakeClient := newFakeClient(kcp.DeepCopy())
	r := &KubeadmControlPlaneReconciler{
		Client:   fakeClient,
		recorder: record.NewFakeRecorder(32),
	}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(kcp)})
	g.Expect(err).To(HaveOccurred())

	machineList := &clusterv1.MachineList{}
	g.Expect(fakeClient.List(ctx, machineList, client.InNamespace(metav1.NamespaceDefault))).To(Succeed())
	g.Expect(machineList.Items).To(BeEmpty())
}

func TestReconcilePaused(t *testing.T) {
	g := NewWithT(t)

	clusterName := "foo"

	// Test: cluster is paused and kcp is not
	cluster := newCluster(&types.NamespacedName{Namespace: metav1.NamespaceDefault, Name: clusterName})
	cluster.Spec.Paused = true
	kcp := &controlplanev1.KubeadmControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: metav1.NamespaceDefault,
			Name:      clusterName,
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind:       "Cluster",
					APIVersion: clusterv1.GroupVersion.String(),
					Name:       clusterName,
				},
			},
		},
		Spec: controlplanev1.KubeadmControlPlaneSpec{
			Version: "v1.16.6",
			MachineTemplate: controlplanev1.KubeadmControlPlaneMachineTemplate{
				InfrastructureRef: corev1.ObjectReference{
					Kind:       "UnknownInfraMachine",
					APIVersion: "test/v1alpha1",
					Name:       "foo",
					Namespace:  metav1.NamespaceDefault,
				},
			},
		},
	}
	kcp.Default()
	g.Expect(kcp.ValidateCreate()).To(Succeed())
	fakeClient := newFakeClient(kcp.DeepCopy(), cluster.DeepCopy())
	r := &KubeadmControlPlaneReconciler{
		Client:   fakeClient,
		recorder: record.NewFakeRecorder(32),
	}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(kcp)})
	g.Expect(err).NotTo(HaveOccurred())

	machineList := &clusterv1.MachineList{}
	g.Expect(fakeClient.List(ctx, machineList, client.InNamespace(metav1.NamespaceDefault))).To(Succeed())
	g.Expect(machineList.Items).To(BeEmpty())

	// Test: kcp is paused and cluster is not
	cluster.Spec.Paused = false
	kcp.ObjectMeta.Annotations = map[string]string{}
	kcp.ObjectMeta.Annotations[clusterv1.PausedAnnotation] = "paused"
	_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(kcp)})
	g.Expect(err).NotTo(HaveOccurred())
}

func TestReconcileClusterNoEndpoints(t *testing.T) {
	g := NewWithT(t)

	cluster := newCluster(&types.NamespacedName{Name: "foo", Namespace: metav1.NamespaceDefault})
	cluster.Status = clusterv1.ClusterStatus{InfrastructureReady: true}

	kcp := &controlplanev1.KubeadmControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Namespace,
			Name:      "foo",
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind:       "Cluster",
					APIVersion: clusterv1.GroupVersion.String(),
					Name:       cluster.Name,
				},
			},
		},
		Spec: controlplanev1.KubeadmControlPlaneSpec{
			Version: "v1.16.6",
			MachineTemplate: controlplanev1.KubeadmControlPlaneMachineTemplate{
				InfrastructureRef: corev1.ObjectReference{
					Kind:       "UnknownInfraMachine",
					APIVersion: "test/v1alpha1",
					Name:       "foo",
					Namespace:  metav1.NamespaceDefault,
				},
			},
		},
	}
	kcp.Default()
	g.Expect(kcp.ValidateCreate()).To(Succeed())

	fakeClient := newFakeClient(kcp.DeepCopy(), cluster.DeepCopy())
	r := &KubeadmControlPlaneReconciler{
		Client:   fakeClient,
		recorder: record.NewFakeRecorder(32),
		managementCluster: &fakeManagementCluster{
			Management: &internal.Management{Client: fakeClient},
			Workload:   fakeWorkloadCluster{},
		},
	}

	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(kcp)})
	g.Expect(err).NotTo(HaveOccurred())
	// this first requeue is to add finalizer
	g.Expect(result).To(Equal(ctrl.Result{}))
	g.Expect(r.Client.Get(ctx, util.ObjectKey(kcp), kcp)).To(Succeed())
	g.Expect(kcp.Finalizers).To(ContainElement(controlplanev1.KubeadmControlPlaneFinalizer))

	result, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(kcp)})
	g.Expect(err).NotTo(HaveOccurred())
	// TODO: this should stop to re-queue as soon as we have a proper remote cluster cache in place.
	g.Expect(result).To(Equal(ctrl.Result{Requeue: false, RequeueAfter: 20 * time.Second}))
	g.Expect(r.Client.Get(ctx, util.ObjectKey(kcp), kcp)).To(Succeed())

	// Always expect that the Finalizer is set on the passed in resource
	g.Expect(kcp.Finalizers).To(ContainElement(controlplanev1.KubeadmControlPlaneFinalizer))

	g.Expect(kcp.Status.Selector).NotTo(BeEmpty())

	_, err = secret.GetFromNamespacedName(ctx, fakeClient, client.ObjectKey{Namespace: metav1.NamespaceDefault, Name: "foo"}, secret.ClusterCA)
	g.Expect(err).NotTo(HaveOccurred())

	machineList := &clusterv1.MachineList{}
	g.Expect(fakeClient.List(ctx, machineList, client.InNamespace(metav1.NamespaceDefault))).To(Succeed())
	g.Expect(machineList.Items).To(BeEmpty())
}

func TestKubeadmControlPlaneReconciler_adoption(t *testing.T) {
	version := "v2.0.0"
	t.Run("adopts existing Machines", func(t *testing.T) {
		g := NewWithT(t)

		cluster, kcp, tmpl := createClusterWithControlPlane(metav1.NamespaceDefault)
		cluster.Spec.ControlPlaneEndpoint.Host = "bar"
		cluster.Spec.ControlPlaneEndpoint.Port = 6443
		kcp.Spec.Version = version

		fmc := &fakeManagementCluster{
			Machines: collections.Machines{},
			Workload: fakeWorkloadCluster{},
		}
		objs := []client.Object{cluster.DeepCopy(), kcp.DeepCopy(), tmpl.DeepCopy()}
		for i := 0; i < 3; i++ {
			name := fmt.Sprintf("test-%d", i)
			m := &clusterv1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: cluster.Namespace,
					Name:      name,
					Labels:    internal.ControlPlaneMachineLabelsForCluster(kcp, cluster.Name),
				},
				Spec: clusterv1.MachineSpec{
					Bootstrap: clusterv1.Bootstrap{
						ConfigRef: &corev1.ObjectReference{
							APIVersion: bootstrapv1.GroupVersion.String(),
							Kind:       "KubeadmConfig",
							Name:       name,
						},
					},
					Version: &version,
				},
			}
			cfg := &bootstrapv1.KubeadmConfig{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: cluster.Namespace,
					Name:      name,
				},
			}
			objs = append(objs, m, cfg)
			fmc.Machines.Insert(m)
		}

		fakeClient := newFakeClient(objs...)
		fmc.Reader = fakeClient
		r := &KubeadmControlPlaneReconciler{
			Client:                    fakeClient,
			managementCluster:         fmc,
			managementClusterUncached: fmc,
		}

		g.Expect(r.reconcile(ctx, cluster, kcp)).To(Equal(ctrl.Result{}))

		machineList := &clusterv1.MachineList{}
		g.Expect(fakeClient.List(ctx, machineList, client.InNamespace(cluster.Namespace))).To(Succeed())
		g.Expect(machineList.Items).To(HaveLen(3))
		for _, machine := range machineList.Items {
			g.Expect(machine.OwnerReferences).To(HaveLen(1))
			g.Expect(machine.OwnerReferences).To(ContainElement(*metav1.NewControllerRef(kcp, controlplanev1.GroupVersion.WithKind("KubeadmControlPlane"))))
			// Machines are adopted but since they are not originally created by KCP, infra template annotation will be missing.
			g.Expect(machine.GetAnnotations()).NotTo(HaveKey(clusterv1.TemplateClonedFromGroupKindAnnotation))
			g.Expect(machine.GetAnnotations()).NotTo(HaveKey(clusterv1.TemplateClonedFromNameAnnotation))
		}
	})
	t.Run("adopts v1alpha2 cluster secrets", func(t *testing.T) {
		g := NewWithT(t)

		cluster, kcp, tmpl := createClusterWithControlPlane(metav1.NamespaceDefault)
		cluster.Spec.ControlPlaneEndpoint.Host = "bar"
		cluster.Spec.ControlPlaneEndpoint.Port = 6443
		kcp.Spec.Version = version

		fmc := &fakeManagementCluster{
			Machines: collections.Machines{},
			Workload: fakeWorkloadCluster{},
		}
		objs := []client.Object{cluster.DeepCopy(), kcp.DeepCopy(), tmpl.DeepCopy()}
		for i := 0; i < 3; i++ {
			name := fmt.Sprintf("test-%d", i)
			m := &clusterv1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: cluster.Namespace,
					Name:      name,
					Labels:    internal.ControlPlaneMachineLabelsForCluster(kcp, cluster.Name),
				},
				Spec: clusterv1.MachineSpec{
					Bootstrap: clusterv1.Bootstrap{
						ConfigRef: &corev1.ObjectReference{
							APIVersion: bootstrapv1.GroupVersion.String(),
							Kind:       "KubeadmConfig",
							Name:       name,
						},
					},
					Version: &version,
				},
			}
			cfg := &bootstrapv1.KubeadmConfig{
				TypeMeta: metav1.TypeMeta{
					APIVersion: bootstrapv1.GroupVersion.String(),
					Kind:       "KubeadmConfig",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: cluster.Namespace,
					Name:      name,
					UID:       types.UID(fmt.Sprintf("my-uid-%d", i)),
				},
			}

			// A simulcrum of the various Certificate and kubeconfig secrets
			// it's a little weird that this is one per KubeadmConfig rather than just whichever config was "first,"
			// but the intent is to ensure that the owner is changed regardless of which Machine we start with
			clusterSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: cluster.Namespace,
					Name:      fmt.Sprintf("important-cluster-secret-%d", i),
					Labels: map[string]string{
						"cluster.x-k8s.io/cluster-name": cluster.Name,
						"previous-owner":                "kubeadmconfig",
					},
					// See: https://github.com/kubernetes-sigs/cluster-api-bootstrap-provider-kubeadm/blob/38af74d92056e64e571b9ea1d664311769779453/internal/cluster/certificates.go#L323-L330
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: bootstrapv1.GroupVersion.String(),
							Kind:       "KubeadmConfig",
							Name:       cfg.Name,
							UID:        cfg.UID,
						},
					},
				},
			}
			objs = append(objs, m, cfg, clusterSecret)
			fmc.Machines.Insert(m)
		}

		fakeClient := newFakeClient(objs...)
		fmc.Reader = fakeClient
		r := &KubeadmControlPlaneReconciler{
			Client:                    fakeClient,
			managementCluster:         fmc,
			managementClusterUncached: fmc,
		}

		g.Expect(r.reconcile(ctx, cluster, kcp)).To(Equal(ctrl.Result{}))

		machineList := &clusterv1.MachineList{}
		g.Expect(fakeClient.List(ctx, machineList, client.InNamespace(cluster.Namespace))).To(Succeed())
		g.Expect(machineList.Items).To(HaveLen(3))
		for _, machine := range machineList.Items {
			g.Expect(machine.OwnerReferences).To(HaveLen(1))
			g.Expect(machine.OwnerReferences).To(ContainElement(*metav1.NewControllerRef(kcp, controlplanev1.GroupVersion.WithKind("KubeadmControlPlane"))))
			// Machines are adopted but since they are not originally created by KCP, infra template annotation will be missing.
			g.Expect(machine.GetAnnotations()).NotTo(HaveKey(clusterv1.TemplateClonedFromGroupKindAnnotation))
			g.Expect(machine.GetAnnotations()).NotTo(HaveKey(clusterv1.TemplateClonedFromNameAnnotation))
		}

		secrets := &corev1.SecretList{}
		g.Expect(fakeClient.List(ctx, secrets, client.InNamespace(cluster.Namespace), client.MatchingLabels{"previous-owner": "kubeadmconfig"})).To(Succeed())
		g.Expect(secrets.Items).To(HaveLen(3))
		for _, secret := range secrets.Items {
			g.Expect(secret.OwnerReferences).To(HaveLen(1))
			g.Expect(secret.OwnerReferences).To(ContainElement(*metav1.NewControllerRef(kcp, controlplanev1.GroupVersion.WithKind("KubeadmControlPlane"))))
		}
	})

	t.Run("Deleted KubeadmControlPlanes don't adopt machines", func(t *testing.T) {
		// Usually we won't get into the inner reconcile with a deleted control plane, but it's possible when deleting with "oprhanDependents":
		// 1. The deletion timestamp is set in the API server, but our cache has not yet updated
		// 2. The garbage collector removes our ownership reference from a Machine, triggering a re-reconcile (or we get unlucky with the periodic reconciliation)
		// 3. We get into the inner reconcile function and re-adopt the Machine
		// 4. The update to our cache for our deletion timestamp arrives
		g := NewWithT(t)

		cluster, kcp, tmpl := createClusterWithControlPlane(metav1.NamespaceDefault)
		cluster.Spec.ControlPlaneEndpoint.Host = "nodomain.example.com1"
		cluster.Spec.ControlPlaneEndpoint.Port = 6443
		kcp.Spec.Version = version

		now := metav1.Now()
		kcp.DeletionTimestamp = &now

		fmc := &fakeManagementCluster{
			Machines: collections.Machines{},
			Workload: fakeWorkloadCluster{},
		}
		objs := []client.Object{cluster.DeepCopy(), kcp.DeepCopy(), tmpl.DeepCopy()}
		for i := 0; i < 3; i++ {
			name := fmt.Sprintf("test-%d", i)
			m := &clusterv1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: cluster.Namespace,
					Name:      name,
					Labels:    internal.ControlPlaneMachineLabelsForCluster(kcp, cluster.Name),
				},
				Spec: clusterv1.MachineSpec{
					Bootstrap: clusterv1.Bootstrap{
						ConfigRef: &corev1.ObjectReference{
							APIVersion: bootstrapv1.GroupVersion.String(),
							Kind:       "KubeadmConfig",
							Name:       name,
						},
					},
					Version: &version,
				},
			}
			cfg := &bootstrapv1.KubeadmConfig{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: cluster.Namespace,
					Name:      name,
				},
			}
			objs = append(objs, m, cfg)
			fmc.Machines.Insert(m)
		}
		fakeClient := newFakeClient(objs...)
		fmc.Reader = fakeClient
		r := &KubeadmControlPlaneReconciler{
			Client:                    fakeClient,
			managementCluster:         fmc,
			managementClusterUncached: fmc,
		}

		result, err := r.reconcile(ctx, cluster, kcp)
		g.Expect(result).To(Equal(ctrl.Result{}))
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("has just been deleted"))

		machineList := &clusterv1.MachineList{}
		g.Expect(fakeClient.List(ctx, machineList, client.InNamespace(cluster.Namespace))).To(Succeed())
		g.Expect(machineList.Items).To(HaveLen(3))
		for _, machine := range machineList.Items {
			g.Expect(machine.OwnerReferences).To(BeEmpty())
		}
	})

	t.Run("refuses to adopt Machines that are more than one version old", func(t *testing.T) {
		g := NewWithT(t)

		cluster, kcp, tmpl := createClusterWithControlPlane(metav1.NamespaceDefault)
		cluster.Spec.ControlPlaneEndpoint.Host = "nodomain.example.com2"
		cluster.Spec.ControlPlaneEndpoint.Port = 6443
		kcp.Spec.Version = "v1.17.0"

		fmc := &fakeManagementCluster{
			Machines: collections.Machines{
				"test0": &clusterv1.Machine{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: cluster.Namespace,
						Name:      "test0",
						Labels:    internal.ControlPlaneMachineLabelsForCluster(kcp, cluster.Name),
					},
					Spec: clusterv1.MachineSpec{
						Bootstrap: clusterv1.Bootstrap{
							ConfigRef: &corev1.ObjectReference{
								APIVersion: bootstrapv1.GroupVersion.String(),
								Kind:       "KubeadmConfig",
							},
						},
						Version: pointer.StringPtr("v1.15.0"),
					},
				},
			},
			Workload: fakeWorkloadCluster{},
		}

		fakeClient := newFakeClient(cluster.DeepCopy(), kcp.DeepCopy(), tmpl.DeepCopy(), fmc.Machines["test0"].DeepCopy())
		fmc.Reader = fakeClient
		recorder := record.NewFakeRecorder(32)
		r := &KubeadmControlPlaneReconciler{
			Client:                    fakeClient,
			recorder:                  recorder,
			managementCluster:         fmc,
			managementClusterUncached: fmc,
		}

		g.Expect(r.reconcile(ctx, cluster, kcp)).To(Equal(ctrl.Result{}))
		// Message: Warning AdoptionFailed Could not adopt Machine test/test0: its version ("v1.15.0") is outside supported +/- one minor version skew from KCP's ("v1.17.0")
		g.Expect(recorder.Events).To(Receive(ContainSubstring("minor version")))

		machineList := &clusterv1.MachineList{}
		g.Expect(fakeClient.List(ctx, machineList, client.InNamespace(cluster.Namespace))).To(Succeed())
		g.Expect(machineList.Items).To(HaveLen(1))
		for _, machine := range machineList.Items {
			g.Expect(machine.OwnerReferences).To(BeEmpty())
		}
	})
}

func TestReconcileInitializeControlPlane(t *testing.T) {
	g := NewWithT(t)

	cluster := newCluster(&types.NamespacedName{Name: "foo", Namespace: metav1.NamespaceDefault})
	cluster.Spec = clusterv1.ClusterSpec{
		ControlPlaneEndpoint: clusterv1.APIEndpoint{
			Host: "test.local",
			Port: 9999,
		},
	}
	cluster.Status = clusterv1.ClusterStatus{InfrastructureReady: true}

	genericMachineTemplate := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "GenericMachineTemplate",
			"apiVersion": "generic.io/v1",
			"metadata": map[string]interface{}{
				"name":      "infra-foo",
				"namespace": cluster.Namespace,
			},
			"spec": map[string]interface{}{
				"template": map[string]interface{}{
					"spec": map[string]interface{}{
						"hello": "world",
					},
				},
			},
		},
	}

	kcp := &controlplanev1.KubeadmControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Namespace,
			Name:      "foo",
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind:       "Cluster",
					APIVersion: clusterv1.GroupVersion.String(),
					Name:       cluster.Name,
				},
			},
		},
		Spec: controlplanev1.KubeadmControlPlaneSpec{
			Replicas: nil,
			Version:  "v1.16.6",
			MachineTemplate: controlplanev1.KubeadmControlPlaneMachineTemplate{
				InfrastructureRef: corev1.ObjectReference{
					Kind:       genericMachineTemplate.GetKind(),
					APIVersion: genericMachineTemplate.GetAPIVersion(),
					Name:       genericMachineTemplate.GetName(),
					Namespace:  cluster.Namespace,
				},
			},
			KubeadmConfigSpec: bootstrapv1.KubeadmConfigSpec{},
		},
	}
	kcp.Default()
	g.Expect(kcp.ValidateCreate()).To(Succeed())

	corednsCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "coredns",
			Namespace: metav1.NamespaceSystem,
		},
		Data: map[string]string{
			"Corefile": "original-core-file",
		},
	}

	kubeadmCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kubeadm-config",
			Namespace: metav1.NamespaceSystem,
		},
		Data: map[string]string{
			"ClusterConfiguration": `apiServer:
dns:
  type: CoreDNS
imageRepository: k8s.gcr.io
kind: ClusterConfiguration
kubernetesVersion: metav1.16.1`,
		},
	}
	corednsDepl := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "coredns",
			Namespace: metav1.NamespaceSystem,
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name: "coredns",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "coredns",
						Image: "k8s.gcr.io/coredns:1.6.2",
					}},
				},
			},
		},
	}

	fakeClient := newFakeClient(
		kcp.DeepCopy(),
		cluster.DeepCopy(),
		genericMachineTemplate.DeepCopy(),
		corednsCM.DeepCopy(),
		kubeadmCM.DeepCopy(),
		corednsDepl.DeepCopy(),
	)
	expectedLabels := map[string]string{clusterv1.ClusterLabelName: "foo"}
	r := &KubeadmControlPlaneReconciler{
		Client:   fakeClient,
		recorder: record.NewFakeRecorder(32),
		managementCluster: &fakeManagementCluster{
			Management: &internal.Management{Client: fakeClient},
			Workload: fakeWorkloadCluster{
				Workload: &internal.Workload{
					Client: fakeClient,
				},
				Status: internal.ClusterStatus{},
			},
		},
		managementClusterUncached: &fakeManagementCluster{
			Management: &internal.Management{Client: fakeClient},
			Workload: fakeWorkloadCluster{
				Workload: &internal.Workload{
					Client: fakeClient,
				},
				Status: internal.ClusterStatus{},
			},
		},
	}

	result, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(kcp)})
	g.Expect(err).NotTo(HaveOccurred())
	// this first requeue is to add finalizer
	g.Expect(result).To(Equal(ctrl.Result{}))
	g.Expect(r.Client.Get(ctx, util.ObjectKey(kcp), kcp)).To(Succeed())
	g.Expect(kcp.Finalizers).To(ContainElement(controlplanev1.KubeadmControlPlaneFinalizer))

	result, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: util.ObjectKey(kcp)})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result).To(Equal(ctrl.Result{Requeue: true}))
	g.Expect(r.Client.Get(ctx, client.ObjectKey{Name: kcp.Name, Namespace: kcp.Namespace}, kcp)).To(Succeed())
	// Expect the referenced infrastructure template to have a Cluster Owner Reference.
	g.Expect(fakeClient.Get(ctx, util.ObjectKey(genericMachineTemplate), genericMachineTemplate)).To(Succeed())
	g.Expect(genericMachineTemplate.GetOwnerReferences()).To(ContainElement(metav1.OwnerReference{
		APIVersion: clusterv1.GroupVersion.String(),
		Kind:       "Cluster",
		Name:       cluster.Name,
	}))

	// Always expect that the Finalizer is set on the passed in resource
	g.Expect(kcp.Finalizers).To(ContainElement(controlplanev1.KubeadmControlPlaneFinalizer))

	g.Expect(kcp.Status.Selector).NotTo(BeEmpty())
	g.Expect(kcp.Status.Replicas).To(BeEquivalentTo(1))
	g.Expect(conditions.IsFalse(kcp, controlplanev1.AvailableCondition)).To(BeTrue())

	s, err := secret.GetFromNamespacedName(ctx, fakeClient, client.ObjectKey{Namespace: metav1.NamespaceDefault, Name: "foo"}, secret.ClusterCA)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(s).NotTo(BeNil())
	g.Expect(s.Data).NotTo(BeEmpty())
	g.Expect(s.Labels).To(Equal(expectedLabels))

	k, err := kubeconfig.FromSecret(ctx, fakeClient, util.ObjectKey(cluster))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(k).NotTo(BeEmpty())

	machineList := &clusterv1.MachineList{}
	g.Expect(fakeClient.List(ctx, machineList, client.InNamespace(metav1.NamespaceDefault))).To(Succeed())
	g.Expect(machineList.Items).To(HaveLen(1))

	machine := machineList.Items[0]
	g.Expect(machine.Name).To(HavePrefix(kcp.Name))
	// Newly cloned infra objects should have the infraref annotation.
	infraObj, err := external.Get(ctx, r.Client, &machine.Spec.InfrastructureRef, machine.Spec.InfrastructureRef.Namespace)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(infraObj.GetAnnotations()).To(HaveKeyWithValue(clusterv1.TemplateClonedFromNameAnnotation, genericMachineTemplate.GetName()))
	g.Expect(infraObj.GetAnnotations()).To(HaveKeyWithValue(clusterv1.TemplateClonedFromGroupKindAnnotation, genericMachineTemplate.GroupVersionKind().GroupKind().String()))
}

func TestKubeadmControlPlaneReconciler_updateCoreDNS(t *testing.T) {
	// TODO: (wfernandes) This test could use some refactor love.

	cluster := newCluster(&types.NamespacedName{Name: "foo", Namespace: metav1.NamespaceDefault})
	kcp := &controlplanev1.KubeadmControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Namespace,
			Name:      "foo",
		},
		Spec: controlplanev1.KubeadmControlPlaneSpec{
			Replicas: nil,
			Version:  "v1.16.6",
			KubeadmConfigSpec: bootstrapv1.KubeadmConfigSpec{
				ClusterConfiguration: &bootstrapv1.ClusterConfiguration{
					DNS: bootstrapv1.DNS{
						ImageMeta: bootstrapv1.ImageMeta{
							ImageRepository: "k8s.gcr.io",
							ImageTag:        "1.7.2",
						},
					},
				},
			},
		},
	}
	depl := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "coredns",
			Namespace: metav1.NamespaceSystem,
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name: "coredns",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "coredns",
						Image: "k8s.gcr.io/coredns:1.6.2",
					}},
					Volumes: []corev1.Volume{{
						Name: "config-volume",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "coredns",
								},
								Items: []corev1.KeyToPath{{
									Key:  "Corefile",
									Path: "Corefile",
								}},
							},
						},
					}},
				},
			},
		},
	}
	originalCorefile := "original core file"
	corednsCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "coredns",
			Namespace: metav1.NamespaceSystem,
		},
		Data: map[string]string{
			"Corefile": originalCorefile,
		},
	}

	kubeadmCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kubeadm-config",
			Namespace: metav1.NamespaceSystem,
		},
		Data: map[string]string{
			"ClusterConfiguration": `apiServer:
dns:
  type: CoreDNS
imageRepository: k8s.gcr.io
kind: ClusterConfiguration
kubernetesVersion: metav1.16.1`,
		},
	}

	t.Run("updates configmaps and deployments successfully", func(t *testing.T) {
		t.Skip("Updating the corefile, after updating controller runtime somehow makes this test fail in a conflict, needs investigation")

		g := NewWithT(t)
		objs := []client.Object{
			cluster.DeepCopy(),
			kcp.DeepCopy(),
			depl.DeepCopy(),
			corednsCM.DeepCopy(),
			kubeadmCM.DeepCopy(),
		}
		fakeClient := newFakeClient(objs...)
		log.SetLogger(klogr.New())

		workloadCluster := fakeWorkloadCluster{
			Workload: &internal.Workload{
				Client: fakeClient,
				CoreDNSMigrator: &fakeMigrator{
					migratedCorefile: "new core file",
				},
			},
		}

		g.Expect(workloadCluster.UpdateCoreDNS(ctx, kcp, semver.MustParse("1.19.1"))).To(Succeed())

		var actualCoreDNSCM corev1.ConfigMap
		g.Expect(fakeClient.Get(ctx, client.ObjectKey{Name: "coredns", Namespace: metav1.NamespaceSystem}, &actualCoreDNSCM)).To(Succeed())
		g.Expect(actualCoreDNSCM.Data).To(HaveLen(2))
		g.Expect(actualCoreDNSCM.Data).To(HaveKeyWithValue("Corefile", "new core file"))
		g.Expect(actualCoreDNSCM.Data).To(HaveKeyWithValue("Corefile-backup", originalCorefile))

		var actualKubeadmConfig corev1.ConfigMap
		g.Expect(fakeClient.Get(ctx, client.ObjectKey{Name: "kubeadm-config", Namespace: metav1.NamespaceSystem}, &actualKubeadmConfig)).To(Succeed())
		g.Expect(actualKubeadmConfig.Data).To(HaveKey("ClusterConfiguration"))
		g.Expect(actualKubeadmConfig.Data["ClusterConfiguration"]).To(ContainSubstring("1.7.2"))

		expectedVolume := corev1.Volume{
			Name: "config-volume",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "coredns",
					},
					Items: []corev1.KeyToPath{{
						Key:  "Corefile",
						Path: "Corefile",
					}},
				},
			},
		}
		var actualCoreDNSDeployment appsv1.Deployment
		g.Expect(fakeClient.Get(ctx, client.ObjectKey{Name: "coredns", Namespace: metav1.NamespaceSystem}, &actualCoreDNSDeployment)).To(Succeed())
		g.Expect(actualCoreDNSDeployment.Spec.Template.Spec.Containers[0].Image).To(Equal("k8s.gcr.io/coredns:1.7.2"))
		g.Expect(actualCoreDNSDeployment.Spec.Template.Spec.Volumes).To(ConsistOf(expectedVolume))
	})

	t.Run("returns no error when no ClusterConfiguration is specified", func(t *testing.T) {
		g := NewWithT(t)
		kcp := kcp.DeepCopy()
		kcp.Spec.KubeadmConfigSpec.ClusterConfiguration = nil

		objs := []client.Object{
			cluster.DeepCopy(),
			kcp,
			depl.DeepCopy(),
			corednsCM.DeepCopy(),
			kubeadmCM.DeepCopy(),
		}

		fakeClient := newFakeClient(objs...)
		log.SetLogger(klogr.New())

		workloadCluster := fakeWorkloadCluster{
			Workload: &internal.Workload{
				Client: fakeClient,
				CoreDNSMigrator: &fakeMigrator{
					migratedCorefile: "new core file",
				},
			},
		}

		g.Expect(workloadCluster.UpdateCoreDNS(ctx, kcp, semver.MustParse("1.19.1"))).To(Succeed())
	})

	t.Run("should not return an error when there is no CoreDNS configmap", func(t *testing.T) {
		g := NewWithT(t)
		objs := []client.Object{
			cluster.DeepCopy(),
			kcp.DeepCopy(),
			depl.DeepCopy(),
			kubeadmCM.DeepCopy(),
		}

		fakeClient := newFakeClient(objs...)
		workloadCluster := fakeWorkloadCluster{
			Workload: &internal.Workload{
				Client: fakeClient,
				CoreDNSMigrator: &fakeMigrator{
					migratedCorefile: "new core file",
				},
			},
		}

		g.Expect(workloadCluster.UpdateCoreDNS(ctx, kcp, semver.MustParse("1.19.1"))).To(Succeed())
	})

	t.Run("should not return an error when there is no CoreDNS deployment", func(t *testing.T) {
		g := NewWithT(t)
		objs := []client.Object{
			cluster.DeepCopy(),
			kcp.DeepCopy(),
			corednsCM.DeepCopy(),
			kubeadmCM.DeepCopy(),
		}

		fakeClient := newFakeClient(objs...)
		log.SetLogger(klogr.New())

		workloadCluster := fakeWorkloadCluster{
			Workload: &internal.Workload{
				Client: fakeClient,
				CoreDNSMigrator: &fakeMigrator{
					migratedCorefile: "new core file",
				},
			},
		}

		g.Expect(workloadCluster.UpdateCoreDNS(ctx, kcp, semver.MustParse("1.19.1"))).To(Succeed())
	})

	t.Run("should not return an error when no DNS upgrade is requested", func(t *testing.T) {
		g := NewWithT(t)
		objs := []client.Object{
			cluster.DeepCopy(),
			corednsCM.DeepCopy(),
			kubeadmCM.DeepCopy(),
		}
		kcp := kcp.DeepCopy()
		kcp.Annotations = map[string]string{controlplanev1.SkipCoreDNSAnnotation: ""}

		depl := depl.DeepCopy()

		depl.Spec.Template.Spec.Containers[0].Image = "my-cool-image!!!!" // something very unlikely for getCoreDNSInfo to parse
		objs = append(objs, depl)

		fakeClient := newFakeClient(objs...)
		workloadCluster := fakeWorkloadCluster{
			Workload: &internal.Workload{
				Client: fakeClient,
			},
		}

		g.Expect(workloadCluster.UpdateCoreDNS(ctx, kcp, semver.MustParse("1.19.1"))).To(Succeed())

		var actualCoreDNSCM corev1.ConfigMap
		g.Expect(fakeClient.Get(ctx, client.ObjectKey{Name: "coredns", Namespace: metav1.NamespaceSystem}, &actualCoreDNSCM)).To(Succeed())
		g.Expect(actualCoreDNSCM.Data).To(Equal(corednsCM.Data))

		var actualKubeadmConfig corev1.ConfigMap
		g.Expect(fakeClient.Get(ctx, client.ObjectKey{Name: "kubeadm-config", Namespace: metav1.NamespaceSystem}, &actualKubeadmConfig)).To(Succeed())
		g.Expect(actualKubeadmConfig.Data).To(Equal(kubeadmCM.Data))

		var actualCoreDNSDeployment appsv1.Deployment
		g.Expect(fakeClient.Get(ctx, client.ObjectKey{Name: "coredns", Namespace: metav1.NamespaceSystem}, &actualCoreDNSDeployment)).To(Succeed())
		g.Expect(actualCoreDNSDeployment.Spec.Template.Spec.Containers[0].Image).ToNot(ContainSubstring("coredns"))
	})

	t.Run("returns error when unable to UpdateCoreDNS", func(t *testing.T) {
		g := NewWithT(t)
		objs := []client.Object{
			cluster.DeepCopy(),
			kcp.DeepCopy(),
			depl.DeepCopy(),
			corednsCM.DeepCopy(),
		}

		fakeClient := newFakeClient(objs...)
		log.SetLogger(klogr.New())

		workloadCluster := fakeWorkloadCluster{
			Workload: &internal.Workload{
				Client: fakeClient,
				CoreDNSMigrator: &fakeMigrator{
					migratedCorefile: "new core file",
				},
			},
		}

		g.Expect(workloadCluster.UpdateCoreDNS(ctx, kcp, semver.MustParse("1.19.1"))).ToNot(Succeed())
	})
}

func TestKubeadmControlPlaneReconciler_reconcileDelete(t *testing.T) {
	t.Run("removes all control plane Machines", func(t *testing.T) {
		g := NewWithT(t)

		cluster, kcp, _ := createClusterWithControlPlane(metav1.NamespaceDefault)
		controllerutil.AddFinalizer(kcp, controlplanev1.KubeadmControlPlaneFinalizer)
		initObjs := []client.Object{cluster.DeepCopy(), kcp.DeepCopy()}

		for i := 0; i < 3; i++ {
			m, _ := createMachineNodePair(fmt.Sprintf("test-%d", i), cluster, kcp, true)
			initObjs = append(initObjs, m)
		}

		fakeClient := newFakeClient(initObjs...)

		r := &KubeadmControlPlaneReconciler{
			Client: fakeClient,
			managementCluster: &fakeManagementCluster{
				Management: &internal.Management{Client: fakeClient},
				Workload:   fakeWorkloadCluster{},
			},

			recorder: record.NewFakeRecorder(32),
		}

		result, err := r.reconcileDelete(ctx, cluster, kcp)
		g.Expect(result).To(Equal(ctrl.Result{RequeueAfter: deleteRequeueAfter}))
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(kcp.Finalizers).To(ContainElement(controlplanev1.KubeadmControlPlaneFinalizer))

		controlPlaneMachines := clusterv1.MachineList{}
		g.Expect(fakeClient.List(ctx, &controlPlaneMachines)).To(Succeed())
		g.Expect(controlPlaneMachines.Items).To(BeEmpty())

		result, err = r.reconcileDelete(ctx, cluster, kcp)
		g.Expect(result).To(Equal(ctrl.Result{}))
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(kcp.Finalizers).To(BeEmpty())
	})

	t.Run("does not remove any control plane Machines if other Machines exist", func(t *testing.T) {
		g := NewWithT(t)

		cluster, kcp, _ := createClusterWithControlPlane(metav1.NamespaceDefault)
		controllerutil.AddFinalizer(kcp, controlplanev1.KubeadmControlPlaneFinalizer)

		workerMachine := &clusterv1.Machine{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "worker",
				Namespace: cluster.Namespace,
				Labels: map[string]string{
					clusterv1.ClusterLabelName: cluster.Name,
				},
			},
		}

		initObjs := []client.Object{cluster.DeepCopy(), kcp.DeepCopy(), workerMachine.DeepCopy()}

		for i := 0; i < 3; i++ {
			m, _ := createMachineNodePair(fmt.Sprintf("test-%d", i), cluster, kcp, true)
			initObjs = append(initObjs, m)
		}

		fakeClient := newFakeClient(initObjs...)

		r := &KubeadmControlPlaneReconciler{
			Client: fakeClient,
			managementCluster: &fakeManagementCluster{
				Management: &internal.Management{Client: fakeClient},
				Workload:   fakeWorkloadCluster{},
			},
			recorder: record.NewFakeRecorder(32),
		}

		result, err := r.reconcileDelete(ctx, cluster, kcp)
		g.Expect(result).To(Equal(ctrl.Result{RequeueAfter: deleteRequeueAfter}))
		g.Expect(err).ToNot(HaveOccurred())

		g.Expect(kcp.Finalizers).To(ContainElement(controlplanev1.KubeadmControlPlaneFinalizer))

		controlPlaneMachines := clusterv1.MachineList{}
		labels := map[string]string{
			clusterv1.MachineControlPlaneLabelName: "",
		}
		g.Expect(fakeClient.List(ctx, &controlPlaneMachines, client.MatchingLabels(labels))).To(Succeed())
		g.Expect(controlPlaneMachines.Items).To(HaveLen(3))
	})

	t.Run("does not remove any control plane Machines if MachinePools exist", func(t *testing.T) {
		_ = feature.MutableGates.Set("MachinePool=true")
		g := NewWithT(t)

		cluster, kcp, _ := createClusterWithControlPlane(metav1.NamespaceDefault)
		controllerutil.AddFinalizer(kcp, controlplanev1.KubeadmControlPlaneFinalizer)

		workerMachinePool := &expv1.MachinePool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "worker",
				Namespace: cluster.Namespace,
				Labels: map[string]string{
					clusterv1.ClusterLabelName: cluster.Name,
				},
			},
		}

		initObjs := []client.Object{cluster.DeepCopy(), kcp.DeepCopy(), workerMachinePool.DeepCopy()}

		for i := 0; i < 3; i++ {
			m, _ := createMachineNodePair(fmt.Sprintf("test-%d", i), cluster, kcp, true)
			initObjs = append(initObjs, m)
		}

		fakeClient := newFakeClient(initObjs...)

		r := &KubeadmControlPlaneReconciler{
			Client: fakeClient,
			managementCluster: &fakeManagementCluster{
				Management: &internal.Management{Client: fakeClient},
				Workload:   fakeWorkloadCluster{},
			},
			recorder: record.NewFakeRecorder(32),
		}

		result, err := r.reconcileDelete(ctx, cluster, kcp)
		g.Expect(result).To(Equal(ctrl.Result{RequeueAfter: deleteRequeueAfter}))
		g.Expect(err).ToNot(HaveOccurred())

		g.Expect(kcp.Finalizers).To(ContainElement(controlplanev1.KubeadmControlPlaneFinalizer))

		controlPlaneMachines := clusterv1.MachineList{}
		labels := map[string]string{
			clusterv1.MachineControlPlaneLabelName: "",
		}
		g.Expect(fakeClient.List(ctx, &controlPlaneMachines, client.MatchingLabels(labels))).To(Succeed())
		g.Expect(controlPlaneMachines.Items).To(HaveLen(3))
	})

	t.Run("removes the finalizer if no control plane Machines exist", func(t *testing.T) {
		g := NewWithT(t)

		cluster, kcp, _ := createClusterWithControlPlane(metav1.NamespaceDefault)
		controllerutil.AddFinalizer(kcp, controlplanev1.KubeadmControlPlaneFinalizer)

		fakeClient := newFakeClient(cluster.DeepCopy(), kcp.DeepCopy())

		r := &KubeadmControlPlaneReconciler{
			Client: fakeClient,
			managementCluster: &fakeManagementCluster{
				Management: &internal.Management{Client: fakeClient},
				Workload:   fakeWorkloadCluster{},
			},
			recorder: record.NewFakeRecorder(32),
		}

		result, err := r.reconcileDelete(ctx, cluster, kcp)
		g.Expect(result).To(Equal(ctrl.Result{}))
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(kcp.Finalizers).To(BeEmpty())
	})
}

// test utils

func newFakeClient(initObjs ...client.Object) client.Client {
	return &fakeClient{
		startTime: time.Now(),
		Client:    fake.NewClientBuilder().WithObjects(initObjs...).Build(),
	}
}

type fakeClient struct {
	startTime time.Time
	mux       sync.Mutex
	client.Client
}

type fakeClientI interface {
	SetCreationTimestamp(timestamp metav1.Time)
}

// controller-runtime's fake client doesn't set a CreationTimestamp
// this sets one that increments by a minute for each object created.
func (c *fakeClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if f, ok := obj.(fakeClientI); ok {
		c.mux.Lock()
		c.startTime = c.startTime.Add(time.Minute)
		f.SetCreationTimestamp(metav1.NewTime(c.startTime))
		c.mux.Unlock()
	}
	return c.Client.Create(ctx, obj, opts...)
}

func createClusterWithControlPlane(namespace string) (*clusterv1.Cluster, *controlplanev1.KubeadmControlPlane, *unstructured.Unstructured) {
	kcpName := fmt.Sprintf("kcp-foo-%s", util.RandomString(6))

	cluster := newCluster(&types.NamespacedName{Name: kcpName, Namespace: namespace})
	cluster.Spec = clusterv1.ClusterSpec{
		ControlPlaneRef: &corev1.ObjectReference{
			Kind:       "KubeadmControlPlane",
			Namespace:  namespace,
			Name:       kcpName,
			APIVersion: controlplanev1.GroupVersion.String(),
		},
	}

	kcp := &controlplanev1.KubeadmControlPlane{
		TypeMeta: metav1.TypeMeta{
			APIVersion: controlplanev1.GroupVersion.String(),
			Kind:       "KubeadmControlPlane",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      kcpName,
			Namespace: namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind:       "Cluster",
					APIVersion: clusterv1.GroupVersion.String(),
					Name:       kcpName,
					UID:        "1",
				},
			},
		},
		Spec: controlplanev1.KubeadmControlPlaneSpec{
			MachineTemplate: controlplanev1.KubeadmControlPlaneMachineTemplate{
				InfrastructureRef: corev1.ObjectReference{
					Kind:       "GenericMachineTemplate",
					Namespace:  namespace,
					Name:       "infra-foo",
					APIVersion: "generic.io/v1",
				},
			},
			Replicas: pointer.Int32Ptr(int32(3)),
			Version:  "v1.16.6",
			RolloutStrategy: &controlplanev1.RolloutStrategy{
				Type: "RollingUpdate",
				RollingUpdate: &controlplanev1.RollingUpdate{
					MaxSurge: &intstr.IntOrString{
						IntVal: 1,
					},
				},
			},
		},
	}

	genericMachineTemplate := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "GenericMachineTemplate",
			"apiVersion": "generic.io/v1",
			"metadata": map[string]interface{}{
				"name":      "infra-foo",
				"namespace": namespace,
				"ownerReferences": []interface{}{
					map[string]interface{}{
						"apiVersion": clusterv1.GroupVersion.String(),
						"kind":       "Cluster",
						"name":       kcpName,
					},
				},
			},
			"spec": map[string]interface{}{
				"template": map[string]interface{}{
					"spec": map[string]interface{}{},
				},
			},
		},
	}
	return cluster, kcp, genericMachineTemplate
}

func setKCPHealthy(kcp *controlplanev1.KubeadmControlPlane) {
	conditions.MarkTrue(kcp, controlplanev1.ControlPlaneComponentsHealthyCondition)
	conditions.MarkTrue(kcp, controlplanev1.EtcdClusterHealthyCondition)
}

func createMachineNodePair(name string, cluster *clusterv1.Cluster, kcp *controlplanev1.KubeadmControlPlane, ready bool) (*clusterv1.Machine, *corev1.Node) {
	machine := &clusterv1.Machine{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Machine",
			APIVersion: clusterv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Namespace,
			Name:      name,
			Labels:    internal.ControlPlaneMachineLabelsForCluster(kcp, cluster.Name),
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(kcp, controlplanev1.GroupVersion.WithKind("KubeadmControlPlane")),
			},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: cluster.Name,
			InfrastructureRef: corev1.ObjectReference{
				Kind:       external.TestGenericInfrastructureCRD.Kind,
				APIVersion: external.TestGenericInfrastructureCRD.APIVersion,
				Name:       external.TestGenericInfrastructureCRD.Name,
				Namespace:  external.TestGenericInfrastructureCRD.Namespace,
			},
		},
		Status: clusterv1.MachineStatus{
			NodeRef: &corev1.ObjectReference{
				Kind:       "Node",
				APIVersion: corev1.SchemeGroupVersion.String(),
				Name:       name,
			},
		},
	}
	machine.Default()

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{"node-role.kubernetes.io/master": ""},
		},
	}

	if ready {
		node.Spec.ProviderID = fmt.Sprintf("test://%s", machine.GetName())
		node.Status.Conditions = []corev1.NodeCondition{
			{
				Type:   corev1.NodeReady,
				Status: corev1.ConditionTrue,
			},
		}
	}
	return machine, node
}

func setMachineHealthy(m *clusterv1.Machine) {
	conditions.MarkTrue(m, controlplanev1.MachineAPIServerPodHealthyCondition)
	conditions.MarkTrue(m, controlplanev1.MachineControllerManagerPodHealthyCondition)
	conditions.MarkTrue(m, controlplanev1.MachineSchedulerPodHealthyCondition)
	conditions.MarkTrue(m, controlplanev1.MachineEtcdPodHealthyCondition)
	conditions.MarkTrue(m, controlplanev1.MachineEtcdMemberHealthyCondition)
}

// newCluster return a CAPI cluster object.
func newCluster(namespacedName *types.NamespacedName) *clusterv1.Cluster {
	return &clusterv1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Cluster",
			APIVersion: clusterv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespacedName.Namespace,
			Name:      namespacedName.Name,
		},
	}
}
