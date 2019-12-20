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
	"flag"
	"net/http"
	_ "net/http/pprof"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	"k8s.io/klog/klogr"
	clusterv1alpha2 "sigs.k8s.io/cluster-api/api/v1alpha2"
	clusterv1alpha3 "sigs.k8s.io/cluster-api/api/v1alpha3"
	kubeadmbootstrapv1alpha2 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1alpha2"
	kubeadmbootstrapv1alpha3 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1alpha3"
	kubeadmbootstrapcontrollers "sigs.k8s.io/cluster-api/bootstrap/kubeadm/controllers"
	"sigs.k8s.io/cluster-api/controllers"
	kubeadmcontrolplanev1alpha3 "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1alpha3"
	kubeadmcontrolplanecontrollers "sigs.k8s.io/cluster-api/controlplane/kubeadm/controllers"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	klog.InitFlags(nil)

	_ = clientgoscheme.AddToScheme(scheme)
	_ = clusterv1alpha2.AddToScheme(scheme)
	_ = clusterv1alpha3.AddToScheme(scheme)
	_ = kubeadmbootstrapv1alpha2.AddToScheme(scheme)
	_ = kubeadmbootstrapv1alpha3.AddToScheme(scheme)
	_ = kubeadmcontrolplanev1alpha3.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme
}

func main() {
	var (
		metricsAddr                    string
		enableLeaderElection           bool
		watchNamespace                 string
		profilerAddress                string
		clusterConcurrency             int
		machineConcurrency             int
		machineSetConcurrency          int
		machineDeploymentConcurrency   int
		machinePoolConcurrency         int
		kubeadmBootstrapperDisabled    bool
		kubeadmConfigConcurrency       int
		kubeadmControlPlaneConcurrency int
		syncPeriod                     time.Duration
		webhookPort                    int
	)

	flag.StringVar(&metricsAddr, "metrics-addr", ":8080",
		"The address the metric endpoint binds to.")

	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")

	flag.StringVar(&watchNamespace, "namespace", "",
		"Namespace that the controller watches to reconcile cluster-api objects. If unspecified, the controller watches for cluster-api objects across all namespaces.")

	flag.StringVar(&profilerAddress, "profiler-address", "",
		"Bind address to expose the pprof profiler (e.g. localhost:6060)")

	flag.IntVar(&clusterConcurrency, "cluster-concurrency", 10,
		"Number of clusters to process simultaneously")

	flag.IntVar(&machineConcurrency, "machine-concurrency", 10,
		"Number of machines to process simultaneously")

	flag.IntVar(&machineSetConcurrency, "machineset-concurrency", 10,
		"Number of machine sets to process simultaneously")

	flag.IntVar(&machineDeploymentConcurrency, "machinedeployment-concurrency", 10,
		"Number of machine deployments to process simultaneously")

	flag.IntVar(&machinePoolConcurrency, "machinepool-concurrency", 10,
		"Number of machine pools to process simultaneously")

	flag.BoolVar(&kubeadmBootstrapperDisabled, "disable-kubeadm-bootstrapper", false,
		"Whether or not to disable the kubeadm bootstrap and controlplane components")

	flag.IntVar(&kubeadmConfigConcurrency, "kubeadmconfig-concurrency", 10,
		"Number of kubeadm configs to process simultaneously")

	flag.IntVar(&kubeadmControlPlaneConcurrency, "kubeadmcontrolplane-concurrency", 1,
		"Number of kubeadm control planes to process simultaneously")

	flag.DurationVar(&syncPeriod, "sync-period", 10*time.Minute,
		"The minimum interval at which watched resources are reconciled (e.g. 15m)")

	flag.DurationVar(&kubeadmbootstrapcontrollers.DefaultTokenTTL, "bootstrap-token-ttl", 15*time.Minute,
		"The amount of time the bootstrap token will be valid")

	flag.IntVar(&webhookPort, "webhook-port", 9443,
		"Webhook Server port (set to 0 to disable)")

	flag.Parse()

	ctrl.SetLogger(klogr.New())

	if profilerAddress != "" {
		klog.Infof("Profiler listening for requests at %s", profilerAddress)
		go func() {
			klog.Info(http.ListenAndServe(profilerAddress, nil))
		}()
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsAddr,
		LeaderElection:     enableLeaderElection,
		LeaderElectionID:   "controller-leader-election-capi",
		Namespace:          watchNamespace,
		SyncPeriod:         &syncPeriod,
		NewClient:          newClientFunc,
		Port:               webhookPort,
	})
	exitIfError(err, "unable to start manager")

	err = (&controllers.ClusterReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("Cluster"),
	}).SetupWithManager(mgr, concurrency(clusterConcurrency))
	exitIfError(err, "unable to create controller", "controller", "Cluster")

	err = (&controllers.MachineReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("Machine"),
	}).SetupWithManager(mgr, concurrency(machineConcurrency))
	exitIfError(err, "unable to create controller", "controller", "Machine")

	err = (&controllers.MachineSetReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("MachineSet"),
	}).SetupWithManager(mgr, concurrency(machineSetConcurrency))
	exitIfError(err, "unable to create controller", "controller", "MachineSet")

	err = (&controllers.MachineDeploymentReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("MachineDeployment"),
	}).SetupWithManager(mgr, concurrency(machineDeploymentConcurrency))
	exitIfError(err, "unable to create controller", "controller", "MachineDeployment")

	err = (&controllers.MachinePoolReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("MachinePool"),
	}).SetupWithManager(mgr, concurrency(machinePoolConcurrency))
	exitIfError(err, "unable to create controller", "controller", "MachinePool")

	if !kubeadmBootstrapperDisabled {
		// Kubeadm controllers.
		err = (&kubeadmbootstrapcontrollers.KubeadmConfigReconciler{
			Client: mgr.GetClient(),
			Log:    ctrl.Log.WithName("controllers").WithName("KubeadmConfig"),
		}).SetupWithManager(mgr, concurrency(kubeadmConfigConcurrency))
		exitIfError(err, "unable to create controller", "controller", "KubeadmConfig")

		// KubeadmControlPlane controllers.
		err = (&kubeadmcontrolplanecontrollers.KubeadmControlPlaneReconciler{
			Client: mgr.GetClient(),
			Log:    ctrl.Log.WithName("controllers").WithName("KubeadmControlPlane"),
		}).SetupWithManager(mgr, concurrency(kubeadmControlPlaneConcurrency))
		exitIfError(err, "unable to create controller", "controller", "KubeadmControlPlane")
	}

	if webhookPort != 0 {
		err = (&clusterv1alpha2.Cluster{}).SetupWebhookWithManager(mgr)
		exitIfError(err, "unable to create webhook", "webhook", "Cluster")
		err = (&clusterv1alpha3.Cluster{}).SetupWebhookWithManager(mgr)
		exitIfError(err, "unable to create webhook", "webhook", "Cluster")

		err = (&clusterv1alpha2.ClusterList{}).SetupWebhookWithManager(mgr)
		exitIfError(err, "unable to create webhook", "webhook", "ClusterList")

		err = (&clusterv1alpha2.Machine{}).SetupWebhookWithManager(mgr)
		exitIfError(err, "unable to create webhook", "webhook", "Machine")
		err = (&clusterv1alpha3.Machine{}).SetupWebhookWithManager(mgr)
		exitIfError(err, "unable to create webhook", "webhook", "Machine")

		err = (&clusterv1alpha2.MachineList{}).SetupWebhookWithManager(mgr)
		exitIfError(err, "unable to create webhook", "webhook", "MachineList")

		err = (&clusterv1alpha2.MachineSet{}).SetupWebhookWithManager(mgr)
		exitIfError(err, "unable to create webhook", "webhook", "MachineSet")
		err = (&clusterv1alpha3.MachineSet{}).SetupWebhookWithManager(mgr)
		exitIfError(err, "unable to create webhook", "webhook", "MachineSet")

		err = (&clusterv1alpha2.MachineSetList{}).SetupWebhookWithManager(mgr)
		exitIfError(err, "unable to create webhook", "webhook", "MachineSetList")

		err = (&clusterv1alpha2.MachineDeployment{}).SetupWebhookWithManager(mgr)
		exitIfError(err, "unable to create webhook", "webhook", "MachineDeployment")
		err = (&clusterv1alpha3.MachineDeployment{}).SetupWebhookWithManager(mgr)
		exitIfError(err, "unable to create webhook", "webhook", "MachineDeployment")

		err = (&clusterv1alpha2.MachineDeploymentList{}).SetupWebhookWithManager(mgr)
		exitIfError(err, "unable to create webhook", "webhook", "MachineDeploymentList")

		err = (&clusterv1alpha3.MachinePool{}).SetupWebhookWithManager(mgr)
		exitIfError(err, "unable to create webhook", "webhook", "MachinePool")

		if !kubeadmBootstrapperDisabled {
			err = (&kubeadmcontrolplanev1alpha3.KubeadmControlPlane{}).SetupWebhookWithManager(mgr)
			exitIfError(err, "unable to create webhook", "webhook", "KubeadmControlPlane")
			err = (&kubeadmbootstrapv1alpha3.KubeadmConfig{}).SetupWebhookWithManager(mgr)
			exitIfError(err, "unable to create webhook", "webhook", "KubeadmConfig")
			err = (&kubeadmbootstrapv1alpha3.KubeadmConfigList{}).SetupWebhookWithManager(mgr)
			exitIfError(err, "unable to create webhook", "webhook", "KubeadmConfigList")
			err = (&kubeadmbootstrapv1alpha3.KubeadmConfigTemplate{}).SetupWebhookWithManager(mgr)
			exitIfError(err, "unable to create webhook", "webhook", "KubeadmConfigTemplate")
			err = (&kubeadmbootstrapv1alpha3.KubeadmConfigTemplateList{}).SetupWebhookWithManager(mgr)
			exitIfError(err, "unable to create webhook", "webhook", "KubeadmConfigTemplateList")
		}
	}

	// +kubebuilder:scaffold:builder
	setupLog.Info("starting manager")
	err = mgr.Start(ctrl.SetupSignalHandler())
	exitIfError(err, "problem running manager")
}

func exitIfError(err error, msg string, keysAndValues ...interface{}) {
	if err == nil {
		return
	}
	setupLog.Error(err, msg, keysAndValues...)
	os.Exit(1)
}

func concurrency(c int) controller.Options {
	return controller.Options{MaxConcurrentReconciles: c}
}

// newClientFunc returns a client reads from cache and write directly to the server
// this avoid get unstructured object directly from the server
// see issue: https://github.com/kubernetes-sigs/cluster-api/issues/1663
func newClientFunc(cache cache.Cache, config *rest.Config, options client.Options) (client.Client, error) {
	// Create the Client for Write operations.
	c, err := client.New(config, options)
	if err != nil {
		return nil, err
	}

	return &client.DelegatingClient{
		Reader:       cache,
		Writer:       c,
		StatusClient: c,
	}, nil
}
