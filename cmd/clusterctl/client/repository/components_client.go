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

package repository

import (
	"github.com/pkg/errors"
	"sigs.k8s.io/cluster-api/cmd/clusterctl/client/config"
	yaml "sigs.k8s.io/cluster-api/cmd/clusterctl/client/yamlprocessor"
	logf "sigs.k8s.io/cluster-api/cmd/clusterctl/log"
)

// ComponentsClient has methods to work with yaml file for generating provider components.
// Assets are yaml files to be used for deploying a provider into a management cluster.
type ComponentsClient interface {
	Get(options ComponentsOptions) (Components, error)
}

// componentsClient implements ComponentsClient.
type componentsClient struct {
	provider     config.Provider
	repository   Repository
	configClient config.Client
	processor    yaml.Processor
}

// ensure componentsClient implements ComponentsClient.
var _ ComponentsClient = &componentsClient{}

// newComponentsClient returns a componentsClient.
func newComponentsClient(provider config.Provider, repository Repository, configClient config.Client) *componentsClient {
	return &componentsClient{
		provider:     provider,
		repository:   repository,
		configClient: configClient,
		processor:    yaml.NewSimpleProcessor(),
	}
}

// Get returns the components from a repository.
func (f *componentsClient) Get(options ComponentsOptions) (Components, error) {
	log := logf.Log

	// If the request does not target a specific version, read from the default repository version that is derived from the repository URL, e.g. latest.
	if options.Version == "" {
		options.Version = f.repository.DefaultVersion()
	}

	// Retrieve the path where the path is stored
	path := f.repository.ComponentsPath()

	// Read the component YAML, reading the local override file if it exists, otherwise read from the provider repository
	file, err := getLocalOverride(&newOverrideInput{
		configVariablesClient: f.configClient.Variables(),
		provider:              f.provider,
		version:               options.Version,
		filePath:              path,
	})
	if err != nil {
		return nil, err
	}

	if file == nil {
		log.V(5).Info("Fetching", "File", path, "Provider", f.provider.Name(), "Type", f.provider.Type(), "Version", options.Version)
		file, err = f.repository.GetFile(options.Version, path)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to read %q from provider's repository %q", path, f.provider.ManifestLabel())
		}
	} else {
		log.Info("Using", "Override", path, "Provider", f.provider.ManifestLabel(), "Version", options.Version)
	}

	return NewComponents(ComponentsInput{f.provider, f.configClient, f.processor, file, nil, nil, options})
}
