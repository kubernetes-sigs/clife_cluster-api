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

package v1alpha4

import (
	"testing"

	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestClusterValidate(t *testing.T) {
	cases := map[string]struct {
		in        *KubeadmConfig
		expectErr bool
	}{
		"valid content": {
			in: &KubeadmConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "baz",
					Namespace: metav1.NamespaceDefault,
				},
				Spec: KubeadmConfigSpec{
					Files: []File{
						{
							Content: "foo",
						},
					},
				},
			},
		},
		"valid contentFrom": {
			in: &KubeadmConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "baz",
					Namespace: metav1.NamespaceDefault,
				},
				Spec: KubeadmConfigSpec{
					Files: []File{
						{
							ContentFrom: &FileSource{
								Secret: SecretFileSource{
									Name: "foo",
									Key:  "bar",
								},
							},
						},
					},
				},
			},
		},
		"invalid content and contentFrom": {
			in: &KubeadmConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "baz",
					Namespace: metav1.NamespaceDefault,
				},
				Spec: KubeadmConfigSpec{
					Files: []File{
						{
							ContentFrom: &FileSource{},
							Content:     "foo",
						},
					},
				},
			},
			expectErr: true,
		},
		"invalid contentFrom without name": {
			in: &KubeadmConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "baz",
					Namespace: metav1.NamespaceDefault,
				},
				Spec: KubeadmConfigSpec{
					Files: []File{
						{
							ContentFrom: &FileSource{
								Secret: SecretFileSource{
									Key: "bar",
								},
							},
							Content: "foo",
						},
					},
				},
			},
			expectErr: true,
		},
		"invalid contentFrom without key": {
			in: &KubeadmConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "baz",
					Namespace: metav1.NamespaceDefault,
				},
				Spec: KubeadmConfigSpec{
					Files: []File{
						{
							ContentFrom: &FileSource{
								Secret: SecretFileSource{
									Name: "foo",
								},
							},
							Content: "foo",
						},
					},
				},
			},
			expectErr: true,
		},
		"invalid with duplicate file path": {
			in: &KubeadmConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "baz",
					Namespace: metav1.NamespaceDefault,
				},
				Spec: KubeadmConfigSpec{
					Files: []File{
						{
							Content: "foo",
						},
						{
							Content: "bar",
						},
					},
				},
			},
			expectErr: true,
		},
	}

	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)
			if tt.expectErr {
				g.Expect(tt.in.ValidateCreate()).NotTo(Succeed())
				g.Expect(tt.in.ValidateUpdate(nil)).NotTo(Succeed())
			} else {
				g.Expect(tt.in.ValidateCreate()).To(Succeed())
				g.Expect(tt.in.ValidateUpdate(nil)).To(Succeed())
			}
		})
	}
}
