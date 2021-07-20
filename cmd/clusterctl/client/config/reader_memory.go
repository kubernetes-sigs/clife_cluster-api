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

package config

import (
	"github.com/pkg/errors"
	clusterctlv1 "sigs.k8s.io/cluster-api/cmd/clusterctl/api/v1alpha3"
	"sigs.k8s.io/yaml"
)

// MemoryReader provides a reader implementation backed by a map.
// This is to be used by the operator to place config from a secret
// and the ProviderSpec.Fetchconfig.
type MemoryReader struct {
	variables  map[string]string
	providers  []configProvider
	imageMetas map[string]imageMeta
}

// NewMemoryReader return a new MemoryReader.
func NewMemoryReader() *MemoryReader {
	return &MemoryReader{
		variables:  map[string]string{},
		imageMetas: map[string]imageMeta{},
		providers:  []configProvider{},
	}
}

// Init initialize the reader.
func (f *MemoryReader) Init(_ string) error {
	data, err := yaml.Marshal(f.providers)
	if err != nil {
		return err
	}
	f.variables["providers"] = string(data)
	data, err = yaml.Marshal(f.imageMetas)
	if err != nil {
		return err
	}
	f.variables["images"] = string(data)
	return nil
}

// Get get a value for the given key.
func (f *MemoryReader) Get(key string) (string, error) {
	if val, ok := f.variables[key]; ok {
		return val, nil
	}
	return "", errors.Errorf("value for variable %q is not set", key)
}

// Set set a value for the given key.
func (f *MemoryReader) Set(key, value string) {
	f.variables[key] = value
}

// UnmarshalKey get a value for the given key, then unmarshal it.
func (f *MemoryReader) UnmarshalKey(key string, rawval interface{}) error {
	data, err := f.Get(key)
	if err != nil {
		return nil // nolint:nilerr // We expect to not error if the key is not present
	}
	return yaml.Unmarshal([]byte(data), rawval)
}

// WithProvider adds the given provider to the "providers" map entry.
func (f *MemoryReader) WithProvider(name string, ttype clusterctlv1.ProviderType, url string) *MemoryReader {
	f.providers = append(f.providers, configProvider{
		Name: name,
		URL:  url,
		Type: ttype,
	})

	yaml, _ := yaml.Marshal(f.providers)
	f.variables["providers"] = string(yaml)

	return f
}

// WithImageMeta adds the given image to the "images" map entry.
func (f *MemoryReader) WithImageMeta(component, repository, tag string) *MemoryReader {
	f.imageMetas[component] = imageMeta{
		Repository: repository,
		Tag:        tag,
	}

	yaml, _ := yaml.Marshal(f.imageMetas)
	f.variables["images"] = string(yaml)

	return f
}
