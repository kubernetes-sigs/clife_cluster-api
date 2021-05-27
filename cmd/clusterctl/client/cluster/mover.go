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

package cluster

import (
	"fmt"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/version"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	logf "sigs.k8s.io/cluster-api/cmd/clusterctl/log"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ObjectMover defines methods for moving Cluster API objects to another management cluster.
type ObjectMover interface {
	// Move moves all the Cluster API objects existing in a namespace (or from all the namespaces if empty) to a target management cluster.
	Move(namespace, cluster string, toCluster Client, dryRun bool) error
}

// objectMover implements the ObjectMover interface.
type objectMover struct {
	fromProxy             Proxy
	fromProviderInventory InventoryClient
	dryRun                bool
}

// ensure objectMover implements the ObjectMover interface.
var _ ObjectMover = &objectMover{}

func (o *objectMover) Move(namespace, cluster string, toCluster Client, dryRun bool) error {
	log := logf.Log
	log.Info("Performing move...")
	o.dryRun = dryRun
	if o.dryRun {
		log.Info("********************************************************")
		log.Info("This is a dry-run move, will not perform any real action")
		log.Info("********************************************************")
	}

	objectGraph := newObjectGraph(o.fromProxy)

	// checks that all the required providers in place in the target cluster.
	if !o.dryRun {
		if err := o.checkTargetProviders(namespace, toCluster.ProviderInventory()); err != nil {
			return err
		}
	}

	// Gets all the types defines by the CRDs installed by clusterctl plus the ConfigMap/Secret core types.
	err := objectGraph.getDiscoveryTypes()
	if err != nil {
		return err
	}

	// Discovery the object graph for the selected types:
	// - Nodes are defined the Kubernetes objects (Clusters, Machines etc.) identified during the discovery process.
	// - Edges are derived by the OwnerReferences between nodes.
	if err := objectGraph.Discovery(namespace, cluster); err != nil {
		return err
	}

	// Checks if Cluster API has already completed the provisioning of the infrastructure for the objects involved in the move operation.
	// This is required because if the infrastructure is provisioned, then we can reasonably assume that the objects we are moving are
	// not currently waiting for long-running reconciliation loops, and so we can safely rely on the pause field on the Cluster object
	// for blocking any further object reconciliation on the source objects.
	if err := o.checkProvisioningCompleted(objectGraph); err != nil {
		return err
	}

	// Check whether nodes are not included in GVK considered for move
	objectGraph.checkVirtualNode()

	// Move the objects to the target cluster.
	var proxy Proxy
	if !o.dryRun {
		proxy = toCluster.Proxy()
	}

	if err := o.move(objectGraph, proxy); err != nil {
		return err
	}

	return nil
}

func newObjectMover(fromProxy Proxy, fromProviderInventory InventoryClient) *objectMover {
	return &objectMover{
		fromProxy:             fromProxy,
		fromProviderInventory: fromProviderInventory,
	}
}

// checkProvisioningCompleted checks if Cluster API has already completed the provisioning of the infrastructure for the objects involved in the move operation.
func (o *objectMover) checkProvisioningCompleted(graph *objectGraph) error {
	if o.dryRun {
		return nil
	}
	errList := []error{}

	// Checking all the clusters have infrastructure is ready
	readClusterBackoff := newReadBackoff()
	clusters := graph.getClusters()
	for i := range clusters {
		cluster := clusters[i]
		clusterObj := &clusterv1.Cluster{}
		if err := retryWithExponentialBackoff(readClusterBackoff, func() error {
			return getClusterObj(o.fromProxy, cluster, clusterObj)
		}); err != nil {
			return err
		}

		if !clusterObj.Status.InfrastructureReady {
			errList = append(errList, errors.Errorf("cannot start the move operation while %q %s/%s is still provisioning the infrastructure", clusterObj.GroupVersionKind(), clusterObj.GetNamespace(), clusterObj.GetName()))
			continue
		}

		// Note: can't use IsFalse here because we need to handle the absence of the condition as well as false.
		if !conditions.IsTrue(clusterObj, clusterv1.ControlPlaneInitializedCondition) {
			errList = append(errList, errors.Errorf("cannot start the move operation while the control plane for %q %s/%s is not yet initialized", clusterObj.GroupVersionKind(), clusterObj.GetNamespace(), clusterObj.GetName()))
			continue
		}

		if clusterObj.Spec.ControlPlaneRef != nil && !clusterObj.Status.ControlPlaneReady {
			errList = append(errList, errors.Errorf("cannot start the move operation while the control plane for %q %s/%s is not yet ready", clusterObj.GroupVersionKind(), clusterObj.GetNamespace(), clusterObj.GetName()))
			continue
		}
	}

	// Checking all the machine have a NodeRef
	// Nb. NodeRef is considered a better signal than InfrastructureReady, because it ensures the node in the workload cluster is up and running.
	readMachinesBackoff := newReadBackoff()
	machines := graph.getMachines()
	for i := range machines {
		machine := machines[i]
		machineObj := &clusterv1.Machine{}
		if err := retryWithExponentialBackoff(readMachinesBackoff, func() error {
			return getMachineObj(o.fromProxy, machine, machineObj)
		}); err != nil {
			return err
		}

		if machineObj.Status.NodeRef == nil {
			errList = append(errList, errors.Errorf("cannot start the move operation while %q %s/%s is still provisioning the node", machineObj.GroupVersionKind(), machineObj.GetNamespace(), machineObj.GetName()))
		}
	}

	return kerrors.NewAggregate(errList)
}

// getClusterObj retrieves the the clusterObj corresponding to a node with type Cluster.
func getClusterObj(proxy Proxy, cluster *node, clusterObj *clusterv1.Cluster) error {
	c, err := proxy.NewClient()
	if err != nil {
		return err
	}
	clusterObjKey := client.ObjectKey{
		Namespace: cluster.identity.Namespace,
		Name:      cluster.identity.Name,
	}

	if err := c.Get(ctx, clusterObjKey, clusterObj); err != nil {
		return errors.Wrapf(err, "error reading %q %s/%s",
			clusterObj.GroupVersionKind(), clusterObj.GetNamespace(), clusterObj.GetName())
	}
	return nil
}

// getMachineObj retrieves the the machineObj corresponding to a node with type Machine.
func getMachineObj(proxy Proxy, machine *node, machineObj *clusterv1.Machine) error {
	c, err := proxy.NewClient()
	if err != nil {
		return err
	}
	machineObjKey := client.ObjectKey{
		Namespace: machine.identity.Namespace,
		Name:      machine.identity.Name,
	}

	if err := c.Get(ctx, machineObjKey, machineObj); err != nil {
		return errors.Wrapf(err, "error reading %q %s/%s",
			machineObj.GroupVersionKind(), machineObj.GetNamespace(), machineObj.GetName())
	}
	return nil
}

// Move moves all the Cluster API objects existing in a namespace (or from all the namespaces if empty) to a target management cluster.
func (o *objectMover) move(graph *objectGraph, toProxy Proxy) error {
	log := logf.Log

	clusters := graph.getClusters()
	log.Info("Moving Cluster API objects", "Clusters", len(clusters))

	// Sets the pause field on the Cluster object in the source management cluster, so the controllers stop reconciling it.
	log.V(1).Info("Pausing the source cluster")
	if err := setClusterPause(o.fromProxy, clusters, true, o.dryRun); err != nil {
		return err
	}

	// Ensure all the expected target namespaces are in place before creating objects.
	log.V(1).Info("Creating target namespaces, if missing")
	if err := o.ensureNamespaces(graph, toProxy); err != nil {
		return err
	}

	// Define the move sequence by processing the ownerReference chain, so we ensure that a Kubernetes object is moved only after its owners.
	// The sequence is bases on object graph nodes, each one representing a Kubernetes object; nodes are grouped, so bulk of nodes can be moved in parallel. e.g.
	// - All the Clusters should be moved first (group 1, processed in parallel)
	// - All the MachineDeployments should be moved second (group 1, processed in parallel)
	// - then all the MachineSets, then all the Machines, etc.
	moveSequence := getMoveSequence(graph)

	// Create all objects group by group, ensuring all the ownerReferences are re-created.
	log.Info("Creating objects in the target cluster")
	for groupIndex := 0; groupIndex < len(moveSequence.groups); groupIndex++ {
		if err := o.createGroup(moveSequence.getGroup(groupIndex), toProxy); err != nil {
			return err
		}
	}

	// Delete all objects group by group in reverse order.
	log.Info("Deleting objects from the source cluster")
	for groupIndex := len(moveSequence.groups) - 1; groupIndex >= 0; groupIndex-- {
		if err := o.deleteGroup(moveSequence.getGroup(groupIndex)); err != nil {
			return err
		}
	}

	// Reset the pause field on the Cluster object in the target management cluster, so the controllers start reconciling it.
	log.V(1).Info("Resuming the target cluster")
	if err := setClusterPause(toProxy, clusters, false, o.dryRun); err != nil {
		return err
	}

	return nil
}

// moveSequence defines a list of group of moveGroups.
type moveSequence struct {
	groups   []moveGroup
	nodesMap map[*node]empty
}

// moveGroup defines is a list of nodes read from the object graph that can be moved in parallel.
type moveGroup []*node

func (s *moveSequence) addGroup(group moveGroup) {
	// Add the group
	s.groups = append(s.groups, group)
	// Add all the nodes in the group to the nodeMap so we can check if a node is already in the move sequence or not
	for _, n := range group {
		s.nodesMap[n] = empty{}
	}
}

func (s *moveSequence) hasNode(n *node) bool {
	_, ok := s.nodesMap[n]
	return ok
}

func (s *moveSequence) getGroup(i int) moveGroup {
	return s.groups[i]
}

// Define the move sequence by processing the ownerReference chain.
func getMoveSequence(graph *objectGraph) *moveSequence {
	moveSequence := &moveSequence{
		groups:   []moveGroup{},
		nodesMap: make(map[*node]empty),
	}

	for {
		// Determine the next move group by processing all the nodes in the graph that belong to a Cluster.
		// NB. it is necessary to filter out nodes not belonging to a cluster because e.g. discovery reads all the secrets,
		// but only few of them are related to Clusters/Machines etc.
		moveGroup := moveGroup{}

		for _, n := range graph.getMoveNodes() {
			// If the node was already included in the moveSequence, skip it.
			if moveSequence.hasNode(n) {
				continue
			}

			// Check if all the ownerReferences are already included in the move sequence; if yes, add the node to move group,
			// otherwise skip it (the node will be re-processed in the next group).
			ownersInPlace := true
			for owner := range n.owners {
				if !moveSequence.hasNode(owner) {
					ownersInPlace = false
					break
				}
			}
			for owner := range n.softOwners {
				if !moveSequence.hasNode(owner) {
					ownersInPlace = false
					break
				}
			}
			if ownersInPlace {
				moveGroup = append(moveGroup, n)
			}
		}

		// If the resulting move group is empty it means that all the nodes are already in the sequence, so exit.
		if len(moveGroup) == 0 {
			break
		}
		moveSequence.addGroup(moveGroup)
	}
	return moveSequence
}

// setClusterPause sets the paused field on nodes referring to Cluster objects.
func setClusterPause(proxy Proxy, clusters []*node, value bool, dryRun bool) error {
	if dryRun {
		return nil
	}

	log := logf.Log
	patch := client.RawPatch(types.MergePatchType, []byte(fmt.Sprintf("{\"spec\":{\"paused\":%t}}", value)))

	setClusterPauseBackoff := newWriteBackoff()
	for i := range clusters {
		cluster := clusters[i]
		log.V(5).Info("Set Cluster.Spec.Paused", "Paused", value, "Cluster", cluster.identity.Name, "Namespace", cluster.identity.Namespace)

		// Nb. The operation is wrapped in a retry loop to make setClusterPause more resilient to unexpected conditions.
		if err := retryWithExponentialBackoff(setClusterPauseBackoff, func() error {
			return patchCluster(proxy, cluster, patch)
		}); err != nil {
			return errors.Wrapf(err, "error setting Cluster.Spec.Paused=%t", value)
		}
	}
	return nil
}

// patchCluster applies a patch to a node referring to a Cluster object.
func patchCluster(proxy Proxy, cluster *node, patch client.Patch) error {
	cFrom, err := proxy.NewClient()
	if err != nil {
		return err
	}

	clusterObj := &clusterv1.Cluster{}
	clusterObjKey := client.ObjectKey{
		Namespace: cluster.identity.Namespace,
		Name:      cluster.identity.Name,
	}

	if err := cFrom.Get(ctx, clusterObjKey, clusterObj); err != nil {
		return errors.Wrapf(err, "error reading %q %s/%s",
			clusterObj.GroupVersionKind(), clusterObj.GetNamespace(), clusterObj.GetName())
	}

	if err := cFrom.Patch(ctx, clusterObj, patch); err != nil {
		return errors.Wrapf(err, "error patching %q %s/%s",
			clusterObj.GroupVersionKind(), clusterObj.GetNamespace(), clusterObj.GetName())
	}

	return nil
}

// ensureNamespaces ensures all the expected target namespaces are in place before creating objects.
func (o *objectMover) ensureNamespaces(graph *objectGraph, toProxy Proxy) error {
	if o.dryRun {
		return nil
	}

	ensureNamespaceBackoff := newWriteBackoff()
	namespaces := sets.NewString()
	for _, node := range graph.getMoveNodes() {
		// ignore global/cluster-wide objects
		if node.isGlobal {
			continue
		}

		namespace := node.identity.Namespace

		// If the namespace was already processed, skip it.
		if namespaces.Has(namespace) {
			continue
		}
		namespaces.Insert(namespace)

		if err := retryWithExponentialBackoff(ensureNamespaceBackoff, func() error {
			return o.ensureNamespace(toProxy, namespace)
		}); err != nil {
			return err
		}
	}

	return nil
}

// ensureNamespace ensures a target namespaces is in place before creating objects.
func (o *objectMover) ensureNamespace(toProxy Proxy, namespace string) error {
	log := logf.Log

	cs, err := toProxy.NewClient()
	if err != nil {
		return err
	}

	// Otherwise check if namespace exists (also dealing with RBAC restrictions).
	ns := &corev1.Namespace{}
	key := client.ObjectKey{
		Name: namespace,
	}

	err = cs.Get(ctx, key, ns)
	if err == nil {
		return nil
	}
	if apierrors.IsForbidden(err) {
		namespaces := &corev1.NamespaceList{}
		namespaceExists := false
		for {
			if err := cs.List(ctx, namespaces, client.Continue(namespaces.Continue)); err != nil {
				return err
			}

			for _, ns := range namespaces.Items {
				if ns.Name == namespace {
					namespaceExists = true
					break
				}
			}

			if namespaces.Continue == "" {
				break
			}
		}
		if namespaceExists {
			return nil
		}
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	// If the namespace does not exists, create it.
	ns = &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Namespace",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}
	log.V(1).Info("Creating", ns.Kind, ns.Name)
	if err := cs.Create(ctx, ns); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

// createGroup creates all the Kubernetes objects into the target management cluster corresponding to the object graph nodes in a moveGroup.
func (o *objectMover) createGroup(group moveGroup, toProxy Proxy) error {
	createTargetObjectBackoff := newWriteBackoff()
	errList := []error{}
	for i := range group {
		nodeToCreate := group[i]

		// Creates the Kubernetes object corresponding to the nodeToCreate.
		// Nb. The operation is wrapped in a retry loop to make move more resilient to unexpected conditions.
		err := retryWithExponentialBackoff(createTargetObjectBackoff, func() error {
			return o.createTargetObject(nodeToCreate, toProxy)
		})
		if err != nil {
			errList = append(errList, err)
		}
	}

	if len(errList) > 0 {
		return kerrors.NewAggregate(errList)
	}

	return nil
}

// createTargetObject creates the Kubernetes object in the target Management cluster corresponding to the object graph node, taking care of restoring the OwnerReference with the owner nodes, if any.
func (o *objectMover) createTargetObject(nodeToCreate *node, toProxy Proxy) error {
	log := logf.Log
	log.V(1).Info("Creating", nodeToCreate.identity.Kind, nodeToCreate.identity.Name, "Namespace", nodeToCreate.identity.Namespace)

	if o.dryRun {
		return nil
	}

	cFrom, err := o.fromProxy.NewClient()
	if err != nil {
		return err
	}

	// Get the source object
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion(nodeToCreate.identity.APIVersion)
	obj.SetKind(nodeToCreate.identity.Kind)
	objKey := client.ObjectKey{
		Namespace: nodeToCreate.identity.Namespace,
		Name:      nodeToCreate.identity.Name,
	}

	if err := cFrom.Get(ctx, objKey, obj); err != nil {
		return errors.Wrapf(err, "error reading %q %s/%s",
			obj.GroupVersionKind(), obj.GetNamespace(), obj.GetName())
	}

	// New objects cannot have a specified resource version. Clear it out.
	obj.SetResourceVersion("")

	// Removes current OwnerReferences
	obj.SetOwnerReferences(nil)

	// Recreate all the OwnerReferences using the newUID of the owner nodes.
	if len(nodeToCreate.owners) > 0 {
		ownerRefs := []metav1.OwnerReference{}
		for ownerNode := range nodeToCreate.owners {
			ownerRef := metav1.OwnerReference{
				APIVersion: ownerNode.identity.APIVersion,
				Kind:       ownerNode.identity.Kind,
				Name:       ownerNode.identity.Name,
				UID:        ownerNode.newUID, // Use the owner's newUID read from the target management cluster (instead of the UID read during discovery).
			}

			// Restores the attributes of the OwnerReference.
			if attributes, ok := nodeToCreate.owners[ownerNode]; ok {
				ownerRef.Controller = attributes.Controller
				ownerRef.BlockOwnerDeletion = attributes.BlockOwnerDeletion
			}

			ownerRefs = append(ownerRefs, ownerRef)
		}
		obj.SetOwnerReferences(ownerRefs)
	}

	// Creates the targetObj into the target management cluster.
	cTo, err := toProxy.NewClient()
	if err != nil {
		return err
	}

	if err := cTo.Create(ctx, obj); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return errors.Wrapf(err, "error creating %q %s/%s",
				obj.GroupVersionKind(), obj.GetNamespace(), obj.GetName())
		}

		// If the object already exists, try to update it.
		// Nb. This should not happen, but it is supported to make move more resilient to unexpected interrupt/restarts of the move process.
		log.V(5).Info("Object already exists, updating", nodeToCreate.identity.Kind, nodeToCreate.identity.Name, "Namespace", nodeToCreate.identity.Namespace)

		// Retrieve the UID and the resource version for the update.
		existingTargetObj := &unstructured.Unstructured{}
		existingTargetObj.SetAPIVersion(obj.GetAPIVersion())
		existingTargetObj.SetKind(obj.GetKind())
		if err := cTo.Get(ctx, objKey, existingTargetObj); err != nil {
			return errors.Wrapf(err, "error reading resource for %q %s/%s",
				existingTargetObj.GroupVersionKind(), existingTargetObj.GetNamespace(), existingTargetObj.GetName())
		}

		obj.SetUID(existingTargetObj.GetUID())
		obj.SetResourceVersion(existingTargetObj.GetResourceVersion())
		if err := cTo.Update(ctx, obj); err != nil {
			return errors.Wrapf(err, "error updating %q %s/%s",
				obj.GroupVersionKind(), obj.GetNamespace(), obj.GetName())
		}
	}

	// Stores the newUID assigned to the newly created object.
	nodeToCreate.newUID = obj.GetUID()

	return nil
}

// deleteGroup deletes all the Kubernetes objects from the source management cluster corresponding to the object graph nodes in a moveGroup.
func (o *objectMover) deleteGroup(group moveGroup) error {
	deleteSourceObjectBackoff := newWriteBackoff()
	errList := []error{}
	for i := range group {
		nodeToDelete := group[i]

		// Don't delete cluster-wide nodes
		if nodeToDelete.isGlobal {
			continue
		}

		// Delete the Kubernetes object corresponding to the current node.
		// Nb. The operation is wrapped in a retry loop to make move more resilient to unexpected conditions.
		err := retryWithExponentialBackoff(deleteSourceObjectBackoff, func() error {
			return o.deleteSourceObject(nodeToDelete)
		})

		if err != nil {
			errList = append(errList, err)
		}
	}

	return kerrors.NewAggregate(errList)
}

var (
	removeFinalizersPatch = client.RawPatch(types.MergePatchType, []byte("{\"metadata\":{\"finalizers\":[]}}"))
)

// deleteSourceObject deletes the Kubernetes object corresponding to the node from the source management cluster, taking care of removing all the finalizers so
// the objects gets immediately deleted (force delete).
func (o *objectMover) deleteSourceObject(nodeToDelete *node) error {
	log := logf.Log
	log.V(1).Info("Deleting", nodeToDelete.identity.Kind, nodeToDelete.identity.Name, "Namespace", nodeToDelete.identity.Namespace)

	if o.dryRun {
		return nil
	}

	cFrom, err := o.fromProxy.NewClient()
	if err != nil {
		return err
	}

	// Get the source object
	sourceObj := &unstructured.Unstructured{}
	sourceObj.SetAPIVersion(nodeToDelete.identity.APIVersion)
	sourceObj.SetKind(nodeToDelete.identity.Kind)
	sourceObjKey := client.ObjectKey{
		Namespace: nodeToDelete.identity.Namespace,
		Name:      nodeToDelete.identity.Name,
	}

	if err := cFrom.Get(ctx, sourceObjKey, sourceObj); err != nil {
		if apierrors.IsNotFound(err) {
			// If the object is already deleted, move on.
			log.V(5).Info("Object already deleted, skipping delete for", nodeToDelete.identity.Kind, nodeToDelete.identity.Name, "Namespace", nodeToDelete.identity.Namespace)
			return nil
		}
		return errors.Wrapf(err, "error reading %q %s/%s",
			sourceObj.GroupVersionKind(), sourceObj.GetNamespace(), sourceObj.GetName())
	}

	if len(sourceObj.GetFinalizers()) > 0 {
		if err := cFrom.Patch(ctx, sourceObj, removeFinalizersPatch); err != nil {
			return errors.Wrapf(err, "error removing finalizers from %q %s/%s",
				sourceObj.GroupVersionKind(), sourceObj.GetNamespace(), sourceObj.GetName())
		}
	}

	if err := cFrom.Delete(ctx, sourceObj); err != nil {
		return errors.Wrapf(err, "error deleting %q %s/%s",
			sourceObj.GroupVersionKind(), sourceObj.GetNamespace(), sourceObj.GetName())
	}

	return nil
}

// checkTargetProviders checks that all the providers installed in the source cluster exists in the target cluster as well (with a version >= of the current version).
func (o *objectMover) checkTargetProviders(namespace string, toInventory InventoryClient) error {
	if o.dryRun {
		return nil
	}

	// Gets the list of providers in the source/target cluster.
	fromProviders, err := o.fromProviderInventory.List()
	if err != nil {
		return errors.Wrapf(err, "failed to get provider list from the source cluster")
	}

	toProviders, err := toInventory.List()
	if err != nil {
		return errors.Wrapf(err, "failed to get provider list from the target cluster")
	}

	// Checks all the providers installed in the source cluster
	errList := []error{}
	for _, sourceProvider := range fromProviders.Items {
		// If we are moving objects in a namespace only, skip all the providers not watching such namespace.
		if namespace != "" && !(sourceProvider.WatchedNamespace == "" || sourceProvider.WatchedNamespace == namespace) {
			continue
		}

		sourceVersion, err := version.ParseSemantic(sourceProvider.Version)
		if err != nil {
			return errors.Wrapf(err, "unable to parse version %q for the %s provider in the source cluster", sourceProvider.Version, sourceProvider.InstanceName())
		}

		// Check corresponding providers in the target cluster and gets the latest version installed.
		var maxTargetVersion *version.Version
		for _, targetProvider := range toProviders.Items {
			// Skips other providers.
			if !sourceProvider.SameAs(targetProvider) {
				continue
			}

			// If we are moving objects in all the namespaces, skip all the providers with a different watching namespace.
			// NB. This introduces a constraints for move all namespaces, that the configuration of source and target provider MUST match (except for the version);
			// however this is acceptable because clusterctl supports only two models of multi-tenancy (n-Infra, n-Core).
			if namespace == "" && !(targetProvider.WatchedNamespace == sourceProvider.WatchedNamespace) {
				continue
			}

			// If we are moving objects in a namespace only, skip all the providers not watching such namespace.
			// NB. This means that when moving a single namespace, we use a lazy matching (the watching namespace MUST overlap; exact match is not required).
			if namespace != "" && !(targetProvider.WatchedNamespace == "" || targetProvider.WatchedNamespace == namespace) {
				continue
			}

			targetVersion, err := version.ParseSemantic(targetProvider.Version)
			if err != nil {
				return errors.Wrapf(err, "unable to parse version %q for the %s provider in the target cluster", targetProvider.Version, targetProvider.InstanceName())
			}
			if maxTargetVersion == nil || maxTargetVersion.LessThan(targetVersion) {
				maxTargetVersion = targetVersion
			}
		}
		if maxTargetVersion == nil {
			watching := sourceProvider.WatchedNamespace
			if namespace != "" {
				watching = namespace
			}
			errList = append(errList, errors.Errorf("provider %s watching namespace %s not found in the target cluster", sourceProvider.Name, watching))
			continue
		}

		if !maxTargetVersion.AtLeast(sourceVersion) {
			errList = append(errList, errors.Errorf("provider %s in the target cluster is older than in the source cluster (source: %s, target: %s)", sourceProvider.Name, sourceVersion.String(), maxTargetVersion.String()))
		}
	}

	return kerrors.NewAggregate(errList)
}
