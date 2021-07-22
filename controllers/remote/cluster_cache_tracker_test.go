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

package remote

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	"sigs.k8s.io/cluster-api/controllers/noderefutil"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/conditions"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func mapper(i client.Object) []reconcile.Request {
	return []reconcile.Request{
		{
			NamespacedName: types.NamespacedName{
				Namespace: i.GetNamespace(),
				Name:      "mapped-" + i.GetName(),
			},
		},
	}
}

func TestClusterCacheTracker(t *testing.T) {
	t.Run("watching", func(t *testing.T) {
		var (
			mgr        manager.Manager
			mgrContext context.Context
			mgrCancel  context.CancelFunc
			cct        *ClusterCacheTracker
			k8sClient  client.Client
			c          *testController
			w          Watcher
			clusterA   *clusterv1.Cluster
		)

		setup := func(t *testing.T, g *WithT) *corev1.Namespace {
			t.Log("Setting up a new manager")
			var err error
			mgr, err = manager.New(env.Config, manager.Options{
				Scheme:             scheme.Scheme,
				MetricsBindAddress: "0",
			})
			g.Expect(err).NotTo(HaveOccurred())

			c = &testController{
				ch: make(chan string),
			}
			w, err = ctrl.NewControllerManagedBy(mgr).For(&clusterv1.MachineDeployment{}).Build(c)
			g.Expect(err).NotTo(HaveOccurred())

			mgrContext, mgrCancel = context.WithCancel(ctx)
			t.Log("Starting the manager")
			go func() {
				g.Expect(mgr.Start(mgrContext)).To(Succeed())
			}()
			<-env.Manager.Elected()

			k8sClient = mgr.GetClient()

			t.Log("Setting up a ClusterCacheTracker")
			cct, err = NewClusterCacheTracker(mgr, ClusterCacheTrackerOptions{
				Indexes: []Index{
					{
						Object:       &corev1.Node{},
						Field:        noderefutil.NodeProviderIDIndex,
						ExtractValue: noderefutil.IndexNodeByProviderID,
					},
				},
			})
			g.Expect(err).NotTo(HaveOccurred())

			t.Log("Creating a namespace for the test")
			ns, err := env.CreateNamespace(ctx, "cluster-cache-tracker-test")
			g.Expect(err).To(BeNil())

			t.Log("Creating a test cluster")
			clusterA = &clusterv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   ns.GetName(),
					Name:        "test-cluster",
					Annotations: make(map[string]string),
				},
			}
			g.Expect(k8sClient.Create(ctx, clusterA)).To(Succeed())
			conditions.MarkTrue(clusterA, clusterv1.ControlPlaneInitializedCondition)
			clusterA.Status.InfrastructureReady = true
			g.Expect(k8sClient.Status().Update(ctx, clusterA)).To(Succeed())

			t.Log("Creating a test cluster kubeconfig")
			g.Expect(env.CreateKubeconfigSecret(ctx, clusterA)).To(Succeed())

			return ns
		}

		teardown := func(t *testing.T, g *WithT, ns *corev1.Namespace) {
			t.Log("Deleting any Secrets")
			g.Expect(cleanupTestSecrets(ctx, k8sClient)).To(Succeed())
			t.Log("Deleting any Clusters")
			g.Expect(cleanupTestClusters(ctx, k8sClient)).To(Succeed())
			t.Log("Deleting Namespace")
			g.Expect(env.Delete(ctx, ns)).To(Succeed())
			t.Log("Stopping the manager")
			mgrCancel()
		}

		t.Run("with the same name should succeed and not have duplicates", func(t *testing.T) {
			g := NewWithT(t)
			ns := setup(t, g)
			defer teardown(t, g, ns)

			t.Log("Creating the watch")
			g.Expect(cct.Watch(ctx, WatchInput{
				Name:         "watch1",
				Cluster:      util.ObjectKey(clusterA),
				Watcher:      w,
				Kind:         &clusterv1.Cluster{},
				EventHandler: handler.EnqueueRequestsFromMapFunc(mapper),
			})).To(Succeed())

			t.Log("Waiting to receive the watch notification")
			g.Expect(<-c.ch).To(Equal("mapped-" + clusterA.Name))

			t.Log("Ensuring no additional watch notifications arrive")
			g.Consistently(func() int {
				return len(c.ch)
			}).Should(Equal(0))

			t.Log("Updating the cluster")
			clusterA.Annotations["update1"] = "1"
			g.Expect(k8sClient.Update(ctx, clusterA)).To(Succeed())

			t.Log("Waiting to receive the watch notification")
			g.Expect(<-c.ch).To(Equal("mapped-" + clusterA.Name))

			t.Log("Ensuring no additional watch notifications arrive")
			g.Consistently(func() int {
				return len(c.ch)
			}).Should(Equal(0))

			t.Log("Creating the same watch a second time")
			g.Expect(cct.Watch(ctx, WatchInput{
				Name:         "watch1",
				Cluster:      util.ObjectKey(clusterA),
				Watcher:      w,
				Kind:         &clusterv1.Cluster{},
				EventHandler: handler.EnqueueRequestsFromMapFunc(mapper),
			})).To(Succeed())

			t.Log("Ensuring no additional watch notifications arrive")
			g.Consistently(func() int {
				return len(c.ch)
			}).Should(Equal(0))

			t.Log("Updating the cluster")
			clusterA.Annotations["update1"] = "2"
			g.Expect(k8sClient.Update(ctx, clusterA)).To(Succeed())

			t.Log("Waiting to receive the watch notification")
			g.Expect(<-c.ch).To(Equal("mapped-" + clusterA.Name))

			t.Log("Ensuring no additional watch notifications arrive")
			g.Consistently(func() int {
				return len(c.ch)
			}).Should(Equal(0))
		})
	})
}

type testController struct {
	ch chan string
}

func (c *testController) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	c.ch <- req.Name
	return ctrl.Result{}, nil
}

func cleanupTestSecrets(ctx context.Context, c client.Client) error {
	secretList := &corev1.SecretList{}
	if err := c.List(ctx, secretList); err != nil {
		return err
	}
	for _, secret := range secretList.Items {
		s := secret
		if err := c.Delete(ctx, &s); err != nil {
			return err
		}
	}
	return nil
}

func cleanupTestClusters(ctx context.Context, c client.Client) error {
	clusterList := &clusterv1.ClusterList{}
	if err := c.List(ctx, clusterList); err != nil {
		return err
	}
	for _, cluster := range clusterList.Items {
		o := cluster
		if err := c.Delete(ctx, &o); err != nil {
			return err
		}
	}
	return nil
}
