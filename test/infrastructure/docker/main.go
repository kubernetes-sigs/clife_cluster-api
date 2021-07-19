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
	"math/rand"
	"os"
	"time"

	"github.com/spf13/pflag"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	"sigs.k8s.io/cluster-api/controllers/remote"
	expv1 "sigs.k8s.io/cluster-api/exp/api/v1alpha4"
	"sigs.k8s.io/cluster-api/feature"
	infrav1old "sigs.k8s.io/cluster-api/test/infrastructure/docker/api/v1alpha3"
	infrav1 "sigs.k8s.io/cluster-api/test/infrastructure/docker/api/v1alpha4"
	"sigs.k8s.io/cluster-api/test/infrastructure/docker/controllers"
	infraexpv1old "sigs.k8s.io/cluster-api/test/infrastructure/docker/exp/api/v1alpha3"
	infraexpv1 "sigs.k8s.io/cluster-api/test/infrastructure/docker/exp/api/v1alpha4"
	expcontrollers "sigs.k8s.io/cluster-api/test/infrastructure/docker/exp/controllers"
	"sigs.k8s.io/cluster-api/util/fieldowner"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	// +kubebuilder:scaffold:imports
)

var (
	myscheme = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")

	// flags.
	metricsBindAddr      string
	enableLeaderElection bool
	syncPeriod           time.Duration
	concurrency          int
	healthAddr           string
	webhookPort          int
	webhookCertDir       string
)

func init() {
	klog.InitFlags(nil)

	_ = scheme.AddToScheme(myscheme)
	_ = infrav1old.AddToScheme(myscheme)
	_ = infrav1.AddToScheme(myscheme)
	_ = infraexpv1old.AddToScheme(myscheme)
	_ = infraexpv1.AddToScheme(myscheme)
	_ = clusterv1.AddToScheme(myscheme)
	_ = expv1.AddToScheme(myscheme)
	// +kubebuilder:scaffold:scheme
}

func initFlags(fs *pflag.FlagSet) {
	fs.StringVar(&metricsBindAddr, "metrics-bind-addr", "localhost:8080",
		"The address the metric endpoint binds to.")
	fs.IntVar(&concurrency, "concurrency", 10,
		"The number of docker machines to process simultaneously")
	fs.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	fs.DurationVar(&syncPeriod, "sync-period", 10*time.Minute,
		"The minimum interval at which watched resources are reconciled (e.g. 15m)")
	fs.StringVar(&healthAddr, "health-addr", ":9440",
		"The address the health endpoint binds to.")
	fs.IntVar(&webhookPort, "webhook-port", 9443,
		"Webhook Server port")
	fs.StringVar(&webhookCertDir, "webhook-cert-dir", "/tmp/k8s-webhook-server/serving-certs/",
		"Webhook cert dir, only used when webhook-port is specified.")

	feature.MutableGates.AddFlag(fs)
}

func main() {
	rand.Seed(time.Now().UnixNano())

	initFlags(pflag.CommandLine)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.CommandLine.SetNormalizeFunc(cliflag.WordSepNormalizeFunc)
	pflag.Parse()

	ctrl.SetLogger(klogr.New())

	restConfig := ctrl.GetConfigOrDie()
	restConfig.UserAgent = remote.DefaultClusterAPIUserAgent("cluster-api-docker-controller-manager")
	mgr, err := ctrl.NewManager(restConfig, ctrl.Options{
		Scheme:                 myscheme,
		MetricsBindAddress:     metricsBindAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "controller-leader-election-capd",
		SyncPeriod:             &syncPeriod,
		HealthProbeBindAddress: healthAddr,
		Port:                   webhookPort,
		CertDir:                webhookCertDir,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Setup the context that's going to be used in controllers and for the manager.
	ctx := ctrl.SetupSignalHandler()

	setupChecks(mgr)
	setupReconcilers(ctx, mgr)
	setupWebhooks(mgr)

	// +kubebuilder:scaffold:builder
	setupLog.Info("starting manager")
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

func setupReconcilers(ctx context.Context, mgr ctrl.Manager) {
	if err := (&controllers.DockerMachineReconciler{
		Client: fieldowner.Wrap(mgr.GetClient(), "docker-machine-controller"),
	}).SetupWithManager(ctx, mgr, controller.Options{
		MaxConcurrentReconciles: concurrency,
	}); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "reconciler")
		os.Exit(1)
	}

	if err := (&controllers.DockerClusterReconciler{
		Client: fieldowner.Wrap(mgr.GetClient(), "docker-cluster-controller"),
		Log:    ctrl.Log.WithName("controllers").WithName("DockerCluster"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DockerCluster")
		os.Exit(1)
	}

	if feature.Gates.Enabled(feature.MachinePool) {
		if err := (&expcontrollers.DockerMachinePoolReconciler{
			Client: fieldowner.Wrap(mgr.GetClient(), "docker-machine-pool-controller"),
			Log:    ctrl.Log.WithName("controllers").WithName("DockerMachinePool"),
		}).SetupWithManager(mgr, controller.Options{
			MaxConcurrentReconciles: concurrency,
		}); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "DockerMachinePool")
			os.Exit(1)
		}
	}
}

func setupWebhooks(mgr ctrl.Manager) {
	if err := (&infrav1.DockerMachineTemplate{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "DockerMachineTemplate")
		os.Exit(1)
	}
}
