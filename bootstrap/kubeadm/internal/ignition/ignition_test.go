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

package ignition_test

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"

	bootstrapv1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1alpha4"
	"sigs.k8s.io/cluster-api/bootstrap/kubeadm/internal/cloudinit"
	"sigs.k8s.io/cluster-api/bootstrap/kubeadm/internal/ignition"
)

const testString = "foo bar baz"

func Test_NewNode(t *testing.T) {
	t.Parallel()

	t.Run("returns_error_when", func(t *testing.T) {
		t.Parallel()

		cases := map[string]*ignition.NodeInput{
			"nil_input_is_given":      nil,
			"nil_node_input_is_given": {},
		}

		for name, input := range cases {
			input := input

			t.Run(name, func(t *testing.T) {
				t.Parallel()

				ignitionData, _, err := ignition.NewNode(input)
				if err == nil {
					t.Fatalf("Expected error")
				}

				if ignitionData != nil {
					t.Fatalf("Unexpected data returned %v", ignitionData)
				}
			})
		}
	})

	t.Run("returns_JSON_data_without_error", func(t *testing.T) {
		t.Parallel()

		input := &ignition.NodeInput{
			NodeInput: &cloudinit.NodeInput{},
			Ignition:  &bootstrapv1.IgnitionSpec{},
		}

		ignitionData, _, err := ignition.NewNode(input)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if ignitionData == nil {
			t.Fatalf("Returned data is nil")
		}

		decodedValue := map[string]interface{}{}

		if err := json.Unmarshal(ignitionData, &decodedValue); err != nil {
			t.Fatalf("Decoding received Ignition data as JSON: %v", err)
		}
	})

	t.Run("returns_ignition_with_user_specified_snippet", func(t *testing.T) {
		t.Parallel()

		input := &ignition.NodeInput{
			NodeInput: &cloudinit.NodeInput{},
			Ignition: &bootstrapv1.IgnitionSpec{
				ContainerLinuxConfig: &bootstrapv1.ContainerLinuxConfig{
					AdditionalConfig: fmt.Sprintf(`storage:
  files:
  - path: /etc/foo
    mode: 0644
    contents:
      inline: |
        %s
`, testString),
				},
			},
		}

		ignitionData, _, err := ignition.NewNode(input)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		// Ignition stores content URL-encoded.
		u := url.URL{Path: testString}

		if !strings.Contains(string(ignitionData), u.String()) {
			t.Fatalf("Expected %q to be included in %q", testString, string(ignitionData))
		}
	})

	t.Run("returns_warnings_if_any", func(t *testing.T) {
		t.Parallel()

		input := &ignition.NodeInput{
			NodeInput: &cloudinit.NodeInput{},
			Ignition: &bootstrapv1.IgnitionSpec{
				ContainerLinuxConfig: &bootstrapv1.ContainerLinuxConfig{
					AdditionalConfig: fmt.Sprintf(`storage:
  files:
  - path: /etc/foo
    contents:
      inline: |
        %s
`, testString),
				},
			},
		}

		ignitionData, warnings, err := ignition.NewNode(input)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if warnings == "" {
			t.Fatalf("Expected warnings")
		}

		if len(ignitionData) == 0 {
			t.Fatalf("Data should be returned with warnings but no errors")
		}
	})
}

func Test_NewJoinControlPlane(t *testing.T) {
	t.Parallel()

	t.Run("returns_error_when", func(t *testing.T) {
		t.Parallel()

		cases := map[string]*ignition.ControlPlaneJoinInput{
			"nil_input_is_given":      nil,
			"nil_node_input_is_given": {},
		}

		for name, input := range cases {
			input := input

			t.Run(name, func(t *testing.T) {
				t.Parallel()

				ignitionData, _, err := ignition.NewJoinControlPlane(input)
				if err == nil {
					t.Fatalf("Expected error")
				}

				if ignitionData != nil {
					t.Fatalf("Unexpected data returned %v", ignitionData)
				}
			})
		}
	})

	t.Run("returns_JSON_data_without_error", func(t *testing.T) {
		t.Parallel()

		input := &ignition.ControlPlaneJoinInput{
			ControlPlaneJoinInput: &cloudinit.ControlPlaneJoinInput{},
			Ignition:              &bootstrapv1.IgnitionSpec{},
		}

		ignitionData, _, err := ignition.NewJoinControlPlane(input)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if ignitionData == nil {
			t.Fatalf("Returned data is nil")
		}

		decodedValue := map[string]interface{}{}

		if err := json.Unmarshal(ignitionData, &decodedValue); err != nil {
			t.Fatalf("Decoding received Ignition data as JSON: %v", err)
		}
	})

	t.Run("returns_ignition_with_user_specified_snippet", func(t *testing.T) {
		t.Parallel()

		input := &ignition.ControlPlaneJoinInput{
			ControlPlaneJoinInput: &cloudinit.ControlPlaneJoinInput{},
			Ignition: &bootstrapv1.IgnitionSpec{
				ContainerLinuxConfig: &bootstrapv1.ContainerLinuxConfig{
					AdditionalConfig: fmt.Sprintf(`storage:
  files:
  - path: /etc/foo
    mode: 0644
    contents:
      inline: |
        %s
`, testString),
				},
			},
		}

		ignitionData, _, err := ignition.NewJoinControlPlane(input)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		// Ignition stores content URL-encoded.
		u := url.URL{Path: testString}

		if !strings.Contains(string(ignitionData), u.String()) {
			t.Fatalf("Expected %q to be included in %q", testString, string(ignitionData))
		}
	})

	t.Run("returns_warnings_if_any", func(t *testing.T) {
		t.Parallel()

		input := &ignition.ControlPlaneJoinInput{
			ControlPlaneJoinInput: &cloudinit.ControlPlaneJoinInput{},
			Ignition: &bootstrapv1.IgnitionSpec{
				ContainerLinuxConfig: &bootstrapv1.ContainerLinuxConfig{
					AdditionalConfig: fmt.Sprintf(`storage:
  files:
  - path: /etc/foo
    contents:
      inline: |
        %s
`, testString),
				},
			},
		}

		ignitionData, warnings, err := ignition.NewJoinControlPlane(input)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if warnings == "" {
			t.Fatalf("Expected to get some warnings")
		}

		if len(ignitionData) == 0 {
			t.Fatalf("Data should be returned with warnings but no errors")
		}
	})
}

func Test_NewInitControlPlane(t *testing.T) {
	t.Parallel()

	t.Run("returns_error_when", func(t *testing.T) {
		t.Parallel()

		cases := map[string]*ignition.ControlPlaneInput{
			"nil_input_is_given":      nil,
			"nil_node_input_is_given": {},
		}

		for name, input := range cases {
			input := input

			t.Run(name, func(t *testing.T) {
				t.Parallel()

				ignitionData, _, err := ignition.NewInitControlPlane(input)
				if err == nil {
					t.Fatalf("Expected error")
				}

				if ignitionData != nil {
					t.Fatalf("Unexpected data returned %v", ignitionData)
				}
			})
		}
	})

	t.Run("returns_without_error", func(t *testing.T) {
		t.Parallel()

		input := &ignition.ControlPlaneInput{
			ControlPlaneInput: &cloudinit.ControlPlaneInput{},
		}

		ignitionData, _, err := ignition.NewInitControlPlane(input)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if ignitionData == nil {
			t.Fatalf("Returned data is nil")
		}

		t.Run("valid_JSON_data", func(t *testing.T) {
			decodedValue := map[string]interface{}{}

			if err := json.Unmarshal(ignitionData, &decodedValue); err != nil {
				t.Fatalf("Decoding received Ignition data as JSON: %v", err)
			}
		})
	})

	t.Run("returns_ignition_with_user_specified_snippet", func(t *testing.T) {
		t.Parallel()

		input := &ignition.ControlPlaneInput{
			ControlPlaneInput: &cloudinit.ControlPlaneInput{},
			Ignition: &bootstrapv1.IgnitionSpec{
				ContainerLinuxConfig: &bootstrapv1.ContainerLinuxConfig{
					AdditionalConfig: fmt.Sprintf(`storage:
  files:
  - path: /etc/foo
    mode: 0644
    contents:
      inline: |
        %s
`, testString),
				},
			},
		}

		ignitionData, _, err := ignition.NewInitControlPlane(input)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		// Ignition stores content URL-encoded.
		u := url.URL{Path: testString}

		if !strings.Contains(string(ignitionData), u.String()) {
			t.Fatalf("Expected %q to be included in %q", testString, string(ignitionData))
		}
	})

	t.Run("returns_warnings_if_any", func(t *testing.T) {
		t.Parallel()

		input := &ignition.ControlPlaneInput{
			ControlPlaneInput: &cloudinit.ControlPlaneInput{},
			Ignition: &bootstrapv1.IgnitionSpec{
				ContainerLinuxConfig: &bootstrapv1.ContainerLinuxConfig{
					AdditionalConfig: fmt.Sprintf(`storage:
  files:
  - path: /etc/foo
    contents:
      inline: |
        %s
`, testString),
				},
			},
		}

		ignitionData, warnings, err := ignition.NewInitControlPlane(input)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if warnings == "" {
			t.Fatalf("Expected warnings")
		}

		if len(ignitionData) == 0 {
			t.Fatalf("Data should be returned with warnings but no errors")
		}
	})
}
