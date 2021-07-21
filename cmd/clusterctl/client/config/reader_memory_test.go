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
	"testing"

	. "github.com/onsi/gomega"

	"sigs.k8s.io/cluster-api/cmd/clusterctl/api/v1alpha3"
)

func TestMemoryReader(t *testing.T) {
	tests := []struct {
		name       string
		variables  map[string]string
		providers  []configProvider
		imageMetas map[string]imageMeta
		wantErr    bool
	}{
		{
			name: "providers",
			providers: []configProvider{
				{
					Name: "foo",
					Type: v1alpha3.BootstrapProviderType,
					URL:  "url",
				},
			},
			imageMetas: map[string]imageMeta{
				"foo": {
					Repository: "repo",
					Tag:        "old",
				},
			},
			variables: map[string]string{
				"one":   "1",
				"two":   "2",
				"three": "3",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			f := NewMemoryReader()
			g.Expect(f.Init("")).To(Succeed())
			for _, p := range tt.providers {
				f = f.WithProvider(p.Name, p.Type, p.URL)
			}
			for n, m := range tt.imageMetas {
				f = f.WithImageMeta(n, m.Repository, m.Tag)
			}
			for n, v := range tt.variables {
				f.Set(n, v)
			}

			providersOut := []configProvider{}
			g.Expect(f.UnmarshalKey("providers", &providersOut)).To(Succeed())
			g.Expect(providersOut).To(Equal(tt.providers))

			imagesOut := map[string]imageMeta{}
			g.Expect(f.UnmarshalKey("images", &imagesOut)).To(Succeed())
			g.Expect(imagesOut).To(Equal(tt.imageMetas))

			for n, v := range tt.variables {
				outV, err := f.Get(n)
				g.Expect(err).To(Succeed())
				g.Expect(outV).To(Equal(v))
			}
			val, err := f.Get("notfound")
			g.Expect(err).ToNot(Succeed())
			g.Expect(val).To(Equal(""))
		})
	}
}
