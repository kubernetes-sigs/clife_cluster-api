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
package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	_ "net/http/pprof"
	"os"
	"time"

	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"
	clusterv1old "sigs.k8s.io/cluster-api/api/v1alpha3"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	"sigs.k8s.io/cluster-api/controllers"
	"sigs.k8s.io/cluster-api/controllers/noderefutil"
	"sigs.k8s.io/cluster-api/controllers/remote"
	addonsv1old "sigs.k8s.io/cluster-api/exp/addons/api/v1alpha3"
	addonsv1 "sigs.k8s.io/cluster-api/exp/addons/api/v1alpha4"
	addonscontrollers "sigs.k8s.io/cluster-api/exp/addons/controllers"
	expv1old "sigs.k8s.io/cluster-api/exp/api/v1alpha3"
	expv1 "sigs.k8s.io/cluster-api/exp/api/v1alpha4"
	expcontrollers "sigs.k8s.io/cluster-api/exp/controllers"
	"sigs.k8s.io/cluster-api/feature"
	"sigs.k8s.io/cluster-api/util/fieldowner"
	"sigs.k8s.io/cluster-api/version"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")

	// flags.
	metricsBindAddr               string
	enableLeaderElection          bool
	leaderElectionLeaseDuration   time.Duration
	leaderElectionRenewDeadline   time.Duration
	leaderElectionRetryPeriod     time.Duration
	watchNamespace                string
	watchFilterValue              string
	profilerAddress               string
	clusterConcurrency            int
	machineConcurrency            int
	machineSetConcurrency         int
	machineDeploymentConcurrency  int
	machinePoolConcurrency        int
	clusterResourceSetConcurrency int
	machineHealthCheckConcurrency int
	syncPeriod                    time.Duration
	webhookPort                   int
	webhookCertDir                string
	healthAddr                    string
)

func init() {
	klog.InitFlags(nil)

	_ = clientgoscheme.AddToScheme(scheme)
	_ = clusterv1old.AddToScheme(scheme)
	_ = clusterv1.AddToScheme(scheme)
	_ = expv1old.AddToScheme(scheme)
	_ = expv1.AddToScheme(scheme)
	_ = addonsv1old.AddToScheme(scheme)
	_ = addonsv1.AddToScheme(scheme)
	_ = apiextensionsv1.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme
}

// InitFlags initializes the flags.
func InitFlags(fs *pflag.FlagSet) {
	fs.StringVar(&metricsBindAddr, "metrics-bind-addr", "localhost:8080",
		"The address the metric endpoint binds to.")

	fs.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")

	fs.DurationVar(&leaderElectionLeaseDuration, "leader-elect-lease-duration", 15*time.Second,
		"Interval at which non-leader candidates will wait to force acquire leadership (duration string)")

	fs.DurationVar(&leaderElectionRenewDeadline, "leader-elect-renew-deadline", 10*time.Second,
		"Duration that the leading controller manager will retry refreshing leadership before giving up (duration string)")

	fs.DurationVar(&leaderElectionRetryPeriod, "leader-elect-retry-period", 2*time.Second,
		"Duration the LeaderElector clients should wait between tries of actions (duration string)")

	fs.StringVar(&watchNamespace, "namespace", "",
		"Namespace that the controller watches to reconcile cluster-api objects. If unspecified, the controller watches for cluster-api objects across all namespaces.")

	fs.StringVar(&watchFilterValue, "watch-filter", "",
		fmt.Sprintf("Label value that the controller watches to reconcile cluster-api objects. Label key is always %s. If unspecified, the controller watches for all cluster-api objects.", clusterv1.WatchLabel))

	fs.StringVar(&profilerAddress, "profiler-address", "",
		"Bind address to expose the pprof profiler (e.g. localhost:6060)")

	fs.IntVar(&clusterConcurrency, "cluster-concurrency", 10,
		"Number of clusters to process simultaneously")

	fs.IntVar(&machineConcurrency, "machine-concurrency", 10,
		"Number of machines to process simultaneously")

	fs.IntVar(&machineSetConcurrency, "machineset-concurrency", 10,
		"Number of machine sets to process simultaneously")

	fs.IntVar(&machineDeploymentConcurrency, "machinedeployment-concurrency", 10,
		"Number of machine deployments to process simultaneously")

	fs.IntVar(&machinePoolConcurrency, "machinepool-concurrency", 10,
		"Number of machine pools to process simultaneously")

	fs.IntVar(&clusterResourceSetConcurrency, "clusterresourceset-concurrency", 10,
		"Number of cluster resource sets to process simultaneously")

	fs.IntVar(&machineHealthCheckConcurrency, "machinehealthcheck-concurrency", 10,
		"Number of machine health checks to process simultaneously")

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
	pflag.CommandLine.SetNormalizeFunc(cliflag.WordSepNormalizeFunc)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()

	ctrl.SetLogger(klogr.New())

	if profilerAddress != "" {
		klog.Infof("Profiler listening for requests at %s", profilerAddress)
		go func() {
			klog.Info(http.ListenAndServe(profilerAddress, nil))
		}()
	}

	restConfig := ctrl.GetConfigOrDie()
	restConfig.UserAgent = remote.DefaultClusterAPIUserAgent("cluster-api-controller-manager")
	mgr, err := ctrl.NewManager(restConfig, ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsBindAddr,
		LeaderElection:     enableLeaderElection,
		LeaderElectionID:   "controller-leader-election-capi",
		LeaseDuration:      &leaderElectionLeaseDuration,
		RenewDeadline:      &leaderElectionRenewDeadline,
		RetryPeriod:        &leaderElectionRetryPeriod,
		Namespace:          watchNamespace,
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
	setupIndexes(ctx, mgr)
	setupReconcilers(ctx, mgr)
	setupWebhooks(mgr)

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

func setupIndexes(ctx context.Context, mgr ctrl.Manager) {
	if err := noderefutil.AddMachineNodeIndex(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to setup index")
		os.Exit(1)
	}
}

func setupReconcilers(ctx context.Context, mgr ctrl.Manager) {
	// Set up a ClusterCacheTracker and ClusterCacheReconciler to provide to controllers
	// requiring a connection to a remote cluster
	tracker, err := remote.NewClusterCacheTracker(
		mgr,
		remote.ClusterCacheTrackerOptions{Log: ctrl.Log.WithName("remote").WithName("ClusterCacheTracker")},
	)
	if err != nil {
		setupLog.Error(err, "unable to create cluster cache tracker")
		os.Exit(1)
	}
	if err := (&remote.ClusterCacheReconciler{
		Client:  mgr.GetClient(),
		Log:     ctrl.Log.WithName("remote").WithName("ClusterCacheReconciler"),
		Tracker: tracker,
	}).SetupWithManager(ctx, mgr, concurrency(clusterConcurrency)); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ClusterCacheReconciler")
		os.Exit(1)
	}

	if err := (&controllers.ClusterReconciler{
		Client:           fieldowner.Wrap(mgr.GetClient(), "cluster-controller"),
		WatchFilterValue: watchFilterValue,
	}).SetupWithManager(ctx, mgr, concurrency(clusterConcurrency)); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Cluster")
		os.Exit(1)
	}
	if err := (&controllers.MachineReconciler{
		Client:           fieldowner.Wrap(mgr.GetClient(), controllers.MachineControllerName),
		Tracker:          tracker,
		WatchFilterValue: watchFilterValue,
	}).SetupWithManager(ctx, mgr, concurrency(machineConcurrency)); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Machine")
		os.Exit(1)
	}
	if err := (&controllers.MachineSetReconciler{
		Client:           fieldowner.Wrap(mgr.GetClient(), "machineset-controller"),
		Tracker:          tracker,
		WatchFilterValue: watchFilterValue,
	}).SetupWithManager(ctx, mgr, concurrency(machineSetConcurrency)); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "MachineSet")
		os.Exit(1)
	}
	if err := (&controllers.MachineDeploymentReconciler{
		Client:           fieldowner.Wrap(mgr.GetClient(), "machinedeployment-controller"),
		WatchFilterValue: watchFilterValue,
	}).SetupWithManager(ctx, mgr, concurrency(machineDeploymentConcurrency)); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "MachineDeployment")
		os.Exit(1)
	}

	if feature.Gates.Enabled(feature.MachinePool) {
		if err := (&expcontrollers.MachinePoolReconciler{
			Client:           fieldowner.Wrap(mgr.GetClient(), expcontrollers.MachinePoolControllerName),
			WatchFilterValue: watchFilterValue,
		}).SetupWithManager(ctx, mgr, concurrency(machinePoolConcurrency)); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "MachinePool")
			os.Exit(1)
		}
	}

	if feature.Gates.Enabled(feature.ClusterResourceSet) {
		if err := (&addonscontrollers.ClusterResourceSetReconciler{
			Client:           fieldowner.Wrap(mgr.GetClient(), "cluster-resource-set-controller"),
			Tracker:          tracker,
			WatchFilterValue: watchFilterValue,
		}).SetupWithManager(ctx, mgr, concurrency(clusterResourceSetConcurrency)); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "ClusterResourceSet")
			os.Exit(1)
		}
		if err := (&addonscontrollers.ClusterResourceSetBindingReconciler{
			Client:           fieldowner.Wrap(mgr.GetClient(), "cluster-resource-set-binding-controller"),
			WatchFilterValue: watchFilterValue,
		}).SetupWithManager(ctx, mgr, concurrency(clusterResourceSetConcurrency)); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "ClusterResourceSetBinding")
			os.Exit(1)
		}
	}

	if err := (&controllers.MachineHealthCheckReconciler{
		Client:           fieldowner.Wrap(mgr.GetClient(), "machinehealthcheck-controller"),
		Tracker:          tracker,
		WatchFilterValue: watchFilterValue,
	}).SetupWithManager(ctx, mgr, concurrency(machineHealthCheckConcurrency)); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "MachineHealthCheck")
		os.Exit(1)
	}
}

func setupWebhooks(mgr ctrl.Manager) {
	if err := (&clusterv1.Cluster{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "Cluster")
		os.Exit(1)
	}

	if err := (&clusterv1.Machine{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "Machine")
		os.Exit(1)
	}

	if err := (&clusterv1.MachineSet{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "MachineSet")
		os.Exit(1)
	}

	if err := (&clusterv1.MachineDeployment{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "MachineDeployment")
		os.Exit(1)
	}

	if feature.Gates.Enabled(feature.MachinePool) {
		if err := (&expv1.MachinePool{}).SetupWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "MachinePool")
			os.Exit(1)
		}
	}

	if feature.Gates.Enabled(feature.ClusterResourceSet) {
		if err := (&addonsv1.ClusterResourceSet{}).SetupWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "ClusterResourceSet")
			os.Exit(1)
		}
	}

	if err := (&clusterv1.MachineHealthCheck{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "MachineHealthCheck")
		os.Exit(1)
	}
}

func concurrency(c int) controller.Options {
	return controller.Options{MaxConcurrentReconciles: c}
}
