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

// MemoryReader provider a reader implementation backed by a map.
type MemoryReader struct {
	variables  map[string]string
	providers  []configProvider
	imageMetas map[string]imageMeta
}

func (f *MemoryReader) Init(config string) error {
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

func (f *MemoryReader) Get(key string) (string, error) {
	if val, ok := f.variables[key]; ok {
		return val, nil
	}
	return "", errors.Errorf("value for variable %q is not set", key)
}

func (f *MemoryReader) Set(key, value string) {
	f.variables[key] = value
}

func (f *MemoryReader) UnmarshalKey(key string, rawval interface{}) error {
	data, err := f.Get(key)
	if err != nil {
		return nil
	}
	return yaml.Unmarshal([]byte(data), rawval)
}

func NewMemoryReader() *MemoryReader {
	return &MemoryReader{
		variables:  map[string]string{},
		imageMetas: map[string]imageMeta{},
	}
}

func (f *MemoryReader) WithVar(key, value string) *MemoryReader {
	f.variables[key] = value
	return f
}

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

func (f *MemoryReader) WithImageMeta(component, repository, tag string) *MemoryReader {
	f.imageMetas[component] = imageMeta{
		Repository: repository,
		Tag:        tag,
	}

	yaml, _ := yaml.Marshal(f.imageMetas)
	f.variables["images"] = string(yaml)

	return f
}
