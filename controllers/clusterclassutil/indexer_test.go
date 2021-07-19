/*
Copyright 2021 The Kubernetes Authors.

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

package clusterclassutil

import (
	"testing"

	. "github.com/onsi/gomega"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestIndexClusterByClassName(t *testing.T) {
	testCases := []struct {
		name     string
		object   client.Object
		expected []string
	}{
		{
			name:     "when cluster has no Topology",
			object:   &clusterv1.Cluster{},
			expected: []string{},
		},
		{
			name: "when cluster has a valid Topology",
			object: &clusterv1.Cluster{
				Spec: clusterv1.ClusterSpec{
					Topology: &clusterv1.Topology{
						Class: "class1",
					},
				},
			},
			expected: []string{"class1"},
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			got := indexClusterByClassName(test.object)
			g.Expect(got).To(ConsistOf(test.expected))
		})
	}
}
