---
title: Opt-in Autoscaling from Zero
authors:
  - "@elmiko"
reviewers:
  - "@"
creation-date: 2021-03-10
last-updated: 2021-03-10
status: implementable
---

# Opt-in Autoscaling from Zero

## Table of Contents

- [Title](#title)
  - [Table of Contents](#table-of-contents)
  - [Glossary](#glossary)
  - [Summary](#summary)
  - [Motivation](#motivation)
    - [Goals](#goals)
    - [Non-Goals/Future Work](#non-goalsfuture-work)
  - [Proposal](#proposal)
    - [User Stories](#user-stories)
      - [Story 1](#story-1)
      - [Story 2](#story-2)
    - [Security Model](#security-model)
    - [Risks and Mitigations](#risks-and-mitigations)
  - [Alternatives](#alternatives)
  - [Upgrade Strategy](#upgrade-strategy)
  - [Additional Details](#additional-details)
    - [Test Plan](#test-plan-optional)
  - [Implementation History](#implementation-history)

## Glossary

* **Node Group** This term has special meaning within the cluster autoscaler, it refers to collections
  of nodes, and related physical hardware, that are organized within the autoscaler for scaling operations.
  These node groups do not have a direct relation to specific CRDs within Kubernetes, and may be handled
  differently by each autoscaler cloud implementation. In the case of Cluster API, node groups correspond
  directly to MachineSets and MachineDeployments that are marked for autoscaling.

Refer to the [Cluster API Book Glossary](https://cluster-api.sigs.k8s.io/reference/glossary.html).

## Summary

The [Kubernetes cluster autoscaler](https://github.com/kubernetes/autoscaler) currently supports
scaling on Cluster API deployed clusters for MachineSets and MachineDeployments. One feature
that is missing from this integration is the ability to scale down to, and up from, a MachineSet
or MachineDeployment with zero replicas.

This proposal defines an opt-in mechanism whereby Cluster API users and cloud providers can define
the specific resource requirements for each Infrastructure Machine Template they create. In situations
where there are zero nodes in the node group, and thus the autoscaler does not have information
about the nodes, the resource requests are utilized to predict the number of nodes needed. This
information is only used by the autoscaler when it is scaling from zero.

## Motivation

Allowing the cluster autoscaler to scale down its node groups to zero replicas is a common feature
implemented for many of the integrated cloud providers. It is a popular feature that has been
requested for Cluster API on multiple occasions. This feature empowers users to reduce their
operational resource needs, and likewise reduce their operating costs.

Given that Cluster API is an abstraction point that provides access to multiple concrete cloud
implementations, this feature might not make sense in all scenarios. To accomodate the wide
range of deployment options in Cluster API, the scale to zero feature will be optional for
users and cloud providers.

### Goals

- Provide capability for Cluster API MachineSets and MachineDeployments to scale from and to zero replicas.
- Create an optional API contract in the Infrastructure Machine Template that allows cloud providers to specify
  instance resource requirements that will be utilized by the cluster autoscaler.
- Provide a mechanism for users to override the defined instance resource requirements on any given MachineSet or MachineDeployment.

### Non-Goals/Future Work

- Create an API contract that cloud providers must follow.
- Create an API that replicates Taint and Label information from Machines to MachineSets and MachineDeployments.
- Support for MachinePools, either with the cluster autoscaler or using cloud provider native implementations (eg AWS AutoScalingGroups).
- Create an autoscaling custom resource for Cluster API.
- Add MachinePool support to the cluster autoscaler.

## Proposal

To facilitate scaling from zero replicas, the minimal information needed by the cluster autoscaler
is the CPU and memory resources for the target node group that will be scaled. The autoscaler uses
this information to create a prediction about how many nodes should be created when scaling. In
most situations this information can be directly read from the nodes that are running within a
node group. But, during a scale from zero situation (ie when a node group has zero replicas) the
autoscaler needs to acquire this information from the cloud provider.

An optional field is proposed for users to add to their Infrastructure Machine Templates which will
carry information about the CPU, memory, and GPU requirements for the machines created from the template.
The cluster autoscaler will then utilize this information by reading the appropriate infrastructure
reference from the resource it is scaling (MachineSet or MachineDeployment).

A user may override the field in the associated infrastructure template by applying annotations to the
MachineSet or MachineDeployment in question. In these cases the autoscaler will evaluate the annotation
information in favor of reading the information from the status.

### User Stories

#### Story 1

As a Cluster API user, I would like to reduce my operating costs by scaling down my workload
cluster when they are not in use. Using the cluster autoscaler with a minimum size of zero for
a MachineSet or MachineDeployment will allow me to automate the scale down actions for my clusters.

#### Story 2

As a Cluster API user, I would like to have special resource nodes (eg GPU enabled) provided when needed by workloads
without the need for human intervention. As these nodes might be more expensive, I would also like to return them when
not in use. By using the cluster autoscaler with a zero-sized MachineSet or MachineDeployment, I can automate the
creation of nodes that will not consume resources until they are required by applications on my cluster.

### Implementation Details/Notes/Constraints

Cloud providers, or users, can add an optional field to their machine template infrastructure objects to indicate
the resources provided by the machine. Internally, this field will be represented by a Go `map` type
utilizing named constants for the keys and `k8s.io/apimachinery/pkg/api/resource.Quantity` as the values
(similar to how resource limits and requests are handled for pods).

**Example implementation in Docker provider**
```
// these constants will be carried in Cluster API, but are repeated here for clarity
const (
    AutoscalerResourceCPU ResourceName = "cpu"
    AutoscalerResourceMemory ResourceName = "memory"
    AutoscalerResourceGPU ResourceName = "gpu"
)

// DockerMachineTemplateSpec defines the desired state of DockerMachineTemplate.
type DockerMachineTemplateSpec struct {
    AutoscalerCapacity ResourceList `json:"autoscalerCapacity,omitempty"`
    Template DockerMachineTemplateResource `json:"template"`
}
```
_Note: the `ResourceList` and `ResourceName` referenced are from k8s.io/kubernetes/pkg/apis/core/types`_

When used as a manifest, it would look like this:

```
apiVersion: infrastructure.cluster.x-k8s.io/v1alpha4
kind: DockerMachineTemplate
metadata:
  name: workload-md-0
  namespace: default
spec:
  autoscalerCapacity:
    memory: 500mb
    cpu: "1"
    gpu: "1"
  template:
    spec: {}
```

If a user wishes to override the resources hints as provided from the machine template, they may
do so by adding the following annotations to their MachineSets or MachineDeployments:

```
kind: MachineSet
metadata:
  annotations:
      autoscaler-capacity.x-k8s.io/gpu: "0"
      autoscaler-capacity.x-k8s.io/memoryMb: "8192"
      autoscaler-capacity.x-k8s.io/cpu: "2"
```

### Security Model

This feature will require the service account associated with the cluster autoscaler to have
the ability to `get` and `list` the Cluster API machine template infrastructure objects.

Beyond the permissions change, there should be no impact on the security model over current
cluster autoscaler usages.

### Risks and Mitigations

One risk for this process is that the user experience is slightly convoluted. A user (or cloud provider)
will need to create these fields for any machine template which will be used to scale from zero.
They will also need to ensure that their cluster autoscaler has the proper permissions for the cloud
provider they are using. Since these machine templates will differ from one provider to the next, it
is difficult to create a generic solution in the cluster autoscaler.

Creating clear documentation about the flow information, and the action of the cluster autoscaler
will be the first line of mitigating the confusion around this process. Additionally, adding
examples in the Docker provider and the cluster autoscaler will help to clarify the usage.

## Alternatives

An alternative approach would be to reconcile the information from the machine templates into the
MachineSet and MachineDeployment statuses. This would make the permissions and implementation on
the cluster autoscaler lighter. The trade off for making things easier on the cluster autoscaler is
that the process of exposing this information becomes more convoluted and the Cluster API controllers
will need to synchronize this data.

Another alternative would be for cloud providers to dynamically provide this information at run time.
This may be an approach to consider in the future, but it would still require a method to expose
the data at the MachineSet and MachineDeployment level. In effect this might look like an
automated method of achieving the same action as this proposal.

A much larger alternative would be to create a new custom resource that would act as an autoscaling
abstraction. This new resource would be accessed by both the cluster autoscaler and the Cluster API
controllers, as well as potentially another operator to own its lifecycle. This approach would
provide the cleanest separation between the components, and allow for future features in a contained
environment. The downside is that this approach requires the most engineering and design work to
accomplish.

## Upgrade Strategy

As this field is optional, it should not negatively affect upgrades. That said, care should be taken
to ensure that this field is copied during any object upgrade as its absence will create unexpected
behavior for end users.

In general, it should be safe for users to run the cluster autoscaler while performing an upgrade, but
this should be tested more and documented clearly in the autoscaler and Cluster API references.

## Additional Details

### Test Plan

The cluster autoscaler tests for Cluster API integration do not currently exist outside of the downstream
testing done by Red Hat on the OpenShift platform. There have talks over the last year to improve this
situation, but it is slow moving currently.

The end goal for testing is to contribute the scale from zero tests that currently exist for OpenShift
to the wider Kubernetes community. This will not be possible until the testing infrastructure around
the cluster autoscaler and Cluster API have resolved more.

## Implementation History

- [X] 06/10/2021: Proposed idea in an issue or [community meeting]
- [X] 03/04/2020: Previous pull request for [Add cluster autoscaler scale from zero ux proposal](https://github.com/kubernetes-sigs/cluster-api/pull/2530)
- [X] 10/07/2020: First round of feedback from community [initial proposal]
- [X] 03/10/2021: Present proposal at a [community meeting]
- [X] 03/10/2021: Open proposal PR

<!-- Links -->
[community meeting]: https://docs.google.com/document/d/1LW5SDnJGYNRB_TH9ZXjAn2jFin6fERqpC9a0Em0gwPE/edit#heading=h.bd545rc3d497
[initial proposal]: https://github.com/kubernetes-sigs/cluster-api/pull/2530
