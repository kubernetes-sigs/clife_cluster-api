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
	"net/http"
	"testing"

	. "github.com/onsi/gomega"

	"github.com/google/go-github/v33/github"
	"k8s.io/utils/pointer"
	clusterctlv1 "sigs.k8s.io/cluster-api/cmd/clusterctl/api/v1alpha3"
	"sigs.k8s.io/cluster-api/cmd/clusterctl/client/config"
	"sigs.k8s.io/cluster-api/cmd/clusterctl/internal/test"
)

func Test_githubRepository_newGitHubRepository(t *testing.T) {
	type field struct {
		providerConfig config.Provider
		variableClient config.VariablesClient
	}
	tests := []struct {
		name    string
		field   field
		want    *gitHubRepository
		wantErr bool
	}{
		{
			name: "can create a new GitHub repo",
			field: field{
				providerConfig: config.NewProvider("test", "https://github.com/o/r1/releases/v0.4.1/path", clusterctlv1.CoreProviderType),
				variableClient: test.NewFakeVariableClient(),
			},
			want: &gitHubRepository{
				providerConfig:           config.NewProvider("test", "https://github.com/o/r1/releases/v0.4.1/path", clusterctlv1.CoreProviderType),
				configVariablesClient:    test.NewFakeVariableClient(),
				authenticatingHTTPClient: nil,
				owner:                    "o",
				repository:               "r1",
				defaultVersion:           "v0.4.1",
				rootPath:                 ".",
				componentsPath:           "path",
				injectClient:             nil,
			},
			wantErr: false,
		},
		{
			name: "missing variableClient",
			field: field{
				providerConfig: config.NewProvider("test", "https://github.com/o/r1/releases/v0.4.1/path", clusterctlv1.CoreProviderType),
				variableClient: nil,
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "provider url is not valid",
			field: field{
				providerConfig: config.NewProvider("test", "%gh&%ij", clusterctlv1.CoreProviderType),
				variableClient: test.NewFakeVariableClient(),
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "provider url should be in https",
			field: field{
				providerConfig: config.NewProvider("test", "http://github.com/blabla", clusterctlv1.CoreProviderType),
				variableClient: test.NewFakeVariableClient(),
			},
			want:    nil,
			wantErr: true,
		},
		{
			name: "provider url should be in github",
			field: field{
				providerConfig: config.NewProvider("test", "http://gitlab.com/blabla", clusterctlv1.CoreProviderType),
				variableClient: test.NewFakeVariableClient(),
			},
			want:    &gitHubRepository{},
			wantErr: true,
		},
		{
			name: "provider url should be in https://github.com/{owner}/{Repository}/%s/{latest|version-tag}/{componentsClient.yaml} format",
			field: field{
				providerConfig: config.NewProvider("test", "https://github.com/dd/", clusterctlv1.CoreProviderType),
				variableClient: test.NewFakeVariableClient(),
			},
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			resetCaches()

			gitHub, err := newGitHubRepository(tt.field.providerConfig, tt.field.variableClient)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}

			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(gitHub).To(Equal(tt.want))
		})
	}
}

func Test_githubRepository_getComponentsPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		rootPath string
		want     string
	}{
		{
			name:     "get the file name",
			path:     "github.com/o/r/releases/v0.4.1/file.yaml",
			rootPath: "github.com/o/r/releases/v0.4.1/",
			want:     "file.yaml",
		},
		{
			name:     "trim github.com",
			path:     "github.com/o/r/releases/v0.4.1/file.yaml",
			rootPath: "github.com",
			want:     "o/r/releases/v0.4.1/file.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			resetCaches()

			g.Expect(getComponentsPath(tt.path, tt.rootPath)).To(Equal(tt.want))
		})
	}
}

func Test_githubRepository_getFile(t *testing.T) {
	client, mux, teardown := test.NewFakeGitHub()
	defer teardown()

	providerConfig := config.NewProvider("test", "https://github.com/o/r/releases/v0.4.1/file.yaml", clusterctlv1.CoreProviderType)

	// test.NewFakeGitHub and handler for returning a fake release
	mux.HandleFunc("/repos/o/r/releases/tags/v0.4.1", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		fmt.Fprint(w, `{"id":13, "tag_name": "v0.4.1", "assets": [{"id": 1, "name": "file.yaml"}] }`)
	})

	// test.NewFakeGitHub an handler for returning a fake release asset
	mux.HandleFunc("/repos/o/r/releases/assets/1", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment; filename=file.yaml")
		fmt.Fprint(w, "content")
	})

	configVariablesClient := test.NewFakeVariableClient()

	tests := []struct {
		name     string
		release  string
		fileName string
		want     []byte
		wantErr  bool
	}{
		{
			name:     "Release and file exist",
			release:  "v0.4.1",
			fileName: "file.yaml",
			want:     []byte("content"),
			wantErr:  false,
		},
		{
			name:     "Release does not exist",
			release:  "not-a-release",
			fileName: "file.yaml",
			want:     nil,
			wantErr:  true,
		},
		{
			name:     "File does not exist",
			release:  "v0.4.1",
			fileName: "404.file",
			want:     nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			resetCaches()

			gitHub, err := newGitHubRepository(providerConfig, configVariablesClient, injectGithubClient(client))
			g.Expect(err).NotTo(HaveOccurred())

			got, err := gitHub.GetFile(tt.release, tt.fileName)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}

			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(got).To(Equal(tt.want))
		})
	}
}

func Test_gitHubRepository_getVersions(t *testing.T) {
	client, mux, teardown := test.NewFakeGitHub()
	defer teardown()

	// setup an handler for returning 5 fake releases
	mux.HandleFunc("/repos/o/r1/releases", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		fmt.Fprint(w, `[`)
		fmt.Fprint(w, `{"id":1, "tag_name": "v0.4.0"},`)
		fmt.Fprint(w, `{"id":2, "tag_name": "v0.4.1"},`)
		fmt.Fprint(w, `{"id":3, "tag_name": "v0.4.2"},`)
		fmt.Fprint(w, `{"id":4, "tag_name": "v0.4.3-alpha"},`) // prerelease
		fmt.Fprint(w, `{"id":5, "tag_name": "foo"}`)           // no semantic version tag
		fmt.Fprint(w, `]`)
	})

	configVariablesClient := test.NewFakeVariableClient()

	type field struct {
		providerConfig config.Provider
	}
	tests := []struct {
		name    string
		field   field
		want    []string
		wantErr bool
	}{
		{
			name: "Get versions",
			field: field{
				providerConfig: config.NewProvider("test", "https://github.com/o/r1/releases/v0.4.1/path", clusterctlv1.CoreProviderType),
			},
			want:    []string{"v0.4.0", "v0.4.1", "v0.4.2", "v0.4.3-alpha"},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			resetCaches()

			gitHub, err := newGitHubRepository(tt.field.providerConfig, configVariablesClient, injectGithubClient(client))
			g.Expect(err).NotTo(HaveOccurred())

			got, err := gitHub.getVersions()
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).NotTo(HaveOccurred())

			g.Expect(got).To(ConsistOf(tt.want))
		})
	}
}

func Test_gitHubRepository_getLatestContractRelease(t *testing.T) {
	client, mux, teardown := test.NewFakeGitHub()
	defer teardown()

	// setup an handler for returning 3 fake releases
	mux.HandleFunc("/repos/o/r1/releases", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		fmt.Fprint(w, `[`)
		fmt.Fprint(w, `{"id":1, "tag_name": "v0.5.0", "assets": [{"id": 1, "name": "metadata.yaml"}]},`)
		fmt.Fprint(w, `{"id":2, "tag_name": "v0.4.0", "assets": [{"id": 1, "name": "metadata.yaml"}]},`)
		fmt.Fprint(w, `{"id":3, "tag_name": "v0.3.2", "assets": [{"id": 1, "name": "metadata.yaml"}]},`)
		fmt.Fprint(w, `{"id":4, "tag_name": "v0.3.1", "assets": [{"id": 1, "name": "metadata.yaml"}]}`)
		fmt.Fprint(w, `]`)
	})

	// test.NewFakeGitHub and handler for returning a fake release
	mux.HandleFunc("/repos/o/r1/releases/tags/v0.5.0", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		fmt.Fprint(w, `{"id":13, "tag_name": "v0.5.0", "assets": [{"id": 1, "name": "metadata.yaml"}] }`)
	})

	// test.NewFakeGitHub an handler for returning a fake release metadata file
	mux.HandleFunc("/repos/o/r1/releases/assets/1", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment; filename=metadata.yaml")
		fmt.Fprint(w, "apiVersion: clusterctl.cluster.x-k8s.io/v1alpha3\nreleaseSeries:\n  - major: 0\n    minor: 4\n    contract: v1alpha4\n  - major: 0\n    minor: 5\n    contract: v1alpha4\n  - major: 0\n    minor: 3\n    contract: v1alpha3\n")
	})

	configVariablesClient := test.NewFakeVariableClient()

	type field struct {
		providerConfig config.Provider
	}
	tests := []struct {
		name     string
		field    field
		contract string
		want     string
		wantErr  bool
	}{
		{
			name: "Get latest release if it matches the contract",
			field: field{
				providerConfig: config.NewProvider("test", "https://github.com/o/r1/releases/latest/path", clusterctlv1.CoreProviderType),
			},
			contract: "v1alpha4",
			want:     "v0.5.0",
			wantErr:  false,
		},
		{
			name: "Get previous release if the latest doesn't match the contract",
			field: field{
				providerConfig: config.NewProvider("test", "https://github.com/o/r1/releases/latest/path", clusterctlv1.CoreProviderType),
			},
			contract: "v1alpha3",
			want:     "v0.3.2",
			wantErr:  false,
		},
		{
			name: "Return the latest release if the contract doesn't exist",
			field: field{
				providerConfig: config.NewProvider("test", "https://github.com/o/r1/releases/latest/path", clusterctlv1.CoreProviderType),
			},
			want:     "v0.5.0",
			contract: "foo",
			wantErr:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			resetCaches()

			gRepo, err := newGitHubRepository(tt.field.providerConfig, configVariablesClient, injectGithubClient(client))
			g.Expect(err).NotTo(HaveOccurred())

			got, err := LatestContractRelease(gRepo, tt.contract)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(got).To(Equal(tt.want))
		})
	}
}

func Test_gitHubRepository_getLatestRelease(t *testing.T) {
	client, mux, teardown := test.NewFakeGitHub()
	defer teardown()

	// setup an handler for returning 4 fake releases
	mux.HandleFunc("/repos/o/r1/releases", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		fmt.Fprint(w, `[`)
		fmt.Fprint(w, `{"id":1, "tag_name": "v0.4.1"},`)
		fmt.Fprint(w, `{"id":2, "tag_name": "v0.4.2"},`)
		fmt.Fprint(w, `{"id":3, "tag_name": "v0.4.3-alpha"},`) // prerelease
		fmt.Fprint(w, `{"id":4, "tag_name": "foo"}`)           // no semantic version tag
		fmt.Fprint(w, `]`)
	})

	// setup an handler for returning no releases
	mux.HandleFunc("/repos/o/r2/releases", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		// no releases
	})

	// setup an handler for returning fake prereleases only
	mux.HandleFunc("/repos/o/r3/releases", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		fmt.Fprint(w, `[`)
		fmt.Fprint(w, `{"id":1, "tag_name": "v0.1.0-alpha.0"},`)
		fmt.Fprint(w, `{"id":2, "tag_name": "v0.1.0-alpha.1"},`)
		fmt.Fprint(w, `{"id":3, "tag_name": "v0.1.0-alpha.2"}`)
		fmt.Fprint(w, `]`)
	})

	configVariablesClient := test.NewFakeVariableClient()

	type field struct {
		providerConfig config.Provider
	}
	tests := []struct {
		name    string
		field   field
		want    string
		wantErr bool
	}{
		{
			name: "Get latest release, ignores pre-release version",
			field: field{
				providerConfig: config.NewProvider("test", "https://github.com/o/r1/releases/latest/path", clusterctlv1.CoreProviderType),
			},
			want:    "v0.4.2",
			wantErr: false,
		},
		{
			name: "Fails, when no release found",
			field: field{
				providerConfig: config.NewProvider("test", "https://github.com/o/r2/releases/v0.4.1/path", clusterctlv1.CoreProviderType),
			},
			want:    "",
			wantErr: true,
		},
		{
			name: "Falls back to latest prerelease when no official release present",
			field: field{
				providerConfig: config.NewProvider("test", "https://github.com/o/r3/releases/latest/path", clusterctlv1.CoreProviderType),
			},
			want:    "v0.1.0-alpha.2",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			resetCaches()

			gRepo, err := newGitHubRepository(tt.field.providerConfig, configVariablesClient, injectGithubClient(client))
			g.Expect(err).NotTo(HaveOccurred())

			got, err := LatestRelease(gRepo)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(got).To(Equal(tt.want))
			g.Expect(gRepo.defaultVersion).To(Equal(tt.want))
		})
	}
}

func Test_gitHubRepository_getLatestPatchRelease(t *testing.T) {
	client, mux, teardown := test.NewFakeGitHub()
	defer teardown()

	// setup an handler for returning 3 fake releases
	mux.HandleFunc("/repos/o/r1/releases", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		fmt.Fprint(w, `[`)
		fmt.Fprint(w, `{"id":1, "tag_name": "v0.4.0"},`)
		fmt.Fprint(w, `{"id":2, "tag_name": "v0.3.2"},`)
		fmt.Fprint(w, `{"id":3, "tag_name": "v1.3.2"}`)
		fmt.Fprint(w, `]`)
	})

	major0 := uint(0)
	minor3 := uint(3)
	minor4 := uint(4)

	configVariablesClient := test.NewFakeVariableClient()

	type field struct {
		providerConfig config.Provider
	}
	tests := []struct {
		name    string
		field   field
		major   *uint
		minor   *uint
		want    string
		wantErr bool
	}{
		{
			name: "Get latest patch release, no Major/Minor specified",
			field: field{
				providerConfig: config.NewProvider("test", "https://github.com/o/r1/releases/latest/path", clusterctlv1.CoreProviderType),
			},
			minor:   nil,
			major:   nil,
			want:    "v1.3.2",
			wantErr: false,
		},
		{
			name: "Get latest patch release, for Major 0 and Minor 3",
			field: field{
				providerConfig: config.NewProvider("test", "https://github.com/o/r1/releases/latest/path", clusterctlv1.CoreProviderType),
			},
			major:   &major0,
			minor:   &minor3,
			want:    "v0.3.2",
			wantErr: false,
		},
		{
			name: "Get latest patch release, for Major 0 and Minor 4",
			field: field{
				providerConfig: config.NewProvider("test", "https://github.com/o/r1/releases/latest/path", clusterctlv1.CoreProviderType),
			},
			major:   &major0,
			minor:   &minor4,
			want:    "v0.4.0",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			resetCaches()

			gRepo, err := newGitHubRepository(tt.field.providerConfig, configVariablesClient, injectGithubClient(client))
			g.Expect(err).NotTo(HaveOccurred())

			got, err := LatestPatchRelease(gRepo, tt.major, tt.minor)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(got).To(Equal(tt.want))
		})
	}
}

func Test_gitHubRepository_getReleaseByTag(t *testing.T) {
	client, mux, teardown := test.NewFakeGitHub()
	defer teardown()

	providerConfig := config.NewProvider("test", "https://github.com/o/r/releases/v0.4.1/path", clusterctlv1.CoreProviderType)

	// setup and handler for returning a fake release
	mux.HandleFunc("/repos/o/r/releases/tags/foo", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		fmt.Fprint(w, `{"id":13, "tag_name": "v0.4.1"}`)
	})

	configVariablesClient := test.NewFakeVariableClient()

	type args struct {
		tag string
	}
	tests := []struct {
		name        string
		args        args
		wantTagName *string
		wantErr     bool
	}{
		{
			name: "Return existing version",
			args: args{
				tag: "foo",
			},
			wantTagName: pointer.StringPtr("v0.4.1"),
			wantErr:     false,
		},
		{
			name: "Fails if version does not exists",
			args: args{
				tag: "bar",
			},
			wantTagName: nil,
			wantErr:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			resetCaches()

			gRepo, err := newGitHubRepository(providerConfig, configVariablesClient, injectGithubClient(client))
			g.Expect(err).NotTo(HaveOccurred())

			got, err := gRepo.getReleaseByTag(tt.args.tag)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).NotTo(HaveOccurred())

			if tt.wantTagName == nil {
				g.Expect(got).To(BeNil())
				return
			}

			g.Expect(got.TagName).To(Equal(tt.wantTagName))
		})
	}
}

func Test_gitHubRepository_downloadFilesFromRelease(t *testing.T) {
	client, mux, teardown := test.NewFakeGitHub()
	defer teardown()

	providerConfig := config.NewProvider("test", "https://github.com/o/r/releases/v0.4.1/file.yaml", clusterctlv1.CoreProviderType) // tree/master/path not relevant for the test

	// test.NewFakeGitHub an handler for returning a fake release asset
	mux.HandleFunc("/repos/o/r/releases/assets/1", func(w http.ResponseWriter, r *http.Request) {
		testMethod(t, r, "GET")
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment; filename=file.yaml")
		fmt.Fprint(w, "content")
	})

	configVariablesClient := test.NewFakeVariableClient()

	var id1 int64 = 1
	var id2 int64 = 2
	tagName := "vO.3.3"
	file := "file.yaml"

	type args struct {
		release  *github.RepositoryRelease
		fileName string
	}
	tests := []struct {
		name    string
		args    args
		want    []byte
		wantErr bool
	}{
		{
			name: "Pass if file exists",
			args: args{
				release: &github.RepositoryRelease{
					TagName: &tagName,
					Assets: []*github.ReleaseAsset{
						{
							ID:   &id1,
							Name: &file,
						},
					},
				},
				fileName: file,
			},
			want:    []byte("content"),
			wantErr: false,
		},
		{
			name: "Fails if file does not exists",
			args: args{
				release: &github.RepositoryRelease{
					TagName: &tagName,
					Assets: []*github.ReleaseAsset{
						{
							ID:   &id1,
							Name: &file,
						},
					},
				},
				fileName: "another file",
			},
			wantErr: true,
		},
		{
			name: "Fails if file does not exists",
			args: args{
				release: &github.RepositoryRelease{
					TagName: &tagName,
					Assets: []*github.ReleaseAsset{
						{
							ID:   &id2, // id does not match any file (this should not happen)
							Name: &file,
						},
					},
				},
				fileName: "another file",
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			resetCaches()

			gRepo, err := newGitHubRepository(providerConfig, configVariablesClient, injectGithubClient(client))
			g.Expect(err).NotTo(HaveOccurred())

			got, err := gRepo.downloadFilesFromRelease(tt.args.release, tt.args.fileName)
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}

			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(got).To(Equal(tt.want))
		})
	}
}

func testMethod(t *testing.T, r *http.Request, want string) {
	if got := r.Method; got != want {
		t.Errorf("Request method: %v, want %v", got, want)
	}
}

// resetCaches is called repeatedly throughout tests to help avoid cross-test pollution.
func resetCaches() {
	cacheVersions = map[string][]string{}
	cacheReleases = map[string]*github.RepositoryRelease{}
	cacheFiles = map[string][]byte{}
}
