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

package main

import (
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	operatorv1 "sigs.k8s.io/cluster-api/exp/operator/api/v1alpha1"
	"sigs.k8s.io/cluster-api/exp/operator/controllers"
	"sigs.k8s.io/cluster-api/feature"
	"sigs.k8s.io/cluster-api/version"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")

	// flags.
	metricsBindAddr             string
	enableLeaderElection        bool
	leaderElectionLeaseDuration time.Duration
	leaderElectionRenewDeadline time.Duration
	leaderElectionRetryPeriod   time.Duration
	watchFilterValue            string
	profilerAddress             string
	concurrencyNumber           int
	syncPeriod                  time.Duration
	webhookPort                 int
	webhookCertDir              string
	healthAddr                  string
)

func init() {
	klog.InitFlags(nil)

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(operatorv1.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

// InitFlags initializes the flags.
func InitFlags(fs *pflag.FlagSet) {
	fs.StringVar(&metricsBindAddr, "metrics-bind-addr", ":8080",
		"The address the metric endpoint binds to.")

	fs.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")

	fs.DurationVar(&leaderElectionLeaseDuration, "leader-elect-lease-duration", 15*time.Second,
		"Interval at which non-leader candidates will wait to force acquire leadership (duration string)")

	fs.DurationVar(&leaderElectionRenewDeadline, "leader-elect-renew-deadline", 10*time.Second,
		"Duration that the leading controller manager will retry refreshing leadership before giving up (duration string)")

	fs.DurationVar(&leaderElectionRetryPeriod, "leader-elect-retry-period", 2*time.Second,
		"Duration the LeaderElector clients should wait between tries of actions (duration string)")

	fs.StringVar(&watchFilterValue, "watch-filter", "",
		fmt.Sprintf("Label value that the controller watches to reconcile cluster-api objects. Label key is always %s. If unspecified, the controller watches for all cluster-api objects.", clusterv1.WatchLabel))

	fs.StringVar(&profilerAddress, "profiler-address", "",
		"Bind address to expose the pprof profiler (e.g. localhost:6060)")

	fs.IntVar(&concurrencyNumber, "concurrency", 10,
		"Number of core resources to process simultaneously")

	fs.DurationVar(&syncPeriod, "sync-period", 10*time.Minute,
		"The minimum interval at which watched resources are reconciled (e.g. 15m)")

	fs.IntVar(&webhookPort, "webhook-port", 9443,
		"Webhook Server port")

	fs.StringVar(&webhookCertDir, "webhook-cert-dir", "/tmp/k8s-webhook-server/serving-certs/",
		"Webhook cert dir, only used when webhook-port is specified.")

	fs.StringVar(&healthAddr, "health-addr", ":9440",
		"The address the health endpoint binds to.")

	feature.MutableGates.AddFlag(fs)
}

func main() {
	rand.Seed(time.Now().UnixNano())

	InitFlags(pflag.CommandLine)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()

	ctrl.SetLogger(klogr.New())

	if profilerAddress != "" {
		klog.Infof("Profiler listening for requests at %s", profilerAddress)
		go func() {
			klog.Info(http.ListenAndServe(profilerAddress, nil))
		}()
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsBindAddr,
		LeaderElection:     enableLeaderElection,
		LeaderElectionID:   "controller-leader-election-capi-operator",
		LeaseDuration:      &leaderElectionLeaseDuration,
		RenewDeadline:      &leaderElectionRenewDeadline,
		RetryPeriod:        &leaderElectionRetryPeriod,
		SyncPeriod:         &syncPeriod,
		ClientDisableCacheFor: []client.Object{
			&corev1.ConfigMap{},
			&corev1.Secret{},
		},
		Port:                   webhookPort,
		CertDir:                webhookCertDir,
		HealthProbeBindAddress: healthAddr,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Setup the context that's going to be used in controllers and for the manager.
	ctx := ctrl.SetupSignalHandler()

	setupChecks(mgr)
	setupReconcilers(mgr)

	// +kubebuilder:scaffold:builder
	setupLog.Info("starting manager", "version", version.Get().String())
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func setupChecks(mgr ctrl.Manager) {
	if err := mgr.AddReadyzCheck("ping", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to create ready check")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("ping", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to create health check")
		os.Exit(1)
	}
}

func setupReconcilers(mgr ctrl.Manager) {
	if err := (&controllers.GenericProviderReconciler{
		Provider:     &operatorv1.CoreProvider{},
		ProviderList: &operatorv1.CoreProviderList{},
		Client:       mgr.GetClient(),
	}).SetupWithManager(mgr, concurrency(concurrencyNumber)); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "CoreProvider")
		os.Exit(1)
	}

	if err := (&controllers.GenericProviderReconciler{
		Provider:     &operatorv1.InfrastructureProvider{},
		ProviderList: &operatorv1.InfrastructureProviderList{},
		Client:       mgr.GetClient(),
	}).SetupWithManager(mgr, concurrency(concurrencyNumber)); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "InfrastructureProvider")
		os.Exit(1)
	}

	if err := (&controllers.GenericProviderReconciler{
		Provider:     &operatorv1.BootstrapProvider{},
		ProviderList: &operatorv1.BootstrapProviderList{},
		Client:       mgr.GetClient(),
	}).SetupWithManager(mgr, concurrency(concurrencyNumber)); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "BootstrapProvider")
		os.Exit(1)
	}

	if err := (&controllers.GenericProviderReconciler{
		Provider:     &operatorv1.ControlPlaneProvider{},
		ProviderList: &operatorv1.ControlPlaneProviderList{},
		Client:       mgr.GetClient(),
	}).SetupWithManager(mgr, concurrency(concurrencyNumber)); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ControlPlaneProvider")
		os.Exit(1)
	}
}

func concurrency(c int) controller.Options {
	return controller.Options{MaxConcurrentReconciles: c}
}
