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

package cluster

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/pager"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog"
	clusterv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	clusterv1alpha1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	"sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset/typed/cluster/v1alpha1"
	controllerError "sigs.k8s.io/cluster-api/pkg/controller/error"
	"sigs.k8s.io/cluster-api/pkg/util"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var DefaultActuator Actuator

func AddWithActuator(mgr manager.Manager, actuator Actuator) error {
	reconciler, err := newReconciler(mgr, actuator)
	if err != nil {
		return err
	}

	return add(mgr, reconciler)
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager, actuator Actuator) (reconcile.Reconciler, error) {
	cclient, err := v1alpha1.NewForConfig(mgr.GetConfig())
	if err != nil {
		return nil, err
	}
	return &ReconcileCluster{
		Client:        mgr.GetClient(),
		clusterClient: cclient,
		scheme:        mgr.GetScheme(),
		actuator:      actuator}, nil
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("cluster-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to Cluster
	err = c.Watch(&source.Kind{Type: &clusterv1alpha1.Cluster{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileCluster{}

// ReconcileCluster reconciles a Cluster object
type ReconcileCluster struct {
	client.Client
	clusterClient v1alpha1.ClusterV1alpha1Interface
	scheme        *runtime.Scheme
	actuator      Actuator
}

// +kubebuilder:rbac:groups=cluster.k8s.io,resources=clusters,verbs=get;list;watch;create;update;patch;delete
func (r *ReconcileCluster) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	cluster := &clusterv1alpha1.Cluster{}
	err := r.Get(context.Background(), request.NamespacedName, cluster)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	name := cluster.Name
	klog.Infof("Running reconcile Cluster for %q", name)

	// If object hasn't been deleted and doesn't have a finalizer, add one
	// Add a finalizer to newly created objects.
	if cluster.ObjectMeta.DeletionTimestamp.IsZero() {
		finalizerCount := len(cluster.Finalizers)

		if !util.Contains(cluster.Finalizers, metav1.FinalizerDeleteDependents) {
			cluster.Finalizers = append(cluster.ObjectMeta.Finalizers, metav1.FinalizerDeleteDependents)
		}

		if !util.Contains(cluster.Finalizers, clusterv1.ClusterFinalizer) {
			cluster.Finalizers = append(cluster.ObjectMeta.Finalizers, clusterv1.ClusterFinalizer)
		}

		if len(cluster.Finalizers) > finalizerCount {
			if err := r.Update(context.Background(), cluster); err != nil {
				klog.Infof("Failed to add finalizer to cluster %q: %v", name, err)
				return reconcile.Result{}, err
			}

			// Since adding the finalizer updates the object return to avoid later update issues.
			return reconcile.Result{Requeue: true}, nil
		}

	}

	if !cluster.ObjectMeta.DeletionTimestamp.IsZero() {
		// no-op if finalizer has been removed.
		if !util.Contains(cluster.ObjectMeta.Finalizers, clusterv1.ClusterFinalizer) {
			klog.Infof("Reconciling cluster object %v causes a no-op as there is no finalizer.", name)
			return reconcile.Result{}, nil
		}

		children, err := r.listChildren(context.Background(), cluster)
		if err != nil {
			klog.Infof("Failed to list dependent objects of cluster %s/%s: %v", cluster.ObjectMeta.Namespace, cluster.ObjectMeta.Name, err)
			return reconcile.Result{}, err
		}

		if len(children) > 0 {
			klog.Infof("Deleting cluster %s: %d children still exist, will requeue", name, len(children))
			for _, child := range children {

				accessor, err := meta.Accessor(child)
				if err != nil {
					return reconcile.Result{}, errors.Wrapf(err, "couldn't create accessor for %T", child)
				}

				if accessor.GetDeletionTimestamp() != nil {
					continue
				}

				gvk := child.GetObjectKind().GroupVersionKind().String()

				klog.V(4).Infof("Deleting cluster %s: Deleting %s %s", name, gvk, accessor.GetName())
				if err := r.Delete(context.Background(), child, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil {
					return reconcile.Result{}, errors.Wrapf(err, "deleting cluster %s: failed to delete %s %s", name, gvk, accessor.GetName())
				}
			}

			return reconcile.Result{Requeue: true}, nil
		}

		klog.Infof("Reconciling cluster object %v triggers delete.", name)
		if err := r.actuator.Delete(cluster); err != nil {
			klog.Errorf("Error deleting cluster object %v; %v", name, err)
			return reconcile.Result{}, err
		}

		// Remove finalizer on successful deletion.
		klog.Infof("Cluster object %v deletion successful, removing finalizer.", name)
		err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
			// It's possible the actuator's Delete call modified the cluster. We can't guarantee that every provider
			// updated the in memory cluster object with the latest copy of the cluster, so try to get a fresh copy.
			//
			// Note, because the get from the client is a cached read from the shared informer's cache, there's still a
			// chance this could be a stale read.
			//
			// Note 2, this is not a Patch call because the version of controller-runtime in the release-0.1 branch
			// does not support patching.
			err := r.Get(context.Background(), request.NamespacedName, cluster)
			if err != nil {
				return err
			}

			cluster.ObjectMeta.Finalizers = util.Filter(cluster.ObjectMeta.Finalizers, clusterv1.ClusterFinalizer)

			return r.Client.Update(context.Background(), cluster)
		})
		if err != nil {
			klog.Errorf("Error removing finalizer from cluster object %v; %v", name, err)
			return reconcile.Result{}, err
		}

		return reconcile.Result{}, nil
	}

	klog.Infof("Reconciling cluster object %v triggers idempotent reconcile.", name)
	if err := r.actuator.Reconcile(cluster); err != nil {
		if requeueErr, ok := errors.Cause(err).(controllerError.HasRequeueAfterError); ok {
			klog.Infof("Actuator returned requeue-after error: %v", requeueErr)
			return reconcile.Result{Requeue: true, RequeueAfter: requeueErr.GetRequeueAfter()}, nil
		}
		klog.Errorf("Error reconciling cluster object %v; %v", name, err)
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

// listChildren returns a list of Deployments, Sets, and Machines than have an ownerref to the given cluster
func (r *ReconcileCluster) listChildren(ctx context.Context, cluster *clusterv1.Cluster) ([]runtime.Object, error) {
	var children []runtime.Object

	ns := cluster.GetNamespace()
	opts := metav1.ListOptions{
		LabelSelector: labels.FormatLabels(
			map[string]string{clusterv1.MachineClusterLabelName: cluster.GetName()},
		),
	}

	dfunc := func(_ context.Context, m metav1.ListOptions) (runtime.Object, error) {
		return r.clusterClient.MachineDeployments(ns).List(m)
	}
	sfunc := func(_ context.Context, m metav1.ListOptions) (runtime.Object, error) {
		return r.clusterClient.MachineSets(ns).List(m)
	}
	mfunc := func(_ context.Context, m metav1.ListOptions) (runtime.Object, error) {
		return r.clusterClient.Machines(ns).List(m)
	}

	deployments, err := pager.New(dfunc).List(ctx, opts)
	if err != nil {
		return []runtime.Object{}, errors.Wrapf(err, "Failed to list MachineDeployments in %s", ns)
	}
	dlist, ok := deployments.(*clusterv1.MachineDeploymentList)
	if !ok {
		return []runtime.Object{}, fmt.Errorf("Expected MachineDeploymentList, got %T", deployments)
	}

	sets, err := pager.New(sfunc).List(ctx, opts)
	if err != nil {
		return []runtime.Object{}, errors.Wrapf(err, "Failed to list MachineSets in %s", ns)
	}
	slist, ok := sets.(*clusterv1.MachineSetList)
	if !ok {
		return []runtime.Object{}, fmt.Errorf("Expected MachineSetList, got %T", sets)
	}

	machines, err := pager.New(mfunc).List(ctx, opts)
	if err != nil {
		return []runtime.Object{}, errors.Wrapf(err, "Failed to list MachineSets in %s", ns)
	}
	mlist, ok := machines.(*clusterv1.MachineList)
	if !ok {
		return []runtime.Object{}, fmt.Errorf("Expected MachineList, got %T", machines)
	}

	for _, d := range dlist.Items {
		if pointsTo(&d.ObjectMeta, &cluster.ObjectMeta) {
			children = append(children, d.DeepCopyObject())
		}
	}

	for _, s := range slist.Items {
		if pointsTo(&s.ObjectMeta, &cluster.ObjectMeta) {
			children = append(children, s.DeepCopyObject())
		}
	}

	for _, m := range mlist.Items {
		if pointsTo(&m.ObjectMeta, &cluster.ObjectMeta) {
			children = append(children, m.DeepCopyObject())
		}
	}

	return children, nil
}

func pointsTo(refs *metav1.ObjectMeta, target *metav1.ObjectMeta) bool {

	for _, ref := range refs.OwnerReferences {
		if ref.UID == target.UID {
			return true
		}
	}

	return false
}
