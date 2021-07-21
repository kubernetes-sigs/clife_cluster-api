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
	"time"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/cluster-api/cmd/clusterctl/client/config"
	"sigs.k8s.io/cluster-api/cmd/clusterctl/client/repository"
	yaml "sigs.k8s.io/cluster-api/cmd/clusterctl/client/yamlprocessor"
	logf "sigs.k8s.io/cluster-api/cmd/clusterctl/log"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	minimumKubernetesVersion = "v1.16.0"
	// firstUnsupportedKubernetesVersion is the first version we don't support as management cluster.
	// We cannot support v1.22.0 as it's not compatible with our controller-runtime version and WebhookConfigurations v1beta1.
	firstUnsupportedKubernetesVersion = "v1.22.0"
)

var (
	ctx = context.TODO()
)

// Kubeconfig is a type that specifies inputs related to the actual
// kubeconfig.
type Kubeconfig struct {
	// Path to the kubeconfig file
	Path string
	// Specify context within the kubeconfig file. If empty, cluster client
	// will use the current context.
	Context string
}

// Client is used to interact with a management cluster.
// A management cluster contains following categories of objects:
// - provider components (e.g. the CRDs, controllers, RBAC)
// - provider inventory items (e.g. the list of installed providers/versions)
// - provider objects (e.g. clusters, AWS clusters, machines etc.)
type Client interface {
	// Kubeconfig returns the kubeconfig used to access to a management cluster.
	Kubeconfig() Kubeconfig

	// Proxy return the Proxy used for operating objects in the management cluster.
	Proxy() Proxy

	// CertManager returns a CertManagerClient that can be user for
	// operating the cert-manager components in the cluster.
	CertManager() CertManagerClient

	// ProviderComponents returns a ComponentsClient object that can be user for
	// operating provider components objects in the management cluster (e.g. the CRDs, controllers, RBAC).
	ProviderComponents() ComponentsClient

	// ProviderInventory returns a InventoryClient object that can be user for
	// operating provider inventory stored in the management cluster (e.g. the list of installed providers/versions).
	ProviderInventory() InventoryClient

	// ProviderInstaller returns a ProviderInstaller that enforces consistency rules for provider installation,
	// trying to prevent e.g. controllers fighting for objects, inconsistent versions, etc.
	ProviderInstaller() ProviderInstaller

	// ObjectMover returns an ObjectMover that implements support for moving Cluster API objects (e.g. clusters, AWS clusters, machines, etc.).
	// from one management cluster to another management cluster.
	ObjectMover() ObjectMover

	// ProviderUpgrader returns a ProviderUpgrader that supports upgrading Cluster API providers.
	ProviderUpgrader() ProviderUpgrader

	// Template has methods to work with templates stored in the cluster.
	Template() TemplateClient

	// WorkloadCluster has methods for fetching kubeconfig of workload cluster from management cluster.
	WorkloadCluster() WorkloadCluster
}

// PollImmediateWaiter tries a condition func until it returns true, an error, or the timeout is reached.
type PollImmediateWaiter func(interval, timeout time.Duration, condition wait.ConditionFunc) error

// clusterClient implements Client.
type clusterClient struct {
	configClient            config.Client
	kubeconfig              Kubeconfig
	proxy                   Proxy
	repositoryClientFactory RepositoryClientFactory
	pollImmediateWaiter     PollImmediateWaiter
	processor               yaml.Processor
}

type RepositoryClientFactory func(provider config.Provider, configClient config.Client, options ...repository.Option) (repository.Client, error)

// ensure clusterClient implements Client.
var _ Client = &clusterClient{}

func (c *clusterClient) Kubeconfig() Kubeconfig {
	return c.kubeconfig
}

func (c *clusterClient) Proxy() Proxy {
	return c.proxy
}

func (c *clusterClient) CertManager() CertManagerClient {
	return newCertMangerClient(c.configClient, c.proxy, c.pollImmediateWaiter)
}

func (c *clusterClient) ProviderComponents() ComponentsClient {
	return newComponentsClient(c.proxy)
}

func (c *clusterClient) ProviderInventory() InventoryClient {
	return newInventoryClient(c.proxy, c.pollImmediateWaiter)
}

func (c *clusterClient) ProviderInstaller() ProviderInstaller {
	return newProviderInstaller(c.configClient, c.repositoryClientFactory, c.proxy, c.ProviderInventory(), c.ProviderComponents())
}

func (c *clusterClient) ObjectMover() ObjectMover {
	return newObjectMover(c.proxy, c.ProviderInventory())
}

func (c *clusterClient) ProviderUpgrader() ProviderUpgrader {
	return newProviderUpgrader(c.configClient, c.repositoryClientFactory, c.ProviderInventory(), c.ProviderComponents())
}

func (c *clusterClient) Template() TemplateClient {
	return newTemplateClient(TemplateClientInput{c.proxy, c.configClient, c.processor})
}

func (c *clusterClient) WorkloadCluster() WorkloadCluster {
	return newWorkloadCluster(c.proxy)
}

// Option is a configuration option supplied to New
type Option func(*clusterClient)

// InjectProxy allows to override the default proxy used by clusterctl.
func InjectProxy(proxy Proxy) Option {
	return func(c *clusterClient) {
		c.proxy = proxy
	}
}

// InjectRepositoryFactory allows to override the default factory used for creating
// RepositoryClient objects.
func InjectRepositoryFactory(factory RepositoryClientFactory) Option {
	return func(c *clusterClient) {
		c.repositoryClientFactory = factory
	}
}

// InjectPollImmediateWaiter allows to override the default PollImmediateWaiter used by clusterctl.
func InjectPollImmediateWaiter(pollImmediateWaiter PollImmediateWaiter) Option {
	return func(c *clusterClient) {
		c.pollImmediateWaiter = pollImmediateWaiter
	}
}

// InjectYamlProcessor allows you to override the yaml processor that the
// cluster client uses. By default, the SimpleProcessor is used. This is
// true even if a nil processor is injected.
func InjectYamlProcessor(p yaml.Processor) Option {
	return func(c *clusterClient) {
		if p != nil {
			c.processor = p
		}
	}
}

// New returns a cluster.Client.
func New(kubeconfig Kubeconfig, configClient config.Client, options ...Option) Client {
	return newClusterClient(kubeconfig, configClient, options...)
}

func newClusterClient(kubeconfig Kubeconfig, configClient config.Client, options ...Option) *clusterClient {
	client := &clusterClient{
		configClient: configClient,
		kubeconfig:   kubeconfig,
		processor:    yaml.NewSimpleProcessor(),
	}
	for _, o := range options {
		o(client)
	}

	// if there is an injected proxy, use it, otherwise use a default one
	if client.proxy == nil {
		client.proxy = newProxy(client.kubeconfig)
	}

	// if there is an injected repositoryClientFactory, use it, otherwise use the default one
	if client.repositoryClientFactory == nil {
		client.repositoryClientFactory = repository.New
	}

	// if there is an injected PollImmediateWaiter, use it, otherwise use the default one
	if client.pollImmediateWaiter == nil {
		client.pollImmediateWaiter = wait.PollImmediate
	}

	return client
}

type Proxy interface {
	// GetConfig returns the rest.Config
	GetConfig() (*rest.Config, error)

	// CurrentNamespace returns the namespace from the current context in the kubeconfig file
	CurrentNamespace() (string, error)

	// ValidateKubernetesVersion returns an error if management cluster version less than minimumKubernetesVersion
	ValidateKubernetesVersion() error

	// ValidateKubernetesMaxVersion returns an error if management cluster version is greater than or equal to firstUnsupportedKubernetesVersion
	ValidateKubernetesMaxVersion() error

	// NewClient returns a new controller runtime Client object for working on the management cluster
	NewClient() (client.Client, error)

	// ListResources returns all the Kubernetes objects with the given labels existing the listed namespaces.
	ListResources(labels map[string]string, namespaces ...string) ([]unstructured.Unstructured, error)
}

// retryWithExponentialBackoff repeats an operation until it passes or the exponential backoff times out.
func retryWithExponentialBackoff(opts wait.Backoff, operation func() error) error {
	log := logf.Log

	i := 0
	err := wait.ExponentialBackoff(opts, func() (bool, error) {
		i++
		if err := operation(); err != nil {
			if i < opts.Steps {
				log.V(5).Info("Operation failed, retrying with backoff", "Cause", err.Error())
				return false, nil
			}
			return false, err
		}
		return true, nil
	})
	if err != nil {
		return errors.Wrapf(err, "action failed after %d attempts", i)
	}
	return nil
}

// newWriteBackoff creates a new API Machinery backoff parameter set suitable for use with clusterctl write operations.
func newWriteBackoff() wait.Backoff {
	// Return a exponential backoff configuration which returns durations for a total time of ~40s.
	// Example: 0, .5s, 1.2s, 2.3s, 4s, 6s, 10s, 16s, 24s, 37s
	// Jitter is added as a random fraction of the duration multiplied by the jitter factor.
	return wait.Backoff{
		Duration: 500 * time.Millisecond,
		Factor:   1.5,
		Steps:    10,
		Jitter:   0.4,
	}
}

// newConnectBackoff creates a new API Machinery backoff parameter set suitable for use when clusterctl connect to a cluster.
func newConnectBackoff() wait.Backoff {
	// Return a exponential backoff configuration which returns durations for a total time of ~15s.
	// Example: 0, .25s, .6s, 1.2, 2.1s, 3.4s, 5.5s, 8s, 12s
	// Jitter is added as a random fraction of the duration multiplied by the jitter factor.
	return wait.Backoff{
		Duration: 250 * time.Millisecond,
		Factor:   1.5,
		Steps:    9,
		Jitter:   0.1,
	}
}

// newReadBackoff creates a new API Machinery backoff parameter set suitable for use with clusterctl read operations.
func newReadBackoff() wait.Backoff {
	// Return a exponential backoff configuration which returns durations for a total time of ~15s.
	// Example: 0, .25s, .6s, 1.2, 2.1s, 3.4s, 5.5s, 8s, 12s
	// Jitter is added as a random fraction of the duration multiplied by the jitter factor.
	return wait.Backoff{
		Duration: 250 * time.Millisecond,
		Factor:   1.5,
		Steps:    9,
		Jitter:   0.1,
	}
}
