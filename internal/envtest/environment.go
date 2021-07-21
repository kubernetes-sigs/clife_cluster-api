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

package envtest

import (
	"context"
	"fmt"
	"net"
	"path"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"sync"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/pkg/errors"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	bootstrapv1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1alpha4"
	"sigs.k8s.io/cluster-api/cmd/clusterctl/log"
	"sigs.k8s.io/cluster-api/controllers/external"
	kcpv1 "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1alpha4"
	addonv1 "sigs.k8s.io/cluster-api/exp/addons/api/v1alpha4"
	expv1 "sigs.k8s.io/cluster-api/exp/api/v1alpha4"
	operatorv1 "sigs.k8s.io/cluster-api/exp/operator/api/v1alpha1"
	"sigs.k8s.io/cluster-api/util/kubeconfig"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

func init() {
	klog.InitFlags(nil)
	logger := klogr.New()
	// Use klog as the internal logger for this envtest environment.
	log.SetLogger(logger)
	// Additionally force all of the controllers to use the Ginkgo logger.
	ctrl.SetLogger(logger)
	// Add logger for ginkgo.
	klog.SetOutput(ginkgo.GinkgoWriter)

	// Calculate the scheme.
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme.Scheme))
	utilruntime.Must(clusterv1.AddToScheme(scheme.Scheme))
	utilruntime.Must(bootstrapv1.AddToScheme(scheme.Scheme))
	utilruntime.Must(expv1.AddToScheme(scheme.Scheme))
	utilruntime.Must(addonv1.AddToScheme(scheme.Scheme))
	utilruntime.Must(kcpv1.AddToScheme(scheme.Scheme))
	utilruntime.Must(admissionv1.AddToScheme(scheme.Scheme))
	utilruntime.Must(operatorv1.AddToScheme(scheme.Scheme))
}

var (
	cacheSyncBackoff = wait.Backoff{
		Duration: 100 * time.Millisecond,
		Factor:   1.5,
		Steps:    8,
		Jitter:   0.4,
	}

	errAlreadyStarted = errors.New("environment has already been started")
	errAlreadyStopped = errors.New("environment has already been stopped")
)

// Environment encapsulates a Kubernetes local test environment.
type Environment struct {
	manager.Manager
	client.Client
	Config *rest.Config

	env           *envtest.Environment
	startOnce     sync.Once
	stopOnce      sync.Once
	cancelManager context.CancelFunc
}

// New creates a new environment spinning up a local api-server.
//
// This function should be called only once for each package you're running tests within,
// usually the environment is initialized in a suite_test.go file within a `BeforeSuite` ginkgo block.
func New(uncachedObjs ...client.Object) *Environment {
	// Get the root of the current file to use in CRD paths.
	_, filename, _, _ := goruntime.Caller(0) //nolint
	root := path.Join(path.Dir(filename), "..", "..")

	// Create the test environment.
	env := &envtest.Environment{
		ErrorIfCRDPathMissing: true,
		CRDDirectoryPaths: []string{
			filepath.Join(root, "config", "crd", "bases"),
			filepath.Join(root, "controlplane", "kubeadm", "config", "crd", "bases"),
			filepath.Join(root, "bootstrap", "kubeadm", "config", "crd", "bases"),
			filepath.Join(root, "exp", "operator", "config", "crd", "bases"),
		},
		CRDs: []client.Object{
			external.TestGenericBootstrapCRD.DeepCopy(),
			external.TestGenericBootstrapTemplateCRD.DeepCopy(),
			external.TestGenericInfrastructureCRD.DeepCopy(),
			external.TestGenericInfrastructureTemplateCRD.DeepCopy(),
			external.TestGenericInfrastructureRemediationCRD.DeepCopy(),
			external.TestGenericInfrastructureRemediationTemplateCRD.DeepCopy(),
		},
		// initialize webhook here to be able to test the envtest install via webhookOptions
		// This should set LocalServingCertDir and LocalServingPort that are used below.
		WebhookInstallOptions: initWebhookInstallOptions(),
	}

	if _, err := env.Start(); err != nil {
		err = kerrors.NewAggregate([]error{err, env.Stop()})
		panic(err)
	}

	objs := []client.Object{}
	if len(uncachedObjs) > 0 {
		objs = append(objs, uncachedObjs...)
	}

	options := manager.Options{
		Scheme:                scheme.Scheme,
		MetricsBindAddress:    "0",
		CertDir:               env.WebhookInstallOptions.LocalServingCertDir,
		Port:                  env.WebhookInstallOptions.LocalServingPort,
		ClientDisableCacheFor: objs,
	}

	mgr, err := ctrl.NewManager(env.Config, options)
	if err != nil {
		klog.Fatalf("Failed to start testenv manager: %v", err)
	}

	// Set minNodeStartupTimeout for Test, so it does not need to be at least 30s
	clusterv1.SetMinNodeStartupTimeout(metav1.Duration{Duration: 1 * time.Millisecond})

	if err := (&clusterv1.Cluster{}).SetupWebhookWithManager(mgr); err != nil {
		klog.Fatalf("unable to create webhook: %+v", err)
	}
	if err := (&clusterv1.Machine{}).SetupWebhookWithManager(mgr); err != nil {
		klog.Fatalf("unable to create webhook: %+v", err)
	}
	if err := (&clusterv1.MachineHealthCheck{}).SetupWebhookWithManager(mgr); err != nil {
		klog.Fatalf("unable to create webhook: %+v", err)
	}
	if err := (&clusterv1.Machine{}).SetupWebhookWithManager(mgr); err != nil {
		klog.Fatalf("unable to create webhook: %+v", err)
	}
	if err := (&clusterv1.MachineSet{}).SetupWebhookWithManager(mgr); err != nil {
		klog.Fatalf("unable to create webhook: %+v", err)
	}
	if err := (&clusterv1.MachineDeployment{}).SetupWebhookWithManager(mgr); err != nil {
		klog.Fatalf("unable to create webhook: %+v", err)
	}
	if err := (&bootstrapv1.KubeadmConfig{}).SetupWebhookWithManager(mgr); err != nil {
		klog.Fatalf("unable to create webhook: %+v", err)
	}
	if err := (&bootstrapv1.KubeadmConfigTemplate{}).SetupWebhookWithManager(mgr); err != nil {
		klog.Fatalf("unable to create webhook: %+v", err)
	}
	if err := (&bootstrapv1.KubeadmConfigTemplateList{}).SetupWebhookWithManager(mgr); err != nil {
		klog.Fatalf("unable to create webhook: %+v", err)
	}
	if err := (&kcpv1.KubeadmControlPlane{}).SetupWebhookWithManager(mgr); err != nil {
		klog.Fatalf("unable to create webhook: %+v", err)
	}
	if err := (&addonv1.ClusterResourceSet{}).SetupWebhookWithManager(mgr); err != nil {
		klog.Fatalf("unable to create webhook for crs: %+v", err)
	}
	if err := (&expv1.MachinePool{}).SetupWebhookWithManager(mgr); err != nil {
		klog.Fatalf("unable to create webhook for machinepool: %+v", err)
	}

	return &Environment{
		Manager: mgr,
		Client:  mgr.GetClient(),
		Config:  mgr.GetConfig(),
		env:     env,
	}
}

// Start starts the manager.
func (e *Environment) Start(ctx context.Context) error {
	err := errAlreadyStarted
	e.startOnce.Do(func() {
		ctx, cancel := context.WithCancel(ctx)
		e.cancelManager = cancel
		err = e.Manager.Start(ctx)
	})
	return err
}

// Stop stops the test environment.
func (e *Environment) Stop() error {
	err := errAlreadyStopped
	e.stopOnce.Do(func() {
		e.cancelManager()
		err = e.env.Stop()
	})
	return err
}

// WaitForWebhooks waits for the webhook server to be available.
func (e *Environment) WaitForWebhooks() {
	port := e.env.WebhookInstallOptions.LocalServingPort

	klog.V(2).Infof("Waiting for webhook port %d to be open prior to running tests", port)
	timeout := 1 * time.Second
	for {
		time.Sleep(1 * time.Second)
		conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), timeout)
		if err != nil {
			klog.V(2).Infof("Webhook port is not ready, will retry in %v: %s", timeout, err)
			continue
		}
		if err := conn.Close(); err != nil {
			klog.V(2).Infof("Closing connection when testing if webhook port is ready failed: %v", err)
		}
		klog.V(2).Info("Webhook port is now open. Continuing with tests...")
		return
	}
}

// CreateKubeconfigSecret generates a new Kubeconfig secret from the envtest config.
func (e *Environment) CreateKubeconfigSecret(ctx context.Context, cluster *clusterv1.Cluster) error {
	return e.Create(ctx, kubeconfig.GenerateSecret(cluster, kubeconfig.FromEnvTestConfig(e.Config, cluster)))
}

// Cleanup deletes all the given objects.
func (e *Environment) Cleanup(ctx context.Context, objs ...client.Object) error {
	errs := []error{}
	for _, o := range objs {
		err := e.Client.Delete(ctx, o)
		if apierrors.IsNotFound(err) {
			continue
		}
		errs = append(errs, err)
	}
	return kerrors.NewAggregate(errs)
}

// CleanupAndWait deletes all the given objects and waits for the cache to be updated accordingly.
//
// NOTE: Waiting for the cache to be updated helps in preventing test flakes due to the cache sync delays.
func (e *Environment) CleanupAndWait(ctx context.Context, objs ...client.Object) error {
	if err := e.Cleanup(ctx, objs...); err != nil {
		return err
	}

	// Makes sure the cache is updated with the deleted object
	errs := []error{}
	for _, o := range objs {
		// Ignoring namespaces because in testenv the namespace cleaner is not running.
		if o.GetObjectKind().GroupVersionKind().GroupKind() == corev1.SchemeGroupVersion.WithKind("Namespace").GroupKind() {
			continue
		}

		oCopy := o.DeepCopyObject().(client.Object)
		key := client.ObjectKeyFromObject(o)
		err := wait.ExponentialBackoff(
			cacheSyncBackoff,
			func() (done bool, err error) {
				if err := e.Get(ctx, key, oCopy); err != nil {
					if apierrors.IsNotFound(err) {
						return true, nil
					}
					return false, err
				}
				return false, nil
			})
		errs = append(errs, errors.Wrapf(err, "key %s, %s is not being deleted from the testenv client cache", o.GetObjectKind().GroupVersionKind().String(), key))
	}
	return kerrors.NewAggregate(errs)
}

// CreateAndWait creates the given object and waits for the cache to be updated accordingly.
//
// NOTE: Waiting for the cache to be updated helps in preventing test flakes due to the cache sync delays.
func (e *Environment) CreateAndWait(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if err := e.Client.Create(ctx, obj, opts...); err != nil {
		return err
	}

	// Makes sure the cache is updated with the new object
	objCopy := obj.DeepCopyObject().(client.Object)
	key := client.ObjectKeyFromObject(obj)
	if err := wait.ExponentialBackoff(
		cacheSyncBackoff,
		func() (done bool, err error) {
			if err := e.Get(ctx, key, objCopy); err != nil {
				if apierrors.IsNotFound(err) {
					return false, nil
				}
				return false, err
			}
			return true, nil
		}); err != nil {
		return errors.Wrapf(err, "object %s, %s is not being added to the testenv client cache", obj.GetObjectKind().GroupVersionKind().String(), key)
	}
	return nil
}

// CreateNamespace creates a new namespace with a generated name.
func (e *Environment) CreateNamespace(ctx context.Context, generateName string) (*corev1.Namespace, error) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-", generateName),
			Labels: map[string]string{
				"testenv/original-name": generateName,
			},
		},
	}
	if err := e.Client.Create(ctx, ns); err != nil {
		return nil, err
	}

	return ns, nil
}
