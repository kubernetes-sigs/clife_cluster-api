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
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"time"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	"sigs.k8s.io/cluster-api/controllers/remote"
	addonsv1 "sigs.k8s.io/cluster-api/exp/addons/api/v1alpha3"
	resourcepredicates "sigs.k8s.io/cluster-api/exp/addons/controllers/predicates"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/cluster-api/util/predicates"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var (
	ErrSecretTypeNotSupported = errors.New("unsupported secret type")
)

// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups=addons.cluster.x-k8s.io,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=addons.cluster.x-k8s.io,resources=clusterresourcesets/status,verbs=get;update;patch

// ClusterResourceSetReconciler reconciles a ClusterResourceSet object
type ClusterResourceSetReconciler struct {
	Client  client.Client
	Log     logr.Logger
	Tracker *remote.ClusterCacheTracker

	scheme *runtime.Scheme
}

func (r *ClusterResourceSetReconciler) SetupWithManager(mgr ctrl.Manager, options controller.Options) error {
	c, err := ctrl.NewControllerManagedBy(mgr).
		For(&addonsv1.ClusterResourceSet{}).
		Watches(
			&source.Kind{Type: &clusterv1.Cluster{}},
			&handler.EnqueueRequestsFromMapFunc{ToRequests: handler.ToRequestsFunc(r.clusterToClusterResourceSet)},
		).
		WithOptions(options).
		WithEventFilter(predicates.ResourceNotPaused(r.Log)).
		Build(r)
	if err != nil {
		return errors.Wrap(err, "failed setting up with a controller manager")
	}
	err = c.Watch(
		&source.Kind{Type: builder.OnlyMetadata(&corev1.ConfigMap{})},
		&handler.EnqueueRequestsFromMapFunc{
			ToRequests: handler.ToRequestsFunc(r.resourceToClusterResourceSet),
		},
		resourcepredicates.ResourceCreate(r.Log),
	)
	if err != nil {
		return errors.Wrap(err, "failed adding Watch for ConfigMaps to controller manager")
	}

	err = c.Watch(
		&source.Kind{Type: builder.OnlyMetadata(&corev1.Secret{})},
		&handler.EnqueueRequestsFromMapFunc{
			ToRequests: handler.ToRequestsFunc(r.resourceToClusterResourceSet),
		},
		resourcepredicates.ResourceCreate(r.Log),
	)
	if err != nil {
		return errors.Wrap(err, "failed adding Watch for Secrets to controller manager")
	}

	r.scheme = mgr.GetScheme()
	return nil
}

func (r *ClusterResourceSetReconciler) Reconcile(req ctrl.Request) (_ ctrl.Result, reterr error) {
	ctx := context.Background()

	// Fetch the ClusterResourceSet instance.
	clusterResourceSet := &addonsv1.ClusterResourceSet{}
	if err := r.Client.Get(ctx, req.NamespacedName, clusterResourceSet); err != nil {
		if apierrors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	// Initialize the patch helper.
	patchHelper, err := patch.NewHelper(clusterResourceSet, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	defer func() {
		// Always attempt to Patch the ClusterResourceSet object and status after each reconciliation.
		if err := patchHelper.Patch(ctx, clusterResourceSet, patch.WithStatusObservedGeneration{}); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	logger := r.Log.WithValues("clusterresourceset", clusterResourceSet.Name, "namespace", clusterResourceSet.Namespace)

	clusters, err := r.getClustersByClusterResourceSetSelector(ctx, clusterResourceSet)
	if err != nil {
		logger.Error(err, "Failed fetching clusters that matches ClusterResourceSet labels", "ClusterResourceSet", clusterResourceSet.Name)
		conditions.MarkFalse(clusterResourceSet, addonsv1.ResourcesAppliedCondition, addonsv1.ClusterMatchFailedReason, clusterv1.ConditionSeverityWarning, err.Error())
		return ctrl.Result{}, err
	}

	// Add finalizer first if not exist to avoid the race condition between init and delete
	if !controllerutil.ContainsFinalizer(clusterResourceSet, addonsv1.ClusterResourceSetFinalizer) {
		controllerutil.AddFinalizer(clusterResourceSet, addonsv1.ClusterResourceSetFinalizer)
		return ctrl.Result{}, nil
	}

	// Handle deletion reconciliation loop.
	if !clusterResourceSet.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, clusters, clusterResourceSet)
	}

	for _, cluster := range clusters {
		if err := r.ApplyClusterResourceSet(ctx, cluster, clusterResourceSet); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// reconcileDelete removes the deleted ClusterResourceSet from all the ClusterResourceSetBindings it is added to.
func (r *ClusterResourceSetReconciler) reconcileDelete(ctx context.Context, clusters []*clusterv1.Cluster, crs *addonsv1.ClusterResourceSet) (ctrl.Result, error) {
	logger := r.Log.WithValues("clusterresourceset", crs.Name, "namespace", crs.Namespace)

	for _, cluster := range clusters {
		clusterResourceSetBinding := &addonsv1.ClusterResourceSetBinding{}
		clusterResourceSetBindingKey := client.ObjectKey{
			Namespace: cluster.Namespace,
			Name:      cluster.Name,
		}
		if err := r.Client.Get(ctx, clusterResourceSetBindingKey, clusterResourceSetBinding); err != nil {
			if !apierrors.IsNotFound(err) {
				return ctrl.Result{}, errors.Wrapf(err, "failed to get ClusterResourceSetBinding during ClusterResourceSet deletion")
			}
			controllerutil.RemoveFinalizer(crs, addonsv1.ClusterResourceSetFinalizer)
			return ctrl.Result{}, nil
		}

		// Initialize the patch helper.
		patchHelper, err := patch.NewHelper(clusterResourceSetBinding, r.Client)
		if err != nil {
			return ctrl.Result{}, err
		}

		clusterResourceSetBinding.DeleteBinding(crs)

		// If CRS list is empty in the binding, delete the binding else
		// attempt to Patch the ClusterResourceSetBinding object after delete reconciliation if there is at least 1 binding left.
		if len(clusterResourceSetBinding.Spec.Bindings) == 0 {
			if r.Client.Delete(ctx, clusterResourceSetBinding) != nil {
				logger.Error(err, "failed to delete empty ClusterResourceSetBinding")
			}
		} else if err := patchHelper.Patch(ctx, clusterResourceSetBinding); err != nil {
			logger.Error(err, "failed to patch ClusterResourceSetBinding")
			return ctrl.Result{}, err
		}
	}

	controllerutil.RemoveFinalizer(crs, addonsv1.ClusterResourceSetFinalizer)
	return ctrl.Result{}, nil
}

// getClustersByClusterResourceSetSelector fetches Clusters matched by the ClusterResourceSet's label selector that are in the same namespace as the ClusterResourceSet object.
func (r *ClusterResourceSetReconciler) getClustersByClusterResourceSetSelector(ctx context.Context, clusterResourceSet *addonsv1.ClusterResourceSet) ([]*clusterv1.Cluster, error) {
	logger := r.Log.WithValues("clusterresourceset", clusterResourceSet.Name, "namespace", clusterResourceSet.Namespace)

	clusterList := &clusterv1.ClusterList{}
	selector, err := metav1.LabelSelectorAsSelector(&clusterResourceSet.Spec.ClusterSelector)
	if err != nil {
		return nil, errors.Wrap(err, "unable to convert selector")
	}

	// If a ClusterResourceSet has a nil or empty selector, it should match nothing, not everything.
	if selector.Empty() {
		logger.Info("Empty ClusterResourceSet selector: No clusters are selected.")
		return nil, nil
	}

	if err := r.Client.List(ctx, clusterList, client.InNamespace(clusterResourceSet.Namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
		return nil, errors.Wrap(err, "failed to list clusters")
	}

	clusters := []*clusterv1.Cluster{}
	for i := range clusterList.Items {
		c := &clusterList.Items[i]
		if c.DeletionTimestamp.IsZero() {
			clusters = append(clusters, c)
		}
	}
	return clusters, nil
}

// ApplyClusterResourceSet applies resources in a ClusterResourceSet to a Cluster. Once applied, a record will be added to the
// cluster's ClusterResourceSetBinding.
// In ApplyOnce strategy, resources are applied only once to a particular cluster. ClusterResourceSetBinding is used to check if a resource is applied before.
// It applies resources best effort and continue on scenarios like: unsupported resource types, failure during creation, missing resources.
// TODO: If a resource already exists in the cluster but not applied by ClusterResourceSet, the resource will be updated ?
func (r *ClusterResourceSetReconciler) ApplyClusterResourceSet(ctx context.Context, cluster *clusterv1.Cluster, clusterResourceSet *addonsv1.ClusterResourceSet) error {
	logger := r.Log.WithValues("clusterresourceset", clusterResourceSet.Name, "namespace", clusterResourceSet.Namespace, "cluster-name", cluster.Name)

	remoteClient, err := r.Tracker.GetClient(ctx, util.ObjectKey(cluster))
	if err != nil {
		conditions.MarkFalse(clusterResourceSet, addonsv1.ResourcesAppliedCondition, addonsv1.RemoteClusterClientFailedReason, clusterv1.ConditionSeverityError, err.Error())
		return err
	}

	// Get ClusterResourceSetBinding object for the cluster.
	clusterResourceSetBinding, err := r.getOrCreateClusterResourceSetBinding(ctx, cluster, clusterResourceSet)
	if err != nil {
		return err
	}

	// Initialize the patch helper.
	patchHelper, err := patch.NewHelper(clusterResourceSetBinding, r.Client)
	if err != nil {
		return err
	}

	defer func() {
		// Always attempt to Patch the ClusterResourceSetBinding object after each reconciliation.
		if err := patchHelper.Patch(ctx, clusterResourceSetBinding); err != nil {
			r.Log.Error(err, "failed to patch config")
		}
	}()

	errList := []error{}
	resourceSetBinding := clusterResourceSetBinding.GetOrCreateBinding(clusterResourceSet)

	// Iterate all resources and apply them to the cluster and update the resource status in the ClusterResourceSetBinding object.
	for _, resource := range clusterResourceSet.Spec.Resources {
		// If resource is already applied successfully and clusterResourceSet mode is "ApplyOnce", continue. (No need to check hash changes here)
		if resourceSetBinding.IsApplied(resource) {
			continue
		}

		unstructuredObj, err := r.getResource(resource, cluster.GetNamespace())
		if err != nil {
			if err == ErrSecretTypeNotSupported {
				conditions.MarkFalse(clusterResourceSet, addonsv1.ResourcesAppliedCondition, addonsv1.WrongSecretTypeReason, clusterv1.ConditionSeverityWarning, err.Error())
			} else {
				conditions.MarkFalse(clusterResourceSet, addonsv1.ResourcesAppliedCondition, addonsv1.RetrievingResourceFailedReason, clusterv1.ConditionSeverityWarning, err.Error())

				// Continue without adding the error to the aggregate if we can't find the resource.
				if apierrors.IsNotFound(errors.Cause(err)) {
					continue
				}
			}
			errList = append(errList, err)
			continue
		}

		// Set status in ClusterResourceSetBinding in case of early continue due to a failure.
		// Set only when resource is retrieved successfully.
		resourceSetBinding.SetBinding(addonsv1.ResourceBinding{
			ResourceRef:     resource,
			Hash:            "",
			Applied:         false,
			LastAppliedTime: &metav1.Time{Time: time.Now().UTC()},
		})

		if err := r.patchOwnerRefToResource(ctx, clusterResourceSet, unstructuredObj); err != nil {
			logger.Error(err, "Failed to patch ClusterResourceSet as resource owner reference",
				"Resource type", unstructuredObj.GetKind(), "Resource name", unstructuredObj.GetName())
			errList = append(errList, err)
		}

		// Since maps are not ordered, we need to order them to get the same hash at each reconcile.
		keys := make([]string, 0)
		data, ok := unstructuredObj.UnstructuredContent()["data"]
		if !ok {
			errList = append(errList, errors.New("failed to get data field from the resource"))
			continue
		}

		unstructuredData := data.(map[string]interface{})
		for key := range unstructuredData {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		dataList := make([][]byte, 0)
		for _, key := range keys {
			val, ok, err := unstructured.NestedString(unstructuredData, key)
			if !ok || err != nil {
				errList = append(errList, errors.New("failed to get value field from the resource"))
				continue
			}

			byteArr := []byte(val)
			// If the resource is a Secret, data needs to be decoded.
			if unstructuredObj.GetKind() == string(addonsv1.SecretClusterResourceSetResourceKind) {
				byteArr, _ = base64.StdEncoding.DecodeString(val)
			}

			dataList = append(dataList, byteArr)
		}

		// Apply all values in the key-value pair of the resource to the cluster.
		// As there can be multiple key-value pairs in a resource, each value may have multiple objects in it.
		isSuccessful := true
		for i := range dataList {
			data := dataList[i]

			if err := apply(ctx, remoteClient, data); err != nil {
				isSuccessful = false
				logger.Error(err, "failed to apply ClusterResourceSet resource", "Resource kind", resource.Kind, "Resource name", resource.Name)
				conditions.MarkFalse(clusterResourceSet, addonsv1.ResourcesAppliedCondition, addonsv1.ApplyFailedReason, clusterv1.ConditionSeverityWarning, err.Error())
				errList = append(errList, err)
			}
		}

		resourceSetBinding.SetBinding(addonsv1.ResourceBinding{
			ResourceRef:     resource,
			Hash:            computeHash(dataList),
			Applied:         isSuccessful,
			LastAppliedTime: &metav1.Time{Time: time.Now().UTC()},
		})
	}
	if len(errList) > 0 {
		return kerrors.NewAggregate(errList)
	}

	conditions.MarkTrue(clusterResourceSet, addonsv1.ResourcesAppliedCondition)

	return nil
}

// getResource retrieves the requested resource and convert it to unstructured type.
// Unsupported resource kinds are not denied by validation webhook, hence no need to check here.
// Only supports Secrets/Configmaps as resource types and allow using resources in the same namespace with the cluster.
func (r *ClusterResourceSetReconciler) getResource(resourceRef addonsv1.ResourceRef, namespace string) (*unstructured.Unstructured, error) {
	resourceName := types.NamespacedName{Name: resourceRef.Name, Namespace: namespace}

	var resourceInterface interface{}
	switch resourceRef.Kind {
	case string(addonsv1.ConfigMapClusterResourceSetResourceKind):
		resourceConfigMap, err := getConfigMap(context.Background(), r.Client, resourceName)
		if err != nil {
			return nil, err
		}

		resourceInterface = resourceConfigMap.DeepCopyObject()
	case string(addonsv1.SecretClusterResourceSetResourceKind):
		resourceSecret, err := getSecret(context.Background(), r.Client, resourceName)
		if err != nil {
			return nil, err
		}

		if resourceSecret.Type != addonsv1.ClusterResourceSetSecretType {
			return nil, ErrSecretTypeNotSupported
		}
		resourceInterface = resourceSecret.DeepCopyObject()
	}

	raw := &unstructured.Unstructured{}
	if err := r.scheme.Convert(resourceInterface, raw, nil); err != nil {
		return nil, err
	}

	return raw, nil
}

// patchOwnerRefToResource adds the ClusterResourceSet as a OwnerReference to the resource.
func (r *ClusterResourceSetReconciler) patchOwnerRefToResource(ctx context.Context, clusterResourceSet *addonsv1.ClusterResourceSet, resource *unstructured.Unstructured) error {
	newRef := metav1.OwnerReference{
		APIVersion: clusterResourceSet.GroupVersionKind().GroupVersion().String(),
		Kind:       clusterResourceSet.GroupVersionKind().Kind,
		Name:       clusterResourceSet.GetName(),
		UID:        clusterResourceSet.GetUID(),
	}

	refs := resource.GetOwnerReferences()
	if !util.IsOwnedByObject(resource, clusterResourceSet) {
		patch := client.MergeFrom(resource.DeepCopy())
		refs = append(refs, newRef)
		resource.SetOwnerReferences(refs)
		return r.Client.Patch(ctx, resource, patch)
	}
	return nil
}

// clusterToClusterResourceSet is mapper function that maps clusters to ClusterResourceSet
func (r *ClusterResourceSetReconciler) clusterToClusterResourceSet(o handler.MapObject) []ctrl.Request {
	result := []ctrl.Request{}

	cluster, ok := o.Object.(*clusterv1.Cluster)
	if !ok {
		r.Log.Error(nil, fmt.Sprintf("Expected a Cluster but got a %T", o.Object))
		return nil
	}

	resourceList := &addonsv1.ClusterResourceSetList{}
	if err := r.Client.List(context.Background(), resourceList, client.InNamespace(cluster.Namespace)); err != nil {
		r.Log.Error(err, "failed to list ClusterResourceSet")
		return nil
	}

	labels := labels.Set(cluster.GetLabels())
	for i := range resourceList.Items {
		rs := &resourceList.Items[i]

		selector, err := metav1.LabelSelectorAsSelector(&rs.Spec.ClusterSelector)
		if err != nil {
			r.Log.Error(err, "unable to convert ClusterSelector to selector")
			return nil
		}

		// If a ClusterResourceSet has a nil or empty selector, it should match nothing, not everything.
		if selector.Empty() {
			return nil
		}

		if !selector.Matches(labels) {
			continue
		}

		name := client.ObjectKey{Namespace: rs.Namespace, Name: rs.Name}
		result = append(result, ctrl.Request{NamespacedName: name})
	}
	return result
}

// resourceToClusterResourceSet is mapper function that maps resources to ClusterResourceSet
func (r *ClusterResourceSetReconciler) resourceToClusterResourceSet(o handler.MapObject) []ctrl.Request {
	result := []ctrl.Request{}

	// Add all ClusterResourceSet owners.
	for _, owner := range o.Meta.GetOwnerReferences() {
		if owner.Kind == "ClusterResourceSet" {
			name := client.ObjectKey{Namespace: o.Meta.GetNamespace(), Name: owner.Name}
			result = append(result, ctrl.Request{NamespacedName: name})
		}
	}

	// If there is any ClusterResourceSet owner, that means the resource is reconciled before,
	// and existing owners are the only matching ClusterResourceSets to this resource, so no need to return all ClusterResourceSets.
	if len(result) > 0 {
		return result
	}

	// Only core group is accepted as resources group
	if o.Object.GetObjectKind().GroupVersionKind().Group != "" {
		return result
	}

	crsList := &addonsv1.ClusterResourceSetList{}
	if err := r.Client.List(context.Background(), crsList, client.InNamespace(o.Meta.GetNamespace())); err != nil {
		return nil
	}
	objKind, err := apiutil.GVKForObject(o.Object, r.scheme)
	if err != nil {
		return nil
	}
	for _, crs := range crsList.Items {
		for _, resource := range crs.Spec.Resources {
			if resource.Kind == objKind.Kind && resource.Name == o.Meta.GetName() {
				name := client.ObjectKey{Namespace: o.Meta.GetNamespace(), Name: crs.Name}
				result = append(result, ctrl.Request{NamespacedName: name})
				break
			}
		}
	}

	return result
}
