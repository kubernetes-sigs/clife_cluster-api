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
	"fmt"

	"github.com/pkg/errors"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
)

// MemoryRepository contains an instance of the repository data.
type MemoryRepository struct {
	defaultVersion string
	rootPath       string
	componentsPath string
	versions       map[string]bool
	files          map[string][]byte
}

// DefaultVersion returns defaultVersion field of MemoryRepository struct.
func (f *MemoryRepository) DefaultVersion() string {
	return f.defaultVersion
}

// RootPath returns rootPath field of MemoryRepository struct.
func (f *MemoryRepository) RootPath() string {
	return f.rootPath
}

// ComponentsPath returns componentsPath field of MemoryRepository struct.
func (f *MemoryRepository) ComponentsPath() string {
	return f.componentsPath
}

// GetFile returns a file for a given provider version.
func (f *MemoryRepository) GetFile(version string, path string) ([]byte, error) {
	if version == "" {
		version = f.defaultVersion
	}
	if version == "latest" {
		var err error
		version, err = LatestRelease(f)
		if err != nil {
			return nil, err
		}
	}
	if _, ok := f.versions[version]; !ok {
		return nil, errors.Errorf("unable to get files for version %s", version)
	}

	for p, c := range f.files {
		if p == vpath(version, path) {
			return c, nil
		}
	}
	return nil, errors.Errorf("unable to get file %s for version %s", path, version)
}

// GetVersions returns the list of versions that are available.
func (f *MemoryRepository) GetVersions() ([]string, error) {
	v := make([]string, 0, len(f.versions))
	for k := range f.versions {
		v = append(v, k)
	}
	return v, nil
}

// NewMemoryRepository returns a new MemoryReposity instance.
func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		versions: map[string]bool{},
		files:    map[string][]byte{},
	}
}

// WithPaths allows setting of the rootPath and componentsPath fields.
func (f *MemoryRepository) WithPaths(rootPath, componentsPath string) *MemoryRepository {
	f.rootPath = rootPath
	f.componentsPath = componentsPath
	return f
}

// WithFile allows setting of a file for a given version.
func (f *MemoryRepository) WithFile(version, path string, content []byte) *MemoryRepository {
	f.versions[version] = true
	f.files[vpath(version, path)] = content

	if _, ok := f.files[vpath(version, "metadata.yaml")]; ok {
		f.updateVersions()
	}
	return f
}

func (f *MemoryRepository) updateVersions() {
	defaultVersion, err := LatestContractRelease(f, clusterv1.GroupVersion.Version)
	if err != nil {
		return
	}
	f.defaultVersion = defaultVersion
}

func vpath(version string, path string) string {
	return fmt.Sprintf("%s/%s", version, path)
}
