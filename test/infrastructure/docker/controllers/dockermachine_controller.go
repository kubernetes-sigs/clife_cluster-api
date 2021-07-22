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
	"encoding/base64"
	"fmt"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	infrav1 "sigs.k8s.io/cluster-api/test/infrastructure/docker/api/v1alpha4"
	"sigs.k8s.io/cluster-api/test/infrastructure/docker/docker"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/cluster-api/util/predicates"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"sigs.k8s.io/kind/pkg/cluster/constants"
)

// DockerMachineReconciler reconciles a DockerMachine object.
type DockerMachineReconciler struct {
	client.Client
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=dockermachines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=dockermachines/status;dockermachines/finalizers,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;machines,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets;,verbs=get;list;watch

// Reconcile handles DockerMachine events.
func (r *DockerMachineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, rerr error) {
	log := ctrl.LoggerFrom(ctx)

	// Fetch the DockerMachine instance.
	dockerMachine := &infrav1.DockerMachine{}
	if err := r.Client.Get(ctx, req.NamespacedName, dockerMachine); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Fetch the Machine.
	machine, err := util.GetOwnerMachine(ctx, r.Client, dockerMachine.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, err
	}
	if machine == nil {
		log.Info("Waiting for Machine Controller to set OwnerRef on DockerMachine")
		return ctrl.Result{}, nil
	}

	log = log.WithValues("machine", machine.Name)

	// Fetch the Cluster.
	cluster, err := util.GetClusterFromMetadata(ctx, r.Client, machine.ObjectMeta)
	if err != nil {
		log.Info("DockerMachine owner Machine is missing cluster label or cluster does not exist")
		return ctrl.Result{}, err
	}
	if cluster == nil {
		log.Info(fmt.Sprintf("Please associate this machine with a cluster using the label %s: <name of cluster>", clusterv1.ClusterLabelName))
		return ctrl.Result{}, nil
	}

	log = log.WithValues("cluster", cluster.Name)

	// Return early if the object or Cluster is paused.
	if annotations.IsPaused(cluster, dockerMachine) {
		log.Info("Reconciliation is paused for this object")
		return ctrl.Result{}, nil
	}

	// Fetch the Docker Cluster.
	dockerCluster := &infrav1.DockerCluster{}
	dockerClusterName := client.ObjectKey{
		Namespace: dockerMachine.Namespace,
		Name:      cluster.Spec.InfrastructureRef.Name,
	}
	if err := r.Client.Get(ctx, dockerClusterName, dockerCluster); err != nil {
		log.Info("DockerCluster is not available yet")
		return ctrl.Result{}, nil
	}

	log = log.WithValues("docker-cluster", dockerCluster.Name)

	// Initialize the patch helper
	patchHelper, err := patch.NewHelper(dockerMachine, r)
	if err != nil {
		return ctrl.Result{}, err
	}
	// Always attempt to Patch the DockerMachine object and status after each reconciliation.
	defer func() {
		if err := patchDockerMachine(ctx, patchHelper, dockerMachine); err != nil {
			log.Error(err, "failed to patch DockerMachine")
			if rerr == nil {
				rerr = err
			}
		}
	}()

	// Add finalizer first if not exist to avoid the race condition between init and delete
	if !controllerutil.ContainsFinalizer(dockerMachine, infrav1.MachineFinalizer) {
		controllerutil.AddFinalizer(dockerMachine, infrav1.MachineFinalizer)
		return ctrl.Result{}, nil
	}

	// Check if the infrastructure is ready, otherwise return and wait for the cluster object to be updated
	if !cluster.Status.InfrastructureReady {
		log.Info("Waiting for DockerCluster Controller to create cluster infrastructure")
		conditions.MarkFalse(dockerMachine, infrav1.ContainerProvisionedCondition, infrav1.WaitingForClusterInfrastructureReason, clusterv1.ConditionSeverityInfo, "")
		return ctrl.Result{}, nil
	}

	// Create a helper for managing the docker container hosting the machine.
	externalMachine, err := docker.NewMachine(cluster, machine.Name, dockerMachine.Spec.CustomImage, nil)
	if err != nil {
		return ctrl.Result{}, errors.Wrapf(err, "failed to create helper for managing the externalMachine")
	}

	// Create a helper for managing a docker container hosting the loadbalancer.
	// NB. the machine controller has to manage the cluster load balancer because the current implementation of the
	// docker load balancer does not support auto-discovery of control plane nodes, so CAPD should take care of
	// updating the cluster load balancer configuration when control plane machines are added/removed
	externalLoadBalancer, err := docker.NewLoadBalancer(cluster, dockerCluster)
	if err != nil {
		return ctrl.Result{}, errors.Wrapf(err, "failed to create helper for managing the externalLoadBalancer")
	}

	// Handle deleted machines
	if !dockerMachine.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, machine, dockerMachine, externalMachine, externalLoadBalancer)
	}

	// Handle non-deleted machines
	return r.reconcileNormal(ctx, cluster, machine, dockerMachine, externalMachine, externalLoadBalancer)
}

func patchDockerMachine(ctx context.Context, patchHelper *patch.Helper, dockerMachine *infrav1.DockerMachine) error {
	// Always update the readyCondition by summarizing the state of other conditions.
	// A step counter is added to represent progress during the provisioning process (instead we are hiding the step counter during the deletion process).
	conditions.SetSummary(dockerMachine,
		conditions.WithConditions(
			infrav1.ContainerProvisionedCondition,
			infrav1.BootstrapExecSucceededCondition,
		),
		conditions.WithStepCounterIf(dockerMachine.ObjectMeta.DeletionTimestamp.IsZero()),
	)

	// Patch the object, ignoring conflicts on the conditions owned by this controller.
	return patchHelper.Patch(
		ctx,
		dockerMachine,
		patch.WithOwnedConditions{Conditions: []clusterv1.ConditionType{
			clusterv1.ReadyCondition,
			infrav1.ContainerProvisionedCondition,
			infrav1.BootstrapExecSucceededCondition,
		}},
	)
}

func (r *DockerMachineReconciler) reconcileNormal(ctx context.Context, cluster *clusterv1.Cluster, machine *clusterv1.Machine, dockerMachine *infrav1.DockerMachine, externalMachine *docker.Machine, externalLoadBalancer *docker.LoadBalancer) (res ctrl.Result, retErr error) {
	log := ctrl.LoggerFrom(ctx)

	// if the machine is already provisioned, return
	if dockerMachine.Spec.ProviderID != nil {
		// ensure ready state is set.
		// This is required after move, because status is not moved to the target cluster.
		dockerMachine.Status.Ready = true
		conditions.MarkTrue(dockerMachine, infrav1.ContainerProvisionedCondition)
		return ctrl.Result{}, nil
	}

	// Make sure bootstrap data is available and populated.
	if machine.Spec.Bootstrap.DataSecretName == nil {
		if !util.IsControlPlaneMachine(machine) && !conditions.IsTrue(cluster, clusterv1.ControlPlaneInitializedCondition) {
			log.Info("Waiting for the control plane to be initialized")
			conditions.MarkFalse(dockerMachine, infrav1.ContainerProvisionedCondition, clusterv1.WaitingForControlPlaneAvailableReason, clusterv1.ConditionSeverityInfo, "")
			return ctrl.Result{}, nil
		}

		log.Info("Waiting for the Bootstrap provider controller to set bootstrap data")
		conditions.MarkFalse(dockerMachine, infrav1.ContainerProvisionedCondition, infrav1.WaitingForBootstrapDataReason, clusterv1.ConditionSeverityInfo, "")
		return ctrl.Result{}, nil
	}

	// Create the docker container hosting the machine
	role := constants.WorkerNodeRoleValue
	if util.IsControlPlaneMachine(machine) {
		role = constants.ControlPlaneNodeRoleValue
	}

	// Create the machine if not existing yet
	if !externalMachine.Exists() {
		if err := externalMachine.Create(ctx, role, machine.Spec.Version, dockerMachine.Spec.ExtraMounts); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "failed to create worker DockerMachine")
		}
	}

	// Preload images into the container
	if len(dockerMachine.Spec.PreLoadImages) > 0 {
		if err := externalMachine.PreloadLoadImages(ctx, dockerMachine.Spec.PreLoadImages); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "failed to pre-load images into the DockerMachine")
		}
	}

	// if the machine is a control plane update the load balancer configuration
	// we should only do this once, as reconfiguration more or less ensures
	// node ref setting fails
	if util.IsControlPlaneMachine(machine) && !dockerMachine.Status.LoadBalancerConfigured {
		if err := externalLoadBalancer.UpdateConfiguration(ctx); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "failed to update DockerCluster.loadbalancer configuration")
		}
		dockerMachine.Status.LoadBalancerConfigured = true
	}

	// Update the ContainerProvisionedCondition condition
	// NOTE: it is required to create the patch helper before this change otherwise it wont surface if
	// we issue a patch down in the code (because if we create patch helper after this point the ContainerProvisionedCondition=True exists both on before and after).
	patchHelper, err := patch.NewHelper(dockerMachine, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}
	conditions.MarkTrue(dockerMachine, infrav1.ContainerProvisionedCondition)

	// At, this stage, we are ready for bootstrap. However, if the BootstrapExecSucceededCondition is missing we add it and we
	// issue an patch so the user can see the change of state before the bootstrap actually starts.
	// NOTE: usually controller should not rely on status they are setting, but on the observed state; however
	// in this case we are doing this because we explicitly want to give a feedback to users.
	if !conditions.Has(dockerMachine, infrav1.BootstrapExecSucceededCondition) {
		conditions.MarkFalse(dockerMachine, infrav1.BootstrapExecSucceededCondition, infrav1.BootstrappingReason, clusterv1.ConditionSeverityInfo, "")
		if err := patchDockerMachine(ctx, patchHelper, dockerMachine); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "failed to patch DockerMachine")
		}
	}

	// if the machine isn't bootstrapped, only then run bootstrap scripts
	if !dockerMachine.Spec.Bootstrapped {
		bootstrapData, err := r.getBootstrapData(ctx, machine)
		if err != nil {
			log.Error(err, "failed to get bootstrap data")
			return ctrl.Result{}, err
		}

		timeoutctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
		// Run the bootstrap script. Simulates cloud-init.
		if err := externalMachine.ExecBootstrap(timeoutctx, bootstrapData); err != nil {
			conditions.MarkFalse(dockerMachine, infrav1.BootstrapExecSucceededCondition, infrav1.BootstrapFailedReason, clusterv1.ConditionSeverityWarning, "Repeating bootstrap")
			return ctrl.Result{}, errors.Wrap(err, "failed to exec DockerMachine bootstrap")
		}
		// Check for bootstrap success
		if err := externalMachine.CheckForBootstrapSuccess(timeoutctx); err != nil {
			conditions.MarkFalse(dockerMachine, infrav1.BootstrapExecSucceededCondition, infrav1.BootstrapFailedReason, clusterv1.ConditionSeverityWarning, "Repeating bootstrap")
			return ctrl.Result{}, errors.Wrap(err, "failed to check for existence of bootstrap success file at /run/cluster-api/bootstrap-success.complete")
		}

		dockerMachine.Spec.Bootstrapped = true
	}

	// Update the BootstrapExecSucceededCondition condition
	conditions.MarkTrue(dockerMachine, infrav1.BootstrapExecSucceededCondition)

	// set address in machine status
	machineAddress, err := externalMachine.Address(ctx)
	if err != nil {
		log.Error(err, "failed to get the machine address")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	dockerMachine.Status.Addresses = []clusterv1.MachineAddress{
		{
			Type:    clusterv1.MachineHostName,
			Address: externalMachine.ContainerName(),
		},
		{
			Type:    clusterv1.MachineInternalIP,
			Address: machineAddress,
		},
		{
			Type:    clusterv1.MachineExternalIP,
			Address: machineAddress,
		},
	}

	// Usually a cloud provider will do this, but there is no docker-cloud provider.
	// Requeue if there is an error, as this is likely momentary load balancer
	// state changes during control plane provisioning.
	if err := externalMachine.SetNodeProviderID(ctx); err != nil {
		if errors.As(err, &docker.ContainerNotRunningError{}) {
			return ctrl.Result{}, errors.Wrap(err, "failed to patch the Kubernetes node with the machine providerID")
		}
		log.Error(err, "failed to patch the Kubernetes node with the machine providerID")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}
	// Set ProviderID so the Cluster API Machine Controller can pull it
	providerID := externalMachine.ProviderID()
	dockerMachine.Spec.ProviderID = &providerID
	dockerMachine.Status.Ready = true
	conditions.MarkTrue(dockerMachine, infrav1.ContainerProvisionedCondition)

	return ctrl.Result{}, nil
}

func (r *DockerMachineReconciler) reconcileDelete(ctx context.Context, machine *clusterv1.Machine, dockerMachine *infrav1.DockerMachine, externalMachine *docker.Machine, externalLoadBalancer *docker.LoadBalancer) (ctrl.Result, error) {
	// Set the ContainerProvisionedCondition reporting delete is started, and issue a patch in order to make
	// this visible to the users.
	// NB. The operation in docker is fast, so there is the chance the user will not notice the status change;
	// nevertheless we are issuing a patch so we can test a pattern that will be used by other providers as well
	patchHelper, err := patch.NewHelper(dockerMachine, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}
	conditions.MarkFalse(dockerMachine, infrav1.ContainerProvisionedCondition, clusterv1.DeletingReason, clusterv1.ConditionSeverityInfo, "")
	if err := patchDockerMachine(ctx, patchHelper, dockerMachine); err != nil {
		return ctrl.Result{}, errors.Wrap(err, "failed to patch DockerMachine")
	}

	// delete the machine
	if err := externalMachine.Delete(ctx); err != nil {
		return ctrl.Result{}, errors.Wrap(err, "failed to delete DockerMachine")
	}

	// if the deleted machine is a control-plane node, remove it from the load balancer configuration;
	if util.IsControlPlaneMachine(machine) {
		if err := externalLoadBalancer.UpdateConfiguration(ctx); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "failed to update DockerCluster.loadbalancer configuration")
		}
	}

	// Machine is deleted so remove the finalizer.
	controllerutil.RemoveFinalizer(dockerMachine, infrav1.MachineFinalizer)
	return ctrl.Result{}, nil
}

// SetupWithManager will add watches for this controller.
func (r *DockerMachineReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, options controller.Options) error {
	clusterToDockerMachines, err := util.ClusterToObjectsMapper(mgr.GetClient(), &infrav1.DockerMachineList{}, mgr.GetScheme())
	if err != nil {
		return err
	}

	c, err := ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.DockerMachine{}).
		WithOptions(options).
		WithEventFilter(predicates.ResourceNotPaused(ctrl.LoggerFrom(ctx))).
		Watches(
			&source.Kind{Type: &clusterv1.Machine{}},
			handler.EnqueueRequestsFromMapFunc(util.MachineToInfrastructureMapFunc(infrav1.GroupVersion.WithKind("DockerMachine"))),
		).
		Watches(
			&source.Kind{Type: &infrav1.DockerCluster{}},
			handler.EnqueueRequestsFromMapFunc(r.DockerClusterToDockerMachines),
		).
		Build(r)
	if err != nil {
		return err
	}
	return c.Watch(
		&source.Kind{Type: &clusterv1.Cluster{}},
		handler.EnqueueRequestsFromMapFunc(clusterToDockerMachines),
		predicates.ClusterUnpausedAndInfrastructureReady(ctrl.LoggerFrom(ctx)),
	)
}

// DockerClusterToDockerMachines is a handler.ToRequestsFunc to be used to enqeue
// requests for reconciliation of DockerMachines.
func (r *DockerMachineReconciler) DockerClusterToDockerMachines(o client.Object) []ctrl.Request {
	result := []ctrl.Request{}
	c, ok := o.(*infrav1.DockerCluster)
	if !ok {
		panic(fmt.Sprintf("Expected a DockerCluster but got a %T", o))
	}

	cluster, err := util.GetOwnerCluster(context.TODO(), r.Client, c.ObjectMeta)
	switch {
	case apierrors.IsNotFound(err) || cluster == nil:
		return result
	case err != nil:
		return result
	}

	labels := map[string]string{clusterv1.ClusterLabelName: cluster.Name}
	machineList := &clusterv1.MachineList{}
	if err := r.Client.List(context.TODO(), machineList, client.InNamespace(c.Namespace), client.MatchingLabels(labels)); err != nil {
		return nil
	}
	for _, m := range machineList.Items {
		if m.Spec.InfrastructureRef.Name == "" {
			continue
		}
		name := client.ObjectKey{Namespace: m.Namespace, Name: m.Name}
		result = append(result, ctrl.Request{NamespacedName: name})
	}

	return result
}

func (r *DockerMachineReconciler) getBootstrapData(ctx context.Context, machine *clusterv1.Machine) (string, error) {
	if machine.Spec.Bootstrap.DataSecretName == nil {
		return "", errors.New("error retrieving bootstrap data: linked Machine's bootstrap.dataSecretName is nil")
	}

	s := &corev1.Secret{}
	key := client.ObjectKey{Namespace: machine.GetNamespace(), Name: *machine.Spec.Bootstrap.DataSecretName}
	if err := r.Client.Get(ctx, key, s); err != nil {
		return "", errors.Wrapf(err, "failed to retrieve bootstrap data secret for DockerMachine %s/%s", machine.GetNamespace(), machine.GetName())
	}

	value, ok := s.Data["value"]
	if !ok {
		return "", errors.New("error retrieving bootstrap data: secret value key is missing")
	}

	return base64.StdEncoding.EncodeToString(value), nil
}
