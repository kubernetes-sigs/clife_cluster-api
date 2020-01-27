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

package cluster

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog/klogr"
	"sigs.k8s.io/cluster-api/cmd/clusterctl/pkg/internal/test"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	ctx = context.TODO()
)

// Client is used to interact with a management cluster.
// A management cluster contains following categories of objects:
// - provider components (e.g. the CRDs, controllers, RBAC)
// - provider inventory items (e.g. the list of installed providers/versions)
// - provider objects (e.g. clusters, AWS clusters, machines etc.)
type Client interface {
	// Kubeconfig return the path to kubeconfig used to access to a management cluster.
	Kubeconfig() string

	// Proxy return the Proxy used for operating objects in the management cluster.
	Proxy() Proxy

	// CertManger returns a CertMangerClient that can be user for
	// operating the cert-manager components in the cluster.
	CertManger() CertMangerClient

	// ProviderComponents returns a ComponentsClient object that can be user for
	// operating provider components objects in the management cluster (e.g. the CRDs, controllers, RBAC).
	ProviderComponents() ComponentsClient

	// ProviderInventory returns a InventoryClient object that can be user for
	// operating provider inventory stored in the management cluster (e.g. the list of installed providers/versions).
	ProviderInventory() InventoryClient

	// ProviderObjects returns a ObjectsClient object that can be user for
	// operating Cluster API objects stored in the management cluster (e.g. clusters, AWS clusters, machines etc.).
	ProviderObjects() ObjectsClient

	// ProviderInstaller returns a ProviderInstaller that enforces consistency rules for provider installation,
	// trying to prevent e.g. controllers fighting for objects, inconsistent versions, etc.
	ProviderInstaller() ProviderInstaller

	// ObjectMover returns an ObjectMover that implements support for moving Cluster API objects (e.g. clusters, AWS clusters, machines, etc.).
	// from one management cluster to another management cluster.
	ObjectMover() ObjectMover
}

// clusterClient implements Client.
type clusterClient struct {
	kubeconfig    string
	proxy         Proxy
	machineWaiter MachineWaiter
	log           logr.Logger
}

// ensure clusterClient implements Client.
var _ Client = &clusterClient{}

func (c *clusterClient) Kubeconfig() string {
	return c.kubeconfig
}

func (c *clusterClient) Proxy() Proxy {
	return c.proxy
}

func (c *clusterClient) CertManger() CertMangerClient {
	return newCertMangerClient(c.proxy, c.log)
}

func (c *clusterClient) ProviderComponents() ComponentsClient {
	return newComponentsClient(c.proxy, c.log)
}

func (c *clusterClient) ProviderInventory() InventoryClient {
	return newInventoryClient(c.proxy, c.log)
}

func (c *clusterClient) ProviderObjects() ObjectsClient {
	return newObjectsClient(c.proxy, c.log)
}

func (c *clusterClient) ProviderInstaller() ProviderInstaller {
	return newProviderInstaller(c.proxy, c.ProviderInventory(), c.ProviderComponents(), c.log)
}

func (c *clusterClient) ObjectMover() ObjectMover {
	return newObjectMover(c.proxy, c.machineWaiter, c.log)
}

// NewOptions carries the options supported by New
type NewOptions struct {
	injectProxy         Proxy
	injectLogger        logr.Logger
	injectMachineWaiter MachineWaiter
}

// Option is a configuration option supplied to New
type Option func(*NewOptions)

// InjectProxy implements a New Option that allows to override the default proxy used by clusterctl.
func InjectProxy(proxy Proxy) Option {
	return func(c *NewOptions) {
		c.injectProxy = proxy
	}
}

// InjectLogger implements a New Option that allows to override the default logger.
func InjectLogger(logger logr.Logger) Option {
	return func(c *NewOptions) {
		c.injectLogger = logger
	}
}

// InjectMachineWaiter implements a New Option that allows to override the default MachineWaiter used by clusterctl.
func InjectMachineWaiter(waiter MachineWaiter) Option {
	return func(c *NewOptions) {
		c.injectMachineWaiter = waiter
	}
}

// New returns a cluster.Client.
func New(kubeconfig string, options ...Option) Client {
	return newClusterClient(kubeconfig, options...)
}

func newClusterClient(kubeconfig string, options ...Option) *clusterClient {
	cfg := &NewOptions{}
	for _, o := range options {
		o(cfg)
	}

	// if there is an injected logger, use it, otherwise use a default one
	logger := cfg.injectLogger
	if logger == nil {
		logger = klogr.New() //TODO: replace with a logger with a better output
	}

	// if there is an injected proxy, use it, otherwise use a default one
	proxy := cfg.injectProxy
	if proxy == nil {
		proxy = newProxy(kubeconfig)
	}

	// if there is an injected machineWaiter, use it, otherwise use the default one
	machineWaiter := cfg.injectMachineWaiter
	if machineWaiter == nil {
		machineWaiter = waitForMachineReady
	}

	return &clusterClient{
		kubeconfig:    kubeconfig,
		proxy:         proxy,
		machineWaiter: machineWaiter,
		log:           logger,
	}
}

type Proxy interface {
	// CurrentNamespace returns the namespace from the current context in the kubeconfig file
	CurrentNamespace() (string, error)

	// NewClient returns a new controller runtime Client object for working on the management cluster
	NewClient() (client.Client, error)

	// ListResources returns all the Kubernetes objects existing in a namespace (or in all namespaces if empty)
	// with the given labels.
	ListResources(namespace string, labels map[string]string) ([]unstructured.Unstructured, error)
}

var _ Proxy = &test.FakeProxy{}
