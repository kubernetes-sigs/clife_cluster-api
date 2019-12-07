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
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	bootstrapv1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1alpha3"
	"sigs.k8s.io/cluster-api/bootstrap/kubeadm/cloudinit"
	internalcluster "sigs.k8s.io/cluster-api/bootstrap/kubeadm/internal/cluster"
	"sigs.k8s.io/cluster-api/bootstrap/kubeadm/internal/locking"
	kubeadmv1beta1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/types/v1beta1"
	"sigs.k8s.io/cluster-api/controllers/remote"
	capierrors "sigs.k8s.io/cluster-api/errors"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/cluster-api/util/secret"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// InitLocker is a lock that is used around kubeadm init
type InitLocker interface {
	Lock(ctx context.Context, cluster *clusterv1.Cluster, machine *clusterv1.Machine) bool
	Unlock(ctx context.Context, cluster *clusterv1.Cluster) bool
}

// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=kubeadmconfigs;kubeadmconfigs/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status;machines;machines/status,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets;events;configmaps,verbs=get;list;watch;create;update;patch;delete

// KubeadmConfigReconciler reconciles a KubeadmConfig object
type KubeadmConfigReconciler struct {
	Client          client.Client
	KubeadmInitLock InitLocker
	Log             logr.Logger
	scheme          *runtime.Scheme

	// for testing
	remoteClient func(client.Client, *clusterv1.Cluster, *runtime.Scheme) (client.Client, error)
}

type Scope struct {
	logr.Logger
	Config  *bootstrapv1.KubeadmConfig
	Cluster *clusterv1.Cluster
	Machine *clusterv1.Machine
}

// SetupWithManager sets up the reconciler with the Manager.
func (r *KubeadmConfigReconciler) SetupWithManager(mgr ctrl.Manager, option controller.Options) error {
	if r.KubeadmInitLock == nil {
		r.KubeadmInitLock = locking.NewControlPlaneInitMutex(ctrl.Log.WithName("init-locker"), mgr.GetClient())
	}
	if r.remoteClient == nil {
		r.remoteClient = remote.NewClusterClient
	}

	r.scheme = mgr.GetScheme()

	err := ctrl.NewControllerManagedBy(mgr).
		For(&bootstrapv1.KubeadmConfig{}).
		Watches(
			&source.Kind{Type: &clusterv1.Machine{}},
			&handler.EnqueueRequestsFromMapFunc{
				ToRequests: handler.ToRequestsFunc(r.MachineToBootstrapMapFunc),
			},
		).
		Watches(
			&source.Kind{Type: &clusterv1.Cluster{}},
			&handler.EnqueueRequestsFromMapFunc{
				ToRequests: handler.ToRequestsFunc(r.ClusterToKubeadmConfigs),
			},
		).
		WithOptions(option).
		Complete(r)

	if err != nil {
		return errors.Wrap(err, "failed setting up with a controller manager")
	}

	return nil
}

// Reconcile handles KubeadmConfig events.
func (r *KubeadmConfigReconciler) Reconcile(req ctrl.Request) (_ ctrl.Result, rerr error) {
	ctx := context.Background()
	log := r.Log.WithValues("kubeadmconfig", req.NamespacedName)

	// Lookup the kubeadm config
	config := &bootstrapv1.KubeadmConfig{}
	if err := r.Client.Get(ctx, req.NamespacedName, config); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "failed to get config")
		return ctrl.Result{}, err
	}

	// Look up the Machine that owns this KubeConfig if there is one
	machine, err := util.GetOwnerMachine(ctx, r.Client, config.ObjectMeta)
	if err != nil {
		log.Error(err, "could not get owner machine")
		return ctrl.Result{}, err
	}
	if machine == nil {
		log.Info("Waiting for Machine Controller to set OwnerRef on the KubeadmConfig")
		return ctrl.Result{}, nil
	}
	log = log.WithValues("machine-name", machine.Name)

	// Lookup the cluster the machine is associated with
	cluster, err := util.GetClusterByName(ctx, r.Client, machine.ObjectMeta.Namespace, machine.Spec.ClusterName)
	if err != nil {
		if errors.Cause(err) == util.ErrNoCluster {
			log.Info("Machine does not belong to a cluster yet, waiting until its part of a cluster")
			return ctrl.Result{}, nil
		}

		if apierrors.IsNotFound(err) {
			log.Info("Cluster does not exist yet , waiting until it is created")
			return ctrl.Result{}, nil
		}
		log.Error(err, "could not get cluster by machine metadata")
		return ctrl.Result{}, err
	}

	switch {
	// Wait patiently for the infrastructure to be ready
	case !cluster.Status.InfrastructureReady:
		log.Info("Infrastructure is not ready, waiting until ready.")
		return ctrl.Result{}, nil
	// bail super early if it's already ready
	case config.Status.Ready && machine.Status.InfrastructureReady:
		log.Info("ignoring config for an already ready machine")
		return ctrl.Result{}, nil
	// Reconcile status for machines that have already copied bootstrap data
	case machine.Spec.Bootstrap.Data != nil && !config.Status.Ready:
		config.Status.Ready = true
		// Initialize the patch helper
		patchHelper, err := patch.NewHelper(config, r.Client)
		if err != nil {
			return ctrl.Result{}, err
		}
		err = patchHelper.Patch(ctx, config)
		return ctrl.Result{}, err
	// If we've already embedded a time-limited join token into a config, but are still waiting for the token to be used, refresh it
	case config.Status.Ready && (config.Spec.JoinConfiguration != nil && config.Spec.JoinConfiguration.Discovery.BootstrapToken != nil):
		token := config.Spec.JoinConfiguration.Discovery.BootstrapToken.Token

		remoteClient, err := r.remoteClient(r.Client, cluster, r.scheme)
		if err != nil {
			log.Error(err, "error creating remote cluster client")
			return ctrl.Result{}, err
		}

		log.Info("refreshing token until the infrastructure has a chance to consume it")
		err = refreshToken(remoteClient, token)
		if err != nil {
			// It would be nice to re-create the bootstrap token if the error was "not found", but we have no way to update the Machine's bootstrap data
			return ctrl.Result{}, errors.Wrapf(err, "failed to refresh bootstrap token")
		}
		// NB: this may not be sufficient to keep the token live if we don't see it before it expires, but when we generate a config we will set the status to "ready" which should generate an update event
		return ctrl.Result{
			RequeueAfter: DefaultTokenTTL / 2,
		}, nil
	}

	// Initialize the patch helper
	patchHelper, err := patch.NewHelper(config, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}
	// Attempt to Patch the KubeadmConfig object and status after each reconciliation if no error occurs.
	defer func() {
		if rerr == nil {
			if rerr = patchHelper.Patch(ctx, config); rerr != nil {
				log.Error(rerr, "failed to patch config")
			}
		}
	}()

	scope := &Scope{
		Logger:  log,
		Config:  config,
		Cluster: cluster,
		Machine: machine,
	}

	if !cluster.Status.ControlPlaneInitialized {
		return r.handleClusterNotInitialized(ctx, scope)
	}

	// Every other case it's a join scenario
	// Nb. in this case ClusterConfiguration and InitConfiguration should not be defined by users, but in case of misconfigurations, CABPK simply ignore them

	// Unlock any locks that might have been set during init process
	r.KubeadmInitLock.Unlock(ctx, cluster)

	// if the JoinConfiguration is missing, create a default one
	if config.Spec.JoinConfiguration == nil {
		log.Info("Creating default JoinConfiguration")
		config.Spec.JoinConfiguration = &kubeadmv1beta1.JoinConfiguration{}
	}

	// it's a control plane join
	if util.IsControlPlaneMachine(machine) {
		return r.joinControlplane(ctx, scope)
	}

	// It's a worker join
	return r.joinWorker(ctx, scope)
}

func (r *KubeadmConfigReconciler) handleClusterNotInitialized(ctx context.Context, scope *Scope) (_ ctrl.Result, rerr error) {
	// if it's NOT a control plane machine, requeue
	if !util.IsControlPlaneMachine(scope.Machine) {
		scope.Info(fmt.Sprintf("Machine is not a control plane. If it should be a control plane, add the label `%s: \"\"` to the Machine", clusterv1.MachineControlPlaneLabelName))
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// if the machine has not ClusterConfiguration and InitConfiguration, requeue
	if scope.Config.Spec.InitConfiguration == nil && scope.Config.Spec.ClusterConfiguration == nil {
		scope.Info("Control plane is not ready, requeing joining control planes until ready.")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// acquire the init lock so that only the first machine configured
	// as control plane get processed here
	// if not the first, requeue
	if !r.KubeadmInitLock.Lock(ctx, scope.Cluster, scope.Machine) {
		scope.Info("A control plane is already being initialized, requeing until control plane is ready")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	defer func() {
		if rerr != nil {
			r.KubeadmInitLock.Unlock(ctx, scope.Cluster)
		}
	}()

	scope.Info("Creating BootstrapData for the init control plane")

	// Nb. in this case JoinConfiguration should not be defined by users, but in case of misconfigurations, CABPK simply ignore it

	// get both of ClusterConfiguration and InitConfiguration strings to pass to the cloud init control plane generator
	// kubeadm allows one of these values to be empty; CABPK replace missing values with an empty config, so the cloud init generation
	// should not handle special cases.

	if scope.Config.Spec.InitConfiguration == nil {
		scope.Config.Spec.InitConfiguration = &kubeadmv1beta1.InitConfiguration{
			TypeMeta: v1.TypeMeta{
				APIVersion: "kubeadm.k8s.io/v1beta1",
				Kind:       "InitConfiguration",
			},
		}
	}
	initdata, err := kubeadmv1beta1.ConfigurationToYAML(scope.Config.Spec.InitConfiguration)
	if err != nil {
		scope.Error(err, "failed to marshal init configuration")
		return ctrl.Result{}, err
	}

	if scope.Config.Spec.ClusterConfiguration == nil {
		scope.Config.Spec.ClusterConfiguration = &kubeadmv1beta1.ClusterConfiguration{
			TypeMeta: v1.TypeMeta{
				APIVersion: "kubeadm.k8s.io/v1beta1",
				Kind:       "ClusterConfiguration",
			},
		}
	}

	// injects into config.ClusterConfiguration values from top level object
	r.reconcileTopLevelObjectSettings(scope.Cluster, scope.Machine, scope.Config)

	clusterdata, err := kubeadmv1beta1.ConfigurationToYAML(scope.Config.Spec.ClusterConfiguration)
	if err != nil {
		scope.Error(err, "failed to marshal cluster configuration")
		return ctrl.Result{}, err
	}

	certificates := internalcluster.NewCertificatesForInitialControlPlane(scope.Config.Spec.ClusterConfiguration)
	if err := certificates.LookupOrGenerate(ctx, r.Client, scope.Cluster, scope.Config); err != nil {
		scope.Error(err, "unable to lookup or create cluster certificates")
		return ctrl.Result{}, err
	}

	additionalFiles, err := r.resolveFiles(ctx, scope.Config)
	if err != nil {
		scope.Error(err, "Failed to resolve files")
		return ctrl.Result{}, err
	}

	cloudInitData, err := cloudinit.NewInitControlPlane(&cloudinit.ControlPlaneInput{
		BaseUserData: cloudinit.BaseUserData{
			Files:               append(certificates.AsFiles(), additionalFiles...),
			NTP:                 scope.Config.Spec.NTP,
			PreKubeadmCommands:  scope.Config.Spec.PreKubeadmCommands,
			PostKubeadmCommands: scope.Config.Spec.PostKubeadmCommands,
			Users:               scope.Config.Spec.Users,
		},
		InitConfiguration:    initdata,
		ClusterConfiguration: clusterdata,
	})
	if err != nil {
		scope.Error(err, "failed to generate cloud init for bootstrap control plane")
		return ctrl.Result{}, err
	}

	scope.Config.Status.BootstrapData = cloudInitData
	scope.Config.Status.Ready = true

	return ctrl.Result{}, nil
}

func (r *KubeadmConfigReconciler) joinWorker(ctx context.Context, scope *Scope) (ctrl.Result, error) {
	certificates := internalcluster.NewCertificatesForWorker(scope.Config.Spec.JoinConfiguration.CACertPath)
	if err := certificates.Lookup(ctx, r.Client, scope.Cluster); err != nil {
		scope.Error(err, "unable to lookup cluster certificates")
		return ctrl.Result{}, err
	}
	if err := certificates.EnsureAllExist(); err != nil {
		scope.Error(err, "Missing certificates")
		return ctrl.Result{}, err
	}

	// ensure that joinConfiguration.Discovery is properly set for joining node on the current cluster
	if err := r.reconcileDiscovery(scope.Cluster, scope.Config, certificates); err != nil {
		if requeueErr, ok := errors.Cause(err).(capierrors.HasRequeueAfterError); ok {
			scope.Info(err.Error())
			return ctrl.Result{RequeueAfter: requeueErr.GetRequeueAfter()}, nil
		}
		return ctrl.Result{}, err
	}

	joinData, err := kubeadmv1beta1.ConfigurationToYAML(scope.Config.Spec.JoinConfiguration)
	if err != nil {
		scope.Error(err, "failed to marshal join configuration")
		return ctrl.Result{}, err
	}

	if scope.Config.Spec.JoinConfiguration.ControlPlane != nil {
		return ctrl.Result{}, errors.New("Machine is a Worker, but JoinConfiguration.ControlPlane is set in the KubeadmConfig object")
	}

	files, err := r.resolveFiles(ctx, scope.Config)
	if err != nil {
		scope.Error(err, "Failed to resolve files")
		return ctrl.Result{}, err
	}

	scope.Info("Creating BootstrapData for the worker node")

	cloudJoinData, err := cloudinit.NewNode(&cloudinit.NodeInput{
		BaseUserData: cloudinit.BaseUserData{
			Files:               files,
			NTP:                 scope.Config.Spec.NTP,
			PreKubeadmCommands:  scope.Config.Spec.PreKubeadmCommands,
			PostKubeadmCommands: scope.Config.Spec.PostKubeadmCommands,
			Users:               scope.Config.Spec.Users,
		},
		JoinConfiguration: joinData,
	})
	if err != nil {
		scope.Error(err, "failed to create a worker join configuration")
		return ctrl.Result{}, err
	}
	scope.Config.Status.BootstrapData = cloudJoinData
	scope.Config.Status.Ready = true
	return ctrl.Result{}, nil
}

func (r *KubeadmConfigReconciler) joinControlplane(ctx context.Context, scope *Scope) (ctrl.Result, error) {
	if scope.Config.Spec.JoinConfiguration.ControlPlane == nil {
		scope.Config.Spec.JoinConfiguration.ControlPlane = &kubeadmv1beta1.JoinControlPlane{}
	}

	certificates := internalcluster.NewCertificatesForJoiningControlPlane()
	if err := certificates.Lookup(ctx, r.Client, scope.Cluster); err != nil {
		scope.Error(err, "unable to lookup cluster certificates")
		return ctrl.Result{}, err
	}
	if err := certificates.EnsureAllExist(); err != nil {
		return ctrl.Result{}, err
	}

	// ensure that joinConfiguration.Discovery is properly set for joining node on the current cluster
	if err := r.reconcileDiscovery(scope.Cluster, scope.Config, certificates); err != nil {
		if requeueErr, ok := errors.Cause(err).(capierrors.HasRequeueAfterError); ok {
			scope.Info(err.Error())
			return ctrl.Result{RequeueAfter: requeueErr.GetRequeueAfter()}, nil
		}
		return ctrl.Result{}, err
	}

	joinData, err := kubeadmv1beta1.ConfigurationToYAML(scope.Config.Spec.JoinConfiguration)
	if err != nil {
		scope.Error(err, "failed to marshal join configuration")
		return ctrl.Result{}, err
	}

	additionalFiles, err := r.resolveFiles(ctx, scope.Config)
	if err != nil {
		scope.Error(err, "Failed to resolve files")
		return ctrl.Result{}, err
	}

	scope.Info("Creating BootstrapData for the join control plane")
	cloudJoinData, err := cloudinit.NewJoinControlPlane(&cloudinit.ControlPlaneJoinInput{
		JoinConfiguration: joinData,
		BaseUserData: cloudinit.BaseUserData{
			Files:               append(certificates.AsFiles(), additionalFiles...),
			NTP:                 scope.Config.Spec.NTP,
			PreKubeadmCommands:  scope.Config.Spec.PreKubeadmCommands,
			PostKubeadmCommands: scope.Config.Spec.PostKubeadmCommands,
			Users:               scope.Config.Spec.Users,
		},
	})
	if err != nil {
		scope.Error(err, "failed to create a control plane join configuration")
		return ctrl.Result{}, err
	}

	scope.Config.Status.BootstrapData = cloudJoinData
	scope.Config.Status.Ready = true
	return ctrl.Result{}, nil
}

// resolveFiles maps .Spec.Files into cloudinit.Files, resolving any object references
// along the way.
func (r *KubeadmConfigReconciler) resolveFiles(ctx context.Context, cfg *bootstrapv1.KubeadmConfig) ([]cloudinit.File, error) {
	var converted []cloudinit.File

	for i, specFile := range cfg.Spec.Files {
		initFile := cloudinit.File{
			Path:        specFile.Path,
			Owner:       specFile.Owner,
			Permissions: specFile.Permissions,
			Encoding:    specFile.Encoding,
		}

		if specFile.Content != "" {
			initFile.Content = specFile.Content
		} else {
			if specFile.ContentFrom == nil {
				return nil, fmt.Errorf("files[%v]: missing content or contentFrom", i)
			}
			content, err := r.resolveFileContentSource(ctx, cfg.Namespace, specFile.ContentFrom)
			if err != nil {
				if err == errOptionalFileContentSourceNotFound {
					continue
				}
				return nil, errors.Wrapf(err, "files[%v]: resolving contentFrom reference", i)
			}
			initFile.Content = content
		}

		converted = append(converted, initFile)
	}

	return converted, nil
}

var errOptionalFileContentSourceNotFound = errors.New("optional file content source not found")

// resolveFileContentSource returns file content fetched from a referenced object.
// If the reference is not found and was marked as optional, errOptionalFileContentSourceNotFound
// is returned.
func (r *KubeadmConfigReconciler) resolveFileContentSource(ctx context.Context, ns string, source *bootstrapv1.FileContentSource) (string, error) {
	if ref := source.ConfigMapKeyRef; ref != nil {
		var cm corev1.ConfigMap
		nn := types.NamespacedName{Namespace: ns, Name: ref.Name}
		if err := r.Client.Get(ctx, nn, &cm); err != nil {
			if apierrors.IsNotFound(err) && ref.Optional != nil && *ref.Optional {
				return "", errOptionalFileContentSourceNotFound
			}
			return "", errors.Wrapf(err, "getting ConfigMap %s", nn)
		}

		val, ok := cm.Data[ref.Key]
		if !ok {
			return "", fmt.Errorf("could not find key %v in ConfigMap %s", ref.Key, nn)
		}

		return val, nil
	} else if ref := source.SecretKeyRef; ref != nil {
		var sec corev1.Secret
		nn := types.NamespacedName{Namespace: ns, Name: ref.Name}
		if err := r.Client.Get(ctx, nn, &sec); err != nil {
			if apierrors.IsNotFound(err) && ref.Optional != nil && *ref.Optional {
				return "", errOptionalFileContentSourceNotFound
			}
			return "", errors.Wrapf(err, "getting Secret %s", nn)
		}

		val, ok := sec.Data[ref.Key]
		if !ok {
			return "", fmt.Errorf("could not find key %v in Secret %s", ref.Key, nn)
		}

		return string(val), nil
	}

	return "", errors.New("no source reference defined")
}

// ClusterToKubeadmConfigs is a handler.ToRequestsFunc to be used to enqeue
// requests for reconciliation of KubeadmConfigs.
func (r *KubeadmConfigReconciler) ClusterToKubeadmConfigs(o handler.MapObject) []ctrl.Request {
	result := []ctrl.Request{}

	c, ok := o.Object.(*clusterv1.Cluster)
	if !ok {
		r.Log.Error(errors.Errorf("expected a Cluster but got a %T", o.Object), "failed to get Machine for Cluster")
		return nil
	}

	selectors := []client.ListOption{
		client.InNamespace(c.Namespace),
		client.MatchingLabels{
			clusterv1.ClusterLabelName: c.Name,
		},
	}

	machineList := &clusterv1.MachineList{}
	if err := r.Client.List(context.Background(), machineList, selectors...); err != nil {
		r.Log.Error(err, "failed to list Machines", "Cluster", c.Name, "Namespace", c.Namespace)
		return nil
	}

	for _, m := range machineList.Items {
		if m.Spec.Bootstrap.ConfigRef != nil &&
			m.Spec.Bootstrap.ConfigRef.GroupVersionKind().GroupKind() == bootstrapv1.GroupVersion.WithKind("KubeadmConfig").GroupKind() {
			name := client.ObjectKey{Namespace: m.Namespace, Name: m.Spec.Bootstrap.ConfigRef.Name}
			result = append(result, ctrl.Request{NamespacedName: name})
		}
	}

	return result
}

// MachineToBootstrapMapFunc is a handler.ToRequestsFunc to be used to enqeue
// request for reconciliation of KubeadmConfig.
func (r *KubeadmConfigReconciler) MachineToBootstrapMapFunc(o handler.MapObject) []ctrl.Request {
	result := []ctrl.Request{}

	m, ok := o.Object.(*clusterv1.Machine)
	if !ok {
		return nil
	}
	if m.Spec.Bootstrap.ConfigRef != nil && m.Spec.Bootstrap.ConfigRef.GroupVersionKind() == bootstrapv1.GroupVersion.WithKind("KubeadmConfig") {
		name := client.ObjectKey{Namespace: m.Namespace, Name: m.Spec.Bootstrap.ConfigRef.Name}
		result = append(result, ctrl.Request{NamespacedName: name})
	}
	return result
}

// reconcileDiscovery ensures that config.JoinConfiguration.Discovery is properly set for the joining node.
// The implementation func respect user provided discovery configurations, but in case some of them are missing, a valid BootstrapToken object
// is automatically injected into config.JoinConfiguration.Discovery.
// This allows to simplify configuration UX, by providing the option to delegate to CABPK the configuration of kubeadm join discovery.
func (r *KubeadmConfigReconciler) reconcileDiscovery(cluster *clusterv1.Cluster, config *bootstrapv1.KubeadmConfig, certificates internalcluster.Certificates) error {
	log := r.Log.WithValues("kubeadmconfig", fmt.Sprintf("%s/%s", config.Namespace, config.Name))

	// if config already contains a file discovery configuration, respect it without further validations
	if config.Spec.JoinConfiguration.Discovery.File != nil {
		return nil
	}

	// otherwise it is necessary to ensure token discovery is properly configured
	if config.Spec.JoinConfiguration.Discovery.BootstrapToken == nil {
		config.Spec.JoinConfiguration.Discovery.BootstrapToken = &kubeadmv1beta1.BootstrapTokenDiscovery{}
	}

	// calculate the ca cert hashes if they are not already set
	if len(config.Spec.JoinConfiguration.Discovery.BootstrapToken.CACertHashes) == 0 {
		hashes, err := certificates.GetByPurpose(secret.ClusterCA).Hashes()
		if err != nil {
			log.Error(err, "Unable to generate Cluster CA certificate hashes")
			return err
		}
		config.Spec.JoinConfiguration.Discovery.BootstrapToken.CACertHashes = hashes
	}

	// if BootstrapToken already contains an APIServerEndpoint, respect it; otherwise inject the APIServerEndpoint endpoint defined in cluster status
	apiServerEndpoint := config.Spec.JoinConfiguration.Discovery.BootstrapToken.APIServerEndpoint
	if apiServerEndpoint == "" {
		if cluster.Spec.ControlPlaneEndpoint.IsZero() {
			return errors.Wrap(&capierrors.RequeueAfterError{RequeueAfter: 10 * time.Second}, "Waiting for Cluster Controller to set Cluster.Spec.ControlPlaneEndpoint")
		}

		apiServerEndpoint = cluster.Spec.ControlPlaneEndpoint.String()
		config.Spec.JoinConfiguration.Discovery.BootstrapToken.APIServerEndpoint = apiServerEndpoint
		log.Info("Altering JoinConfiguration.Discovery.BootstrapToken", "APIServerEndpoint", apiServerEndpoint)
	}

	// if BootstrapToken already contains a token, respect it; otherwise create a new bootstrap token for the node to join
	if config.Spec.JoinConfiguration.Discovery.BootstrapToken.Token == "" {
		remoteClient, err := r.remoteClient(r.Client, cluster, r.scheme)
		if err != nil {
			return err
		}

		token, err := createToken(remoteClient)
		if err != nil {
			return errors.Wrapf(err, "failed to create new bootstrap token")
		}

		config.Spec.JoinConfiguration.Discovery.BootstrapToken.Token = token
		log.Info("Altering JoinConfiguration.Discovery.BootstrapToken", "Token", token)
	}

	// If the BootstrapToken does not contain any CACertHashes then force skip CA Verification
	if len(config.Spec.JoinConfiguration.Discovery.BootstrapToken.CACertHashes) == 0 {
		log.Info("No CAs were provided. Falling back to insecure discover method by skipping CA Cert validation")
		config.Spec.JoinConfiguration.Discovery.BootstrapToken.UnsafeSkipCAVerification = true
	}

	return nil
}

// reconcileTopLevelObjectSettings injects into config.ClusterConfiguration values from top level objects like cluster and machine.
// The implementation func respect user provided config values, but in case some of them are missing, values from top level objects are used.
func (r *KubeadmConfigReconciler) reconcileTopLevelObjectSettings(cluster *clusterv1.Cluster, machine *clusterv1.Machine, config *bootstrapv1.KubeadmConfig) {
	log := r.Log.WithValues("kubeadmconfig", fmt.Sprintf("%s/%s", config.Namespace, config.Name))

	// If there is no ControlPlaneEndpoint defined in ClusterConfiguration but
	// there is a ControlPlaneEndpoint defined at Cluster level (e.g. the load balancer endpoint),
	// then use Cluster's ControlPlaneEndpoint as a control plane endpoint for the Kubernetes cluster.
	if config.Spec.ClusterConfiguration.ControlPlaneEndpoint == "" && !cluster.Spec.ControlPlaneEndpoint.IsZero() {
		config.Spec.ClusterConfiguration.ControlPlaneEndpoint = cluster.Spec.ControlPlaneEndpoint.String()
		log.Info("Altering ClusterConfiguration", "ControlPlaneEndpoint", config.Spec.ClusterConfiguration.ControlPlaneEndpoint)
	}

	// If there are no ClusterName defined in ClusterConfiguration, use Cluster.Name
	if config.Spec.ClusterConfiguration.ClusterName == "" {
		config.Spec.ClusterConfiguration.ClusterName = cluster.Name
		log.Info("Altering ClusterConfiguration", "ClusterName", config.Spec.ClusterConfiguration.ClusterName)
	}

	// If there are no Network settings defined in ClusterConfiguration, use ClusterNetwork settings, if defined
	if cluster.Spec.ClusterNetwork != nil {
		if config.Spec.ClusterConfiguration.Networking.DNSDomain == "" && cluster.Spec.ClusterNetwork.ServiceDomain != "" {
			config.Spec.ClusterConfiguration.Networking.DNSDomain = cluster.Spec.ClusterNetwork.ServiceDomain
			log.Info("Altering ClusterConfiguration", "DNSDomain", config.Spec.ClusterConfiguration.Networking.DNSDomain)
		}
		if config.Spec.ClusterConfiguration.Networking.ServiceSubnet == "" &&
			cluster.Spec.ClusterNetwork.Services != nil &&
			len(cluster.Spec.ClusterNetwork.Services.CIDRBlocks) > 0 {
			config.Spec.ClusterConfiguration.Networking.ServiceSubnet = strings.Join(cluster.Spec.ClusterNetwork.Services.CIDRBlocks, "")
			log.Info("Altering ClusterConfiguration", "ServiceSubnet", config.Spec.ClusterConfiguration.Networking.ServiceSubnet)
		}
		if config.Spec.ClusterConfiguration.Networking.PodSubnet == "" &&
			cluster.Spec.ClusterNetwork.Pods != nil &&
			len(cluster.Spec.ClusterNetwork.Pods.CIDRBlocks) > 0 {
			config.Spec.ClusterConfiguration.Networking.PodSubnet = strings.Join(cluster.Spec.ClusterNetwork.Pods.CIDRBlocks, "")
			log.Info("Altering ClusterConfiguration", "PodSubnet", config.Spec.ClusterConfiguration.Networking.PodSubnet)
		}
	}

	// If there are no KubernetesVersion settings defined in ClusterConfiguration, use Version from machine, if defined
	if config.Spec.ClusterConfiguration.KubernetesVersion == "" && machine.Spec.Version != nil {
		config.Spec.ClusterConfiguration.KubernetesVersion = *machine.Spec.Version
		log.Info("Altering ClusterConfiguration", "KubernetesVersion", config.Spec.ClusterConfiguration.KubernetesVersion)
	}
}
