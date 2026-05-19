# Image URL to use all building/pushing image targets
IMG ?= ghcr.io/scitix/agent-sandbox-controller:latest
IMG_EXTPROC ?= ghcr.io/scitix/agent-sandbox-envoyextproc:latest
IMG_DASHBOARD ?= ghcr.io/scitix/agent-sandbox-dashboard:latest
IMG_WSPROXY ?= ghcr.io/scitix/agent-sandbox-wsproxy:latest
IMG_DOCS ?= ghcr.io/scitix/agent-sandbox-docs:latest
IMG_IDLEIMAGE ?= ghcr.io/scitix/agent-sandbox-idle:latest

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

ifeq ($(shell uname), Darwin)
    SED_INPLACE := sed -i ''
else
    SED_INPLACE := sed -i
endif

# CONTAINER_TOOL defines the container tool to be used for building images.
# Be aware that the target commands are only tested with Docker which is
# scaffolded by default. However, you might want to replace it to use other
# tools. (i.e. podman)
CONTAINER_TOOL ?= docker

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

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
manifests: controller-gen ## Generate ClusterRole and CustomResourceDefinition objects from kubebuilder markers.
	"$(CONTROLLER_GEN)" rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	"$(CONTROLLER_GEN)" object:headerFile="hack/boilerplate.go.txt" paths="./..."
	go fmt ./...

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: imports
imports: goimports ## Run goimports on all go files.
	$(GOIMPORTS) -w -local github.com/scitix/agent-sandbox .

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: code-formatter
code-formatter: imports fmt ## Run all code formatting tools.

.PHONY: test
test: manifests generate generate-api imports fmt vet setup-envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell "$(ENVTEST)" use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path)" go test -p $$(nproc) $$(go list ./... | grep -v /e2e) -coverprofile cover.out

.PHONY: test-e2e
test-e2e: manifests generate imports fmt vet ## Run the e2e tests against the current kubeconfig cluster using a locally started controller.
	go test -tags=e2e ./test/e2e/ -v -ginkgo.v

.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter
	"$(GOLANGCI_LINT)" run

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	"$(GOLANGCI_LINT)" run --fix

.PHONY: lint-config
lint-config: golangci-lint ## Verify golangci-lint linter configuration
	"$(GOLANGCI_LINT)" config verify

##@ Build

# VERSION is read from the VERSION file at the repo root.
# All Go binaries are stamped with this version via -ldflags.
VERSION ?= $(shell cat VERSION 2>/dev/null || echo "0.0.0")
LDFLAGS = -X github.com/scitix/agent-sandbox/pkg/version.Version=$(VERSION)

.PHONY: build
build: build-controller build-extproc build-wsproxy ## Build all binaries.

.PHONY: build-controller
build-controller: manifests generate generate-api imports fmt vet ## Build manager binary.
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/manager -ldflags="$(LDFLAGS)" -gcflags="all=-N -l" ./cmd/sandbox

.PHONY: build-extproc
build-extproc: manifests generate imports fmt vet ## Build envoyextproc binary.
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/envoyextproc -ldflags="$(LDFLAGS)" -gcflags="all=-N -l" ./cmd/envoyextproc

.PHONY: build-wsproxy
build-wsproxy: imports fmt vet ## Build wsproxy binary.
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/wsproxy -ldflags="$(LDFLAGS)" -gcflags="all=-N -l" ./cmd/wsproxy

.PHONY: run
run: manifests generate generate-api fmt vet ## Run a controller from your host.
	go run ./cmd/sandbox/main.go --metrics-bind-address=0 --health-probe-bind-address=:0 --leader-elect=false

.PHONY: sync-version
sync-version: ## Sync VERSION file content to OpenAPI spec, Python SDK, and Helm charts.
	@echo "Syncing version $(VERSION) to all components..."
	@$(SED_INPLACE) 's/^  version: .*/  version: "$(VERSION)"/' pkg/openapi/native/openapi.yaml
	@$(SED_INPLACE) 's/^version = .*/version = "$(VERSION)"/' sdk/python/abx/pyproject.toml
	@$(SED_INPLACE) 's/^__version__ = .*/__version__ = "$(VERSION)"/' sdk/python/abx/agentbox_sdk/__init__.py
	@$(SED_INPLACE) 's/^appVersion:.*/appVersion: "$(VERSION)"/' installer/helm/agent-sandbox-worker/Chart.yaml
	@$(SED_INPLACE) 's/^appVersion:.*/appVersion: "$(VERSION)"/' installer/helm/agent-sandbox-hub/Chart.yaml
	@echo "Done. Run 'make generate-api' to regenerate Go code from the updated spec."

# If you wish to build the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: docker-build
docker-build: docker-build-controller docker-build-extproc docker-build-dashboard docker-build-wsproxy docker-build-docs docker-build-idleimage ## Build all docker images.

.PHONY: docker-build-controller
docker-build-controller: ## Build agentbox docker image.
	$(CONTAINER_TOOL) build -t ${IMG} -f installer/dockerfile/Dockerfile.controller .

.PHONY: docker-build-extproc
docker-build-extproc: ## Build envoyextproc docker image.
	$(CONTAINER_TOOL) build -t ${IMG_EXTPROC} -f installer/dockerfile/Dockerfile.envoyextproc .

.PHONY: docker-build-dashboard
docker-build-dashboard: ## Build dashboard docker image.
	$(CONTAINER_TOOL) build -t ${IMG_DASHBOARD} \
	  -f installer/dockerfile/Dockerfile.dashboard .

.PHONY: docker-build-wsproxy
docker-build-wsproxy: ## Build wsproxy docker image.
	$(CONTAINER_TOOL) build -t ${IMG_WSPROXY} -f installer/dockerfile/Dockerfile.wsproxy .

.PHONY: docker-build-docs
docker-build-docs: ## Build docs docker image.
	$(CONTAINER_TOOL) build -t ${IMG_DOCS} \
	  --build-arg NEXT_BASE_PATH=/agentbox \
	  -f installer/dockerfile/Dockerfile.docs .

.PHONY: docker-build-idleimage
docker-build-idleimage: ## Build the sandbox idle image (what pre-warmed pool pods run before being claimed).
	$(CONTAINER_TOOL) build -t ${IMG_IDLEIMAGE} -f installer/dockerfile/Dockerfile.idleimage .

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	$(CONTAINER_TOOL) push ${IMG}

# PLATFORMS defines the target platforms for the manager image be built to provide support to multiple
# architectures. (i.e. make docker-buildx IMG=myregistry/mypoperator:0.0.1). To use this option you need to:
# - be able to use docker buildx. More info: https://docs.docker.com/build/buildx/
# - have enabled BuildKit. More info: https://docs.docker.com/develop/develop-images/build_enhancements/
# - be able to push the image to your registry (i.e. if you do not set a valid value via IMG=<myregistry/image:<tag>> then the export will fail)
# To adequately provide solutions that are compatible with multiple platforms, you should consider using this option.
PLATFORMS ?= linux/arm64,linux/amd64,linux/s390x,linux/ppc64le
.PHONY: docker-buildx
docker-buildx: ## Build and push docker image for the manager for cross-platform support
	# copy existing Dockerfile and insert --platform=${BUILDPLATFORM} into Dockerfile.cross, and preserve the original Dockerfile
	sed -e '1 s/\(^FROM\)/FROM --platform=\$$\{BUILDPLATFORM\}/; t' -e ' 1,// s//FROM --platform=\$$\{BUILDPLATFORM\}/' Dockerfile > Dockerfile.cross
	- $(CONTAINER_TOOL) buildx create --name agentbox-builder
	$(CONTAINER_TOOL) buildx use agentbox-builder
	- $(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) --tag ${IMG} -f Dockerfile.cross .
	- $(CONTAINER_TOOL) buildx rm agentbox-builder
	rm Dockerfile.cross

.PHONY: sync-crds-to-helm
sync-crds-to-helm: manifests ## Sync generated CRDs and manager ClusterRole into Helm chart directories.
	python3 hack/scripts/generate-helm.py

##@ Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p "$(LOCALBIN)"

## Tool Binaries
KUBECTL ?= kubectl
KIND ?= kind
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint
GOIMPORTS ?= $(LOCALBIN)/goimports
OAPI_CODEGEN ?= $(LOCALBIN)/oapi-codegen
ADDLICENSE ?= $(LOCALBIN)/addlicense

## Tool Versions
CONTROLLER_TOOLS_VERSION ?= v0.20.1
OAPI_CODEGEN_VERSION ?= v2.6.0
ADDLICENSE_VERSION ?= v1.2.0
E2B_SPEC_VERSION ?= 2026.10

#ENVTEST_VERSION is the version of controller-runtime release branch to fetch the envtest setup script (i.e. release-0.20)
ENVTEST_VERSION ?= $(shell v='$(call gomodver,sigs.k8s.io/controller-runtime)'; \
  [ -n "$$v" ] || { echo "Set ENVTEST_VERSION manually (controller-runtime replace has no tag)" >&2; exit 1; }; \
  printf '%s\n' "$$v" | sed -E 's/^v?([0-9]+)\.([0-9]+).*/release-\1.\2/')

#ENVTEST_K8S_VERSION is the version of Kubernetes to use for setting up ENVTEST binaries (i.e. 1.31)
ENVTEST_K8S_VERSION ?= $(shell v='$(call gomodver,k8s.io/api)'; \
  [ -n "$$v" ] || { echo "Set ENVTEST_K8S_VERSION manually (k8s.io/api replace has no tag)" >&2; exit 1; }; \
  printf '%s\n' "$$v" | sed -E 's/^v?[0-9]+\.([0-9]+).*/1.\1/')

GOLANGCI_LINT_VERSION ?= v2.8.0

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: setup-envtest
setup-envtest: envtest ## Download the binaries required for ENVTEST in the local bin directory.
	@echo "Setting up envtest binaries for Kubernetes version $(ENVTEST_K8S_VERSION)..."
	@"$(ENVTEST)" use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path || { \
		echo "Error: Failed to set up envtest binaries for version $(ENVTEST_K8S_VERSION)."; \
		exit 1; \
	}

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary.
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/v2/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))
	@test -f .custom-gcl.yml && test ! -f $(LOCALBIN)/.gcl-custom-built-$(GOLANGCI_LINT_VERSION) && { \
		echo "Building custom golangci-lint with plugins..." && \
		$(GOLANGCI_LINT) custom --destination $(LOCALBIN) --name golangci-lint-custom && \
		mv -f $(LOCALBIN)/golangci-lint-custom $(GOLANGCI_LINT)-$(GOLANGCI_LINT_VERSION) && \
		touch $(LOCALBIN)/.gcl-custom-built-$(GOLANGCI_LINT_VERSION); \
	} || true

.PHONY: goimports
goimports: $(GOIMPORTS) ## Download goimports locally if necessary.
$(GOIMPORTS): $(LOCALBIN)
	$(call go-install-tool,$(GOIMPORTS),golang.org/x/tools/cmd/goimports,latest)

.PHONY: oapi-codegen
oapi-codegen: $(OAPI_CODEGEN) ## Download oapi-codegen locally if necessary.
$(OAPI_CODEGEN): $(LOCALBIN)
	$(call go-install-tool,$(OAPI_CODEGEN),github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen,$(OAPI_CODEGEN_VERSION))

.PHONY: addlicense
addlicense: $(ADDLICENSE) ## Download addlicense locally if necessary.
$(ADDLICENSE): $(LOCALBIN)
	$(call go-install-tool,$(ADDLICENSE),github.com/google/addlicense,$(ADDLICENSE_VERSION))

.PHONY: sync-e2b-spec
sync-e2b-spec: ## Sync E2B OpenAPI spec from GitHub (use E2B_SPEC_VERSION=x.y to pin version)
	mkdir -p pkg/openapi/e2b
	curl -fsSL "https://raw.githubusercontent.com/e2b-dev/infra/$(E2B_SPEC_VERSION)/spec/openapi.yml" \
		-o pkg/openapi/e2b/openapi.yaml
	@echo "E2B spec synced: version=$(E2B_SPEC_VERSION)"

.PHONY: generate-api
generate-api: oapi-codegen ## Generate API code from OpenAPI specs
	mkdir -p pkg/apiserver/gen pkg/e2bcompat/gen pkg/wsproxy/gen
	"$(OAPI_CODEGEN)" --config pkg/openapi/native/oapi-codegen.yaml pkg/openapi/native/openapi.yaml
	"$(OAPI_CODEGEN)" --config pkg/openapi/global/oapi-codegen.yaml pkg/openapi/global/openapi.yaml
	@if [ -f pkg/openapi/e2b/openapi.yaml ]; then \
		"$(OAPI_CODEGEN)" --config pkg/openapi/e2b/oapi-codegen.yaml pkg/openapi/e2b/openapi.yaml; \
	else \
		echo "E2B spec not found, skipping E2B code generation. Run 'make sync-e2b-spec' first."; \
	fi

.PHONY: gen-internal-proto
gen-internal-proto: ## Generate Go/gRPC code from pkg/proto/ (internal Controller ↔ ExtProc RPCs).
	@command -v protoc >/dev/null 2>&1 || { echo "protoc not found; install protobuf-compiler"; exit 1; }
	@command -v protoc-gen-go >/dev/null 2>&1 || go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	@command -v protoc-gen-go-grpc >/dev/null 2>&1 || go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	protoc \
		--proto_path=pkg/proto \
		--go_out=. --go_opt=module=github.com/scitix/agent-sandbox \
		--go-grpc_out=. --go-grpc_opt=module=github.com/scitix/agent-sandbox \
		pkg/proto/sandbox/ctrlplane/v1/ctrlplane.proto
	@echo "Internal proto code regenerated alongside the .proto file."

.PHONY: gen-all-api
gen-all-api: generate-api gen-internal-proto ## Regenerate all API clients from OpenAPI spec: Go server, TS dashboard, Python SDK.
	cd dashboard && pnpm run gen:types
	cd dashboard && pnpm run gen:global-types
	uvx openapi-python-client generate \
		--path pkg/openapi/native/openapi.yaml \
		--config sdk/python/abx/openapi-gen-config.yaml \
		--output-path /tmp/agentbox_sdk_gen \
		--overwrite
	rm -rf sdk/python/abx/agentbox_sdk/_generated
	mv /tmp/agentbox_sdk_gen/agentbox_sdk._generated sdk/python/abx/agentbox_sdk/_generated
	rm -rf /tmp/agentbox_sdk_gen
	$(MAKE) add-license
	@echo "All API code regenerated:"
	@echo "  Go     → pkg/apiserver/gen/agentbox.gen.go"
	@echo "  TS     → dashboard/lib/api/schema.d.ts"
	@echo "  TS hub → dashboard/lib/api/global-schema.d.ts"
	@echo "  Python → sdk/python/abx/agent_sandbox_e2b/_generated/"

.PHONY: add-license
add-license: addlicense
	git ls-files --cached --others --exclude-standard \
	| grep -E '\.(go|ts|tsx?|py|proto)$$' \
	| xargs $(ADDLICENSE) \
	-ignore dashboard/next-env.d.ts \
	-ignore docs/website/next-env.d.ts \
	-ignore 'docs/website/.source/**' \
	-ignore 'dashboard/components/ui/**' \
	-ignore dashboard/hooks/use-mobile.ts \
	-ignore 'pkg/proto/**/*.pb.go' \
	-l apache -c "ScitiX" -y 2026

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] && [ "$$(readlink -- "$(1)" 2>/dev/null)" = "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f "$(1)" ;\
GOBIN="$(LOCALBIN)" go install $${package} ;\
mv "$(LOCALBIN)/$$(basename "$(1)")" "$(1)-$(3)" ;\
} ;\
ln -sf "$$(realpath "$(1)-$(3)")" "$(1)"
endef

define gomodver
$(shell go list -m -f '{{if .Replace}}{{.Replace.Version}}{{else}}{{.Version}}{{end}}' $(1) 2>/dev/null)
endef

##@ Helm

## Helm binary to use
HELM ?= helm
## Namespace for Helm releases
HELM_NAMESPACE ?= agentbox-system
## Release name prefix
HELM_RELEASE ?= agent-sandbox
## Worker chart directory (controller + extproc)
HELM_WORKER_CHART_DIR ?= installer/helm/agent-sandbox-worker
## Hub chart directory (dashboard + ws-proxy)
HELM_HUB_CHART_DIR ?= installer/helm/agent-sandbox-hub
## Additional arguments to pass to helm commands
HELM_EXTRA_ARGS ?=

.PHONY: install-helm
install-helm: ## Install the latest version of Helm.
	@command -v $(HELM) >/dev/null 2>&1 || { \
		echo "Installing Helm..." && \
		curl -fsSL https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-4 | bash; \
	}

.PHONY: helm-lint
helm-lint: install-helm ## Lint all Helm charts.
	$(HELM) lint $(HELM_WORKER_CHART_DIR) --strict
	$(HELM) lint $(HELM_HUB_CHART_DIR) --strict

.PHONY: helm-deploy-worker
helm-deploy-worker: install-helm ## Deploy agent-sandbox-worker to the K8s cluster via Helm.
	$(HELM) upgrade --install $(HELM_RELEASE)-worker $(HELM_WORKER_CHART_DIR) \
		--namespace $(HELM_NAMESPACE) \
		--create-namespace \
		--set controller.image.repository=$${IMG%:*} \
		--set controller.image.tag=$${IMG##*:} \
		--set extproc.image.repository=$${IMG_EXTPROC%:*} \
		--set extproc.image.tag=$${IMG_EXTPROC##*:} \
		--wait \
		--timeout 5m \
		$(HELM_EXTRA_ARGS)

.PHONY: helm-deploy-hub
helm-deploy-hub: install-helm ## Deploy agent-sandbox-hub (dashboard) to the K8s cluster via Helm.
	$(HELM) upgrade --install $(HELM_RELEASE)-hub $(HELM_HUB_CHART_DIR) \
		--namespace $(HELM_NAMESPACE) \
		--create-namespace \
		--set image.repository=$${IMG_DASHBOARD%:*} \
		--set image.tag=$${IMG_DASHBOARD##*:} \
		--set wsProxy.image.repository=$${IMG_WSPROXY%:*} \
		--set wsProxy.image.tag=$${IMG_WSPROXY##*:} \
		--wait \
		--timeout 5m \
		$(HELM_EXTRA_ARGS)

.PHONY: helm-uninstall-worker
helm-uninstall-worker: ## Uninstall the worker Helm release.
	$(HELM) uninstall $(HELM_RELEASE)-worker --namespace $(HELM_NAMESPACE)

.PHONY: helm-uninstall-hub
helm-uninstall-hub: ## Uninstall the hub Helm release.
	$(HELM) uninstall $(HELM_RELEASE)-hub --namespace $(HELM_NAMESPACE)

.PHONY: helm-status
helm-status: ## Show status of both Helm releases.
	$(HELM) status $(HELM_RELEASE)-worker --namespace $(HELM_NAMESPACE) 2>/dev/null || true
	$(HELM) status $(HELM_RELEASE)-hub --namespace $(HELM_NAMESPACE) 2>/dev/null || true
