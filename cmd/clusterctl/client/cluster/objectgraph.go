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
	"strings"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	clusterctlv1 "sigs.k8s.io/cluster-api/cmd/clusterctl/api/v1alpha3"
	logf "sigs.k8s.io/cluster-api/cmd/clusterctl/log"
	addonsv1 "sigs.k8s.io/cluster-api/exp/addons/api/v1alpha4"
	secretutil "sigs.k8s.io/cluster-api/util/secret"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type empty struct{}

type ownerReferenceAttributes struct {
	Controller         *bool
	BlockOwnerDeletion *bool
}

// node defines a node in the Kubernetes object graph that is visited during the discovery phase for the move operation.
type node struct {
	identity corev1.ObjectReference

	// owners contains the list of nodes that are owned by the current node.
	owners map[*node]ownerReferenceAttributes

	// softOwners contains the list of nodes that are soft-owned by the current node.
	// E.g. secrets are soft-owned by a cluster via a naming convention, but without an explicit OwnerReference.
	softOwners map[*node]empty

	// forceMove is set to true if the CRD of this object has the "move" label attached.
	// This ensures the node is moved, regardless of its owner refs.
	forceMove bool

	// isGlobal gets set to true if this object is a global resource (no namespace).
	isGlobal bool

	// virtual records if this node was discovered indirectly, e.g. by processing an OwnerRef, but not yet observed as a concrete object.
	virtual bool

	// newID stores the new UID the objects gets once created in the target cluster.
	newUID types.UID

	// tenantClusters define the list of Clusters which are tenant for the node, no matter if the node has a direct OwnerReference to the Cluster or if
	// the node is linked to a Cluster indirectly in the OwnerReference chain.
	tenantClusters map[*node]empty

	// tenantCRSs define the list of ClusterResourceSet which are tenant for the node, no matter if the node has a direct OwnerReference to the ClusterResourceSet or if
	// the node is linked to a ClusterResourceSet indirectly in the OwnerReference chain.
	tenantCRSs map[*node]empty
}

type discoveryTypeInfo struct {
	typeMeta  metav1.TypeMeta
	forceMove bool
}

// markObserved marks the fact that a node was observed as a concrete object.
func (n *node) markObserved() {
	n.virtual = false
}

func (n *node) addOwner(owner *node, attributes ownerReferenceAttributes) {
	n.owners[owner] = attributes
}

func (n *node) addSoftOwner(owner *node) {
	n.softOwners[owner] = struct{}{}
}

func (n *node) isOwnedBy(other *node) bool {
	_, ok := n.owners[other]
	return ok
}

func (n *node) isSoftOwnedBy(other *node) bool {
	_, ok := n.softOwners[other]
	return ok
}

// objectGraph manages the Kubernetes object graph that is generated during the discovery phase for the move operation.
type objectGraph struct {
	proxy     Proxy
	uidToNode map[types.UID]*node
	types     map[string]*discoveryTypeInfo
}

func newObjectGraph(proxy Proxy) *objectGraph {
	return &objectGraph{
		proxy:     proxy,
		uidToNode: map[types.UID]*node{},
	}
}

// addObj adds a Kubernetes object to the object graph that is generated during the move discovery phase.
// During add, OwnerReferences are processed in order to create the dependency graph.
func (o *objectGraph) addObj(obj *unstructured.Unstructured) {
	// Adds the node to the Graph.
	newNode := o.objToNode(obj)

	// Process OwnerReferences; if the owner object doe not exists yet, create a virtual node as a placeholder for it.
	for _, ownerReference := range obj.GetOwnerReferences() {
		ownerNode, ok := o.uidToNode[ownerReference.UID]
		if !ok {
			ownerNode = o.ownerToVirtualNode(ownerReference, newNode.identity.Namespace)
		}

		newNode.addOwner(ownerNode, ownerReferenceAttributes{
			Controller:         ownerReference.Controller,
			BlockOwnerDeletion: ownerReference.BlockOwnerDeletion,
		})
	}
}

// ownerToVirtualNode creates a virtual node as a placeholder for the Kubernetes owner object received in input.
// The virtual node will be eventually converted to an actual node when the node will be visited during discovery.
func (o *objectGraph) ownerToVirtualNode(owner metav1.OwnerReference, namespace string) *node {
	isGlobal := false
	if namespace == "" {
		isGlobal = true
	}

	ownerNode := &node{
		identity: corev1.ObjectReference{
			APIVersion: owner.APIVersion,
			Kind:       owner.Kind,
			Name:       owner.Name,
			UID:        owner.UID,
			Namespace:  namespace,
		},
		owners:         make(map[*node]ownerReferenceAttributes),
		softOwners:     make(map[*node]empty),
		tenantClusters: make(map[*node]empty),
		tenantCRSs:     make(map[*node]empty),
		virtual:        true,
		forceMove:      o.getForceMove(owner.Kind, owner.APIVersion, nil),
		isGlobal:       isGlobal,
	}

	o.uidToNode[ownerNode.identity.UID] = ownerNode
	return ownerNode
}

// objToNode creates a node for the Kubernetes object received in input.
// If the node corresponding to the Kubernetes object already exists as a virtual node detected when processing OwnerReferences,
// the node is marked as Observed.
func (o *objectGraph) objToNode(obj *unstructured.Unstructured) *node {
	existingNode, found := o.uidToNode[obj.GetUID()]
	if found {
		existingNode.markObserved()

		// In order to compensate the lack of labels when adding a virtual node,
		// it is required to re-compute the forceMove flag when the real node is processed
		// Without this, there is the risk that, forceMove will report false negatives depending on the discovery order
		existingNode.forceMove = o.getForceMove(obj.GetKind(), obj.GetAPIVersion(), obj.GetLabels())
		return existingNode
	}

	isGlobal := false
	if obj.GetNamespace() == "" {
		isGlobal = true
	}

	newNode := &node{
		identity: corev1.ObjectReference{
			APIVersion: obj.GetAPIVersion(),
			Kind:       obj.GetKind(),
			UID:        obj.GetUID(),
			Name:       obj.GetName(),
			Namespace:  obj.GetNamespace(),
		},
		owners:         make(map[*node]ownerReferenceAttributes),
		softOwners:     make(map[*node]empty),
		tenantClusters: make(map[*node]empty),
		tenantCRSs:     make(map[*node]empty),
		virtual:        false,
		forceMove:      o.getForceMove(obj.GetKind(), obj.GetAPIVersion(), obj.GetLabels()),
		isGlobal:       isGlobal,
	}

	o.uidToNode[newNode.identity.UID] = newNode
	return newNode
}

func (o *objectGraph) getForceMove(kind, apiVersion string, labels map[string]string) bool {
	if _, ok := labels[clusterctlv1.ClusterctlMoveLabelName]; ok {
		return true
	}

	kindAPIStr := getKindAPIString(metav1.TypeMeta{Kind: kind, APIVersion: apiVersion})

	if discoveryType, ok := o.types[kindAPIStr]; ok {
		return discoveryType.forceMove
	}
	return false
}

// getDiscoveryTypes returns the list of TypeMeta to be considered for the the move discovery phase.
// This list includes all the types defines by the CRDs installed by clusterctl and the ConfigMap/Secret core types.
func (o *objectGraph) getDiscoveryTypes() error {
	crdList := &apiextensionsv1.CustomResourceDefinitionList{}
	getDiscoveryTypesBackoff := newReadBackoff()
	if err := retryWithExponentialBackoff(getDiscoveryTypesBackoff, func() error {
		return getCRDList(o.proxy, crdList)
	}); err != nil {
		return err
	}

	o.types = make(map[string]*discoveryTypeInfo)

	for _, crd := range crdList.Items {
		for _, version := range crd.Spec.Versions {
			if !version.Storage {
				continue
			}

			forceMove := false
			if _, ok := crd.Labels[clusterctlv1.ClusterctlMoveLabelName]; ok {
				forceMove = true
			}

			typeMeta := metav1.TypeMeta{
				Kind: crd.Spec.Names.Kind,
				APIVersion: metav1.GroupVersion{
					Group:   crd.Spec.Group,
					Version: version.Name,
				}.String(),
			}

			o.types[getKindAPIString(typeMeta)] = &discoveryTypeInfo{
				typeMeta:  typeMeta,
				forceMove: forceMove,
			}
		}
	}

	secretTypeMeta := metav1.TypeMeta{Kind: "Secret", APIVersion: "v1"}
	o.types[getKindAPIString(secretTypeMeta)] = &discoveryTypeInfo{typeMeta: secretTypeMeta}

	configMapTypeMeta := metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"}
	o.types[getKindAPIString(configMapTypeMeta)] = &discoveryTypeInfo{typeMeta: configMapTypeMeta}

	return nil
}

// getKindAPIString returns a concatenated string of the API name and the plural of the kind
// Ex: KIND=Foo API NAME=foo.bar.domain.tld => foos.foo.bar.domain.tld.
func getKindAPIString(typeMeta metav1.TypeMeta) string {
	api := strings.Split(typeMeta.APIVersion, "/")[0]
	return fmt.Sprintf("%ss.%s", strings.ToLower(typeMeta.Kind), api)
}

func getCRDList(proxy Proxy, crdList *apiextensionsv1.CustomResourceDefinitionList) error {
	c, err := proxy.NewClient()
	if err != nil {
		return err
	}

	if err := c.List(ctx, crdList, client.HasLabels{clusterctlv1.ClusterctlLabelName}); err != nil {
		return errors.Wrap(err, "failed to get the list of CRDs required for the move discovery phase")
	}
	return nil
}

// Discovery reads all the Kubernetes objects existing in a namespace (or in all namespaces if empty) for the types received in input, and then adds
// everything to the objects graph.
func (o *objectGraph) Discovery(namespace, cluster string) error {
	log := logf.Log
	log.Info("Discovering Cluster API objects")

	selectors := []client.ListOption{}
	if namespace != "" {
		selectors = append(selectors, client.InNamespace(namespace))
	}

	if cluster != "" {
		selectors = append(selectors, client.MatchingLabels(map[string]string{
			clusterv1.ClusterLabelName: cluster,
		}))
	}

	discoveryBackoff := newReadBackoff()
	for _, discoveryType := range o.types {
		typeMeta := discoveryType.typeMeta
		objList := new(unstructured.UnstructuredList)

		if err := retryWithExponentialBackoff(discoveryBackoff, func() error {
			return getObjList(o.proxy, typeMeta, selectors, objList)
		}); err != nil {
			return err
		}

		if len(objList.Items) == 0 {
			continue
		}

		log.V(5).Info(typeMeta.Kind, "Count", len(objList.Items))
		for i := range objList.Items {
			obj := objList.Items[i]
			o.addObj(&obj)
		}
	}

	log.V(1).Info("Total objects", "Count", len(o.uidToNode))

	// Completes the graph by searching for soft ownership relations such as secrets linked to the cluster
	// by a naming convention (without any explicit OwnerReference).
	o.setSoftOwnership()

	// Completes the graph by setting for each node the list of Clusters the node belong to.
	o.setClusterTenants()

	// Completes the graph by setting for each node the list of ClusterResourceSet the node belong to.
	o.setCRSTenants()

	return nil
}

func getObjList(proxy Proxy, typeMeta metav1.TypeMeta, selectors []client.ListOption, objList *unstructured.UnstructuredList) error {
	c, err := proxy.NewClient()
	if err != nil {
		return err
	}

	objList.SetAPIVersion(typeMeta.APIVersion)
	objList.SetKind(typeMeta.Kind)

	if err := c.List(ctx, objList, selectors...); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return errors.Wrapf(err, "failed to list %q resources", objList.GroupVersionKind())
	}
	return nil
}

// getClusters returns the list of Clusters existing in the object graph.
func (o *objectGraph) getClusters() []*node {
	clusters := []*node{}
	for _, node := range o.uidToNode {
		if node.identity.GroupVersionKind().GroupKind() == clusterv1.GroupVersion.WithKind("Cluster").GroupKind() {
			clusters = append(clusters, node)
		}
	}
	return clusters
}

// getClusters returns the list of Secrets existing in the object graph.
func (o *objectGraph) getSecrets() []*node {
	secrets := []*node{}
	for _, node := range o.uidToNode {
		if node.identity.APIVersion == "v1" && node.identity.Kind == "Secret" {
			secrets = append(secrets, node)
		}
	}
	return secrets
}

// getNodes returns the list of nodes existing in the object graph.
func (o *objectGraph) getNodes() []*node {
	nodes := []*node{}
	for _, node := range o.uidToNode {
		nodes = append(nodes, node)
	}
	return nodes
}

// getCRSs returns the list of ClusterResourceSet existing in the object graph.
func (o *objectGraph) getCRSs() []*node {
	clusters := []*node{}
	for _, node := range o.uidToNode {
		if node.identity.GroupVersionKind().GroupKind() == addonsv1.GroupVersion.WithKind("ClusterResourceSet").GroupKind() {
			clusters = append(clusters, node)
		}
	}
	return clusters
}

// getMoveNodes returns the list of nodes existing in the object graph that belong at least to one Cluster or to a ClusterResourceSet
// or to a CRD containing the "move" label.
func (o *objectGraph) getMoveNodes() []*node {
	nodes := []*node{}
	for _, node := range o.uidToNode {
		if len(node.tenantClusters) > 0 || len(node.tenantCRSs) > 0 || node.forceMove {
			nodes = append(nodes, node)
		}
	}
	return nodes
}

// getMachines returns the list of Machine existing in the object graph.
func (o *objectGraph) getMachines() []*node {
	machines := []*node{}
	for _, node := range o.uidToNode {
		if node.identity.GroupVersionKind().GroupKind() == clusterv1.GroupVersion.WithKind("Machine").GroupKind() {
			machines = append(machines, node)
		}
	}
	return machines
}

// setSoftOwnership searches for soft ownership relations such as secrets linked to the cluster by a naming convention (without any explicit OwnerReference).
func (o *objectGraph) setSoftOwnership() {
	log := logf.Log
	clusters := o.getClusters()
	for _, secret := range o.getSecrets() {
		// If the secret has at least one OwnerReference ignore it.
		// NB. Cluster API generated secrets have an explicit OwnerReference to the ControlPlane or the KubeadmConfig object while user provided secrets might not have one.
		if len(secret.owners) > 0 {
			continue
		}

		// If the secret name is not a valid cluster secret name, ignore it.
		secretClusterName, _, err := secretutil.ParseSecretName(secret.identity.Name)
		if err != nil {
			log.V(5).Info("Excluding secret from move (not linked with any Cluster)", "name", secret.identity.Name)
			continue
		}

		// If the secret is linked to a cluster, then add the cluster to the list of the secrets's softOwners.
		for _, cluster := range clusters {
			if secretClusterName == cluster.identity.Name && secret.identity.Namespace == cluster.identity.Namespace {
				secret.addSoftOwner(cluster)
			}
		}
	}
}

// setClusterTenants sets the cluster tenants for the clusters itself and all their dependent object tree.
func (o *objectGraph) setClusterTenants() {
	for _, cluster := range o.getClusters() {
		o.setClusterTenant(cluster, cluster)
	}
}

// setNodeTenant sets a cluster tenant for a node and for its own dependents/sofDependents.
func (o *objectGraph) setClusterTenant(node, tenant *node) {
	node.tenantClusters[tenant] = empty{}
	for _, other := range o.getNodes() {
		if other.isOwnedBy(node) || other.isSoftOwnedBy(node) {
			o.setClusterTenant(other, tenant)
		}
	}
}

// setClusterTenants sets the ClusterResourceSet tenants for the ClusterResourceSet itself and all their dependent object tree.
func (o *objectGraph) setCRSTenants() {
	for _, crs := range o.getCRSs() {
		o.setCRSTenant(crs, crs)
	}
}

// setCRSTenant sets a ClusterResourceSet tenant for a node and for its own dependents/sofDependents.
func (o *objectGraph) setCRSTenant(node, tenant *node) {
	node.tenantCRSs[tenant] = empty{}
	for _, other := range o.getNodes() {
		if other.isOwnedBy(node) {
			o.setCRSTenant(other, tenant)
		}
	}
}

// checkVirtualNode logs if nodes are still virtual.
func (o *objectGraph) checkVirtualNode() {
	log := logf.Log
	for _, node := range o.uidToNode {
		if node.virtual {
			log.V(5).Info("Object won't be moved because it's not included in GVK considered for move", "kind", node.identity.Kind, "name", node.identity.Name)
		}
	}
}
