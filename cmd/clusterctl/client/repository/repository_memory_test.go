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
	"testing"

	. "github.com/onsi/gomega"
)

func Test_memoryRepository(t *testing.T) {
	metadata := `
apiVersion: clusterctl.cluster.x-k8s.io/v1alpha3
kind: Metadata
releaseSeries:
- major: 1
  minor: 0
  contract: v1alpha1
- major: 2
  minor: 0
  contract: v1alpha3`

	type want struct {
		versions       []string
		defaultVersion []byte
		latestVersion  []byte
	}
	tests := []struct {
		name        string
		addTestData func(mr *MemoryRepository)
		fileVersion string
		want        want
		wantErr     bool
	}{
		{
			name: "Get the only release available from release directory",
			addTestData: func(mr *MemoryRepository) {
				mr.WithFile("v1.0.0", "metadata.yaml", []byte(metadata))
				mr.WithFile("v1.0.0", "components.yaml", []byte("v1.0.0"))
				mr.WithPaths("", "components.yaml")
			},
			want: want{
				versions:       []string{"v1.0.0"},
				defaultVersion: []byte("v1.0.0"),
				latestVersion:  []byte("v1.0.0"),
			},
			wantErr: false,
		},
		{
			name: "Get all valid releases available from release directory",
			addTestData: func(mr *MemoryRepository) {
				mr.WithFile("v1.0.0", "components.yaml", []byte("v1.0.0"))
				mr.WithFile("v1.0.0", "metadata.yaml", []byte(metadata))
				mr.WithFile("v1.0.1", "components.yaml", []byte("v1.0.1"))
				mr.WithFile("v1.0.1", "metadata.yaml", []byte(metadata))
				mr.WithFile("v2.0.1", "components.yaml", []byte("v2.0.1"))
				mr.WithFile("v2.0.1", "metadata.yaml", []byte(metadata))
				mr.WithFile("v2.0.2+exp.sha.5114f85", "components.yaml", []byte("v2.0.2+exp.sha.5114f85"))
				mr.WithFile("v2.0.2+exp.sha.5114f85", "metadata.yaml", []byte(metadata))
				mr.WithFile("v2.0.3-alpha", "components.yaml", []byte("v2.0.3-alpha"))
				mr.WithFile("v2.0.3-alpha", "metadata.yaml", []byte(metadata))
				mr.WithPaths("", "components.yaml")
			},
			want: want{
				versions:       []string{"v1.0.0", "v1.0.1", "v2.0.1", "v2.0.2+exp.sha.5114f85", "v2.0.3-alpha"},
				defaultVersion: []byte("v2.0.2+exp.sha.5114f85"),
				latestVersion:  []byte("v2.0.2+exp.sha.5114f85"),
			},
			wantErr: false,
		},
		{
			name: "Get pre-release",
			addTestData: func(mr *MemoryRepository) {
				mr.WithFile("v2.0.3-alpha", "components.yaml", []byte("v2.0.3-alpha"))
				mr.WithFile("v2.0.3-alpha", "metadata.yaml", []byte(metadata))
				mr.WithPaths("", "components.yaml")
			},
			want: want{
				versions:       []string{"v2.0.3-alpha"},
				defaultVersion: []byte("v2.0.3-alpha"),
				latestVersion:  []byte("v2.0.3-alpha"),
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			r := NewMemoryRepository()
			tt.addTestData(r)

			g.Expect(r.RootPath()).To(Equal(""))
			g.Expect(r.DefaultVersion()).To(Equal(string(tt.want.defaultVersion)))

			g.Expect(r.GetFile(r.DefaultVersion(), r.ComponentsPath())).To(Equal(tt.want.defaultVersion))
			g.Expect(r.GetFile("", r.ComponentsPath())).To(Equal(tt.want.defaultVersion))
			g.Expect(r.GetFile("latest", r.ComponentsPath())).To(Equal(tt.want.latestVersion))

			got, err := r.GetVersions()
			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(got).To(ConsistOf(tt.want.versions))
		})
	}
}
