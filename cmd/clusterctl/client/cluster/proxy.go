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
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilversion "k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/cluster-api/cmd/clusterctl/internal/scheme"
	"sigs.k8s.io/cluster-api/cmd/version"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	Scheme = scheme.Scheme
)

type proxy struct {
	kubeconfig         Kubeconfig
	timeout            time.Duration
	configLoadingRules *clientcmd.ClientConfigLoadingRules
}

var _ Proxy = &proxy{}

// CurrentNamespace returns the namespace for the specified context or the
// first valid context as determined by the default config loading rules.
func (k *proxy) CurrentNamespace() (string, error) {
	config, err := k.configLoadingRules.Load()
	if err != nil {
		return "", errors.Wrap(err, "failed to load Kubeconfig")
	}

	context := config.CurrentContext
	// If a context is explicitly provided use that instead
	if k.kubeconfig.Context != "" {
		context = k.kubeconfig.Context
	}

	v, ok := config.Contexts[context]
	if !ok {
		if k.kubeconfig.Path != "" {
			return "", errors.Errorf("failed to get context %q from %q", context, k.configLoadingRules.GetExplicitFile())
		}
		return "", errors.Errorf("failed to get context %q from %q", context, k.configLoadingRules.GetLoadingPrecedence())
	}

	if v.Namespace != "" {
		return v.Namespace, nil
	}

	return "default", nil
}

func (k *proxy) ValidateKubernetesVersion() error {
	config, err := k.GetConfig()
	if err != nil {
		return err
	}

	client := discovery.NewDiscoveryClientForConfigOrDie(config)
	serverVersion, err := client.ServerVersion()
	if err != nil {
		return errors.Wrap(err, "failed to retrieve server version")
	}

	compver, err := utilversion.MustParseGeneric(serverVersion.String()).Compare(minimumKubernetesVersion)
	if err != nil {
		return errors.Wrap(err, "failed to parse and compare server version")
	}

	if compver == -1 {
		return errors.Errorf("unsupported management cluster server version: %s - minimum required version is %s", serverVersion.String(), minimumKubernetesVersion)
	}

	return nil
}

func (k *proxy) ValidateKubernetesMaxVersion() error {
	config, err := k.GetConfig()
	if err != nil {
		return err
	}

	client := discovery.NewDiscoveryClientForConfigOrDie(config)
	serverVersion, err := client.ServerVersion()
	if err != nil {
		return errors.Wrap(err, "failed to retrieve server version")
	}

	compver, err := utilversion.MustParseGeneric(serverVersion.String()).Compare(firstUnsupportedKubernetesVersion)
	if err != nil {
		return errors.Wrap(err, "failed to parse and compare server version")
	}

	// We want to return an error if the serverVersion is greater or equal than firstUnsupportedKubernetesVersion
	if compver >= 0 {
		return errors.Errorf("unsupported management cluster server version: %s - versions greater or equal than %s are not supported", serverVersion.String(), firstUnsupportedKubernetesVersion)
	}

	return nil
}

// GetConfig returns the config for a kubernetes client.
func (k *proxy) GetConfig() (*rest.Config, error) {
	config, err := k.configLoadingRules.Load()
	if err != nil {
		return nil, errors.Wrap(err, "failed to load Kubeconfig")
	}

	configOverrides := &clientcmd.ConfigOverrides{
		CurrentContext: k.kubeconfig.Context,
		Timeout:        k.timeout.String(),
	}
	restConfig, err := clientcmd.NewDefaultClientConfig(*config, configOverrides).ClientConfig()
	if err != nil {
		if strings.HasPrefix(err.Error(), "invalid configuration:") {
			return nil, errors.New(strings.Replace(err.Error(), "invalid configuration:", "invalid kubeconfig file; clusterctl requires a valid kubeconfig file to connect to the management cluster:", 1))
		}
		return nil, err
	}
	restConfig.UserAgent = fmt.Sprintf("clusterctl/%s (%s)", version.Get().GitVersion, version.Get().Platform)

	// Set QPS and Burst to a threshold that ensures the controller runtime client/client go does't generate throttling log messages
	restConfig.QPS = 20
	restConfig.Burst = 100

	return restConfig, nil
}

func (k *proxy) NewClient() (client.Client, error) {
	config, err := k.GetConfig()
	if err != nil {
		return nil, err
	}

	var c client.Client
	// Nb. The operation is wrapped in a retry loop to make newClientSet more resilient to temporary connection problems.
	connectBackoff := newConnectBackoff()
	if err := retryWithExponentialBackoff(connectBackoff, func() error {
		var err error
		c, err = client.New(config, client.Options{Scheme: Scheme})
		if err != nil {
			return err
		}
		return nil
	}); err != nil {
		return nil, errors.Wrap(err, "failed to connect to the management cluster")
	}

	return c, nil
}

func (k *proxy) ListResources(labels map[string]string, namespaces ...string) ([]unstructured.Unstructured, error) {
	cs, err := k.newClientSet()
	if err != nil {
		return nil, err
	}

	c, err := k.NewClient()
	if err != nil {
		return nil, err
	}

	// Get all the API resources in the cluster.
	resourceListBackoff := newReadBackoff()
	var resourceList []*metav1.APIResourceList
	if err := retryWithExponentialBackoff(resourceListBackoff, func() error {
		resourceList, err = cs.Discovery().ServerPreferredResources()
		return err
	}); err != nil {
		return nil, errors.Wrap(err, "failed to list api resources")
	}

	// Select resources with list and delete methods (list is required by this method, delete by the callers of this method)
	resourceList = discovery.FilteredBy(discovery.SupportsAllVerbs{Verbs: []string{"list", "delete"}}, resourceList)

	var ret []unstructured.Unstructured
	for _, resourceGroup := range resourceList {
		for _, resourceKind := range resourceGroup.APIResources {
			// Discard the resourceKind that exists in two api groups (we are excluding one of the two groups arbitrarily).
			if resourceGroup.GroupVersion == "extensions/v1beta1" &&
				(resourceKind.Name == "daemonsets" || resourceKind.Name == "deployments" || resourceKind.Name == "replicasets" || resourceKind.Name == "networkpolicies" || resourceKind.Name == "ingresses") {
				continue
			}

			// List all the object instances of this resourceKind with the given labels
			if resourceKind.Namespaced {
				for _, namespace := range namespaces {
					objList, err := listObjByGVK(c, resourceGroup.GroupVersion, resourceKind.Kind, []client.ListOption{client.MatchingLabels(labels), client.InNamespace(namespace)})
					if err != nil {
						return nil, err
					}
					ret = append(ret, objList.Items...)
				}
			} else {
				objList, err := listObjByGVK(c, resourceGroup.GroupVersion, resourceKind.Kind, []client.ListOption{client.MatchingLabels(labels)})
				if err != nil {
					return nil, err
				}
				ret = append(ret, objList.Items...)
			}
		}
	}
	return ret, nil
}

func listObjByGVK(c client.Client, groupVersion, kind string, options []client.ListOption) (*unstructured.UnstructuredList, error) {
	objList := new(unstructured.UnstructuredList)
	objList.SetAPIVersion(groupVersion)
	objList.SetKind(kind)

	if err := c.List(ctx, objList, options...); err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, errors.Wrapf(err, "failed to list objects for the %q GroupVersionKind", objList.GroupVersionKind())
		}
	}
	return objList, nil
}

type ProxyOption func(p *proxy)

func InjectProxyTimeout(t time.Duration) ProxyOption {
	return func(p *proxy) {
		p.timeout = t
	}
}

func InjectKubeconfigPaths(paths []string) ProxyOption {
	return func(p *proxy) {
		p.configLoadingRules.Precedence = paths
	}
}

func newProxy(kubeconfig Kubeconfig, opts ...ProxyOption) Proxy {
	// If a kubeconfig file isn't provided, find one in the standard locations.
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig.Path != "" {
		rules.ExplicitPath = kubeconfig.Path
	}
	p := &proxy{
		kubeconfig:         kubeconfig,
		timeout:            30 * time.Second,
		configLoadingRules: rules,
	}

	for _, o := range opts {
		o(p)
	}

	return p
}

func (k *proxy) newClientSet() (*kubernetes.Clientset, error) {
	config, err := k.GetConfig()
	if err != nil {
		return nil, err
	}

	var cs *kubernetes.Clientset
	// Nb. The operation is wrapped in a retry loop to make newClientSet more resilient to temporary connection problems.
	connectBackoff := newConnectBackoff()
	if err := retryWithExponentialBackoff(connectBackoff, func() error {
		var err error
		cs, err = kubernetes.NewForConfig(config)
		if err != nil {
			return err
		}
		return nil
	}); err != nil {
		return nil, errors.Wrap(err, "failed to create the client-go client")
	}

	return cs, nil
}
