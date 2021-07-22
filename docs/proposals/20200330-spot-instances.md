---
title: Add support for Spot Instances
authors:
  - "@JoelSpeed"
reviewers:
  - "@enxebre"
  - "@vincepri"
  - "@detiber"
  - "@ncdc"
  - "@CecileRobertMichon"
  - "@randomvariable"
creation-date: 2020-03-30
last-updated: 2020-06-24
status: provisional
see-also:
replaces:
superseded-by:
---

# Add support for Spot Instances

## Table of contents

<!--ts-->
   * [Add support for Spot Instances](#add-support-for-spot-instances)
      * [Table of contents](#table-of-contents)
      * [Glossary](#glossary)
      * [Summary](#summary)
      * [Motivation](#motivation)
         * [Goals](#goals)
         * [Non-Goals/Future Work](#non-goalsfuture-work)
      * [Proposal](#proposal)
         * [User Stories](#user-stories)
            * [Story 1](#story-1)
            * [Story 2](#story-2)
         * [Implementation Details/Notes/Constraints](#implementation-detailsnotesconstraints)
         * [Interruptible label](#interruptible-label)
            * [Cloud Provider Implementation Specifics](#cloud-provider-implementation-specifics)
               * [AWS](#aws)
                  * [Launching instances](#launching-instances)
               * [GCP](#gcp)
                  * [Launching instances](#launching-instances-1)
               * [Azure](#azure)
                  * [Launching Instances](#launching-instances-2)
                  * [Deallocation](#deallocation)
         * [Termination handler](#termination-handler)
            * [Termination handler design](#termination-handler-design)
            * [Running the termination handler](#running-the-termination-handler)
            * [Termination handler security](#termination-handler-security)
         * [Future Work](#future-work)
            * [Support for MachinePools](#support-for-machinepools)
         * [Risks and Mitigations](#risks-and-mitigations)
            * [Control-Plane instances](#control-plane-instances)
            * [Cloud Provider rate limits](#cloud-provider-rate-limits)
      * [Alternatives](#alternatives)
         * [Reserved Instances](#reserved-instances)
      * [Upgrade Strategy](#upgrade-strategy)
      * [Additional Details](#additional-details)
         * [Non-Guaranteed instances](#non-guaranteed-instances)
            * [AWS Spot Instances](#aws-spot-instances)
                  * [Spot backed Autoscaling Groups](#spot-backed-autoscaling-groups)
                  * [Spot Fleet](#spot-fleet)
                  * [Singular Spot Instances](#singular-spot-instances)
               * [Other AWS Spot features of note](#other-aws-spot-features-of-note)
                  * [Stop/Hibernate](#stophibernate)
                  * [Termination Notices](#termination-notices)
                  * [Persistent Requests](#persistent-requests)
            * [GCP Preemptible instances](#gcp-preemptible-instances)
                  * [Instance Groups](#instance-groups)
                  * [Single Instance](#single-instance)
               * [Limitations of Preemptible](#limitations-of-preemptible)
                  * [24 Hour limitation](#24-hour-limitation)
                  * [Shutdown warning](#shutdown-warning)
            * [Azure Spot VMs](#azure-spot-vms)
                  * [Scale Sets](#scale-sets)
                  * [Single Instances](#single-instances)
               * [Important Spot VM notes](#important-spot-vm-notes)
                  * [Termination Notices](#termination-notices-1)
                  * [Eviction Policies](#eviction-policies)
      * [Implementation History](#implementation-history)

<!-- Added by: jspeed, at: Tue 17 Mar 2020 15:21:18 GMT -->

<!--te-->

## Glossary

Refer to the [Cluster API Book Glossary](https://cluster-api.sigs.k8s.io/reference/glossary.html).

## Summary

Enable Cluster API users to leverage cheaper, non-guaranteed instances to back Cluster API Machines across multiple cloud providers.

## Motivation

Allow users to cut costs of running Kubernetes clusters on cloud providers by moving interruptible workloads onto non-guaranteed instances.

### Goals

- Provide sophisticated provider-specific automation for running Machines on non-guaranteed instances

- Utilise as much of the existing Cluster API as possible

### Non-Goals/Future Work

- Any logic for choosing instances types based on availability from the cloud provider

- A one to one map for each provider available mechanism for deploying spot instances, e.g. aws fleet.

- Support Spot instances via MachinePool for any cloud provider that doesn't already support MachinePool

- Ensure graceful shutdown of pods is attempted on non-guaranteed instances

## Proposal

To provide a consistent behaviour using non-guaranteed instances (Spot on AWS and Azure, Preepmtible on GCP)
across cloud providers, we must define a common behaviour based on the common features across each provider.

Based on the research on [non-guaranteed instances](#non-guaranteed-instances),
the following requirements for integration will work for each of AWS, Azure and GCP:

- Required configuration for enabling spot/preemptible instances should be added to the Infrastructure MachineSpec
  - No configuration should be required outside of this scope
  - MachineSpecs are part of the Infrastructure Templates used to create new Machines and as such, consistency is guaranteed across all instances built from this Template
  - All instances created by a MachineSet/MachinePool will either be on spot/preemptible or on on-demand instances

- A Machine should be paired 1:1 with an instance on the cloud provider
  - If the instance is preempted/terminated, the Infrastructure controller should not replace it
  - If the instance is preempted/terminated, the cloud provider should not replace it

- The Infrastructure controller is responsible for creation of the instance only and should not attempt to remediate problems

- The Infrastructure controller should not attempt to verify that an instance can be created before attempting to create the instance
  - If the cloud provider does not have capacity, the Machine Health Checker can (given required MHC) remove the Machine after a period.
    MachineSets will ensure the correct number of Machines are created.

- Initially, support will focus on Machine/MachineSets with MachinePool support being added at a later date

### User Stories

#### Story 1

As an operator of a Management Cluster, I want to reduce costs were possible by leveraging cheaper nodes for interruptible workloads on my Workload Clusters.

#### Story 2

As a user of a Workload Cluster, when a spot/preemptible node is due for termination, I want my workloads to be gracefully moved onto other nodes to minimise interruptions to my service.

### Implementation Details/Notes/Constraints

#### Cloud Provider Implementation Specifics

##### AWS

###### Launching instances

To launch an instance as a Spot instance on AWS, a [SpotMarketOptions](https://docs.aws.amazon.com/sdk-for-go/api/service/ec2/#SpotMarketOptions)
needs to be added to the `RunInstancesInput`. Within this there are 3 options that matter:

- InstanceInterruptionBehaviour (default: terminate): This must be set to `terminate` otherwise the SpotInstanceType cannot be `one-time`

- SpotInstanceType (default: one-time): This must be set to `one-time` to ensure that each Machine only creates on EC2 instance and that the spot request is

- MaxPrice (default: On-Demand price): This can be **optionally** set to a string representation of the hourly maximum spot price.
  If not set, the option will default to the On-Demand price of the EC2 instance type requested

The only option from this that needs exposing to the user from this is the `MaxPrice`, this option should be in an optional struct, if the struct is not nil,
then spot instances should be used, if the MaxPrice is set, this should be used instead of the default On-Demand price.

```
type SpotMarketOptions struct {
  MaxPrice *string `json:”maxPrice,omitempty”`
}

type AWSMachineSpec struct {
  ...

  SpotMarketOptions *SpotMarketOptions `json:”spotMarketOptions,omitempty”`
}
```

##### GCP

###### Launching instances

To launch an instance as Preemptible on GCP, the `Preemptible` field must be set:

```
&compute.Instance{
  ...
  Scheduling: &compute.Scheduling{
    ...
    Preemptible: true,
  },
}
```

Therefore, to make the choice up to the user, this field should be added to the `GCPMachineSpec`:

```
type GCPMachineSpec struct {
  ...
  Preemptible bool `json:”preemptible”`
}
```

##### Azure

###### Launching Instances

To launch a VM as a Spot VM on Azure, the following 3 options need to be set within the [VirtualMachineProperties](https://github.com/Azure/azure-sdk-for-go/blob/8d7ac6eb6a149f992df6f0392eebf48544e2564a/services/compute/mgmt/2019-07-01/compute/models.go#L10274-L10309)
when the instance is created:

- Priority: This must be set to `Spot` to request a Spot VM

- Eviction Policy: This has two options, `Deallocate` or `Delete`.
  Only `Deallocate` is valid when using singular Spot VMs and as such, this must be set to `Deallocate`.
  (Delete is supported for VMs as part of VMSS only).

- BillingProfile (default: -1) : This is a struct containing a single field, `MaxPrice`.
  This is a string representation of the maximum price the user wishes to pay for their VM.
  Uses a string representation because [floats are disallowed](https://github.com/kubernetes-sigs/controller-tools/issues/245#issuecomment-518465214) in Kubernetes APIs.
  This defaults to -1 which makes the maximum price the On-Demand price for the instance type.
  This also means the instance will never be evicted for price reasons as Azure caps Spot Market prices at the On-Demand price.
  (Note instances may still be evicted based on resource pressure within a region).

The only option that a user needs to interact with is the `MaxPrice` field within the `BillingProfile`, other fields only have 1 valid choice and as such can be inferred.
Similar to AWS, we can make an optional struct for SpotVMOptions, which, if present, implies the priority is `Spot`.

```
type SpotVMOptions struct {
  MaxPrice *string `json:”maxPrice,omitempty”`
}

type AzureMachineSpec struct {
  ...

  SpotVMOptions *SpotVMOptions `json:”spotVMOptions,omitempty”`
}
```

###### Deallocation

Since Spot VMs are not deleted when they are preempted and instead are deallocated,
users should utilise a MachineHealthCheck to monitor for preempted instances and replace them once they are stopped.
If they are left deallocated, their Disks and Networking are still active and chargeable by Azure.

When the MachineHealthCheck triggers a delete on the VM,
this will trigger the VM to be deleted which in turn will delete the other resources created as part of the VM.

**Note**: Because the instance is stopped, its Node is not removed from the API.
The Node will transition to an unready state which would be detected by a MachineHealthCheck,
though there may be some delay depending on the configuration of the MachineHealthCheck.
In the future, a termination handler could trigger the Machine to be deleted sooner.

### 'Interruptible' label

In order to deploy the termination handler, we'll need to create a DaemonSet that runs it on each spot instance node.

Having `"cluster.x-k8s.io/interruptible"` label on Nodes that run on interruptible instances should help us with it.

Based on the discussion here https://github.com/kubernetes-sigs/cluster-api/pull/3668 ([1](https://github.com/kubernetes-sigs/cluster-api/pull/3668#issuecomment-696143653), [2](https://github.com/kubernetes-sigs/cluster-api/pull/3668#issuecomment-696862994).) we can do following: 
1. User creates InfraMachine with whatever spec field(s) are required for that provider to indicate it's interruptible.
2. Infra provider sets InfraMachine.status.interruptible=true
3. Machine controller looks at InfraMachine.status.interruptible and ensures a label is set on the node if it is true.
4. Machine controller ensures the interruptible label is always present on the Node if InfraMachine.status.interruptible is true.

This snippet should work and it's similar to what is currently done to set node reference:

```
// Get and set the failure domain from the infrastructure provider.
var interruptible bool
err = util.UnstructuredUnmarshalField(infraConfig, &interruptible, "status", "interruptible")
switch {
case err == util.ErrUnstructuredFieldNotFound: // no-op
case err != nil:
	return errors.Wrapf(err, "failed to get interruptible status from infrastructure provider for Machine %q in namespace %q", m.Name, m.Namespace)
}

if !interruptible {
	return nil
}

// Here goes logic for assigning a label to node
```

### Future Work
### Termination handler

To enable graceful termination of workloads running on non-guaranteed instances,
a DaemonSet will need to be deployed to watch for termination notices and gracefully move workloads.

Alternatively, on AWS, termination events can be sourced via CloudWatch.
This would be preferable as a DaemonSet would not be required on workload clusters.

Since this is not essential for running on non-guaranteed instances and existing solutions exist for each provider,
users can deploy these existing solutions until CAPI has capacity to implement a solution.

#### Termination handler design

To enable graceful termination of workloads running on non-guaranteed instances,
a termination handler pod will need to be deployed on each spot instance.

The termination pod will be developed to poll the metadata service for the Node
that it is running on.
We will implement request/response handlers for each infrastructure provider that supports non-guaranteed instances
that will enable the handler to determine if the cloud provider instance is due
for termination.

The code for all providers will be mostly similar, the only difference is logic for polling termination endpoint and processing the response.
This means that cloud provider type can be passed as an argument and termination handler can be common for all providers and placed in a separate repository.

Should the instance be marked for termination, the handler will add a condition
to the Node object that it is running on to signal that the instance will be
terminating imminently.

```yaml
- type:     Terminating
  status:   True
  Reason:   TerminationRequested
  Message:  The cloud provider has marked this instance for termination
```

Once the Node has been marked with the `Terminating` condition, it will be
the MachineHealthCheck controller's responsibility to ensure that the Machine
is deleted, triggering it to be drained and removed from the cluster.

To enable this, the following MachineHealthCheck should be deployed:

```yaml
---
apiVersion: cluster.x-k8s.io/v1alpha3
kind: MachineHealthCheck
metadata:
  name: cluster-api-termination-handler
  labels:
    api: clusterapi
    k8s-app: termination-handler
spec:
  selector:
    matchLabels:
      cluster.x-k8s.io/interruptible-instance: "" # This label should be automatically applied to all spot instances
  maxUnhealthy: 100% # No reason to block the interruption, it's inevitable
  unhealthyConditions:
  - type: Terminating
    status: "True"
    timeout: 0s # Immediately terminate the instance
```

#### Running the termination handler
The Termination Pod will be part of a DaemonSet, that can be deployed using [ClusterResourceSet](https://github.com/kubernetes-sigs/cluster-api/blob/master/docs/proposals/20200220-cluster-resource-set.md). The DaemonSet will select Nodes which are labelled as spot instances to ensure the Termination Pod only runs on instances that require termination handlers.

The spot label will be added to the Node by the machine controller as described [here](#interruptible-label), provided they support spot instances and the instance is a spot instance. 
#### Termination handler security
The metadata services that are hosted by the cloud providers are only accessible from the hosts themselves, so the pod will need to run within the host network.

To restrict the possible effects of the termination handler, it should re-use the Kubelet credentials which pass through the [NodeRestriction](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/#noderestriction) admission controller. This limits the termination handler to only be able to modify the Node on which it is running. Eg, it would not be able to set the conditions on a different Node.

To be able to mount and read the Kubelet credentials, the pod must be able to mount host paths and, since the credentials have to be mounted as read-only just for the root account, the termination handler must be able to run as root.

### Future Work

#### Support for MachinePools

While MachinePools are being implemented across the three cloud providers that this project covers,
we will not be focusing on support non-guaranteed instances within MachinePools.

Once initial support for non-guaranteed instances has been tested and implemented within the providers,
we will investigate supporting non-guaranteed instances within MachinePools in a follow up proposal.

### Risks and Mitigations

#### Control-Plane instances

Due to control-plane instances typically hosting etcd for the cluster,
running this on top of spot instances, where termination is more likely,
could introduce instability to the cluster or even result in a loss of quorum for the etcd cluster.
Running control-plane instances on top of spot instances should be forbidden.

There may also be limitations within cloud providers that restrict the usage of spot instances within the control-plane,
e.g. Azure Spot VMs do not support [ephemeral disks](https://docs.microsoft.com/en-us/azure/virtual-machines/linux/spot-vms#limitations) which may be desired for control-plane instances.

This risk will be documented and it will be strongly advised that users do not attempt to create control-plane instances on spot instances.
To prevent it completely, an admission controller could be used to verify that Infrastructure Machines do not get created with the control-plane label,
specifying that they should run on spot-instances.

#### Cloud Provider rate limits

Currently, if there is an issue creating the Infrastructure instance for any reason,
the request to create the instance will be requeued.
When the issue is persistent (eg. Spot Bid too low on AWS),
this could lead to the Infrastructure controller attempting to create machines and failing in a loop.

To prevent this, Machine's could enter a failed state if persistent errors such as this occur.
This also has the added benefit of being more visible to a user, as currently, no error is reported apart from in logs.

Failing the Machine would allow a MachineHealthCheck to be used to clean up the Failed machines.
The MachineHealthCheck controller could handle the looping by using backoff on deletion of failed Machine's for a particular MachineHealthCheck,
which would be useful for MachineHealthCheck and keep this logic centralling in a non-cloud provider specific component of Cluster API.

## Alternatives

### Reserved Instances

Reserved instances offer cheaper compute costs by charging for the capacity up front for larger time periods.
Typically this is a yearly commitment to spending a certain amount.

While this would also allow users to save money on their compute,
it commits them to large up front spends, the savings are not as high and this could also be implemented tangentially to this proposal.

## Upgrade Strategy

This proposal only adds new features and should not affect existing clusters.
No special upgrade considerations should be required.

## Additional Details

### Non-Guaranteed instances

Behaviour of non-guaranteed instances varies from provider to provider.
With each provider offering different ways to create the instances and different guarantees for the instances.
Each of the following sections details how non-guaranteed instances works for each provider.

#### AWS Spot Instances

Amazon’s Spot instances are available to customers via three different mechanisms.
Each mechanism requires the user to set a maximum price (a bid) they are willing to pay for the instances and,
until either no-capacity is left, or the market price exceeds their bid, the user will retain access to the machine.

###### Spot backed Autoscaling Groups

Spot backed Autoscaling groups are identical to other Autoscaling groups, other than that they use Spot instances instead of On-Demand instances.

Autoscaling Groups are not currently supported within Cluster API, though adding support could be part of the MachinePool efforts.
If support were added, enabling Spot backed Autoscaling Groups would be a case of modifying the launch configuration to provide the relevant Spot options.

###### Spot Fleet

Spot Fleets are similar to Spot backed Autoscaling Groups, but they differ in that there is no dedicated instance type for the group.
They can launch both On-Demand and Spot instances from a range of instance types available based on the market prices and the bid put forward by the user.

Similarly to Spot backed Autoscaling groups, there is currently no support within the Cluster API.
Spot Fleet could become part of the MachinePool effort, however this would require a considerable effort to design and implement and as such,
support should not be considered a goal within this proposal.

###### Singular Spot Instances
Singular Spot instances are created using the same API as singular On-Demand instances.
By providing a single additional parameter, the API will instead launch a Spot Instance.

Given that the Cluster API currently implements Machine’s by using singular On-Demand instances,
adding singular Spot Instance support via this mechanism should be trivial.

##### Other AWS Spot features of note

###### Stop/Hibernate

Instead of terminating an instance when it is being interrupted,
Spot instances can be “stopped” or “hibernated” so that they can resume their workloads when new capacity becomes available.

Using this feature would contradict the functionality of the Machine Health Check remediation of failed nodes.
In cloud environments, it is expected that if a node is being switched off or taken away, a new one will replace it.
This option should not be made available to users to avoid conflicts within the Cluster API ecosystem.

###### Termination Notices

Amazon provides a 2 minute notice of termination for Spot instances via it’s instance metadata service.
Each instance can poll the metadata service to see if it has been marked for termination.
There are [existing solutions](https://github.com/kube-aws/kube-spot-termination-notice-handler)
that run Daemonsets on Spot instances to gracefully drain workloads when the termination notice is given.
This is something that should be provided as part of the spot instance availability within Cluster API.

###### Persistent Requests

Persistent requests allow users to ask that a Spot instance, once terminated, be replace by another instance when new capacity is available.

Using this feature would break assumptions in Cluster API since the instance ID for the Machine would change during its lifecycle.
The usage of this feature should be explicitly forbidden so that we do not break existing assumptions.

#### GCP Preemptible instances

GCP’s Preemptible instances are available to customers via two mechanisms.
For each, the instances are available at a fixed price and will be made available to users whenever there is capacity.

###### Instance Groups

GCP Instance Groups can leverage Preemptible instances by modifying the instance template and setting Preemptible option.

Instance Groups are not currently supported within Cluster API, though adding support could be part of the MachinePool efforts.
If support were added, enabling Preemptible Instance Groups would be a case of modifying the configuration to provide the relevant Preemptible option.

###### Single Instance

GCP Single Instances can run on Preemptible instances given the launch request specifies the preemptible option.

Given that the Cluster API currently implements Machine’s by using single instances, adding singular Preemptible Instance support via this mechanism should be trivial.

##### Limitations of Preemptible

###### 24 Hour limitation

Preemptible instance will, if not already, be terminated after 24 hours.
This means that the instances will be cycled regularly and as such, good handling of shutdown events should be implemented.

###### Shutdown warning

GCP gives a 30 second warning for termination of Preemptible instances.
This signal comes via an ACPI G2 soft-off signal to the machine, which, could be intercepted to start a graceful termination of pods on the machine.
There are [existing projects](https://github.com/GoogleCloudPlatform/k8s-node-termination-handler) that already do this.

In the case that the node is reaching its 24 hour termination mark,
it may be safer to preempt this warning and shut down the node before the 30s shut down signal to provide adequate time for workloads to be moved gracefully/

#### Azure Spot VMs

Azure recently announced Spot VMs as a replacement for their Low-Priority VMs which were in customer preview through the latter half of 2019.
Spot VMs work in a similar manner to AWS Spot Instances. A maximum price is set on the instance when it is created, and, until that price is reached,
the instance will be given to you and you will be charged the market rate. Should the price go above your maximum price, the instance will be preempted.
Additionally, at any point in time when Azure needs the capacity back, the Azure infrastructure will evict Spot instance.

Spot VMs are available in two forms in Azure.

###### Scale Sets

Scale sets include support for Spot VMs by indicating when created, that they should be backed by Spot VMs.
At this point, an eviction policy should be set and a maximum price you wish to pay.
Alternatively, you can also choose to only be preempted in the case that there are capacity constraints,
in which case, you will pay whatever the market rate is, but will be preempted less often.

Scale Set are not currently supported within Cluster API, though they are being added as part of the MachinePool efforts.
Once support is added, enabling Spot backed Scale Sets would be a case of modifying the configuration to provide the relevant Spot options.

###### Single Instances
Azure supports Spot VMs on single VM instances by indicating when created, that the VM should be a Spot VM.
At this point, an eviction policy should be set and a maximum price you wish to pay.
Alternatively, you can also choose to only be preempted in the case that there are capacity constraints,
in which case, you will pay whatever the market rate is, but will be preempted less often.

Given that the Cluster API currently implements Machine’s by using single instances, adding singular Spot VM support via this mechanism should be trivial.

##### Important Spot VM notes

###### Termination Notices

Azure uses their Scheduled Events API to notify Spot VMs that they are due to be preempted.
This is a similar service to the AWS metadata service that each machine can poll to see events for itself.
Azure only gives 30 seconds warning for nodes being preempted though.

A Daemonset solution similar to the AWS termination handlers could be implemented to provide graceful shutdown with Azure Spot VMs.
For example see this [existing solution](https://github.com/awesomenix/drainsafe).

###### Eviction Policies

Azure Spot VMs support two types of eviction policy:

- Deallocate: This stops the VM but keeps disks and networking ready to be restarted.
  In this state, VMs maintain usage of the CPU quota and as such, are effectively just paused or hibernating.
  This is the *only* supported eviction policy for Single Instance Spot VMs.

- Delete: This deletes the VM and all associated disks and networking when the node is preempted.
  This is *only* supported on Scale Sets backed by Spot VMs.

## Implementation History

- [x] 12/11/2019: Proposed idea in an [issue](https://github.com/kubernetes-sigs/cluster-api/issues/1876)
- [x] 02/25/2020: Compile a Google Doc following the CAEP template (https://docs.google.com/document/d/1naxBVVlI_O-u6TchvQyZFbIaKrwU9qAzYD4akyV68nQ)
- [ ] MM/DD/YYYY: First round of feedback from community
- [ ] MM/DD/YYYY: Present proposal at a [community meeting]
- [x] 03/30/2020: Open proposal PR

<!-- Links -->
[community meeting]: https://docs.google.com/document/d/1Ys-DOR5UsgbMEeciuG0HOgDQc8kZsaWIWJeKJ1-UfbY
