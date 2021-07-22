---
title: ClusterClass and managed topologies
authors:
  - "@srm09"
  - "@vincepri"
  - "@fabriziopandini"
  - "@CecileRobertMichon"
reviewers:
  - "@vincepri"
  - "@fabriziopandini"
  - "@CecileRobertMichon"
  - "@enxebre"
  - "@schrej"
creation-date: 2021-05-26
status: provisional
replaces:
  - [Proposal Google Doc](https://docs.google.com/document/d/1lwxgBK3Q7zmNkOSFqzTGmrSys_vinkwubwgoyqSRAbI/edit#)
---

# ClusterClass and Managed Topologies

## Table of Contents

- [ClusterClass and Managed Topologies](#clusterclass-and-managed-topologies)
  - [Table of Contents](#table-of-contents)
  - [Glossary](#glossary)
      - [ClusterClass](#clusterclass)
      - [Topology](#topology)
      - [Worker class](#worker-class)
  - [Summary](#summary)
  - [Motivation](#motivation)
      - [Goals](#goals)
      - [Prospective future Work](#prospective-future-work)
  - [Proposal](#proposal)
      - [User Stories](#user-stories)
        - [Story 1 - Use ClusterClass to easily stamp clusters](#story-1---use-clusterclass-to-easily-stamp-clusters)
        - [Story 2 - Easier UX for kubernetes version upgrades](#story-2---easier-ux-for-kubernetes-version-upgrades)
        - [Story 3 - Easier UX for scaling workers nodes](#story-3---easier-ux-for-scaling-workers-nodes)
      - [Implementation Details/Notes/Constraints](#implementation-detailsnotesconstraints)
        - [New API types](#new-api-types)
            - [ClusterClass](#clusterclass-1)
        - [Modification to existing API Types](#modification-to-existing-api-types)
            - [Cluster](#cluster)
        - [Validations](#validations)
            - [ClusterClass](#clusterclass-2)
            - [Cluster](#cluster-1)
        - [Behaviors](#behaviors)
            - [Create a new Cluster using ClusterClass object](#create-a-new-cluster-using-clusterclass-object)
            - [Update an existing Cluster using ClusterClass](#update-an-existing-cluster-using-clusterclass)
        - [Provider implementation](#provider-implementation)
            - [For infrastructure providers](#for-infrastructure-providers)
            - [For Control plane providers](#for-control-plane-providers)
      - [Risks and Mitigations](#risks-and-mitigations)
  - [Alternatives](#alternatives)
  - [Upgrade Strategy](#upgrade-strategy)
  - [Additional Details](#additional-details)
      - [Test Plan [optional]](#test-plan-optional)
      - [Graduation Criteria [optional]](#graduation-criteria-optional)
      - [Version Skew Strategy [optional]](#version-skew-strategy-optional)
  - [Implementation History](#implementation-history)

## Glossary

### ClusterClass
A collection of templates that define a topology (control plane and machine deployments) to be used to create one or more clusters.

### Topology
A topology refers to a Cluster that provides a single control point to manage its own topology; the topology is defined by a ClusterClass.

### WorkerClass
A collection of templates that define a set of worker nodes in the cluster. A ClusterClass contains zero or more WorkerClass definitions.


## Summary

This proposal introduces a new ClusterClass object which will be used to provide easy stamping of clusters of similar shapes. It serves as a collection of template resources which are used to generate one or more clusters of the same flavor.

We're enhancing the Cluster CRD and controller to use a ClusterClass resource to provision the underlying objects that compose a cluster. Additionally, the Cluster provides a single control point to manage the Kubernetes version, worker pools, labels, replicas, and so on.

## Motivation

Currently, Cluster API does not expose a native way to provision multiple clusters of the same configuration. The ClusterClass object is supposed to act as a collection of template references which can be used to create managed topologies.

Today, the Cluster object is a logical grouping of components which describe an underlying cluster. The user experience to create a cluster requires the user to create a bunch of underlying resources such as KCP (control plane provider), MachineDeployments, and infrastructure or bootstrap templates for those resources which logically end up representing the cluster. Since the cluster configuration is spread around multiple components, upgrading the cluster version is hard as it requires changes to different fields in different resources to perform an upgrade. The ClusterClass object aims at reducing this complexity by delegating the responsibility of lifecycle managing these underlying resources to the Cluster controller.

This method of provisioning the cluster would act as a single control point for the entire cluster. Scaling the nodes, adding/removing sets of worker nodes and upgrading cluster kubernetes versions would be achievable by editing the topology. This would facilitate the maintenance of existing clusters as well as ease the creation of newer clusters.

### Goals

- Create the new ClusterClass CRD which can serve as a collection of templates to create clusters.
- Extend the Cluster object to use ClusterClass for creating managed topologies.
- Enhance the Cluster object to act as a single point of control for the topology.
- Extend the Cluster controller to create/update/delete managed topologies.

### Prospective future Work

⚠️ The following points are mostly ideas and can change at any given time  ⚠️

We are fully aware that in order to exploit the potential of ClusterClass and managed topologies, the following class of problems still needs to be addressed:
- **Lifecycle of the ClusterClass**: Introduce mechanisms for allowing mutation of a ClusterClass, and the continuous reconciliation of the Cluster managed resources.
- **Upgrade/rollback strategy**: Implement a strategy to upgrade and rollback the managed topologies.
- **Extensibility/Transformation**: Introduce mechanism for allowing Cluster specific transformations of a ClusterClass (e.g. inject API Endpoint for CAPV, customize machine image by version etc.)
- **Adoption**: Providing a way to convert existing clusters into managed topologies.
- **Observability**: Build an SDK and enhance the Cluster object status to surface a summary of the status of the topology.
- **Lifecycle integrations**: Extend ClusterClass to include lifecycle management integrations such as MachineHealthCheck and Cluster Autoscaler to manage the state and health of the managed topologies.

However we are intentionally leaving them out from this initial iteration for the following reasons:
- We want the community to reach a consensus on cornerstone elements of the design before iterating on additional features.
- We want to enable starting the implementation of the required scaffolding and the initial support for managed topologies as soon as possible, so we can surface problems which are not easy to identify at this stage of the proposal.
- We would like the community to rally in defining use cases for the advanced features, help in prioritizing them, so we can chart a more effective roadmap for the next steps.

## Proposal

This proposal enhances the `Cluster` object to create topologies using the `ClusterClass` object.

### User Stories

#### Story 1 - Use ClusterClass to easily stamp clusters
As an end user, I want to use one `ClusterClass` to create multiple topologies of similar flavor.
- Rather than recreating the KCP and MD objects for every cluster that needs to be provisioned, the end user can create a template once and reuse it to create multiple clusters with similar configurations.

#### Story 2 - Easier UX for kubernetes version upgrades
For an end user, the UX to update the kubernetes version of the control plane and worker nodes in the cluster should be easy.
- Instead of individually modifying the KCP and each MachineDeployment, updating a single option should result in k8s version updates for all the CP and worker nodes.

**Note**: In order to complete the user story for all the providers, some of the advanced features (such as Extensibility/Transformation) are required. However, getting this in place even only for a subset of providers allows us to build and test a big chunk of the entire machinery.

#### Story 3 - Easier UX for scaling workers nodes
As an end user, I want to be able to easily scale up/down the number of replicas for each set of worker nodes in the cluster.
- Currently, (for a cluster with 3 machine deployments) this is possible by updating these three different objects representing the sets of worker nodes in the pool. An easier user experience would be to update a single object to enable the scaling of multiple sets of worker nodes.

### Implementation Details/Notes/Constraints

The following section provides details about the introduction of new types and modifications to existing types to implement the ClusterClass functionality.
If instead you are eager to see an example of ClusterClass and how the Cluster object will look like, you can jump to the Behavior paragraph.

#### New API types
##### ClusterClass
This CRD is a collection of templates that describe the topology for one or more clusters.
```golang
// +kubebuilder:object:root=true
// +kubebuilder:resource:path=clusterclasses,shortName=cc,scope=Namespaced,categories=cluster-api
// +kubebuilder:storageversion

// ClusterClass is a template which can be used to create managed topologies.
type ClusterClass struct {
  metav1.TypeMeta   `json:",inline"`
  metav1.ObjectMeta `json:"metadata,omitempty"`

  Spec ClusterClassSpec `json:"spec,omitempty"`
}

// ClusterClassSpec describes the desired state of the ClusterClass.
type ClusterClassSpec struct {
  // Infrastructure is a reference to a provider-specific template that holds
  // the details for provisioning infrastructure specific cluster
  // for the underlying provider.
  // The underlying provider is responsible for the implementation
  // of the template to an infrastructure cluster.
  Infrastructure LocalObjectTemplate `json:"infrastructure,omitempty"`

  // ControlPlane is a reference to a local struct that holds the details
  // for provisioning the Control Plane for the Cluster.
  ControlPlane LocalObjectTemplate `json:"controlPlane,omitempty"`

  // Workers describes the worker nodes for the cluster.
  // It is a collection of node types which can be used to create
  // the worker nodes of the cluster.
  // +optional
  Workers WorkersClass `json:"workers,omitempty"`
}

// WorkersClass is a collection of deployment classes.
type WorkersClass struct {
  // MachineDeployments is a list of machine deployment classes that can be used to create
  // a set of worker nodes.
  MachineDeployments []MachineDeploymentClass `json:"machineDeployments,omitempty"`
}

// MachineDeploymentClass serves as a template to define a set of worker nodes of the cluster
// provisioned using the `ClusterClass`.
type MachineDeploymentClass struct {
  // Class denotes a type of worker node present in the cluster,
  // this name MUST be unique within a ClusterClass and can be referenced
  // in the Cluster to create a managed MachineDeployment.
  Class string `json:"class"`

  // Template is a local struct containing a collection of templates for creation of
  // MachineDeployment objects representing a set of worker nodes.
  Template MachineDeploymentClassTemplate `json:"template"`
}

// MachineDeploymentClassTemplate defines how a MachineDeployment generated from a MachineDeploymentClass
// should look like.
type MachineDeploymentClassTemplate struct {
  Metadata ObjectMeta `json:"metadata,omitempty"`

  // Bootstrap contains the bootstrap template reference to be used
  // for the creation of worker Machines.
  Bootstrap LocalObjectTemplate `json:"bootstrap"`

  // Infrastructure contains the infrastructure template reference to be used
  // for the creation of worker Machines.
  Infrastructure LocalObjectTemplate `json:"infrastructure"`
}

// LocalObjectTemplate defines a template for a topology Class.
type LocalObjectTemplate struct {
  // Ref is a required reference to a custom resource
  // offered by a provider.
  Ref *corev1.ObjectReference `json:"ref"`
}
```

#### Modification to existing API Types
##### Cluster
1.  Add `Cluster.Spec.Topology` defined as
    ```golang
    // This encapsulates the topology for the cluster.
	// NOTE: This feature is alpha; it is required to enable the ClusterTopology
	// feature gate flag to activate managed topologies support.
	// +optional
	Topology *Topology `json:"topology,omitempty"`
    ```
1.  The `Topology` object has the following definition:
    ```golang
    // Topology encapsulates the information of the managed resources.
    type Topology struct {
      // The name of the ClusterClass object to create the topology.
      Class string `json:"class"`

	  // The kubernetes version of the cluster.
	  Version string `json:"version"`

	  // RolloutAfter performs a rollout of the entire cluster one component at a time,
	  // control plane first and then machine deployments.
	  // +optional
	  RolloutAfter *metav1.Time `json:"rolloutAfter,omitempty"`

	  // The information for the Control plane of the cluster.
	  ControlPlane ControlPlaneTopology `json:"controlPlane"`

	  // Workers encapsulates the different constructs that form the worker nodes
	  // for the cluster.
	  // +optional
	  Workers *WorkersTopology `json:"workers,omitempty"`
    }
    ```
1.  The `ControlPlaneTopology` object contains the parameters for the control plane nodes of the topology.
    ```golang
    // ControlPlaneTopology specifies the parameters for the control plane nodes in the cluster.
    type ControlPlaneTopology struct {
      Metadata ObjectMeta `json:"metadata,omitempty"`

      // Replicas is the number of control plane nodes.
      //
      // A nil value indicates that the ControlPlane object is created without the number of Replicas.
      // The underlying assumption is that the control plane provider does not support this field.
      // When specified against a control plane provider that lacks support for this field, this value will be ignored.
      // +optional
      Replicas *int `json:"replicas,omitempty"`
    }
    ```
1.  The `WorkersTopology` object represents the sets of worker nodes of the topology.

    **Note**: In this proposal, a set of worker nodes is handled by a MachineDeployment object. In the future, this can be extended to include Machine Pools as another backing mechanism for managing worker node sets.
    ```golang
    // WorkersTopology represents the different sets of worker nodes in the cluster.
    type WorkersTopology struct {
      // MachineDeployments is a list of machine deployment in the cluster.
      MachineDeployments []MachineDeploymentTopology `json:"machineDeployments,omitempty"`
    }   
    ```
1.  The `MachineDeploymentTopology` object represents a single set of worker nodes of the topology.
    ```golang
    // MachineDeploymentTopology specifies the different parameters for a set of worker nodes in the topology.
    // This set of nodes is managed by a MachineDeployment object whose lifecycle is managed by the Cluster controller.
    type MachineDeploymentTopology struct {
    Metadata ObjectMeta `json:"metadata,omitempty"`
    
      // Class is the name of the MachineDeploymentClass used to create the set of worker nodes.
      // This should match one of the deployment classes defined in the ClusterClass object
      // mentioned in the `Cluster.Spec.Class` field.
      Class string `json:"class"`
    
      // Name is the unique identifier for this MachineDeploymentTopology.
      // The value is used with other unique identifiers to create a MachineDeployment's Name
      // (e.g. cluster's name, etc). In case the name is greater than the allowed maximum length,
      // the values are hashed together.
      Name string `json:"name"`
    
      // The number of worker nodes belonging to this set.
      // If the value is nil, the MachineDeployment is created without the number of Replicas (defaulting to zero)
      // and it's assumed that an external entity (like cluster autoscaler) is responsible for the management
      // of this value.
      // +optional
      Replicas *int `json:"replicas,omitempty"`
    }
    ```

#### Validations
##### ClusterClass
- For object creation:
  - (defaulting) if namespace field is empty for a reference, default it to `metadata.Namespace`
  - all the reference must be in the same namespace of `metadata.Namespace`  
  - `spec.workers.machineDeployments[i].class` field must be unique within a ClusterClass.
- For object updates:
  - all the reference must be in the same namespace of `metadata.Namespace`
  - `spec.workers.machineDeployments[i].class` field must be unique within a ClusterClass.
  - `spec.workers.machineDeployments` supports adding new deployment classes.

##### Cluster
- For object creation:
  - `spec.topology` and `spec.infrastructureRef` cannot be simultaneously set.
  - `spec.topology` and `spec.controlPlaneRef` cannot be simultaneously set.
  - If `spec.topology` is set, `spec.topology.class` cannot be empty.
  - If `spec.topology` is set, `spec.topology.version` cannot be empty and must be a valid semver.  
  - `spec.topology.workers.machineDeployments[i].name` field must be unique within a Cluster
  
- For object updates:
  - If `spec.topology.class` is set, it cannot be unset or modified.
  - `spec.topology.version` cannot be unset and must be a valid semver, if being updated.
  - `spec.topology.version` cannot be downgraded.
  - `spec.topology.workers.machineDeployments[i].name` field must be unique within a Cluster
  - A set of worker nodes can be added to or removed from the `spec.topology.workers.machineDeployments` list.

#### Behaviors
This section lists out the behavior for Cluster objects using `ClusterClass` in case of creates and updates.

##### Create a new Cluster using ClusterClass object
1. User creates a ClusterClass object.
   ```yaml
    apiVersion: cluster.x-k8s.io/v1alpha4
    kind: ClusterClass
    metadata:
      name: mixed
      namespace: bar
    spec:
      controlPlane:
        ref:
          apiVersion: controlplane.cluster.x-k8s.io/v1alpha4
          kind: KubeadmControlPlaneTemplate
          name: vsphere-prod-cluster-template-kcp
      workers:
        deployments:
        - class: linux-worker
          template:
            bootstrap:
              ref:
                apiVersion: bootstrap.cluster.x-k8s.io/v1alpha4
                kind: KubeadmConfigTemplate
                name: existing-boot-ref
            infrastructure:
              ref:
                apiVersion: infrastructure.cluster.x-k8s.io/v1alpha4
                kind: VSphereMachineTemplate
                name: linux-vsphere-template
        - class: windows-worker
          template:
            bootstrap:
              ref:
                apiVersion: bootstrap.cluster.x-k8s.io/v1alpha4
                kind: KubeadmConfigTemplate
                name: existing-boot-ref-windows
            infrastructure:
              ref:
                apiVersion: infrastructure.cluster.x-k8s.io/v1alpha4
                kind: VSphereMachineTemplate
                name: windows-vsphere-template
      infrastructure:
        ref:
          apiVersion: infrastructure.cluster.x-k8s.io/v1alpha4
          kind: VSphereClusterTemplate
          name: vsphere-prod-cluster-template
   ```
2. User creates a cluster using the class name and defining the topology.
   ```yaml
    apiVersion: cluster.x-k8s.io/v1alpha4
    kind: Cluster
    metadata:
      name: foo
      namespace: bar
    spec:
      topology:
        class: mixed
        version: v1.19.1
        controlPlane:
          replicas: 3
          labels: {}
          annotations: {}
        workers:
          machineDeployments:
          - class: linux-worker
            name: big-pool-of-machines-1
            replicas: 5
            labels:
              # This label is additive to the class' labels,
              # or if the same label exists, it overwrites it.
              custom-label: "production"
          - class: linux-worker
            name: small-pool-of-machines-1
            replicas: 1
          - class: windows-worker
            name: microsoft-1
            replicas: 3
   ```
3. The Cluster controller checks for the presence of the `spec.topology.class` field. If the field is missing, the Cluster controller behaves in the existing way. Otherwise the controller starts the creation process of the topology defined in the steps below.
4. Cluster and Control plane object creation
    1. Creates the infrastructure provider specific cluster using the cluster template referenced in the `ClusterClass.spec.infrastructure.ref` field.
    1. Add the topology label to the provider cluster object:
       ```yaml
        cluster.x-k8s.io/topology: <cluster-name>
       ```
    1. For the ControlPlane object in `cluster.spec.topology.controlPlane`
    1. Initializes a control plane object using the control plane template defined in the `ClusterClass.spec.controlPlane.ref field`. Use the name `<cluster-name>`.
    1. Checks if the control plane object implements support for the Replicas field. If yes, sets the number of replicas on the control plane object from `spec.topology.controlPlane.replicas`, else ignore the field.
    1. Sets the k8s version on the control plane object from the `spec.topology.version`.
    1. Add the following labels to the control plane object:
       ```yaml
        cluster.x-k8s.io/topology: <cluster-name>
       ```
    1. Creates the control plane object.
    1. Sets the `spec.infrastructureRef` and `spec.controlPlaneRef` fields for the Cluster object.
    1. Saves the cluster object in the API server.
5. Machine deployment object creation
    1. For each `spec.topology.workers.machineDeployments` item in the list
    1. Create a name `<cluster-name>-<worker-set-name>` (if too long, hash it)
    1. Initializes a new MachineDeployment object.
    1. Sets the `clusterName` field on the MD object
    1. Sets the `replicas` field on the MD object using `replicas` field for the set of worker nodes.
    1. Sets the `version` field on the MD object from the `spec.topology.version`.
    1. Sets the `spec.template.spec.bootstrap` on the MD object from the `ClusterClass.spec.workers.machineDeployments[i].template.bootstrap.ref` field.
    1. Sets the `spec.template.spec.infrastructureRef` on the MD object from the `ClusterClass.spec.workers.machineDeployments[i].template.infrastructure.ref` field.
    1. Generates the set of labels to be set on the MD object. The labels are additive to the class' labels list, and the value in the `spec.topology.workers.machineDeployments[i].labels` takes precedence over any set by the ClusterClass. Also, include the topology label stated below:
       ```yaml
        cluster.x-k8s.io/topology: <cluster-name>-<worker-set-name>
       ```
      Note: The topology label needs to be set on the individual Machine objects as well.
    1. Creates the Machine Deployment object in the API server.

![Creation of cluster with ClusterClass](./images/cluster-class/create.png)

##### Update an existing Cluster using ClusterClass
This section talks about updating a cluster which was created using a `ClusterClass` object.
1. User updates the `cluster.spec.topology` field adhering to the update validation [criteria](#clusterclass-2).
2. For the ControlPlane object in `spec.topology.controlPlane`, the cluster controller checks for the presence of the control plane object using the name `<cluster-name>`. If found,
    1. Compares and updates the number of replicas, if necessary.
    1. Compares and updates the k8s version, if necessary.
    1. Updates the KCP object in the API server.
3. The cluster controller reconciles the list of required machine deployments with the current list of managed machine deployments by:
    1. Adding/Removing MachineDeployment if necessary.
    1. Comparing and updating the number of replicas, if necessary.
    1. Comparing and updating the k8s version for the MD, if necessary.
    1. Updating the Machine Deployment object in the API server.

![Update cluster with ClusterClass](./images/cluster-class/update.png)

#### Provider implementation
##### For infrastructure providers
With the introduction of ClusterClass, we are extending the responsibility of the Cluster controller to create/update the actual objects from the collection of templates. Following this pattern, a template is required to create the actual infrastructure cluster object as part of the topology. For starter, the template should consist the InfraClusterSpec object. It can be enhanced to include other fields such as labels/annotations in the future.

The proposal calls for the implementation of an infrastructure cluster template to be referenced by the `ClusterClass` object. This is a required field for the `ClusterClass` object. This template would be used to create an infrastructure cluster object. Currently, this responsibility is assumed by the cluster operator/end user.

**Note:** As per this proposal, the definition of ClusterClass is immutable. The CC definition consists of infrastructure object references, say AWSMachineTemplate, which could be immutable. For such immutable infrastructure objects, hard-coding the image identifiers leads to those templates being tied to a particular kubernetes version, thus making kubernetes version upgrades impossible. Hence, when using CC, infrastructure objects MUST NOT have mandatory static fields whose values prohibit version upgrades.

##### For Control plane providers
Similarly, a control plane provider should also create a template for use of creation for the control plane objects. For instance, the kubeadm control plane provider should introduce the notion of a KubeadmControlPlaneTemplate for use by the cluster controller to create a managed Kubeadm Control plane object.

The CAPI Cluster Controller would use this template to instantiate a control plane object for the topology. The current CRD contract mandates that the control plane provider supports `replicas` in the `spec` fields. With the introduction of ClusterClass which is responsible for handling the kubernetes version too, the current contract needs to be expanded to include the support for `version` in the `spec` fields.

### Risks and Mitigations

This proposal tries to model the API design for ClusterClass with a narrow set of use cases. This initial implementation provides a baseline on which incremental changes can be introduced in the future. Instead of encompassing of all use cases under a single proposal, this proposal mitigates the risk of waiting too long to consider all required use cases under this topic.

## Alternatives

## Upgrade Strategy

Existing clusters created without ClusterClass cannot switch over to using ClusterClass for a topology.

## Additional Details

### Test Plan [optional]

TBD

### Graduation Criteria [optional]

The initial plan is to rollout Cluster Class and support for managed topologies under a feature flag which would be unset by default.

## Implementation History

- [x] 04/05/2021: Proposed idea in an [issue](https://github.com/kubernetes-sigs/cluster-api/issues/4430)
- [x] 05/05/2021: Compile a [Google Doc](https://docs.google.com/document/d/1lwxgBK3Q7zmNkOSFqzTGmrSys_vinkwubwgoyqSRAbI/edit#) following the CAEP template
- [ ] MM/DD/YYYY: First round of feedback from community
- [x] 05/19/2021: Present proposal at a [community meeting](https://docs.google.com/document/d/1LdooNTbb9PZMFWy3_F-XAsl7Og5F2lvG3tCgQvoB5e4/edit#heading=h.bz527cpoqorn)
- [x] 05/26/2021: Open proposal PR

<!-- Links -->
[community meeting]: https://docs.google.com/document/d/1Ys-DOR5UsgbMEeciuG0HOgDQc8kZsaWIWJeKJ1-UfbY
