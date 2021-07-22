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

package v1beta1

import (
	"testing"

	fuzz "github.com/google/gofuzz"
	"k8s.io/apimachinery/pkg/api/apitesting/fuzzer"
	runtimeserializer "k8s.io/apimachinery/pkg/runtime/serializer"
	"sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1alpha4"
	kubeadmbootstrapv1alpha4 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1alpha4"
	utilconversion "sigs.k8s.io/cluster-api/util/conversion"
)

func TestFuzzyConversion(t *testing.T) {
	t.Run("for ClusterConfiguration", utilconversion.FuzzTestFunc(utilconversion.FuzzTestFuncInput{
		Hub:   &v1alpha4.ClusterConfiguration{},
		Spoke: &ClusterConfiguration{},
		// NOTE: Kubeadm types does not have ObjectMeta, so we are required to skip data annotation cleanup in the spoke-hub-spoke round trip test.
		SkipSpokeAnnotationCleanup: true,
		FuzzerFuncs:                []fuzzer.FuzzerFuncs{fuzzFuncs},
	}))
	t.Run("for ClusterStatus", utilconversion.FuzzTestFunc(utilconversion.FuzzTestFuncInput{
		Hub:   &v1alpha4.ClusterStatus{},
		Spoke: &ClusterStatus{},
		// NOTE: Kubeadm types does not have ObjectMeta, so we are required to skip data annotation cleanup in the spoke-hub-spoke round trip test.
		SkipSpokeAnnotationCleanup: true,
		FuzzerFuncs:                []fuzzer.FuzzerFuncs{fuzzFuncs},
	}))
	t.Run("for InitConfiguration", utilconversion.FuzzTestFunc(utilconversion.FuzzTestFuncInput{
		Hub:   &v1alpha4.InitConfiguration{},
		Spoke: &InitConfiguration{},
		// NOTE: Kubeadm types does not have ObjectMeta, so we are required to skip data annotation cleanup in the spoke-hub-spoke round trip test.
		SkipSpokeAnnotationCleanup: true,
		FuzzerFuncs:                []fuzzer.FuzzerFuncs{fuzzFuncs},
	}))
	t.Run("for JoinConfiguration", utilconversion.FuzzTestFunc(utilconversion.FuzzTestFuncInput{
		Hub:   &v1alpha4.JoinConfiguration{},
		Spoke: &JoinConfiguration{},
		// NOTE: Kubeadm types does not have ObjectMeta, so we are required to skip data annotation cleanup in the spoke-hub-spoke round trip test.
		SkipSpokeAnnotationCleanup: true,
		FuzzerFuncs:                []fuzzer.FuzzerFuncs{fuzzFuncs},
	}))
}

func fuzzFuncs(_ runtimeserializer.CodecFactory) []interface{} {
	return []interface{}{
		dnsFuzzer,
		clusterConfigurationFuzzer,
		kubeadmNodeRegistrationOptionsFuzzer,
	}
}

func dnsFuzzer(obj *DNS, c fuzz.Continue) {
	c.FuzzNoCustom(obj)

	// DNS.Type does not exists in v1alpha4, so setting it to empty string in order to avoid v1beta1 --> v1alpha4 --> v1beta1 round trip errors.
	obj.Type = ""
}

func clusterConfigurationFuzzer(obj *ClusterConfiguration, c fuzz.Continue) {
	c.FuzzNoCustom(obj)

	// ClusterConfiguration.UseHyperKubeImage has been removed in v1alpha4, so setting it to false in order to avoid v1beta1 --> v1alpha4 --> v1beta1 round trip errors.
	obj.UseHyperKubeImage = false
}

func kubeadmNodeRegistrationOptionsFuzzer(obj *kubeadmbootstrapv1alpha4.NodeRegistrationOptions, c fuzz.Continue) {
	c.FuzzNoCustom(obj)

	// NodeRegistrationOptions.IgnorePreflightErrors does not exist in kubeadm v1beta1 API, so setting it to nil in order to avoid
	// v1alpha4 --> v1beta1 -> v1alpha4 round trip errors.
	obj.IgnorePreflightErrors = nil
}
