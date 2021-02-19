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

package v1alpha3

import (
	"math/rand"
	"testing"

	fuzz "github.com/google/gofuzz"
	"k8s.io/apimachinery/pkg/api/apitesting/fuzzer"
	runtimeserializer "k8s.io/apimachinery/pkg/runtime/serializer"
	"sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1alpha4"
	"sigs.k8s.io/cluster-api/bootstrap/kubeadm/types/v1beta1"
	utilconversion "sigs.k8s.io/cluster-api/util/conversion"
)

func TestFuzzyConversion(t *testing.T) {
	t.Run("for KubeadmConfig", utilconversion.FuzzTestFunc(utilconversion.FuzzTestFuncInput{
		Hub:         &v1alpha4.KubeadmConfig{},
		Spoke:       &KubeadmConfig{},
		FuzzerFuncs: []fuzzer.FuzzerFuncs{fuzzFuncs},
	}))
	t.Run("for KubeadmConfigTemplate", utilconversion.FuzzTestFunc(utilconversion.FuzzTestFuncInput{
		Hub:         &v1alpha4.KubeadmConfigTemplate{},
		Spoke:       &KubeadmConfigTemplate{},
		FuzzerFuncs: []fuzzer.FuzzerFuncs{fuzzFuncs},
	}))
}

func fuzzFuncs(_ runtimeserializer.CodecFactory) []interface{} {
	return append([]interface{}{
		KubeadmConfigStatusFuzzer,
		dnsFuzzer,
		clusterConfigurationFuzzer,
	}, bootstrapTokenFuzzers()...)
}

func KubeadmConfigStatusFuzzer(obj *KubeadmConfigStatus, c fuzz.Continue) {
	c.FuzzNoCustom(obj)

	// KubeadmConfigStatus.BootstrapData has been removed in v1alpha4, so setting it to nil in order to avoid v1alpha3 --> v1alpha4 --> v1alpha3 round trip errors.
	obj.BootstrapData = nil
}

func dnsFuzzer(obj *v1beta1.DNS, c fuzz.Continue) {
	c.FuzzNoCustom(obj)

	// DNS.Type does not exists in v1alpha4, so setting it to empty string in order to avoid v1alpha3 --> v1alpha4 --> v1alpha3 round trip errors.
	obj.Type = ""
}

func clusterConfigurationFuzzer(obj *v1beta1.ClusterConfiguration, c fuzz.Continue) {
	c.FuzzNoCustom(obj)

	// ClusterConfiguration.UseHyperKubeImage has been removed in v1alpha4, so setting it to false in order to avoid v1beta1 --> v1alpha4 --> v1beta1 round trip errors.
	obj.UseHyperKubeImage = false
}

func bootstrapTokenFuzzers() []interface{} {
	return []interface{}{
		// Fuzzer for BootstrapToken to ensure correctness of the token format.
		func(j **v1beta1.BootstrapTokenString, c fuzz.Continue) {
			if c.RandBool() {
				t := &v1beta1.BootstrapTokenString{}
				c.Fuzz(t)

				t.ID = randTokenString(6)
				t.Secret = randTokenString(16)

				*j = t
			} else {
				*j = nil
			}
		},
		func(j **v1alpha4.BootstrapTokenString, c fuzz.Continue) {
			if c.RandBool() {
				t := &v1alpha4.BootstrapTokenString{}
				c.Fuzz(t)

				t.ID = randTokenString(6)
				t.Secret = randTokenString(16)

				*j = t
			} else {
				*j = nil
			}
		},
	}
}

const tokenCharsBytes = "abcdefghijklmnopqrstuvwxyz0123456789"

func randTokenString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = tokenCharsBytes[rand.Intn(len(tokenCharsBytes))]
	}
	return string(b)
}
