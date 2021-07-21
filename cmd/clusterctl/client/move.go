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

package client

import (
	"sigs.k8s.io/cluster-api/cmd/clusterctl/client/cluster"
)

// MoveOptions carries the options supported by move.
type MoveOptions struct {
	// FromKubeconfig defines the kubeconfig to use for accessing the source management cluster. If empty,
	// default rules for kubeconfig discovery will be used.
	FromKubeconfig Kubeconfig

	// ToKubeconfig defines the kubeconfig to use for accessing the target management cluster. If empty,
	// default rules for kubeconfig discovery will be used.
	ToKubeconfig Kubeconfig

	// Namespace where the objects describing the workload cluster exists. If unspecified, the current
	// namespace will be used.
	Namespace string

	// DryRun means the move action is a dry run, no real action will be performed
	DryRun bool
}

func (c *clusterctlClient) Move(options MoveOptions) error {
	// Get the client for interacting with the source management cluster.
	fromCluster, err := c.clusterClientFactory(ClusterClientFactoryInput{Kubeconfig: options.FromKubeconfig})
	if err != nil {
		return err
	}

	// Ensure this command only runs against management clusters with the current Cluster API contract.
	if err := fromCluster.ProviderInventory().CheckCAPIContract(); err != nil {
		return err
	}

	// Ensures the custom resource definitions required by clusterctl are in place.
	if err := fromCluster.ProviderInventory().EnsureCustomResourceDefinitions(); err != nil {
		return err
	}

	var toCluster cluster.Client
	if !options.DryRun {
		// Get the client for interacting with the target management cluster.
		toCluster, err = c.clusterClientFactory(ClusterClientFactoryInput{Kubeconfig: options.ToKubeconfig})
		if err != nil {
			return err
		}
		// TODO: do we also want to check this in the dry-run case?
		if err := toCluster.Proxy().ValidateKubernetesMaxVersion(); err != nil {
			return err
		}

		// Ensure this command only runs against management clusters with the current Cluster API contract.
		if err := toCluster.ProviderInventory().CheckCAPIContract(); err != nil {
			return err
		}

		// Ensures the custom resource definitions required by clusterctl are in place
		if err := toCluster.ProviderInventory().EnsureCustomResourceDefinitions(); err != nil {
			return err
		}
	}

	// If the option specifying the Namespace is empty, try to detect it.
	if options.Namespace == "" {
		currentNamespace, err := fromCluster.Proxy().CurrentNamespace()
		if err != nil {
			return err
		}
		options.Namespace = currentNamespace
	}

	if err := fromCluster.ObjectMover().Move(options.Namespace, toCluster, options.DryRun); err != nil {
		return err
	}

	return nil
}
