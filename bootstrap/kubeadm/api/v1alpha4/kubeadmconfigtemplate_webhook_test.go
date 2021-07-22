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

package v1alpha4_test

import (
	"testing"

	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	bootstrapv1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1alpha4"
)

// These tests are written in BDD-style using Ginkgo framework. Refer to
// http://onsi.github.io/ginkgo to learn more.

func Test_KubeadmConfigTemplate_validates_creation_and_deletion_and(t *testing.T) {
	cases := map[string]struct {
		in        *bootstrapv1.KubeadmConfigTemplate
		expectErr bool
	}{
		"accepts valid configuration": {
			in: &bootstrapv1.KubeadmConfigTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "baz",
					Namespace: "default",
				},
				Spec: bootstrapv1.KubeadmConfigTemplateSpec{
					Template: bootstrapv1.KubeadmConfigTemplateResource{
						Spec: bootstrapv1.KubeadmConfigSpec{},
					},
				},
			},
		},
		"rejects bad Ignition configuration": {
			expectErr: true,
			in: &bootstrapv1.KubeadmConfigTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "baz",
					Namespace: "default",
				},
				Spec: bootstrapv1.KubeadmConfigTemplateSpec{
					Template: bootstrapv1.KubeadmConfigTemplateResource{
						Spec: bootstrapv1.KubeadmConfigSpec{
							Format: bootstrapv1.Ignition,
						},
					},
				},
			},
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
