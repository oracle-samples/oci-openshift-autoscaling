# Copyright (c) 2025, 2026 Oracle and/or its affiliates.
# Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/.

# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
VERSION ?= 0.0.2

# CHANNELS define the bundle channels used in the bundle.
# Add a new line here if you would like to change its default config. (E.g CHANNELS = "candidate,fast,stable")
# To re-generate a bundle for other specific channels without changing the standard setup, you can:
# - use the CHANNELS as arg of the bundle target (e.g make bundle CHANNELS=candidate,fast,stable)
# - use environment variables to overwrite this value (e.g export CHANNELS="candidate,fast,stable")
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif

# DEFAULT_CHANNEL defines the default channel used in the bundle.
# Add a new line here if you would like to change its default config. (E.g DEFAULT_CHANNEL = "stable")
# To re-generate a bundle for any other default channel without changing the default setup, you can:
# - use the DEFAULT_CHANNEL as arg of the bundle target (e.g make bundle DEFAULT_CHANNEL=stable)
# - use environment variables to overwrite this value (e.g export DEFAULT_CHANNEL="stable")
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

# IMAGE_TAG_BASE defines the docker.io namespace and part of the image name for remote images.
# This variable is used to construct full image tags for bundle and catalog images.
#
# For example, running 'make bundle-build bundle-push catalog-build catalog-push' will build and push both
# openshift.io/oci-capi-operator-bundle:$VERSION and openshift.io/oci-capi-operator-catalog:$VERSION.
IMAGE_TAG_BASE ?= openshift.io/oci-capi-operator

# BUNDLE_IMG defines the image:tag used for the bundle.
# You can use it as an arg. (E.g make bundle-build BUNDLE_IMG=<some-registry>/<project-name-bundle>:<tag>)
BUNDLE_IMG ?= $(IMAGE_TAG_BASE)-bundle:v$(VERSION)

# BUNDLE_GEN_FLAGS are the flags passed to the operator-sdk generate bundle command
BUNDLE_GEN_FLAGS ?= -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)

# USE_IMAGE_DIGESTS defines if images are resolved via tags or digests
# You can enable this value if you would like to use SHA Based Digests
# To enable set flag to true
USE_IMAGE_DIGESTS ?= false
ifeq ($(USE_IMAGE_DIGESTS), true)
	BUNDLE_GEN_FLAGS += --use-image-digests
endif

# Set the Operator SDK version to use. By default, what is installed on the system is used.
# This is useful for CI or a project to utilize a specific version of the operator-sdk toolkit.
OPERATOR_SDK_VERSION ?= v1.39.2
# Image URL to use all building/pushing image targets
IMG ?= controller:latest
# CAPI_VERSION defines the version used for CAPI provider components
CAPI_VERSION ?= v1.12.3
# CAPOCI_VERSION defines the version used for CAPOCI provider components
CAPOCI_VERSION ?= v0.24.0
# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.31.0

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# CONTAINER_TOOL defines the container tool to be used for building images.
# Be aware that the target commands are only tested with Docker which is
# scaffolded by default. However, you might want to replace it to use other
# tools. (i.e. podman)
CONTAINER_TOOL ?= docker

# REQUEST_TIMEOUT defines the timeout passed to kubectl delete for cleanup
# operations in the undeploy target.
REQUEST_TIMEOUT ?= 60s

# Default OCIClusterAutoscaler identity used by day0 manifests and cleanup helpers.
AUTOSCALER_NAMESPACE ?= oci-openshift-autoscaling-operator
AUTOSCALER_NAME ?= ociclusterautoscaler
AUTOSCALER_CLUSTER_NAMESPACE ?= oci-openshift-autoscaling-operator
AUTOSCALER_CLUSTER_NAME ?=
AUTOSCALER_LABEL_SELECTOR ?= capi.openshift.io/managed-by=$(AUTOSCALER_NAME)
AUTOSCALER_PROVIDER_INSTALLER_JOB ?= oci-capi-operator-provider-installer
AUTOSCALER_DELETE_NAMESPACE ?= false
CERT_MANAGER_NAMESPACE ?= cert-manager
AUTOSCALER_DELETE_CERT_MANAGER ?= false
AUTOSCALER_LEGACY_CAPI_NAMESPACE ?= capi-system
AUTOSCALER_LEGACY_CAPOCI_NAMESPACE ?= cluster-api-provider-oci-system
AUTOSCALER_LEGACY_OPERATOR_NAMESPACE ?= oci-capi-operator

# Run recipes under Bash because some targets use Bash-only conditionals and regex matching.
# Exit on command failure and fail pipelines when any segment fails.
SHELL := /bin/bash
.SHELLFLAGS := -o pipefail -ec

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk command is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	mkdir -p "$(GO_BUILD_CACHE)" "$(XDG_CACHE_HOME)"
	GOCACHE="$(GO_BUILD_CACHE)" XDG_CACHE_HOME="$(XDG_CACHE_HOME)" $(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	mkdir -p "$(GO_BUILD_CACHE)" "$(XDG_CACHE_HOME)"
	GOCACHE="$(GO_BUILD_CACHE)" XDG_CACHE_HOME="$(XDG_CACHE_HOME)" $(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	mkdir -p "$(GO_BUILD_CACHE)" "$(XDG_CACHE_HOME)"
	GOCACHE="$(GO_BUILD_CACHE)" XDG_CACHE_HOME="$(XDG_CACHE_HOME)" go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	mkdir -p "$(GO_BUILD_CACHE)" "$(XDG_CACHE_HOME)"
	GOCACHE="$(GO_BUILD_CACHE)" XDG_CACHE_HOME="$(XDG_CACHE_HOME)" go vet ./...

.PHONY: test
test: manifests generate fmt vet envtest ## Run tests.
	mkdir -p "$(GO_BUILD_CACHE)" "$(XDG_CACHE_HOME)" "$(TEST_HOME)/Library/Caches"
	HOME="$(TEST_HOME)" KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" GOCACHE="$(GO_BUILD_CACHE)" XDG_CACHE_HOME="$(XDG_CACHE_HOME)" /bin/sh -c 'go test $$(go list ./... | grep -v /e2e) -coverprofile cover.out'

# Utilize Kind or modify the e2e tests to load the image locally, enabling compatibility with other vendors.
.PHONY: test-e2e  # Run the e2e tests against a Kind k8s instance that is spun up.
test-e2e:
	mkdir -p "$(GO_BUILD_CACHE)" "$(XDG_CACHE_HOME)"
	GOCACHE="$(GO_BUILD_CACHE)" XDG_CACHE_HOME="$(XDG_CACHE_HOME)" go test ./test/e2e/ -v -ginkgo.v

.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter
	$(GOLANGCI_LINT) run

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	$(GOLANGCI_LINT) run --fix

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build manager binary.
	go build -o bin/manager cmd/main.go

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./cmd/main.go

# If you wish to build the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: build-image
build-image: ## Build docker image with the manager.
	$(CONTAINER_TOOL) build -t ${IMG} . --platform=linux/arm64 --build-arg TARGETOS=linux --build-arg TARGETARCH=arm64 --build-arg CAPOCI_VERSION=$(CAPOCI_VERSION)

.PHONY: docker-build
docker-build: build-image ## Backward-compatible alias used by the e2e suite.

.PHONY: push-image
push-image: ## Build and push docker image with the manager for linux/amd64.
	$(CONTAINER_TOOL) build -t ${IMG} . --platform=linux/amd64 --build-arg TARGETOS=linux --build-arg TARGETARCH=amd64
	$(CONTAINER_TOOL) push ${IMG}

PLATFORMS ?= linux/amd64,linux/arm64
BUILDX_DRIVER ?= docker
.PHONY: buildx
buildx: ## Build and push a multi-arch manager image manifest
	# copy existing Dockerfile to Dockerfile.cross, ensuring the builder stage has a build platform only when missing
	mkdir -p .tmp
	perl -pe 'if ($$. == 1) { s/^FROM (?!.*--platform=)/FROM --platform=$${BUILDPLATFORM} / }' Dockerfile > .tmp/Dockerfile.cross
	if [ "$(BUILDX_DRIVER)" != "docker" ]; then \
		$(CONTAINER_TOOL) buildx create --name oci-capi-operator-builder --driver $(BUILDX_DRIVER) || true; \
		$(CONTAINER_TOOL) buildx use oci-capi-operator-builder; \
	fi
	$(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) --tag ${IMG} -f .tmp/Dockerfile.cross . --build-arg CAPOCI_VERSION=$(CAPOCI_VERSION)
	if [ "$(BUILDX_DRIVER)" != "docker" ]; then \
		$(CONTAINER_TOOL) buildx rm oci-capi-operator-builder || true; \
	fi
	rm .tmp/Dockerfile.cross

.PHONY: build-installer
build-installer: manifests generate kustomize ## Generate a consolidated YAML with CRDs and deployment.
	mkdir -p dist
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	CAPI_VERSION=$(CAPI_VERSION) CAPOCI_VERSION=$(CAPOCI_VERSION) $(KUSTOMIZE) build config/default | envsubst > dist/install.yaml

##@ Deployment

ifndef INF
  INF = false
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) apply -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) delete --ignore-not-found=$(INF) -f -

.PHONY: deploy
deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	CAPI_VERSION=$(CAPI_VERSION) CAPOCI_VERSION=$(CAPOCI_VERSION) $(KUSTOMIZE) build config/default | envsubst | $(KUBECTL) apply -f -

.PHONY: undeploy
undeploy: kustomize ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	CAPI_VERSION=$(CAPI_VERSION) CAPOCI_VERSION=$(CAPOCI_VERSION) $(KUSTOMIZE) build config/default | envsubst | $(KUBECTL) delete --ignore-not-found=$(INF) -f -
	@for ns in $(AUTOSCALER_CLUSTER_NAMESPACE) $(AUTOSCALER_LEGACY_CAPI_NAMESPACE); do \
		$(KUBECTL) -n $$ns delete deployment capi-manager oci-cluster-autoscaler --ignore-not-found=$(INF) || true; \
	done
	@for ns in $(AUTOSCALER_CLUSTER_NAMESPACE) $(AUTOSCALER_LEGACY_CAPOCI_NAMESPACE); do \
		$(KUBECTL) -n $$ns delete deployment capoci-controller-manager --ignore-not-found=$(INF) || true; \
	done
	@for ns in $(AUTOSCALER_NAMESPACE) $(AUTOSCALER_LEGACY_OPERATOR_NAMESPACE); do \
		$(KUBECTL) -n $$ns delete deployment oci-capi-operator-controller-manager --ignore-not-found=$(INF) || true; \
	done
	$(KUBECTL) delete validatingwebhookconfiguration capoci-validating-webhook-configuration --wait=false --ignore-not-found=$(INF) --request-timeout=$(REQUEST_TIMEOUT)
	$(KUBECTL) delete mutatingwebhookconfiguration capoci-mutating-webhook-configuration --wait=false --ignore-not-found=$(INF) --request-timeout=$(REQUEST_TIMEOUT)
	$(KUBECTL) delete validatingwebhookconfiguration capi-validating-webhook-configuration --wait=false --ignore-not-found=$(INF) --request-timeout=$(REQUEST_TIMEOUT)
	$(KUBECTL) delete mutatingwebhookconfiguration capi-mutating-webhook-configuration --wait=false --ignore-not-found=$(INF) --request-timeout=$(REQUEST_TIMEOUT)
	@for ns in $(AUTOSCALER_CLUSTER_NAMESPACE) $(AUTOSCALER_LEGACY_CAPI_NAMESPACE) $(AUTOSCALER_LEGACY_CAPOCI_NAMESPACE); do \
		$(KUBECTL) delete ns $$ns --wait=false --ignore-not-found=$(INF) --request-timeout=$(REQUEST_TIMEOUT) || true; \
	done
	$(KUBECTL) delete crd -l cluster.x-k8s.io/provider --ignore-not-found=$(INF) --request-timeout=$(REQUEST_TIMEOUT)
	@for ns in $(AUTOSCALER_CLUSTER_NAMESPACE) $(AUTOSCALER_LEGACY_CAPI_NAMESPACE) $(AUTOSCALER_LEGACY_CAPOCI_NAMESPACE); do \
		$(KUBECTL) patch ns $$ns --type=json -p '[{"op":"remove","path":"/spec/finalizers"}]' || true; \
	done

##@ Cleanup

.PHONY: cleanup-autoscaler
cleanup-autoscaler: ## Remove the OCI CAPI Operator resources using day0 defaults.
	CONFIRM_OPERATOR_ONLY_TEARDOWN=true $(MAKE) cleanup-operator-only AUTOSCALER_DELETE_NAMESPACE=true

.PHONY: cleanup-autoscaler-full
cleanup-autoscaler-full: ## Remove CAPI/CAPOCI, cluster-autoscaler, and OCI CAPI Operator resources using day0 defaults.
	CONFIRM_PROVIDER_TEARDOWN=true $(MAKE) cleanup-capi-autoscaler

## Remove OCI CAPI Operator resources only (keep CAPI/CAPOCI).
.PHONY: cleanup-operator-only
cleanup-operator-only: ## Remove OCI CAPI Operator resources only (keep CAPI/CAPOCI).
	@test "$(CONFIRM_OPERATOR_ONLY_TEARDOWN)" = "true" || (echo "Refusing destructive cleanup. Re-run with CONFIRM_OPERATOR_ONLY_TEARDOWN=true." >&2; exit 1)
	@for ns in $(AUTOSCALER_NAMESPACE) $(AUTOSCALER_LEGACY_OPERATOR_NAMESPACE); do \
		if $(KUBECTL) get ociclusterautoscaler -n $$ns $(AUTOSCALER_NAME) >/dev/null 2>&1; then \
			$(KUBECTL) delete ociclusterautoscaler -n $$ns $(AUTOSCALER_NAME) --ignore-not-found=true --wait=false --request-timeout=$(REQUEST_TIMEOUT) || true; \
			$(KUBECTL) patch ociclusterautoscaler -n $$ns $(AUTOSCALER_NAME) --type=merge -p '{"metadata":{"finalizers":[]}}' --request-timeout=$(REQUEST_TIMEOUT) || true; \
		else \
			echo "OCIClusterAutoscaler $$ns/$(AUTOSCALER_NAME) not found; continuing cleanup."; \
		fi; \
	done
	@if $(KUBECTL) get crd ociclusterautoscalers.capi.openshift.io >/dev/null 2>&1; then \
		$(KUSTOMIZE) build config/samples | $(KUBECTL) delete --ignore-not-found=true --wait=false -f -; \
	else \
		echo "OCIClusterAutoscaler API is not registered; skipping sample CR cleanup."; \
	fi
	CAPI_VERSION=$(CAPI_VERSION) CAPOCI_VERSION=$(CAPOCI_VERSION) $(KUSTOMIZE) build config/default | envsubst | $(KUBECTL) delete --ignore-not-found=true -f -
	@for ns in $(AUTOSCALER_NAMESPACE) $(AUTOSCALER_LEGACY_OPERATOR_NAMESPACE); do \
		$(KUBECTL) -n $$ns delete deployment oci-capi-operator-controller-manager --ignore-not-found=true || true; \
		$(KUBECTL) -n $$ns delete job $(AUTOSCALER_PROVIDER_INSTALLER_JOB) oci-capi-operator-activate-after-install --ignore-not-found=true --wait=false --request-timeout=$(REQUEST_TIMEOUT) || true; \
		$(KUBECTL) -n $$ns delete configmap oci-capi-operator-config oci-capi-operator-runtime-manifest --ignore-not-found=true --request-timeout=$(REQUEST_TIMEOUT) || true; \
		$(KUBECTL) -n $$ns delete serviceaccount oci-capi-operator-controller-manager oci-capi-operator-activator --ignore-not-found=true --request-timeout=$(REQUEST_TIMEOUT) || true; \
		$(KUBECTL) -n $$ns delete role oci-capi-operator-leader-election-role --ignore-not-found=true --request-timeout=$(REQUEST_TIMEOUT) || true; \
		$(KUBECTL) -n $$ns delete rolebinding oci-capi-operator-leader-election-rolebinding --ignore-not-found=true --request-timeout=$(REQUEST_TIMEOUT) || true; \
	done
	$(KUBECTL) delete clusterrole oci-capi-operator-manager-role oci-capi-operator-ociclusterautoscaler-editor-role oci-capi-operator-ociclusterautoscaler-viewer-role --ignore-not-found=true --request-timeout=$(REQUEST_TIMEOUT)
	$(KUBECTL) delete clusterrolebinding oci-capi-operator-manager-rolebinding oci-capi-operator-oci-capi-operator-admin oci-capi-operator-activator-admin --ignore-not-found=true --request-timeout=$(REQUEST_TIMEOUT)
	$(KUBECTL) delete validatingwebhookconfiguration oci-capi-operator-validating-webhook-configuration --wait=false --ignore-not-found=true --request-timeout=$(REQUEST_TIMEOUT)
	$(KUBECTL) delete mutatingwebhookconfiguration oci-capi-operator-mutating-webhook-configuration --wait=false --ignore-not-found=true --request-timeout=$(REQUEST_TIMEOUT)
	$(KUSTOMIZE) build config/crd | $(KUBECTL) delete --ignore-not-found=true -f -
	@if [ "$(AUTOSCALER_DELETE_NAMESPACE)" = "true" ]; then \
		for ns in $(AUTOSCALER_NAMESPACE) $(AUTOSCALER_LEGACY_OPERATOR_NAMESPACE); do \
			[ "$$ns" = "default" ] && continue; \
			$(KUBECTL) delete ns $$ns --ignore-not-found=true --wait=false --request-timeout=$(REQUEST_TIMEOUT) || true; \
		done; \
	fi

.PHONY: cleanup-provider-finalizers
cleanup-provider-finalizers: ## Remove finalizers from provider-managed resources across all namespaces as a teardown fallback.
	@test "$(CONFIRM_PROVIDER_FINALIZER_CLEANUP)" = "true" || (echo "Refusing destructive finalizer cleanup. Re-run with CONFIRM_PROVIDER_FINALIZER_CLEANUP=true." >&2; exit 1)
	@cluster_names="$$( { \
		[ -n "$(AUTOSCALER_CLUSTER_NAME)" ] && printf '%s\n' "$(AUTOSCALER_CLUSTER_NAME)"; \
		$(KUBECTL) get clusters.cluster.x-k8s.io -n $(AUTOSCALER_CLUSTER_NAMESPACE) -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null || true; \
		$(KUBECTL) get machinedeployments.cluster.x-k8s.io -n $(AUTOSCALER_CLUSTER_NAMESPACE) -o jsonpath='{range .items[*]}{.spec.clusterName}{"\n"}{end}' 2>/dev/null || true; \
		$(KUBECTL) get machinesets.cluster.x-k8s.io -n $(AUTOSCALER_CLUSTER_NAMESPACE) -o jsonpath='{range .items[*]}{.spec.clusterName}{"\n"}{end}' 2>/dev/null || true; \
		$(KUBECTL) get machines.cluster.x-k8s.io -n $(AUTOSCALER_CLUSTER_NAMESPACE) -o jsonpath='{range .items[*]}{.spec.clusterName}{"\n"}{end}' 2>/dev/null || true; \
	} | sed '/^$$/d' | sort -u )"; \
	if [ -n "$$cluster_names" ]; then \
		echo "Discovered CAPI cluster names in namespace $(AUTOSCALER_CLUSTER_NAMESPACE): $$cluster_names"; \
	else \
		echo "No CAPI cluster names discovered in namespace $(AUTOSCALER_CLUSTER_NAMESPACE)."; \
	fi; \
	printf '%s\n' "$$cluster_names" > /tmp/oci-capi-autoscaler-cleanup-clusters
	@echo "Deleting provider-managed resources in namespace $(AUTOSCALER_CLUSTER_NAMESPACE) matching label selector: $(AUTOSCALER_LABEL_SELECTOR)"
	@for r in machines.cluster.x-k8s.io machinesets.cluster.x-k8s.io machinedeployments.cluster.x-k8s.io ocimachines.infrastructure.cluster.x-k8s.io ocimachinetemplates.infrastructure.cluster.x-k8s.io clusters.cluster.x-k8s.io ociclusters.infrastructure.cluster.x-k8s.io ociclusteridentities.infrastructure.cluster.x-k8s.io; do \
		$(KUBECTL) delete $$r -n $(AUTOSCALER_CLUSTER_NAMESPACE) -l '$(AUTOSCALER_LABEL_SELECTOR)' --ignore-not-found=true --wait=false --request-timeout=$(REQUEST_TIMEOUT) >/dev/null 2>&1 || true; \
	done
	@while read cluster_name; do \
		[ -n "$$cluster_name" ] || continue; \
		echo "Deleting CAPI resources in namespace $(AUTOSCALER_CLUSTER_NAMESPACE) for cluster $$cluster_name"; \
		for r in machines.cluster.x-k8s.io machinesets.cluster.x-k8s.io machinedeployments.cluster.x-k8s.io ocimachines.infrastructure.cluster.x-k8s.io ocimachinetemplates.infrastructure.cluster.x-k8s.io; do \
			$(KUBECTL) delete $$r -n $(AUTOSCALER_CLUSTER_NAMESPACE) -l "cluster.x-k8s.io/cluster-name=$$cluster_name" --ignore-not-found=true --wait=false --request-timeout=$(REQUEST_TIMEOUT) >/dev/null 2>&1 || true; \
		done; \
		for r in clusters.cluster.x-k8s.io ociclusters.infrastructure.cluster.x-k8s.io ociclusteridentities.infrastructure.cluster.x-k8s.io; do \
			$(KUBECTL) delete $$r "$$cluster_name" -n $(AUTOSCALER_CLUSTER_NAMESPACE) --ignore-not-found=true --wait=false --request-timeout=$(REQUEST_TIMEOUT) >/dev/null 2>&1 || true; \
		done; \
	done < /tmp/oci-capi-autoscaler-cleanup-clusters
	@echo "Waiting for provider-managed resources to delete before finalizer fallback"
	@for r in machines.cluster.x-k8s.io machinesets.cluster.x-k8s.io machinedeployments.cluster.x-k8s.io ocimachines.infrastructure.cluster.x-k8s.io ocimachinetemplates.infrastructure.cluster.x-k8s.io clusters.cluster.x-k8s.io ociclusters.infrastructure.cluster.x-k8s.io ociclusteridentities.infrastructure.cluster.x-k8s.io; do \
		$(KUBECTL) wait --for=delete $$r -n $(AUTOSCALER_CLUSTER_NAMESPACE) -l '$(AUTOSCALER_LABEL_SELECTOR)' --timeout=$(REQUEST_TIMEOUT) >/dev/null 2>&1 || true; \
	done
	@while read cluster_name; do \
		[ -n "$$cluster_name" ] || continue; \
		for r in machines.cluster.x-k8s.io machinesets.cluster.x-k8s.io machinedeployments.cluster.x-k8s.io ocimachines.infrastructure.cluster.x-k8s.io ocimachinetemplates.infrastructure.cluster.x-k8s.io; do \
			$(KUBECTL) wait --for=delete $$r -n $(AUTOSCALER_CLUSTER_NAMESPACE) -l "cluster.x-k8s.io/cluster-name=$$cluster_name" --timeout=$(REQUEST_TIMEOUT) >/dev/null 2>&1 || true; \
		done; \
		for r in clusters.cluster.x-k8s.io ociclusters.infrastructure.cluster.x-k8s.io ociclusteridentities.infrastructure.cluster.x-k8s.io; do \
			if $(KUBECTL) get $$r "$$cluster_name" -n $(AUTOSCALER_CLUSTER_NAMESPACE) >/dev/null 2>&1; then \
				$(KUBECTL) wait --for=delete $$r "$$cluster_name" -n $(AUTOSCALER_CLUSTER_NAMESPACE) --timeout=$(REQUEST_TIMEOUT) >/dev/null 2>&1 || true; \
			fi; \
		done; \
	done < /tmp/oci-capi-autoscaler-cleanup-clusters
	@echo "Removing provider-managed finalizers in namespace $(AUTOSCALER_CLUSTER_NAMESPACE) matching label selector: $(AUTOSCALER_LABEL_SELECTOR)"
	@for r in machines.cluster.x-k8s.io machinesets.cluster.x-k8s.io machinedeployments.cluster.x-k8s.io ocimachines.infrastructure.cluster.x-k8s.io ocimachinetemplates.infrastructure.cluster.x-k8s.io clusters.cluster.x-k8s.io ociclusters.infrastructure.cluster.x-k8s.io ociclusteridentities.infrastructure.cluster.x-k8s.io; do \
		$(KUBECTL) get $$r -n $(AUTOSCALER_CLUSTER_NAMESPACE) -l '$(AUTOSCALER_LABEL_SELECTOR)' --no-headers -o custom-columns='NAMESPACE:.metadata.namespace,NAME:.metadata.name' 2>/dev/null | while read ns name; do \
			[ -n "$$name" ] || continue; \
			echo "$$r $$ns/$$name"; \
			$(KUBECTL) patch $$r "$$name" -n "$$ns" --type=merge -p '{"metadata":{"finalizers":[]}}' --request-timeout=$(REQUEST_TIMEOUT) || true; \
		done; \
	done
	@while read cluster_name; do \
		[ -n "$$cluster_name" ] || continue; \
		echo "Removing CAPI finalizers in namespace $(AUTOSCALER_CLUSTER_NAMESPACE) for cluster $$cluster_name"; \
		for r in machines.cluster.x-k8s.io machinesets.cluster.x-k8s.io machinedeployments.cluster.x-k8s.io ocimachines.infrastructure.cluster.x-k8s.io ocimachinetemplates.infrastructure.cluster.x-k8s.io; do \
			$(KUBECTL) get $$r -n $(AUTOSCALER_CLUSTER_NAMESPACE) -l "cluster.x-k8s.io/cluster-name=$$cluster_name" --no-headers -o custom-columns='NAMESPACE:.metadata.namespace,NAME:.metadata.name' 2>/dev/null | while read ns name; do \
				[ -n "$$name" ] || continue; \
				echo "$$r $$ns/$$name"; \
				$(KUBECTL) patch $$r "$$name" -n "$$ns" --type=merge -p '{"metadata":{"finalizers":[]}}' --request-timeout=$(REQUEST_TIMEOUT) || true; \
			done; \
		done; \
		for r in clusters.cluster.x-k8s.io ociclusters.infrastructure.cluster.x-k8s.io ociclusteridentities.infrastructure.cluster.x-k8s.io; do \
			if $(KUBECTL) get $$r "$$cluster_name" -n $(AUTOSCALER_CLUSTER_NAMESPACE) >/dev/null 2>&1; then \
				echo "$$r $(AUTOSCALER_CLUSTER_NAMESPACE)/$$cluster_name"; \
				$(KUBECTL) patch $$r "$$cluster_name" -n $(AUTOSCALER_CLUSTER_NAMESPACE) --type=merge -p '{"metadata":{"finalizers":[]}}' --request-timeout=$(REQUEST_TIMEOUT) || true; \
			fi; \
		done; \
	done < /tmp/oci-capi-autoscaler-cleanup-clusters

.PHONY: cleanup-provider-installer-resources
cleanup-provider-installer-resources: ## Remove CAPI/CAPOCI provider-installer resources while preserving cert-manager.
	@test "$(CONFIRM_PROVIDER_INSTALLER_CLEANUP)" = "true" || (echo "Refusing destructive provider-installer cleanup. Re-run with CONFIRM_PROVIDER_INSTALLER_CLEANUP=true." >&2; exit 1)
	@for ns in $(AUTOSCALER_NAMESPACE) $(AUTOSCALER_LEGACY_OPERATOR_NAMESPACE); do \
		$(KUBECTL) -n $$ns delete job $(AUTOSCALER_PROVIDER_INSTALLER_JOB) --ignore-not-found=true --wait=false --request-timeout=$(REQUEST_TIMEOUT) || true; \
	done
	@for ns in $(AUTOSCALER_CLUSTER_NAMESPACE) $(AUTOSCALER_LEGACY_CAPI_NAMESPACE); do \
		$(KUBECTL) -n $$ns delete deployment capi-manager capi-controller-manager oci-cluster-autoscaler --ignore-not-found=true || true; \
	done
	@for ns in $(AUTOSCALER_CLUSTER_NAMESPACE) $(AUTOSCALER_LEGACY_CAPOCI_NAMESPACE); do \
		$(KUBECTL) -n $$ns delete deployment capoci-controller-manager --ignore-not-found=true || true; \
	done
	$(KUBECTL) delete validatingwebhookconfiguration capoci-validating-webhook-configuration capi-validating-webhook-configuration --wait=false --ignore-not-found=true --request-timeout=$(REQUEST_TIMEOUT)
	$(KUBECTL) delete mutatingwebhookconfiguration capoci-mutating-webhook-configuration capi-mutating-webhook-configuration --wait=false --ignore-not-found=true --request-timeout=$(REQUEST_TIMEOUT)
	$(KUBECTL) delete scc oci-capi --ignore-not-found=true --request-timeout=$(REQUEST_TIMEOUT)
	@for ns in $(AUTOSCALER_CLUSTER_NAMESPACE) $(AUTOSCALER_LEGACY_CAPI_NAMESPACE) $(AUTOSCALER_LEGACY_CAPOCI_NAMESPACE); do \
		$(KUBECTL) delete ns $$ns --wait=false --ignore-not-found=true --request-timeout=$(REQUEST_TIMEOUT) || true; \
	done
	@$(KUBECTL) get crd -o name 2>/dev/null | grep -E '/((clusterclasses|clusters|machinedeployments|machinedrainrules|machinehealthchecks|machinepools|machines|machinesets)\.cluster\.x-k8s\.io|(clusterresourcesetbindings|clusterresourcesets)\.addons\.cluster\.x-k8s\.io|extensionconfigs\.runtime\.cluster\.x-k8s\.io|(ocicluster|ocimachine|ocimanaged|ocivirtual).*\.infrastructure\.cluster\.x-k8s\.io)$$' | while read crd; do \
		$(KUBECTL) delete "$$crd" --ignore-not-found=true --request-timeout=$(REQUEST_TIMEOUT); \
	done || true
	@$(KUBECTL) get clusterrole -o name 2>/dev/null | grep -E '/(capi-|capoci-|oci-cluster-autoscaler)' | while read role; do \
		$(KUBECTL) delete "$$role" --ignore-not-found=true --request-timeout=$(REQUEST_TIMEOUT); \
	done || true
	@$(KUBECTL) get clusterrolebinding -o name 2>/dev/null | grep -E '/(capi-|capoci-|oci-capi-operator-capoci-privileged-scc|oci-cluster-autoscaler)' | while read rolebinding; do \
		$(KUBECTL) delete "$$rolebinding" --ignore-not-found=true --request-timeout=$(REQUEST_TIMEOUT); \
	done || true
	@for ns in $(AUTOSCALER_CLUSTER_NAMESPACE) $(AUTOSCALER_LEGACY_CAPI_NAMESPACE) $(AUTOSCALER_LEGACY_CAPOCI_NAMESPACE); do \
		if $(KUBECTL) get ns $$ns >/dev/null 2>&1; then \
			$(KUBECTL) patch ns $$ns --type=json -p '[{"op":"remove","path":"/spec/finalizers"}]' || true; \
		fi; \
	done

## Remove CAPI and autoscaler deployments/resources (includes force finalizer cleanup).
.PHONY: cleanup-capi-autoscaler
cleanup-capi-autoscaler: ## Remove CAPI and autoscaler deployments/resources (includes force finalizer cleanup).
	@test "$(CONFIRM_PROVIDER_TEARDOWN)" = "true" || (echo "Refusing destructive cleanup. Re-run with CONFIRM_PROVIDER_TEARDOWN=true." >&2; exit 1)
	@for ns in $(AUTOSCALER_NAMESPACE) $(AUTOSCALER_LEGACY_OPERATOR_NAMESPACE); do \
		$(KUBECTL) -n $$ns delete job $(AUTOSCALER_PROVIDER_INSTALLER_JOB) --ignore-not-found=true --wait=false --request-timeout=$(REQUEST_TIMEOUT) || true; \
	done
	@for ns in $(AUTOSCALER_NAMESPACE) $(AUTOSCALER_LEGACY_OPERATOR_NAMESPACE); do \
		$(KUBECTL) -n $$ns delete deployment oci-capi-operator-controller-manager --ignore-not-found=true --wait=true --timeout=$(REQUEST_TIMEOUT) || true; \
	done
	@for ns in $(AUTOSCALER_NAMESPACE) $(AUTOSCALER_LEGACY_OPERATOR_NAMESPACE); do \
		if $(KUBECTL) get ociclusterautoscaler -n $$ns $(AUTOSCALER_NAME) >/dev/null 2>&1; then \
			$(KUBECTL) delete ociclusterautoscaler -n $$ns $(AUTOSCALER_NAME) --ignore-not-found=true --wait=false --request-timeout=$(REQUEST_TIMEOUT) || true; \
			$(KUBECTL) patch ociclusterautoscaler -n $$ns $(AUTOSCALER_NAME) --type=merge -p '{"metadata":{"finalizers":[]}}' --request-timeout=$(REQUEST_TIMEOUT) || true; \
		else \
			echo "OCIClusterAutoscaler $$ns/$(AUTOSCALER_NAME) not found; continuing provider teardown."; \
		fi; \
	done
	CONFIRM_PROVIDER_FINALIZER_CLEANUP=true $(MAKE) cleanup-provider-finalizers AUTOSCALER_CLUSTER_NAMESPACE='$(AUTOSCALER_CLUSTER_NAMESPACE)' AUTOSCALER_CLUSTER_NAME='$(AUTOSCALER_CLUSTER_NAME)' AUTOSCALER_LABEL_SELECTOR='$(AUTOSCALER_LABEL_SELECTOR)'
	CONFIRM_PROVIDER_FINALIZER_CLEANUP=true $(MAKE) cleanup-provider-finalizers AUTOSCALER_CLUSTER_NAMESPACE='$(AUTOSCALER_LEGACY_CAPI_NAMESPACE)' AUTOSCALER_CLUSTER_NAME='$(AUTOSCALER_CLUSTER_NAME)' AUTOSCALER_LABEL_SELECTOR='$(AUTOSCALER_LABEL_SELECTOR)'
	$(KUBECTL) delete validatingwebhookconfiguration capoci-validating-webhook-configuration --wait=false --ignore-not-found=true --request-timeout=$(REQUEST_TIMEOUT)
	$(KUBECTL) delete mutatingwebhookconfiguration capoci-mutating-webhook-configuration --wait=false --ignore-not-found=true --request-timeout=$(REQUEST_TIMEOUT)
	$(KUBECTL) delete validatingwebhookconfiguration capi-validating-webhook-configuration --wait=false --ignore-not-found=true --request-timeout=$(REQUEST_TIMEOUT)
	$(KUBECTL) delete mutatingwebhookconfiguration capi-mutating-webhook-configuration --wait=false --ignore-not-found=true --request-timeout=$(REQUEST_TIMEOUT)
	@if [ "$(AUTOSCALER_DELETE_CERT_MANAGER)" = "true" ]; then \
		$(KUBECTL) delete validatingwebhookconfiguration cert-manager-webhook --wait=false --ignore-not-found=true --request-timeout=$(REQUEST_TIMEOUT); \
		$(KUBECTL) delete mutatingwebhookconfiguration cert-manager-webhook --wait=false --ignore-not-found=true --request-timeout=$(REQUEST_TIMEOUT); \
		$(KUBECTL) -n $(CERT_MANAGER_NAMESPACE) delete deployment cert-manager cert-manager-cainjector cert-manager-webhook --ignore-not-found=true --wait=false --request-timeout=$(REQUEST_TIMEOUT); \
	else \
		echo "Preserving cert-manager resources. Re-run with AUTOSCALER_DELETE_CERT_MANAGER=true to remove them."; \
	fi
	@for ns in $(AUTOSCALER_CLUSTER_NAMESPACE) $(AUTOSCALER_LEGACY_CAPI_NAMESPACE); do \
		$(KUBECTL) -n $$ns delete deployment capi-manager capi-controller-manager oci-cluster-autoscaler --ignore-not-found=true || true; \
	done
	@for ns in $(AUTOSCALER_CLUSTER_NAMESPACE) $(AUTOSCALER_LEGACY_CAPOCI_NAMESPACE); do \
		$(KUBECTL) -n $$ns delete deployment capoci-controller-manager --ignore-not-found=true || true; \
	done
	@echo "Second-pass provider finalizer cleanup"
	@for ns in $(AUTOSCALER_CLUSTER_NAMESPACE) $(AUTOSCALER_LEGACY_CAPI_NAMESPACE); do \
		for r in machines.cluster.x-k8s.io machinesets.cluster.x-k8s.io machinedeployments.cluster.x-k8s.io ocimachines.infrastructure.cluster.x-k8s.io ocimachinetemplates.infrastructure.cluster.x-k8s.io clusters.cluster.x-k8s.io ociclusters.infrastructure.cluster.x-k8s.io ociclusteridentities.infrastructure.cluster.x-k8s.io; do \
			$(KUBECTL) get $$r -n $$ns --no-headers -o custom-columns='NAMESPACE:.metadata.namespace,NAME:.metadata.name' 2>/dev/null | while read obj_ns name; do \
				[ -n "$$name" ] || continue; \
				echo "$$r $$obj_ns/$$name"; \
				$(KUBECTL) patch $$r "$$name" -n "$$obj_ns" --type=merge -p '{"metadata":{"finalizers":[]}}' --request-timeout=$(REQUEST_TIMEOUT) || true; \
			done; \
		done; \
	done
	@if [ "$(AUTOSCALER_DELETE_CERT_MANAGER)" = "true" ]; then \
		$(KUBECTL) -n kube-system delete role cert-manager-cainjector:leaderelection cert-manager:leaderelection --ignore-not-found=true --request-timeout=$(REQUEST_TIMEOUT); \
		$(KUBECTL) -n kube-system delete rolebinding cert-manager-cainjector:leaderelection cert-manager:leaderelection --ignore-not-found=true --request-timeout=$(REQUEST_TIMEOUT); \
		$(KUBECTL) delete clusterrole cert-manager-cainjector cert-manager-cluster-view cert-manager-controller-approve:cert-manager-io cert-manager-controller-certificates cert-manager-controller-certificatesigningrequests cert-manager-controller-challenges cert-manager-controller-clusterissuers cert-manager-controller-ingress-shim cert-manager-controller-issuers cert-manager-controller-orders cert-manager-edit cert-manager-view cert-manager-webhook:subjectaccessreviews --ignore-not-found=true --request-timeout=$(REQUEST_TIMEOUT); \
		$(KUBECTL) delete clusterrolebinding cert-manager-cainjector cert-manager-controller-approve:cert-manager-io cert-manager-controller-certificates cert-manager-controller-certificatesigningrequests cert-manager-controller-challenges cert-manager-controller-clusterissuers cert-manager-controller-ingress-shim cert-manager-controller-issuers cert-manager-controller-orders cert-manager-webhook:subjectaccessreviews --ignore-not-found=true --request-timeout=$(REQUEST_TIMEOUT); \
	fi
	@for ns in $(AUTOSCALER_CLUSTER_NAMESPACE) $(AUTOSCALER_LEGACY_CAPI_NAMESPACE) $(AUTOSCALER_LEGACY_CAPOCI_NAMESPACE); do \
		$(KUBECTL) delete ns $$ns --wait=false --ignore-not-found=true --request-timeout=$(REQUEST_TIMEOUT) || true; \
	done
	$(KUBECTL) delete crd -l cluster.x-k8s.io/provider --ignore-not-found=true --request-timeout=$(REQUEST_TIMEOUT)
	@if [ "$(AUTOSCALER_DELETE_CERT_MANAGER)" = "true" ]; then \
		$(KUBECTL) delete ns $(CERT_MANAGER_NAMESPACE) --wait=false --ignore-not-found=true --request-timeout=$(REQUEST_TIMEOUT); \
		$(KUBECTL) delete crd certificaterequests.cert-manager.io certificates.cert-manager.io challenges.acme.cert-manager.io clusterissuers.cert-manager.io issuers.cert-manager.io orders.acme.cert-manager.io --ignore-not-found=true --wait=false --request-timeout=$(REQUEST_TIMEOUT); \
	fi
	@for ns in $(AUTOSCALER_CLUSTER_NAMESPACE) $(AUTOSCALER_LEGACY_CAPI_NAMESPACE) $(AUTOSCALER_LEGACY_CAPOCI_NAMESPACE); do \
		if $(KUBECTL) get ns $$ns >/dev/null 2>&1; then \
			$(KUBECTL) patch ns $$ns --type=json -p '[{"op":"remove","path":"/spec/finalizers"}]' || true; \
		fi; \
	done
	@if [ "$(AUTOSCALER_DELETE_CERT_MANAGER)" = "true" ] && $(KUBECTL) get ns $(CERT_MANAGER_NAMESPACE) >/dev/null 2>&1; then \
		$(KUBECTL) patch ns $(CERT_MANAGER_NAMESPACE) --type=json -p '[{"op":"remove","path":"/spec/finalizers"}]' || true; \
	fi
	CONFIRM_PROVIDER_INSTALLER_CLEANUP=true $(MAKE) cleanup-provider-installer-resources AUTOSCALER_NAMESPACE='$(AUTOSCALER_NAMESPACE)'
	CONFIRM_OPERATOR_ONLY_TEARDOWN=true $(MAKE) cleanup-operator-only AUTOSCALER_NAMESPACE='$(AUTOSCALER_NAMESPACE)' AUTOSCALER_NAME='$(AUTOSCALER_NAME)' AUTOSCALER_DELETE_NAMESPACE=true REQUEST_TIMEOUT='$(REQUEST_TIMEOUT)'

##@ Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
LOCAL_CACHE_DIR ?= $(LOCALBIN)/.cache
GO_BUILD_CACHE ?= $(LOCAL_CACHE_DIR)/go-build
TEST_HOME ?= $(LOCAL_CACHE_DIR)/home
XDG_CACHE_HOME ?= $(LOCAL_CACHE_DIR)/xdg
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUBECTL ?= kubectl
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint

## Tool Versions
KUSTOMIZE_VERSION ?= v5.4.3
# Keep controller-gen aligned with the Cluster API/CAPOCI dependency set used to generate manifests.
CONTROLLER_TOOLS_VERSION ?= v0.20.0
ENVTEST_VERSION ?= release-0.19
GOLANGCI_LINT_VERSION ?= v1.59.1

.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5,$(KUSTOMIZE_VERSION))

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary.
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f $(1) || true ;\
GOBIN=$(LOCALBIN) go install $${package} ;\
mv $(1) $(1)-$(3) ;\
} ;\
ln -sf $(1)-$(3) $(1)
endef

.PHONY: operator-sdk
OPERATOR_SDK ?= $(LOCALBIN)/operator-sdk
operator-sdk: ## Download operator-sdk locally if necessary.
ifeq (,$(wildcard $(OPERATOR_SDK)))
ifeq (, $(shell which operator-sdk 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPERATOR_SDK)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPERATOR_SDK) https://github.com/operator-framework/operator-sdk/releases/download/$(OPERATOR_SDK_VERSION)/operator-sdk_$${OS}_$${ARCH} ;\
	chmod +x $(OPERATOR_SDK) ;\
	}
else
OPERATOR_SDK = $(shell which operator-sdk)
endif
endif

.PHONY: bundle
bundle: manifests kustomize operator-sdk ## Generate bundle manifests and metadata, then validate generated files.
	$(OPERATOR_SDK) generate kustomize manifests -q
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/manifests | $(OPERATOR_SDK) generate bundle $(BUNDLE_GEN_FLAGS)
	$(OPERATOR_SDK) bundle validate ./bundle

.PHONY: bundle-build
bundle-build: ## Build the bundle image.
	docker build -f bundle.Dockerfile -t $(BUNDLE_IMG) .

.PHONY: bundle-push
bundle-push: ## Push the bundle image.
	$(MAKE) docker-push IMG=$(BUNDLE_IMG)

.PHONY: opm
OPM = $(LOCALBIN)/opm
opm: ## Download opm locally if necessary.
ifeq (,$(wildcard $(OPM)))
ifeq (,$(shell which opm 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPM)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPM) https://github.com/operator-framework/operator-registry/releases/download/v1.23.0/$${OS}-$${ARCH}-opm ;\
	chmod +x $(OPM) ;\
	}
else
OPM = $(shell which opm)
endif
endif

# A comma-separated list of bundle images (e.g. make catalog-build BUNDLE_IMGS=example.com/operator-bundle:v0.1.0,example.com/operator-bundle:v0.2.0).
# These images MUST exist in a registry and be pull-able.
BUNDLE_IMGS ?= $(BUNDLE_IMG)

# The image tag given to the resulting catalog image (e.g. make catalog-build CATALOG_IMG=example.com/operator-catalog:v0.2.0).
CATALOG_IMG ?= $(IMAGE_TAG_BASE)-catalog:v$(VERSION)

# Set CATALOG_BASE_IMG to an existing catalog image tag to add $BUNDLE_IMGS to that image.
ifneq ($(origin CATALOG_BASE_IMG), undefined)
FROM_INDEX_OPT := --from-index $(CATALOG_BASE_IMG)
endif

# Build a catalog image by adding bundle images to an empty catalog using the operator package manager tool, 'opm'.
# This recipe invokes 'opm' in 'semver' bundle add mode. For more information on add modes, see:
# https://github.com/operator-framework/community-operators/blob/7f1438c/docs/packaging-operator.md#updating-your-existing-operator
.PHONY: catalog-build
catalog-build: opm ## Build a catalog image.
	$(OPM) index add --container-tool docker --mode semver --tag $(CATALOG_IMG) --bundles $(BUNDLE_IMGS) $(FROM_INDEX_OPT)

# Push the catalog image.
.PHONY: catalog-push
catalog-push: ## Push a catalog image.
	$(MAKE) docker-push IMG=$(CATALOG_IMG)

.PHONY: apply
apply:
	$(KUSTOMIZE) build config/samples | $(KUBECTL) apply -f - 

.PHONY: remove-cr
remove-cr:
	$(KUSTOMIZE) build config/samples | $(KUBECTL) delete --ignore-not-found=true --wait=false -f -

.PHONY: remove
remove:
	$(KUBECTL) delete validatingwebhookconfiguration capi-validating-webhook-configuration --wait=false --ignore-not-found=true
	$(KUBECTL) delete mutatingwebhookconfiguration capi-mutating-webhook-configuration --wait=false --ignore-not-found=true
	$(KUBECTL) delete validatingwebhookconfiguration capoci-validating-webhook-configuration --wait=false --ignore-not-found=true
	$(KUBECTL) delete mutatingwebhookconfiguration capoci-mutating-webhook-configuration --wait=false --ignore-not-found=true
	$(KUBECTL) delete ns $(AUTOSCALER_CLUSTER_NAMESPACE) --wait=false --ignore-not-found=true
	$(KUBECTL) delete cluster -n $(AUTOSCALER_CLUSTER_NAMESPACE) --wait=false --ignore-not-found=true --all
	$(KUBECTL) patch -p '[{"op":"remove","path":"/metadata/finalizers"}]' cluster $(shell $(KUBECTL) get cluster -n $(AUTOSCALER_CLUSTER_NAMESPACE) --no-headers | awk '{print $$1}') -n $(AUTOSCALER_CLUSTER_NAMESPACE) --type=json || true
	$(KUBECTL) delete ocicluster -n $(AUTOSCALER_CLUSTER_NAMESPACE) --wait=false --ignore-not-found=true --all
	$(KUBECTL) patch -p '[{"op":"remove","path":"/metadata/finalizers"}]' ocicluster $(shell $(KUBECTL) get ocicluster -n $(AUTOSCALER_CLUSTER_NAMESPACE) --no-headers | awk '{print $$1}') -n $(AUTOSCALER_CLUSTER_NAMESPACE) --type=json || true
	$(KUBECTL) delete ocimachinetemplate -n $(AUTOSCALER_CLUSTER_NAMESPACE) --wait=false --ignore-not-found=true --all
	$(KUBECTL) patch -p '[{"op":"remove","path":"/metadata/finalizers"}]' ocimachinetemplate $(shell $(KUBECTL) get ocimachinetemplate -n $(AUTOSCALER_CLUSTER_NAMESPACE) --no-headers | awk '{print $$1}') -n $(AUTOSCALER_CLUSTER_NAMESPACE) --type=json || true
	$(KUBECTL) delete machinedeployment -n $(AUTOSCALER_CLUSTER_NAMESPACE) --wait=false --ignore-not-found=true --all
	$(KUBECTL) patch -p '[{"op":"remove","path":"/metadata/finalizers"}]' machinedeployment $(shell $(KUBECTL) get machinedeployment -n $(AUTOSCALER_CLUSTER_NAMESPACE) --no-headers | awk '{print $$1}') -n $(AUTOSCALER_CLUSTER_NAMESPACE) --type=json || true
	$(KUBECTL) delete machineset -n $(AUTOSCALER_CLUSTER_NAMESPACE) --wait=false --ignore-not-found=true --all
	$(KUBECTL) patch -p '[{"op":"remove","path":"/metadata/finalizers"}]' machineset $(shell $(KUBECTL) get machineset -n $(AUTOSCALER_CLUSTER_NAMESPACE) --no-headers | awk '{print $$1}') -n $(AUTOSCALER_CLUSTER_NAMESPACE) --type=json || true
	$(KUBECTL) delete machine -n $(AUTOSCALER_CLUSTER_NAMESPACE) --wait=false --ignore-not-found=true --all
	$(KUBECTL) patch -p '[{"op":"remove","path":"/metadata/finalizers"}]' machine $(shell $(KUBECTL) get machine -n $(AUTOSCALER_CLUSTER_NAMESPACE) --no-headers | awk '{print $$1}') -n $(AUTOSCALER_CLUSTER_NAMESPACE) --type=json || true
	$(KUBECTL) delete ocimachine -n $(AUTOSCALER_CLUSTER_NAMESPACE) --wait=false --ignore-not-found=true --all
	$(KUBECTL) patch -p '[{"op":"remove","path":"/metadata/finalizers"}]' ocimachine $(shell $(KUBECTL) get ocimachine -n $(AUTOSCALER_CLUSTER_NAMESPACE) --no-headers | awk '{print $$1}') -n $(AUTOSCALER_CLUSTER_NAMESPACE) --type=json || true
	$(KUSTOMIZE) build config/samples | $(KUBECTL) delete --ignore-not-found=true --wait=false -f -
	$(KUBECTL) patch -p '[{"op":"remove","path":"/metadata/finalizers"}]' ociclusterautoscalers ociclusterautoscaler-sample --type=json
