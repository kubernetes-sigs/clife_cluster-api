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
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/util/version"
	"sigs.k8s.io/cluster-api/cmd/clusterctl/client/config"
)

const (
	latestVersionTag = "latest"
)

// localRepository provides support for providers located on the local filesystem.
// As part of the provider object, the URL is expected to contain the absolute
// path to the components yaml on the local filesystem.
// To support different versions, the directories containing provider
// specific data must adhere to the following layout:
// [file://]{basepath}/{provider-label}/{version}/{components.yaml}
//
// (1): {provider-label} must match the value returned by Provider.ManifestLabel()
// (2): {version} must obey the syntax and semantics of the "Semantic Versioning"
// specification (http://semver.org/); however, "latest" is also an acceptable value.
//
// Concrete example (linux):
// /home/user/go/src/sigs.k8s.io/infrastructure-aws/v0.4.7/infrastructure-components.yaml
// basepath: /home/user/go/src/sigs.k8s.io
// provider-label: infrastructure-aws
// version: v0.4.7
// components.yaml: infrastructure-components.yaml
//
// Concrete example (windows):
// NB. the input is an URI specification, not a windows path. see https://blogs.msdn.microsoft.com/ie/2006/12/06/file-uris-in-windows/ for more details
// /C:/cluster-api/out/repo/infrastructure-docker/latest/infrastructure-components.yaml
// basepath: C:\cluster-api\out\repo
// provider-label: infrastructure-docker
// version: v0.3.0 (whatever latest resolve to)
// components.yaml: infrastructure-components.yaml.
type localRepository struct {
	providerConfig        config.Provider
	configVariablesClient config.VariablesClient
	basepath              string
	providerLabel         string
	defaultVersion        string
	componentsPath        string
}

var _ Repository = &localRepository{}

// DefaultVersion returns the default version for the local repository.
func (r *localRepository) DefaultVersion() string {
	return r.defaultVersion
}

// RootPath returns the empty string as it is not applicable to local repositories.
func (r *localRepository) RootPath() string {
	return ""
}

// ComponentsPath returns the path to the components file for the local repository.
func (r *localRepository) ComponentsPath() string {
	return r.componentsPath
}

// GetFile returns a file for a given provider version.
func (r *localRepository) GetFile(version, fileName string) ([]byte, error) {
	var err error

	if version == latestVersionTag {
		version, err = LatestRelease(r)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to get the latest release")
		}
	} else if version == "" {
		version = r.defaultVersion
	}

	absolutePath := filepath.Join(r.basepath, r.providerLabel, version, r.RootPath(), fileName)

	f, err := os.Stat(absolutePath)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read file %q from local release %s", absolutePath, version)
	}
	if f.IsDir() {
		return nil, errors.Errorf("invalid path: file %q is actually a directory %q", fileName, absolutePath)
	}
	content, err := os.ReadFile(absolutePath)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read file %q from local release %s", absolutePath, version)
	}
	return content, nil
}

// GetVersions returns the list of versions that are available for a local repository.
func (r *localRepository) GetVersions() ([]string, error) {
	// get all the sub-directories under {basepath}/{provider-id}/
	releasesPath := filepath.Join(r.basepath, r.providerLabel)
	files, err := os.ReadDir(releasesPath)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list release directories")
	}
	versions := []string{}
	for _, f := range files {
		if !f.IsDir() {
			continue
		}
		r := f.Name()
		_, err := version.ParseSemantic(r)
		if err != nil {
			// discard releases with tags that are not a valid semantic versions (the user can point explicitly to such releases)
			continue
		}
		versions = append(versions, r)
	}
	return versions, nil
}

// newLocalRepository returns a new localRepository.
func newLocalRepository(providerConfig config.Provider, configVariablesClient config.VariablesClient) (*localRepository, error) {
	url, err := url.Parse(providerConfig.URL())
	if err != nil {
		return nil, errors.Wrap(err, "invalid url")
	}

	// gets the path part of the url and check it is an absolute path
	path := url.Path
	if runtime.GOOS == "windows" {
		// in case of windows, we should take care of removing the additional / which is required by the URI standard
		// for windows local paths. see https://blogs.msdn.microsoft.com/ie/2006/12/06/file-uris-in-windows/ for more details.
		// Encoded file paths are not required in Windows 10 versions <1803 and are unsupported in Windows 10 >=1803
		// https://support.microsoft.com/en-us/help/4467268/url-encoded-unc-paths-not-url-decoded-in-windows-10-version-1803-later
		path = strings.TrimPrefix(path, "/")
		path = filepath.FromSlash(path)
	}
	if !filepath.IsAbs(path) {
		return nil, errors.Errorf("invalid path: path %q must be an absolute path", providerConfig.URL())
	}

	// Extracts provider-name, version, componentsPath from the url
	// NB. format is {basepath}/{provider-name}/{version}/{components.yaml}
	urlSplit := strings.Split(path, string(os.PathSeparator))
	if len(urlSplit) < 3 {
		return nil, errors.Errorf("invalid path: path should be in the form {basepath}/{provider-name}/{version}/{components.yaml}")
	}

	componentsPath := urlSplit[len(urlSplit)-1]
	defaultVersion := urlSplit[len(urlSplit)-2]
	if defaultVersion != latestVersionTag {
		_, err = version.ParseSemantic(defaultVersion)
		if err != nil {
			return nil, errors.Errorf("invalid version: %q. Version must obey the syntax and semantics of the \"Semantic Versioning\" specification (http://semver.org/) and path format {basepath}/{provider-name}/{version}/{components.yaml}", defaultVersion)
		}
	}
	providerID := urlSplit[len(urlSplit)-3]
	if providerID != providerConfig.ManifestLabel() {
		return nil, errors.Errorf("invalid path: path %q must contain provider %q in the format {basepath}/{provider-label}/{version}/{components.yaml}", providerConfig.URL(), providerConfig.ManifestLabel())
	}

	// Get the base path, by trimming the last parts which are treated as a separated fields
	var basePath string
	basePath = strings.TrimSuffix(path, filepath.Join(providerID, defaultVersion, componentsPath))
	basePath = filepath.Clean(basePath)

	repo := &localRepository{
		providerConfig:        providerConfig,
		configVariablesClient: configVariablesClient,
		basepath:              basePath,
		providerLabel:         providerID,
		defaultVersion:        defaultVersion,
		componentsPath:        componentsPath,
	}

	if defaultVersion == latestVersionTag {
		repo.defaultVersion, err = LatestContractRelease(repo, clusterv1.GroupVersion.Version)
		if err != nil {
			return nil, errors.Wrap(err, "failed to get latest version")
		}
	}
	return repo, nil
}
