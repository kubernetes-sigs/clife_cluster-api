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
	"fmt"
	"os"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	operatorv1 "sigs.k8s.io/cluster-api/exp/operator/api/v1alpha1"
	"sigs.k8s.io/cluster-api/internal/envtest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
	// +kubebuilder:scaffold:imports
)

const (
	timeout = time.Second * 30
)

var (
	env *envtest.Environment
	ctx = ctrl.SetupSignalHandler()
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"Operator Controller Suite",
		[]Reporter{printer.NewlineReporter{}})
}

func TestMain(m *testing.M) {
	fmt.Println("Creating new test environment")

	env = envtest.New()

	if err := (&GenericProviderReconciler{
		Provider:     &operatorv1.CoreProvider{},
		ProviderList: &operatorv1.CoreProviderList{},
		Client:       env,
	}).SetupWithManager(env.Manager, controller.Options{MaxConcurrentReconciles: 1}); err != nil {
		panic(fmt.Sprintf("Failed to start CoreProviderReconciler: %v", err))
	}

	if err := (&GenericProviderReconciler{
		Provider:     &operatorv1.InfrastructureProvider{},
		ProviderList: &operatorv1.InfrastructureProviderList{},
		Client:       env,
	}).SetupWithManager(env.Manager, controller.Options{MaxConcurrentReconciles: 1}); err != nil {
		panic(fmt.Sprintf("Failed to start InfrastructureProviderReconciler: %v", err))
	}

	if err := (&GenericProviderReconciler{
		Provider:     &operatorv1.BootstrapProvider{},
		ProviderList: &operatorv1.BootstrapProviderList{},
		Client:       env,
	}).SetupWithManager(env.Manager, controller.Options{MaxConcurrentReconciles: 1}); err != nil {
		panic(fmt.Sprintf("Failed to start BootstrapProviderReconciler: %v", err))
	}

	if err := (&GenericProviderReconciler{
		Provider:     &operatorv1.ControlPlaneProvider{},
		ProviderList: &operatorv1.ControlPlaneProviderList{},
		Client:       env,
	}).SetupWithManager(env.Manager, controller.Options{MaxConcurrentReconciles: 1}); err != nil {
		panic(fmt.Sprintf("Failed to start ControlPlaneProviderReconciler: %v", err))
	}

	go func() {
		if err := env.Start(ctx); err != nil {
			panic(fmt.Sprintf("Failed to start the envtest manager: %v", err))
		}
	}()
	<-env.Manager.Elected()
	env.WaitForWebhooks()

	// Run tests
	code := m.Run()
	// Tearing down the test environment
	if err := env.Stop(); err != nil {
		panic(fmt.Sprintf("Failed to stop the envtest: %v", err))
	}

	// Report exit code
	os.Exit(code)
}
