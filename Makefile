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

# If you update this file, please follow
# https://suva.sh/posts/well-documented-makefiles

# Ensure Make is run with bash shell as some syntax below is bash-specific
SHELL:=/usr/bin/env bash

.DEFAULT_GOAL:=help

#
# Go.
#
GO_VERSION ?= 1.16.6
GO_CONTAINER_IMAGE ?= docker.io/library/golang:$(GO_VERSION)

# Use GOPROXY environment variable if set
GOPROXY := $(shell go env GOPROXY)
ifeq ($(GOPROXY),)
GOPROXY := https://proxy.golang.org
endif
export GOPROXY

# Active module mode, as we use go modules to manage dependencies
export GO111MODULE=on

#
# Kubebuilder.
#
export KUBEBUILDER_ENVTEST_KUBERNETES_VERSION ?= 1.21.2
export KUBEBUILDER_CONTROLPLANE_START_TIMEOUT ?= 60s
export KUBEBUILDER_CONTROLPLANE_STOP_TIMEOUT ?= 60s

# This option is for running docker manifest command
export DOCKER_CLI_EXPERIMENTAL := enabled

#
# Directories.
#
# Full directory of where the Makefile resides
ROOT_DIR:=$(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))
EXP_DIR := exp
BIN_DIR := bin
TEST_DIR := test
TOOLS_DIR := hack/tools
TOOLS_BIN_DIR := $(TOOLS_DIR)/$(BIN_DIR)
E2E_FRAMEWORK_DIR := $(TEST_DIR)/framework
CAPD_DIR := $(TEST_DIR)/infrastructure/docker
GO_APIDIFF_BIN := $(BIN_DIR)/go-apidiff
GO_APIDIFF := $(TOOLS_DIR)/$(GO_APIDIFF_BIN)
ENVSUBST_BIN := $(BIN_DIR)/envsubst
ENVSUBST := $(TOOLS_DIR)/$(ENVSUBST_BIN)

export PATH := $(abspath $(TOOLS_BIN_DIR)):$(PATH)

# Set --output-base for conversion-gen if we are not within GOPATH
ifneq ($(abspath $(ROOT_DIR)),$(shell go env GOPATH)/src/sigs.k8s.io/cluster-api)
	CONVERSION_GEN_OUTPUT_BASE := --output-base=$(ROOT_DIR)
else
	export GOPATH := $(shell go env GOPATH)
endif

#
# Binaries.
#
# Note: Need to use abspath so we can invoke these from subdirectories
KUSTOMIZE := $(abspath $(TOOLS_BIN_DIR)/kustomize)
SETUP_ENVTEST := $(abspath $(TOOLS_BIN_DIR)/setup-envtest)
CONTROLLER_GEN := $(abspath $(TOOLS_BIN_DIR)/controller-gen)
GOTESTSUM := $(abspath $(TOOLS_BIN_DIR)/gotestsum)
GOLANGCI_LINT := $(abspath $(TOOLS_BIN_DIR)/golangci-lint)
CONVERSION_GEN := $(abspath $(TOOLS_BIN_DIR)/conversion-gen)
ENVSUBST := $(abspath $(TOOLS_BIN_DIR)/envsubst)

# clusterctl.
CLUSTERCTL_MANIFEST_DIR := cmd/clusterctl/config

# Define Docker related variables. Releases should modify and double check these vars.
REGISTRY ?= gcr.io/$(shell gcloud config get-value project)
PROD_REGISTRY ?= k8s.gcr.io/cluster-api

STAGING_REGISTRY ?= gcr.io/k8s-staging-cluster-api
STAGING_BUCKET ?= artifacts.k8s-staging-cluster-api.appspot.com

# core
IMAGE_NAME ?= cluster-api-controller
CONTROLLER_IMG ?= $(REGISTRY)/$(IMAGE_NAME)

# bootstrap
KUBEADM_BOOTSTRAP_IMAGE_NAME ?= kubeadm-bootstrap-controller
KUBEADM_BOOTSTRAP_CONTROLLER_IMG ?= $(REGISTRY)/$(KUBEADM_BOOTSTRAP_IMAGE_NAME)

# control plane
KUBEADM_CONTROL_PLANE_IMAGE_NAME ?= kubeadm-control-plane-controller
KUBEADM_CONTROL_PLANE_CONTROLLER_IMG ?= $(REGISTRY)/$(KUBEADM_CONTROL_PLANE_IMAGE_NAME)

# It is set by Prow GIT_TAG, a git-based tag of the form vYYYYMMDD-hash, e.g., v20210120-v0.3.10-308-gc61521971
TAG ?= dev
ARCH ?= amd64
ALL_ARCH = amd64 arm arm64 ppc64le s390x

# Allow overriding the imagePullPolicy
PULL_POLICY ?= Always

# Hosts running SELinux need :z added to volume mounts
SELINUX_ENABLED := $(shell cat /sys/fs/selinux/enforce 2> /dev/null || echo 0)

ifeq ($(SELINUX_ENABLED),1)
  DOCKER_VOL_OPTS?=:z
endif

# Set build time variables including version details
LDFLAGS := $(shell hack/version.sh)

all: test managers clusterctl

help:  ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[0-9A-Za-z_-]+:.*?##/ { printf "  \033[36m%-45s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

## --------------------------------------
## Testing
## --------------------------------------

ARTIFACTS ?= ${ROOT_DIR}/_artifacts

KUBEBUILDER_ASSETS ?= $(shell $(SETUP_ENVTEST) use --use-env -p path $(KUBEBUILDER_ENVTEST_KUBERNETES_VERSION))

.PHONY: test
test: $(SETUP_ENVTEST) ## Run tests.
	KUBEBUILDER_ASSETS="$(KUBEBUILDER_ASSETS)" go test ./... $(TEST_ARGS)

.PHONY: test-verbose
test-verbose: ## Run tests with verbose settings.
	$(MAKE) test TEST_ARGS="$(TEST_ARGS) -v"

.PHONY: test-junit
test-junit: $(SETUP_ENVTEST) $(GOTESTSUM) ## Run tests with verbose setting and generate a junit report.
	set +o errexit; (KUBEBUILDER_ASSETS="$(KUBEBUILDER_ASSETS)" go test -json ./... $(TEST_ARGS); echo $$? > $(ARTIFACTS)/junit.exitcode) | tee $(ARTIFACTS)/junit.stdout
	$(GOTESTSUM) --junitfile $(ARTIFACTS)/junit.xml --raw-command cat $(ARTIFACTS)/junit.stdout
	exit $$(cat $(ARTIFACTS)/junit.exitcode)

.PHONY: test-cover
test-cover: ## Run tests with code coverage and code generate reports.
	$(MAKE) test TEST_ARGS="$(TEST_ARGS) -coverprofile=out/coverage.out"
	go tool cover -func=out/coverage.out -o out/coverage.txt
	go tool cover -html=out/coverage.out -o out/coverage.html

.PHONY: docker-build-e2e
docker-build-e2e: ## Rebuild all Cluster API provider images to be used in the e2e tests
	make docker-build REGISTRY=gcr.io/k8s-staging-cluster-api PULL_POLICY=IfNotPresent
	$(MAKE) -C $(CAPD_DIR) docker-build REGISTRY=gcr.io/k8s-staging-cluster-api PULL_POLICY=IfNotPresent

.PHONY: test-e2e
test-e2e: ## Run the e2e tests
	$(MAKE) -C $(TEST_DIR)/e2e run

## --------------------------------------
## Binaries
## --------------------------------------

.PHONY: manager-core
manager-core: ## Build core manager binary
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/manager sigs.k8s.io/cluster-api

.PHONY: manager-kubeadm-bootstrap
manager-kubeadm-bootstrap: ## Build kubeadm bootstrap manager
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/kubeadm-bootstrap-manager sigs.k8s.io/cluster-api/bootstrap/kubeadm

.PHONY: manager-kubeadm-control-plane
manager-kubeadm-control-plane: ## Build kubeadm control plane manager
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/kubeadm-control-plane-manager sigs.k8s.io/cluster-api/controlplane/kubeadm

.PHONY: managers
managers: ## Build all managers
	$(MAKE) manager-core
	$(MAKE) manager-kubeadm-bootstrap
	$(MAKE) manager-kubeadm-control-plane

.PHONY: clusterctl
clusterctl: ## Build clusterctl binary
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/clusterctl sigs.k8s.io/cluster-api/cmd/clusterctl

$(SETUP_ENVTEST): $(TOOLS_DIR)/go.mod # Build setup-envtest from tools folder.
	cd $(TOOLS_DIR); go build -tags=tools -o $(BIN_DIR)/setup-envtest sigs.k8s.io/controller-runtime/tools/setup-envtest

$(CONTROLLER_GEN): $(TOOLS_DIR)/go.mod # Build controller-gen from tools folder.
	cd $(TOOLS_DIR); go build -tags=tools -o $(BIN_DIR)/controller-gen sigs.k8s.io/controller-tools/cmd/controller-gen

$(GOTESTSUM): $(TOOLS_DIR)/go.mod # Build gotestsum from tools folder.
	cd $(TOOLS_DIR); go build -tags=tools -o $(BIN_DIR)/gotestsum gotest.tools/gotestsum

$(CONVERSION_GEN): $(TOOLS_DIR)/go.mod
	cd $(TOOLS_DIR); go build -tags=tools -o $(BIN_DIR)/conversion-gen k8s.io/code-generator/cmd/conversion-gen

$(GO_APIDIFF): $(TOOLS_DIR)/go.mod
	cd $(TOOLS_DIR) && go build -tags=tools -o $(GO_APIDIFF_BIN) github.com/joelanford/go-apidiff

$(ENVSUBST): $(TOOLS_DIR)/go.mod
	cd $(TOOLS_DIR) && go build -tags=tools -o $(ENVSUBST_BIN) github.com/drone/envsubst/v2/cmd/envsubst

$(KUSTOMIZE): # Download kustomize using hack script into tools folder.
	hack/ensure-kustomize.sh

$(GOLANGCI_LINT): .github/workflows/golangci-lint.yml # Download golanci-lint using hack script into tools folder.
	hack/ensure-golangci-lint.sh \
		-b $(TOOLS_DIR)/$(BIN_DIR) \
		$(shell cat .github/workflows/golangci-lint.yml | grep version | sed 's/.*version: //')

envsubst: $(ENVSUBST) ## Build a local copy of envsubst.
kustomize: $(KUSTOMIZE) ## Build a local copy of kustomize.
setup-envtest: $(SETUP_ENVTEST) ## Build a local copy of setup-envtest.
controller-gen: $(CONTROLLER_GEN) ## Build a local copy of controller-gen.
conversion-gen: $(CONVERSION_GEN) ## Build a local copy of conversion-gen.
gotestsum: $(GOTESTSUM) ## Build a local copy of gotestsum.

.PHONY: e2e-framework
e2e-framework: ## Builds the CAPI e2e framework
	cd $(E2E_FRAMEWORK_DIR); go build ./...

## --------------------------------------
## Linting
## --------------------------------------

.PHONY: lint
lint: $(GOLANGCI_LINT) ## Lint codebase
	$(GOLANGCI_LINT) run -v $(GOLANGCI_LINT_EXTRA_ARGS)
	cd $(TEST_DIR); $(GOLANGCI_LINT) run -v $(GOLANGCI_LINT_EXTRA_ARGS)

.PHONY: lint-fix
lint-fix: $(GOLANGCI_LINT) ## Lint the codebase and run auto-fixers if supported by the linter.
	GOLANGCI_LINT_EXTRA_ARGS=--fix $(MAKE) lint

apidiff: $(GO_APIDIFF) ## Check for API differences
	$(GO_APIDIFF) $(shell git rev-parse origin/master) --print-compatible

## --------------------------------------
## Generate / Manifests
## --------------------------------------

ALL_GENERATE_MODULES = core cabpk kcp

.PHONY: generate
generate: ## Generate code
	$(MAKE) generate-manifests generate-go
	$(MAKE) -C $(CAPD_DIR) generate

.PHONY: generate-go
generate-go:  ## Runs Go related generate targets
	$(MAKE) $(addprefix generate-go-,$(ALL_GENERATE_MODULES)) $(addprefix generate-go-conversions-,$(ALL_GENERATE_MODULES))

.PHONY: generate-go-core
generate-go-core: $(CONTROLLER_GEN)
	$(CONTROLLER_GEN) \
		object:headerFile=./hack/boilerplate/boilerplate.generatego.txt \
		paths=./api/... \
		paths=./$(EXP_DIR)/api/... \
		paths=./$(EXP_DIR)/addons/api/... \
		paths=./cmd/clusterctl/...

.PHONY: generate-go-conversions-core
generate-go-conversions-core: $(CONVERSION_GEN)
	$(MAKE) clean-generated-conversions SRC_DIRS="./api/v1alpha3,./$(EXP_DIR)/api/v1alpha3,./$(EXP_DIR)/addons/api/v1alpha3"
	$(CONVERSION_GEN) \
		--input-dirs=./api/v1alpha3 \
		--build-tag=ignore_autogenerated_core_v1alpha3 \
		--output-file-base=zz_generated.conversion $(CONVERSION_GEN_OUTPUT_BASE) \
		--go-header-file=./hack/boilerplate/boilerplate.generatego.txt
	$(CONVERSION_GEN) \
		--input-dirs=./$(EXP_DIR)/api/v1alpha3 \
		--input-dirs=./$(EXP_DIR)/addons/api/v1alpha3 \
		--extra-peer-dirs=sigs.k8s.io/cluster-api/api/v1alpha3 \
		--output-file-base=zz_generated.conversion $(CONVERSION_GEN_OUTPUT_BASE) \
		--go-header-file=./hack/boilerplate/boilerplate.generatego.txt

.PHONY: generate-go-cabpk
generate-go-cabpk: $(CONTROLLER_GEN)
	$(CONTROLLER_GEN) \
		object:headerFile=./hack/boilerplate/boilerplate.generatego.txt \
		paths=./bootstrap/kubeadm/api/... \
		paths=./bootstrap/kubeadm/types/...

.PHONY: generate-go-conversions-cabpk
generate-go-conversions-cabpk: $(CONVERSION_GEN)
	$(MAKE) clean-generated-conversions SRC_DIRS="./bootstrap/kubeadm/api"
	$(CONVERSION_GEN) \
		--input-dirs=./bootstrap/kubeadm/api/v1alpha3 \
		--build-tag=ignore_autogenerated_kubeadm_bootstrap_v1alpha3 \
		--extra-peer-dirs=sigs.k8s.io/cluster-api/api/v1alpha3 \
		--output-file-base=zz_generated.conversion $(CONVERSION_GEN_OUTPUT_BASE) \
		--go-header-file=./hack/boilerplate/boilerplate.generatego.txt
	$(MAKE) clean-generated-conversions SRC_DIRS="./bootstrap/kubeadm/types/v1beta1,./bootstrap/kubeadm/types/v1beta2,./bootstrap/kubeadm/types/v1beta3"
	$(CONVERSION_GEN) \
		--input-dirs=./bootstrap/kubeadm/types/v1beta1 \
		--input-dirs=./bootstrap/kubeadm/types/v1beta2 \
		--input-dirs=./bootstrap/kubeadm/types/v1beta3 \
		--build-tag=ignore_autogenerated_kubeadm_bootstrap_v1alpha3 \
		--output-file-base=zz_generated.conversion $(CONVERSION_GEN_OUTPUT_BASE) \
		--go-header-file=./hack/boilerplate/boilerplate.generatego.txt

.PHONY: generate-go-kcp
generate-go-kcp: $(CONTROLLER_GEN)
	$(CONTROLLER_GEN) \
		object:headerFile=./hack/boilerplate/boilerplate.generatego.txt \
		paths=./controlplane/kubeadm/api/...

.PHONY: generate-go-conversions-kcp
generate-go-conversions-kcp: $(CONVERSION_GEN)
	$(MAKE) clean-generated-conversions SRC_DIRS="./controlplane/kubeadm/api"
	$(CONVERSION_GEN) \
		--input-dirs=./controlplane/kubeadm/api/v1alpha3 \
		--build-tag=ignore_autogenerated_kubeadm_controlplane_v1alpha3 \
		--extra-peer-dirs=sigs.k8s.io/cluster-api/api/v1alpha3,sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1alpha3 \
		--output-file-base=zz_generated.conversion $(CONVERSION_GEN_OUTPUT_BASE) \
		--go-header-file=./hack/boilerplate/boilerplate.generatego.txt


.PHONY: generate-manifests
generate-manifests: $(addprefix generate-manifests-,$(ALL_GENERATE_MODULES)) ## Generate manifests e.g. CRD, RBAC etc.

.PHONY: generate-manifests-core
generate-manifests-core: $(CONTROLLER_GEN) $(KUSTOMIZE)
	$(CONTROLLER_GEN) \
		paths=./api/... \
		paths=./controllers/... \
		paths=./$(EXP_DIR)/api/... \
		paths=./$(EXP_DIR)/controllers/... \
		paths=./$(EXP_DIR)/addons/api/... \
		paths=./$(EXP_DIR)/addons/controllers/... \
		crd:crdVersions=v1 \
		rbac:roleName=manager-role \
		output:crd:dir=./config/crd/bases \
		output:webhook:dir=./config/webhook \
		webhook
	$(CONTROLLER_GEN) \
		paths=./cmd/clusterctl/api/... \
		crd:crdVersions=v1 \
		output:crd:dir=./cmd/clusterctl/config/crd/bases
	$(KUSTOMIZE) build $(CLUSTERCTL_MANIFEST_DIR)/crd > $(CLUSTERCTL_MANIFEST_DIR)/manifest/clusterctl-api.yaml

.PHONY: generate-manifests-cabpk
generate-manifests-cabpk: $(CONTROLLER_GEN)
	$(CONTROLLER_GEN) \
		paths=./bootstrap/kubeadm/api/... \
		paths=./bootstrap/kubeadm/controllers/... \
		crd:crdVersions=v1 \
		rbac:roleName=manager-role \
		output:crd:dir=./bootstrap/kubeadm/config/crd/bases \
		output:rbac:dir=./bootstrap/kubeadm/config/rbac \
		output:webhook:dir=./bootstrap/kubeadm/config/webhook \
		webhook

.PHONY: generate-manifests-kcp
generate-manifests-kcp: $(CONTROLLER_GEN)
	$(CONTROLLER_GEN) \
		paths=./controlplane/kubeadm/api/... \
		paths=./controlplane/kubeadm/controllers/... \
		crd:crdVersions=v1 \
		rbac:roleName=manager-role \
		output:crd:dir=./controlplane/kubeadm/config/crd/bases \
		output:rbac:dir=./controlplane/kubeadm/config/rbac \
		output:webhook:dir=./controlplane/kubeadm/config/webhook \
		webhook

## --------------------------------------
## Modules
## --------------------------------------

.PHONY: modules
modules: ## Runs go mod to ensure modules are up to date.
	go mod tidy
	cd $(TOOLS_DIR); go mod tidy
	cd $(TEST_DIR); go mod tidy

## --------------------------------------
## Docker
## --------------------------------------

.PHONY: docker-pull-prerequisites
docker-pull-prerequisites:
	docker pull docker.io/docker/dockerfile:1.1-experimental
	docker pull $(GO_CONTAINER_IMAGE)
	docker pull gcr.io/distroless/static:latest

.PHONY: docker-build
docker-build: docker-pull-prerequisites ## Build the docker images for controller managers
	$(MAKE) ARCH=$(ARCH) docker-build-core
	$(MAKE) ARCH=$(ARCH) docker-build-kubeadm-bootstrap
	$(MAKE) ARCH=$(ARCH) docker-build-kubeadm-control-plane

.PHONY: docker-build-core
docker-build-core: ## Build the docker image for core controller manager
	DOCKER_BUILDKIT=1 docker build --build-arg builder_image=$(GO_CONTAINER_IMAGE) --build-arg goproxy=$(GOPROXY) --build-arg ARCH=$(ARCH) --build-arg ldflags="$(LDFLAGS)" . -t $(CONTROLLER_IMG)-$(ARCH):$(TAG)
	$(MAKE) set-manifest-image MANIFEST_IMG=$(CONTROLLER_IMG)-$(ARCH) MANIFEST_TAG=$(TAG) TARGET_RESOURCE="./config/default/manager_image_patch.yaml"
	$(MAKE) set-manifest-pull-policy TARGET_RESOURCE="./config/default/manager_pull_policy.yaml"

.PHONY: docker-build-kubeadm-bootstrap
docker-build-kubeadm-bootstrap: ## Build the docker image for kubeadm bootstrap controller manager
	DOCKER_BUILDKIT=1 docker build --build-arg builder_image=$(GO_CONTAINER_IMAGE) --build-arg goproxy=$(GOPROXY) --build-arg ARCH=$(ARCH) --build-arg package=./bootstrap/kubeadm --build-arg ldflags="$(LDFLAGS)" . -t $(KUBEADM_BOOTSTRAP_CONTROLLER_IMG)-$(ARCH):$(TAG)
	$(MAKE) set-manifest-image MANIFEST_IMG=$(KUBEADM_BOOTSTRAP_CONTROLLER_IMG)-$(ARCH) MANIFEST_TAG=$(TAG) TARGET_RESOURCE="./bootstrap/kubeadm/config/default/manager_image_patch.yaml"
	$(MAKE) set-manifest-pull-policy TARGET_RESOURCE="./bootstrap/kubeadm/config/default/manager_pull_policy.yaml"

.PHONY: docker-build-kubeadm-control-plane
docker-build-kubeadm-control-plane: ## Build the docker image for kubeadm control plane controller manager
	DOCKER_BUILDKIT=1 docker build --build-arg builder_image=$(GO_CONTAINER_IMAGE) --build-arg goproxy=$(GOPROXY) --build-arg ARCH=$(ARCH) --build-arg package=./controlplane/kubeadm --build-arg ldflags="$(LDFLAGS)" . -t $(KUBEADM_CONTROL_PLANE_CONTROLLER_IMG)-$(ARCH):$(TAG)
	$(MAKE) set-manifest-image MANIFEST_IMG=$(KUBEADM_CONTROL_PLANE_CONTROLLER_IMG)-$(ARCH) MANIFEST_TAG=$(TAG) TARGET_RESOURCE="./controlplane/kubeadm/config/default/manager_image_patch.yaml"
	$(MAKE) set-manifest-pull-policy TARGET_RESOURCE="./controlplane/kubeadm/config/default/manager_pull_policy.yaml"

.PHONY: docker-push
docker-push: ## Push the docker images
	docker push $(CONTROLLER_IMG)-$(ARCH):$(TAG)
	docker push $(KUBEADM_BOOTSTRAP_CONTROLLER_IMG)-$(ARCH):$(TAG)
	docker push $(KUBEADM_CONTROL_PLANE_CONTROLLER_IMG)-$(ARCH):$(TAG)

## --------------------------------------
## Docker — All ARCH
## --------------------------------------

.PHONY: docker-build-all ## Build all the architecture docker images
docker-build-all: $(addprefix docker-build-,$(ALL_ARCH))

docker-build-%:
	$(MAKE) ARCH=$* docker-build

.PHONY: docker-push-all ## Push all the architecture docker images
docker-push-all: $(addprefix docker-push-,$(ALL_ARCH))
	$(MAKE) docker-push-core-manifest
	$(MAKE) docker-push-kubeadm-bootstrap-manifest
	$(MAKE) docker-push-kubeadm-control-plane-manifest

docker-push-%:
	$(MAKE) ARCH=$* docker-push

.PHONY: docker-push-core-manifest
docker-push-core-manifest: ## Push the fat manifest docker image for the core image.
	## Minimum docker version 18.06.0 is required for creating and pushing manifest images.
	docker manifest create --amend $(CONTROLLER_IMG):$(TAG) $(shell echo $(ALL_ARCH) | sed -e "s~[^ ]*~$(CONTROLLER_IMG)\-&:$(TAG)~g")
	@for arch in $(ALL_ARCH); do docker manifest annotate --arch $${arch} ${CONTROLLER_IMG}:${TAG} ${CONTROLLER_IMG}-$${arch}:${TAG}; done
	docker manifest push --purge $(CONTROLLER_IMG):$(TAG)
	$(MAKE) set-manifest-image MANIFEST_IMG=$(CONTROLLER_IMG) MANIFEST_TAG=$(TAG) TARGET_RESOURCE="./config/default/manager_image_patch.yaml"
	$(MAKE) set-manifest-pull-policy TARGET_RESOURCE="./config/default/manager_pull_policy.yaml"

.PHONY: docker-push-kubeadm-bootstrap-manifest
docker-push-kubeadm-bootstrap-manifest: ## Push the fat manifest docker image for the kubeadm bootstrap image.
	## Minimum docker version 18.06.0 is required for creating and pushing manifest images.
	docker manifest create --amend $(KUBEADM_BOOTSTRAP_CONTROLLER_IMG):$(TAG) $(shell echo $(ALL_ARCH) | sed -e "s~[^ ]*~$(KUBEADM_BOOTSTRAP_CONTROLLER_IMG)\-&:$(TAG)~g")
	@for arch in $(ALL_ARCH); do docker manifest annotate --arch $${arch} ${KUBEADM_BOOTSTRAP_CONTROLLER_IMG}:${TAG} ${KUBEADM_BOOTSTRAP_CONTROLLER_IMG}-$${arch}:${TAG}; done
	docker manifest push --purge $(KUBEADM_BOOTSTRAP_CONTROLLER_IMG):$(TAG)
	$(MAKE) set-manifest-image MANIFEST_IMG=$(KUBEADM_BOOTSTRAP_CONTROLLER_IMG) MANIFEST_TAG=$(TAG) TARGET_RESOURCE="./bootstrap/kubeadm/config/default/manager_image_patch.yaml"
	$(MAKE) set-manifest-pull-policy TARGET_RESOURCE="./bootstrap/kubeadm/config/default/manager_pull_policy.yaml"

.PHONY: docker-push-kubeadm-control-plane-manifest
docker-push-kubeadm-control-plane-manifest: ## Push the fat manifest docker image for the kubeadm control plane image.
	## Minimum docker version 18.06.0 is required for creating and pushing manifest images.
	docker manifest create --amend $(KUBEADM_CONTROL_PLANE_CONTROLLER_IMG):$(TAG) $(shell echo $(ALL_ARCH) | sed -e "s~[^ ]*~$(KUBEADM_CONTROL_PLANE_CONTROLLER_IMG)\-&:$(TAG)~g")
	@for arch in $(ALL_ARCH); do docker manifest annotate --arch $${arch} ${KUBEADM_CONTROL_PLANE_CONTROLLER_IMG}:${TAG} ${KUBEADM_CONTROL_PLANE_CONTROLLER_IMG}-$${arch}:${TAG}; done
	docker manifest push --purge $(KUBEADM_CONTROL_PLANE_CONTROLLER_IMG):$(TAG)
	$(MAKE) set-manifest-image MANIFEST_IMG=$(KUBEADM_CONTROL_PLANE_CONTROLLER_IMG) MANIFEST_TAG=$(TAG) TARGET_RESOURCE="./controlplane/kubeadm/config/default/manager_image_patch.yaml"
	$(MAKE) set-manifest-pull-policy TARGET_RESOURCE="./controlplane/kubeadm/config/default/manager_pull_policy.yaml"

.PHONY: set-manifest-pull-policy
set-manifest-pull-policy:
	$(info Updating kustomize pull policy file for manager resources)
	sed -i'' -e 's@imagePullPolicy: .*@imagePullPolicy: '"$(PULL_POLICY)"'@' $(TARGET_RESOURCE)

.PHONY: set-manifest-image
set-manifest-image:
	$(info Updating kustomize image patch file for manager resource)
	sed -i'' -e 's@image: .*@image: '"${MANIFEST_IMG}:$(MANIFEST_TAG)"'@' $(TARGET_RESOURCE)

## --------------------------------------
## Release
## --------------------------------------

## latest git tag for the commit, e.g., v0.3.10
RELEASE_TAG := $(shell git describe --abbrev=0 2>/dev/null)
## set by Prow, ref name of the base branch, e.g., master
RELEASE_ALIAS_TAG := $(PULL_BASE_REF)
RELEASE_DIR := out

$(RELEASE_DIR):
	mkdir -p $(RELEASE_DIR)/

.PHONY: release
release: clean-release ## Builds and push container images using the latest git tag for the commit.
	@if [ -z "${RELEASE_TAG}" ]; then echo "RELEASE_TAG is not set"; exit 1; fi
	@if ! [ -z "$$(git status --porcelain)" ]; then echo "Your local git repository contains uncommitted changes, use git clean before proceeding."; exit 1; fi
	git checkout "${RELEASE_TAG}"
	# Build binaries first.
	$(MAKE) release-binaries
	# Set the manifest image to the production bucket.
	$(MAKE) manifest-modification REGISTRY=$(PROD_REGISTRY)
	## Build the manifests
	$(MAKE) release-manifests clean-release-git
	## Build the development manifests
	$(MAKE) release-manifests-dev clean-release-git

.PHONY: manifest-modification
manifest-modification: # Set the manifest images to the staging/production bucket.
	$(MAKE) set-manifest-image \
		MANIFEST_IMG=$(REGISTRY)/$(IMAGE_NAME) MANIFEST_TAG=$(RELEASE_TAG) \
		TARGET_RESOURCE="./config/default/manager_image_patch.yaml"
	$(MAKE) set-manifest-image \
		MANIFEST_IMG=$(REGISTRY)/$(KUBEADM_BOOTSTRAP_IMAGE_NAME) MANIFEST_TAG=$(RELEASE_TAG) \
		TARGET_RESOURCE="./bootstrap/kubeadm/config/default/manager_image_patch.yaml"
	$(MAKE) set-manifest-image \
		MANIFEST_IMG=$(REGISTRY)/$(KUBEADM_CONTROL_PLANE_IMAGE_NAME) MANIFEST_TAG=$(RELEASE_TAG) \
		TARGET_RESOURCE="./controlplane/kubeadm/config/default/manager_image_patch.yaml"
	$(MAKE) set-manifest-pull-policy PULL_POLICY=IfNotPresent TARGET_RESOURCE="./config/default/manager_pull_policy.yaml"
	$(MAKE) set-manifest-pull-policy PULL_POLICY=IfNotPresent TARGET_RESOURCE="./bootstrap/kubeadm/config/default/manager_pull_policy.yaml"
	$(MAKE) set-manifest-pull-policy PULL_POLICY=IfNotPresent TARGET_RESOURCE="./controlplane/kubeadm/config/default/manager_pull_policy.yaml"

.PHONY: release-manifests
release-manifests: $(RELEASE_DIR) $(KUSTOMIZE) ## Builds the manifests to publish with a release
	# Build core-components.
	$(KUSTOMIZE) build config/default > $(RELEASE_DIR)/core-components.yaml
	# Build bootstrap-components.
	$(KUSTOMIZE) build bootstrap/kubeadm/config/default > $(RELEASE_DIR)/bootstrap-components.yaml
	# Build control-plane-components.
	$(KUSTOMIZE) build controlplane/kubeadm/config/default > $(RELEASE_DIR)/control-plane-components.yaml

	## Build cluster-api-components (aggregate of all of the above).
	cat $(RELEASE_DIR)/core-components.yaml > $(RELEASE_DIR)/cluster-api-components.yaml
	echo "---" >> $(RELEASE_DIR)/cluster-api-components.yaml
	cat $(RELEASE_DIR)/bootstrap-components.yaml >> $(RELEASE_DIR)/cluster-api-components.yaml
	echo "---" >> $(RELEASE_DIR)/cluster-api-components.yaml
	cat $(RELEASE_DIR)/control-plane-components.yaml >> $(RELEASE_DIR)/cluster-api-components.yaml
	# Add metadata to the release artifacts
	cp metadata.yaml $(RELEASE_DIR)/metadata.yaml

.PHONY: release-manifests-dev
release-manifests-dev: ## Builds the development manifests and copies them in the release folder
	# Release CAPD components and add them to the release dir
	$(MAKE) -C $(CAPD_DIR) release
	cp $(CAPD_DIR)/out/infrastructure-components.yaml $(RELEASE_DIR)/infrastructure-components-development.yaml
	# Adds CAPD templates
	cp $(CAPD_DIR)/templates/* $(RELEASE_DIR)/

release-binaries: ## Builds the binaries to publish with a release
	RELEASE_BINARY=./cmd/clusterctl GOOS=linux GOARCH=amd64 $(MAKE) release-binary
	RELEASE_BINARY=./cmd/clusterctl GOOS=linux GOARCH=arm64 $(MAKE) release-binary
	RELEASE_BINARY=./cmd/clusterctl GOOS=darwin GOARCH=amd64 $(MAKE) release-binary
	RELEASE_BINARY=./cmd/clusterctl GOOS=darwin GOARCH=arm64 $(MAKE) release-binary

release-binary: $(RELEASE_DIR)
	docker run \
		--rm \
		-e CGO_ENABLED=0 \
		-e GOOS=$(GOOS) \
		-e GOARCH=$(GOARCH) \
		-v "$$(pwd):/workspace$(DOCKER_VOL_OPTS)" \
		-w /workspace \
		golang:$(GO_VERSION) \
		go build -a -ldflags "$(LDFLAGS) -extldflags '-static'" \
		-o $(RELEASE_DIR)/$(notdir $(RELEASE_BINARY))-$(GOOS)-$(GOARCH) $(RELEASE_BINARY)

.PHONY: release-staging
release-staging: ## Builds and push container images to the staging bucket.
	REGISTRY=$(STAGING_REGISTRY) $(MAKE) docker-build-all docker-push-all release-alias-tag

.PHONY: release-staging-nightly
release-staging-nightly: ## Tags and push container images to the staging bucket. Example image tag: cluster-api-controller:nightly_master_20210121
	$(eval NEW_RELEASE_ALIAS_TAG := nightly_$(RELEASE_ALIAS_TAG)_$(shell date +'%Y%m%d'))
	echo $(NEW_RELEASE_ALIAS_TAG)
	$(MAKE) release-alias-tag TAG=$(RELEASE_ALIAS_TAG) RELEASE_ALIAS_TAG=$(NEW_RELEASE_ALIAS_TAG)
	# Set the manifest image to the staging bucket.
	$(MAKE) manifest-modification REGISTRY=$(STAGING_REGISTRY) RELEASE_TAG=$(NEW_RELEASE_ALIAS_TAG)
	## Build the manifests
	$(MAKE) release-manifests
	# Example manifest location: artifacts.k8s-staging-cluster-api.appspot.com/components/nightly_master_20210121/bootstrap-components.yaml
	gsutil cp $(RELEASE_DIR)/* gs://$(STAGING_BUCKET)/components/$(NEW_RELEASE_ALIAS_TAG)

.PHONY: release-alias-tag
release-alias-tag: ## Adds the tag to the last build tag.
	gcloud container images add-tag $(CONTROLLER_IMG):$(TAG) $(CONTROLLER_IMG):$(RELEASE_ALIAS_TAG)
	gcloud container images add-tag $(KUBEADM_BOOTSTRAP_CONTROLLER_IMG):$(TAG) $(KUBEADM_BOOTSTRAP_CONTROLLER_IMG):$(RELEASE_ALIAS_TAG)
	gcloud container images add-tag $(KUBEADM_CONTROL_PLANE_CONTROLLER_IMG):$(TAG) $(KUBEADM_CONTROL_PLANE_CONTROLLER_IMG):$(RELEASE_ALIAS_TAG)

## --------------------------------------
## Cleanup / Verification
## --------------------------------------

.PHONY: clean
clean: ## Remove all generated files
	$(MAKE) clean-bin
	$(MAKE) clean-book

.PHONY: clean-bin
clean-bin: ## Remove all generated binaries
	rm -rf $(BIN_DIR)
	rm -rf $(TOOLS_BIN_DIR)

.PHONY: clean-release
clean-release: ## Remove the release folder
	rm -rf $(RELEASE_DIR)

.PHONY: clean-release-git
clean-release-git: ## Restores the git files usually modified during a release
	git restore ./*manager_image_patch.yaml ./*manager_pull_policy.yaml

.PHONY: clean-book
clean-book: ## Remove all generated GitBook files
	rm -rf ./docs/book/_book

.PHONY: clean-manifests ## Reset manifests in config directories back to master
clean-manifests:
	@read -p "WARNING: This will reset all config directories to local master. Press [ENTER] to continue."
	git checkout master config bootstrap/kubeadm/config controlplane/kubeadm/config $(CAPD_DIR)/config

.PHONY: format-tiltfile
format-tiltfile: ## Format Tiltfile
	./hack/verify-starlark.sh fix

.PHONY: verify
verify:
	./hack/verify-boilerplate.sh
	./hack/verify-doctoc.sh
	./hack/verify-shellcheck.sh
	./hack/verify-starlark.sh
	$(MAKE) verify-modules
	$(MAKE) verify-gen
	$(MAKE) verify-docker-provider

.PHONY: verify-modules
verify-modules: modules
	@if !(git diff --quiet HEAD -- go.sum go.mod $(TOOLS_DIR)/go.mod $(TOOLS_DIR)/go.sum); then \
		git diff; \
		echo "go module files are out of date"; exit 1; \
	fi
	@if (find . -name 'go.mod' | xargs -n1 grep -q -i 'k8s.io/client-go.*+incompatible'); then \
		find . -name "go.mod" -exec grep -i 'k8s.io/client-go.*+incompatible' {} \; -print; \
		echo "go module contains an incompatible client-go version"; exit 1; \
	fi

.PHONY: verify-gen
verify-gen: generate
	@if !(git diff --quiet HEAD); then \
		git diff; \
		echo "generated files are out of date, run make generate"; exit 1; \
	fi

.PHONY: verify-docker-provider
verify-docker-provider:
	@echo "Verifying CAPD"
	cd $(CAPD_DIR); $(MAKE) verify

.PHONY: verify-book-links
verify-book-links:
	$(MAKE) -C docs/book verify

## --------------------------------------
## Others / Utilities
## --------------------------------------

.PHONY: diagrams
diagrams: ## Build proposal diagrams
	$(MAKE) -C docs diagrams

.PHONY: serve-book
serve-book: ## Build and serve the book with live-reloading enabled
	$(MAKE) -C docs/book serve

.PHONY: clean-generated-conversions
clean-generated-conversions: ## Remove files generated by conversion-gen from the mentioned dirs
	(IFS=','; for i in $(SRC_DIRS); do find $$i -type f -name 'zz_generated.conversion*' -exec rm -f {} \;; done)
