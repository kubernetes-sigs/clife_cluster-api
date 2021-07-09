/*
Copyright 2021 The Kubernetes Authors.

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
	"reflect"
	"strings"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"sigs.k8s.io/cluster-api/api/v1alpha4"
	"sigs.k8s.io/cluster-api/cmd/clusterctl/client/cluster"
	configclient "sigs.k8s.io/cluster-api/cmd/clusterctl/client/config"
	"sigs.k8s.io/cluster-api/cmd/clusterctl/client/repository"
	"sigs.k8s.io/cluster-api/cmd/clusterctl/client/yamlprocessor"
	clusterctllog "sigs.k8s.io/cluster-api/cmd/clusterctl/log"
	operatorv1 "sigs.k8s.io/cluster-api/exp/operator/api/v1alpha1"
	"sigs.k8s.io/cluster-api/exp/operator/controllers/genericprovider"
	"sigs.k8s.io/cluster-api/exp/operator/util"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"
)

type GenericProviderReconciler struct {
	Provider     client.Object
	ProviderList client.ObjectList
	Client       client.Client
}

func (r *GenericProviderReconciler) SetupWithManager(mgr ctrl.Manager, options controller.Options) error {
	clusterctllog.SetLogger(mgr.GetLogger())
	return ctrl.NewControllerManagedBy(mgr).
		For(r.Provider).
		WithOptions(options).
		Complete(r)
}

func (r *GenericProviderReconciler) Reconcile(ctx context.Context, req reconcile.Request) (_ reconcile.Result, reterr error) {
	typedProvider, err := r.NewGenericProvider()
	if err != nil {
		return ctrl.Result{}, err
	}

	typedProviderList, err := r.NewGenericProviderList()
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := r.Client.Get(ctx, req.NamespacedName, typedProvider.GetObject()); err != nil {
		if apierrors.IsNotFound(err) {
			// Object not found, return. Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	// Initialize the patch helper
	patchHelper, err := patch.NewHelper(typedProvider.GetObject(), r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	defer func() {
		// Always attempt to patch the object and status after each reconciliation.
		if err := patchHelper.Patch(ctx, typedProvider.GetObject()); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	// Ignore deleted provider, this can happen when foregroundDeletion
	// is enabled
	// Cleanup logic is not needed because owner references set on resource created by
	// Provider will cause GC to do the cleanup for us.
	if !typedProvider.GetDeletionTimestamp().IsZero() {
		return ctrl.Result{}, nil
	}

	return r.reconcile(ctx, typedProvider, typedProviderList)
}

func (r *GenericProviderReconciler) secretReader(ctx context.Context, provider genericprovider.GenericProvider) (configclient.Reader, error) {
	mr := configclient.NewMemoryReader()

	// TODO: maybe set a shorter default, so we don't block when installing cert-manager.
	//mr.Set("cert-manager-timeout", "120s")

	if provider.GetSpec().SecretName != nil {
		secret := &corev1.Secret{}
		key := types.NamespacedName{Namespace: provider.GetNamespace(), Name: *provider.GetSpec().SecretName}
		if err := r.Client.Get(ctx, key, secret); err != nil {
			return nil, err
		}
		for k, v := range secret.Data {
			mr.Set(k, string(v))
		}
	} else {
		klog.Info("no configuration secret was specified")
	}

	if provider.GetSpec().FetchConfig != nil && provider.GetSpec().FetchConfig.URL != nil {
		mr.WithProvider(provider.GetName(), util.ClusterctlProviderType(provider), *provider.GetSpec().FetchConfig.URL)
	}

	return mr, nil
}

func (r *GenericProviderReconciler) configmapRepository(ctx context.Context, provider genericprovider.GenericProvider) (repository.Repository, error) {
	mr := repository.NewMemoryRepository()

	cml := &corev1.ConfigMapList{}
	selector, err := metav1.LabelSelectorAsSelector(provider.GetSpec().FetchConfig.Selector)
	if err != nil {
		return nil, err
	}
	err = r.Client.List(ctx, cml, &client.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, err
	}
	if len(cml.Items) == 0 {
		return nil, fmt.Errorf("no ConfigMaps found with selector %s", provider.GetSpec().FetchConfig.Selector.String())
	}
	for _, cm := range cml.Items {
		metadata, ok := cm.Data["metadata"]
		if !ok {
			return nil, fmt.Errorf("ConfigMap %s/%s has no metadata", cm.Namespace, cm.Name)
		}
		mr.WithFile(cm.Name, "metadata.yaml", []byte(metadata))
		components, ok := cm.Data["components"]
		if !ok {
			return nil, fmt.Errorf("ConfigMap %s/%s has no components", cm.Namespace, cm.Name)
		}
		mr.WithFile(cm.Name, "components.yaml", []byte(components))
		mr.WithPaths("", "components.yaml")
		// the ConfigMaps should be ordered by Name (version), so we should end up
		// with the highest version as the default.
		mr.WithDefaultVersion(cm.Name)
	}

	return mr, nil
}

func (r *GenericProviderReconciler) reconcile(ctx context.Context, provider genericprovider.GenericProvider, genericProviderList genericprovider.GenericProviderList) (_ ctrl.Result, reterr error) {
	// Run preflight checks to ensure that core provider can be installed properly
	result, err := preflightChecks(ctx, r.Client, provider, genericProviderList)
	if err != nil || !result.IsZero() {
		return result, err
	}

	reader, err := r.secretReader(ctx, provider)
	if err != nil {
		return ctrl.Result{}, err
	}

	cfg, err := configclient.New("", configclient.InjectReader(reader))
	if err != nil {
		return ctrl.Result{}, err
	}

	providerConfig, err := cfg.Providers().Get(provider.GetName(), util.ClusterctlProviderType(provider))
	if err != nil {
		conditions.Set(provider, conditions.FalseCondition(
			operatorv1.PreflightCheckCondition,
			operatorv1.UnknownProviderReason,
			v1alpha4.ConditionSeverityWarning,
			fmt.Sprintf(unknownProviderMessage, provider.GetName()),
		))
		return ctrl.Result{}, nil
	}

	spec := provider.GetSpec()

	var repo repository.Repository
	if spec.FetchConfig != nil && spec.FetchConfig.Selector != nil {
		repo, err = r.configmapRepository(ctx, provider)
	} else {
		repo, err = repository.NewGitHubRepository(providerConfig, cfg.Variables())
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	options := repository.ComponentsOptions{
		TargetNamespace:   provider.GetNamespace(),
		WatchingNamespace: "",
		SkipVariables:     false,
		Version:           repo.DefaultVersion(),
	}
	if spec.Version != nil {
		options.Version = *spec.Version
	}

	componentsFile, err := repo.GetFile(options.Version, repo.ComponentsPath())
	if err != nil {
		return ctrl.Result{}, errors.Wrapf(err, "failed to read %q from provider's repository %q", repo.ComponentsPath(), providerConfig.ManifestLabel())
	}

	components, err := repository.NewComponents(repository.ComponentsInput{
		Provider:            providerConfig,
		ConfigClient:        cfg,
		Processor:           yamlprocessor.NewSimpleProcessor(),
		RawYaml:             componentsFile,
		InstanceObjModifier: customizeInstanceObjectsFn(provider),
		SharedObjModifier:   customizeSharedObjectsFn(provider),
		Options:             options})
	if err != nil {
		conditions.Set(provider, conditions.FalseCondition(
			v1alpha4.ReadyCondition,
			operatorv1.ComponentsGatherErrorReason,
			v1alpha4.ConditionSeverityWarning,
			err.Error(),
		))

		return ctrl.Result{}, err
	}

	clusterClient := cluster.New(cluster.Kubeconfig{}, cfg)
	installer := clusterClient.ProviderInstaller()
	installer.Add(components)

	// ensure the custom resource definitions required by clusterctl are in place
	if err := clusterClient.ProviderInventory().EnsureCustomResourceDefinitions(); err != nil {
		return ctrl.Result{}, err
	}

	if isCertManagerRequired(components) {
		// Before installing the providers, ensure the cert-manager Webhook is in place.
		certManager, err := clusterClient.CertManager()
		if err != nil {
			return ctrl.Result{}, err
		}

		// NOTE: this can block for a while..
		if err := certManager.EnsureInstalled(); err != nil {
			return ctrl.Result{}, err
		}
	}

	_, err = installer.Install()
	if err != nil {
		conditions.Set(provider, conditions.FalseCondition(
			v1alpha4.ReadyCondition,
			"Install failed",
			v1alpha4.ConditionSeverityError,
			err.Error(),
		))
		return ctrl.Result{}, err
	}

	conditions.Set(provider, conditions.TrueCondition(v1alpha4.ReadyCondition))
	return ctrl.Result{}, nil
}

func isCertManagerRequired(components repository.Components) bool {
	for _, obj := range components.InstanceObjs() {
		if strings.Contains(obj.GetAPIVersion(), "cert-manager.io/") {
			klog.V(4).Infof("cert-manager is required by %s %s/%s", obj.GetKind(), obj.GetNamespace(), obj.GetName())
			return true
		}
	}
	for _, obj := range components.SharedObjs() {
		if strings.Contains(obj.GetAPIVersion(), "cert-manager.io/") {
			klog.V(4).Infof("cert-manager is required by %s %s/%s", obj.GetKind(), obj.GetNamespace(), obj.GetName())
			return true
		}
	}
	klog.V(4).Info("cert-manager not required")
	return false
}

func (r *GenericProviderReconciler) NewGenericProvider() (genericprovider.GenericProvider, error) {
	switch r.Provider.(type) {
	case *operatorv1.CoreProvider:
		return &genericprovider.CoreProviderWrapper{CoreProvider: &operatorv1.CoreProvider{}}, nil
	case *operatorv1.BootstrapProvider:
		return &genericprovider.BootstrapProviderWrapper{BootstrapProvider: &operatorv1.BootstrapProvider{}}, nil
	case *operatorv1.ControlPlaneProvider:
		return &genericprovider.ControlPlaneProviderWrapper{ControlPlaneProvider: &operatorv1.ControlPlaneProvider{}}, nil
	case *operatorv1.InfrastructureProvider:
		return &genericprovider.InfrastructureProviderWrapper{InfrastructureProvider: &operatorv1.InfrastructureProvider{}}, nil
	default:
		providerKind := reflect.Indirect(reflect.ValueOf(r.Provider)).Type().Name()
		failedToCastInterfaceErr := fmt.Errorf("failed to cast interface for type: %s", providerKind)
		return nil, failedToCastInterfaceErr
	}
}

func (r *GenericProviderReconciler) NewGenericProviderList() (genericprovider.GenericProviderList, error) {
	switch r.ProviderList.(type) {
	case *operatorv1.CoreProviderList:
		return &genericprovider.CoreProviderListWrapper{CoreProviderList: &operatorv1.CoreProviderList{}}, nil
	case *operatorv1.BootstrapProviderList:
		return &genericprovider.BootstrapProviderListWrapper{BootstrapProviderList: &operatorv1.BootstrapProviderList{}}, nil
	case *operatorv1.ControlPlaneProviderList:
		return &genericprovider.ControlPlaneProviderListWrapper{ControlPlaneProviderList: &operatorv1.ControlPlaneProviderList{}}, nil
	case *operatorv1.InfrastructureProviderList:
		return &genericprovider.InfrastructureProviderListWrapper{InfrastructureProviderList: &operatorv1.InfrastructureProviderList{}}, nil
	default:
		providerKind := reflect.Indirect(reflect.ValueOf(r.ProviderList)).Type().Name()
		failedToCastInterfaceErr := fmt.Errorf("failed to cast interface for type: %s", providerKind)
		return nil, failedToCastInterfaceErr
	}
}
