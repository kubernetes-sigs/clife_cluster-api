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

package controllers

import (
	"fmt"
	"os"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/cluster-api/controllers/noderefutil"
	"sigs.k8s.io/cluster-api/controllers/remote"
	"sigs.k8s.io/cluster-api/exp/addons/api/v1alpha4"
	"sigs.k8s.io/cluster-api/internal/envtest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	// +kubebuilder:scaffold:imports
)

var (
	env *envtest.Environment
	ctx = ctrl.SetupSignalHandler()
)

func TestMain(m *testing.M) {
	fmt.Println("Creating new test environment")
	env = envtest.New([]client.Object{&corev1.ConfigMap{}, &corev1.Secret{}, &v1alpha4.ClusterResourceSetBinding{}}...)

	// Set up the MachineNodeIndex
	if err := noderefutil.AddMachineNodeIndex(ctx, env.Manager); err != nil {
		panic(fmt.Sprintf("unable to setup machine node index: %v", err))
	}

	// Set up the MachineProviderIDIndex
	if err := noderefutil.AddMachineProviderIDIndex(ctx, env.Manager); err != nil {
		panic(fmt.Sprintf("unable to setup machine providerID index: %v", err))
	}

	trckr, err := remote.NewClusterCacheTracker(env.Manager, remote.ClusterCacheTrackerOptions{})
	if err != nil {
		panic(fmt.Sprintf("Failed to create new cluster cache tracker: %v", err))
	}
	reconciler := ClusterResourceSetReconciler{
		Client:  env,
		Tracker: trckr,
	}
	if err = reconciler.SetupWithManager(ctx, env.Manager, controller.Options{MaxConcurrentReconciles: 1}); err != nil {
		panic(fmt.Sprintf("Failed to set up cluster resource set reconciler: %v", err))
	}
	bindingReconciler := ClusterResourceSetBindingReconciler{
		Client: env,
	}
	if err = bindingReconciler.SetupWithManager(ctx, env.Manager, controller.Options{MaxConcurrentReconciles: 1}); err != nil {
		panic(fmt.Sprintf("Failed to set up cluster resource set binding reconciler: %v", err))
	}

	go func() {
		fmt.Println("Starting the manager")
		if err := env.Start(ctx); err != nil {
			panic(fmt.Sprintf("Failed to start the envtest manager: %v", err))
		}
	}()
	<-env.Manager.Elected()
	env.WaitForWebhooks()

	code := m.Run()

	fmt.Println("Tearing down test suite")
	if err := env.Stop(); err != nil {
		panic(fmt.Sprintf("Failed to stop envtest: %v", err))
	}

	os.Exit(code)
}
