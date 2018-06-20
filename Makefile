# Copyright 2018 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

.PHONY: genapi genconversion genclientset

all: genapi genconversion genclientset

genapi:
	go install github.com/kubernetes-incubator/apiserver-builder/cmd/apiregister-gen
	apiregister-gen -i ./pkg/apis,./pkg/apis/cluster/v1alpha1

genconversion:
	go install k8s.io/code-generator/cmd/conversion-gen
	conversion-gen -i ./pkg/apis/cluster/v1alpha1/ -O zz_generated.conversion

genclientset:
	go build -o $$GOPATH/bin/client-gen sigs.k8s.io/cluster-api/vendor/k8s.io/code-generator/cmd/client-gen
	client-gen \
	  --input="cluster/v1alpha1" \
		--clientset-name="clientset" \
		--input-base="sigs.k8s.io/cluster-api/pkg/apis" \
		--output-package "sigs.k8s.io/cluster-api/pkg/client/clientset_generated" \
		--go-header-file boilerplate.go.txt \
		--clientset-path sigs.k8s.io/cluster-api/pkg/client/clientset_generated
