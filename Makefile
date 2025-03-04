# Debug mode setting (0 for minimal output, 1 for verbose)
DEBUG ?= 0

# Docker settings
DOCKER_REGISTRY ?= docker.io
DOCKER_REPO ?= supporttools/dr-syncer
DOCKER_AGENT_REPO ?= supporttools/dr-syncer-agent
DOCKER_RSYNC_REPO ?= supporttools/dr-syncer-rsync
TIMESTAMP ?= $(shell date +%Y%m%d%H%M%S)

# Controller image settings
IMG ?= $(DOCKER_REGISTRY)/$(DOCKER_REPO):$(TIMESTAMP)
DOCKER_LATEST_TAG ?= $(DOCKER_REGISTRY)/$(DOCKER_REPO):latest
DOCKER_BUILD_CACHE_FROM ?= type=registry,ref=$(DOCKER_LATEST_TAG)
DOCKER_BUILD_CACHE_TO ?= type=inline

# Agent image settings
AGENT_IMG ?= $(DOCKER_REGISTRY)/$(DOCKER_AGENT_REPO):$(TIMESTAMP)
DOCKER_AGENT_LATEST_TAG ?= $(DOCKER_REGISTRY)/$(DOCKER_AGENT_REPO):latest
DOCKER_AGENT_BUILD_CACHE_FROM ?= type=registry,ref=$(DOCKER_AGENT_LATEST_TAG)

# Rsync image settings
RSYNC_IMG ?= $(DOCKER_REGISTRY)/$(DOCKER_RSYNC_REPO):$(TIMESTAMP)
DOCKER_RSYNC_LATEST_TAG ?= $(DOCKER_REGISTRY)/$(DOCKER_RSYNC_REPO):latest
DOCKER_RSYNC_BUILD_CACHE_FROM ?= type=registry,ref=$(DOCKER_RSYNC_LATEST_TAG)

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes
SHELL = /bin/bash

# Enable BuildKit for Docker builds
export DOCKER_BUILDKIT=1


.PHONY: all
all: build

# Version information
VERSION ?= $(shell git describe --tags --always --dirty)
GIT_COMMIT ?= $(shell git rev-parse HEAD)
BUILD_DATE ?= $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')

# Helm chart settings
HELM_NAMESPACE ?= dr-syncer
HELM_RELEASE_NAME ?= dr-syncer
HELM_VALUES ?= charts/dr-syncer/values.yaml

# Kubernetes settings
CONTROLLER_KUBECONFIG ?= $(PWD)/kubeconfig/controller
DR_KUBECONFIG ?= $(PWD)/kubeconfig/dr
PROD_KUBECONFIG ?= $(PWD)/kubeconfig/prod
KUBECONFIG ?= $(CONTROLLER_KUBECONFIG)

.PHONY: check-docker
check-docker:
	@if [ $(DEBUG) -eq 1 ]; then \
		echo "Checking Docker registry access..."; \
	fi
	@docker info >/dev/null 2>&1 || (echo "Error: Docker daemon not running" && exit 1)
	@docker pull $(DOCKER_LATEST_TAG) >/dev/null 2>&1 || (echo "Warning: Could not pull latest image for cache. Continuing without cache." && exit 0)

.PHONY: create-namespace
create-namespace: ## Create namespace for deployment
	@if [ $(DEBUG) -eq 1 ]; then \
		echo "Creating namespace..."; \
	fi
	KUBECONFIG=$(KUBECONFIG) kubectl create namespace $(HELM_NAMESPACE) --dry-run=client -o yaml | KUBECONFIG=$(KUBECONFIG) kubectl apply -f -
	@echo "✓ Namespace created"

.PHONY: deploy-local
deploy-local: check-docker create-namespace manifests install-crds ## Build, push image, install CRDs, and deploy to current cluster
	@echo "Starting local deployment..."
	@if [ ! -f "$(KUBECONFIG)" ]; then \
		echo "Error: Kubeconfig not found at $(KUBECONFIG)"; \
		exit 1; \
	fi
	@echo "Using kubeconfig: $(KUBECONFIG)"
	
	@echo "Building controller image with version $(VERSION)..."
	$(eval DEPLOY_TIMESTAMP := $(shell date +%Y%m%d%H%M%S))
	docker build \
		$(if $(filter 0,$(DEBUG)),--quiet) \
		--cache-from=$(DOCKER_BUILD_CACHE_FROM) \
		--cache-to=$(DOCKER_BUILD_CACHE_TO) \
		--build-arg VERSION=$(VERSION) \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-f build/Dockerfile \
		-t $(DOCKER_REGISTRY)/$(DOCKER_REPO):$(DEPLOY_TIMESTAMP) \
		-t $(DOCKER_LATEST_TAG) .
	
	@echo "Building agent image..."
	docker build \
		$(if $(filter 0,$(DEBUG)),--quiet) \
		--cache-from=$(DOCKER_AGENT_BUILD_CACHE_FROM) \
		--cache-to=$(DOCKER_BUILD_CACHE_TO) \
		--build-arg VERSION=$(VERSION) \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-f build/Dockerfile.agent \
		-t $(DOCKER_REGISTRY)/$(DOCKER_AGENT_REPO):$(DEPLOY_TIMESTAMP) \
		-t $(DOCKER_AGENT_LATEST_TAG) .
	
	@echo "Building rsync image..."
	docker build \
		$(if $(filter 0,$(DEBUG)),--quiet) \
		--cache-from=$(DOCKER_RSYNC_BUILD_CACHE_FROM) \
		--cache-to=$(DOCKER_BUILD_CACHE_TO) \
		--build-arg VERSION=$(VERSION) \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-f build/Dockerfile.rsync \
		-t $(DOCKER_REGISTRY)/$(DOCKER_RSYNC_REPO):$(DEPLOY_TIMESTAMP) \
		-t $(DOCKER_RSYNC_LATEST_TAG) .

	@echo "Pushing images..."
	docker push $(if $(filter 0,$(DEBUG)),--quiet) $(DOCKER_REGISTRY)/$(DOCKER_REPO):$(DEPLOY_TIMESTAMP)
	docker push $(if $(filter 0,$(DEBUG)),--quiet) $(DOCKER_LATEST_TAG)
	docker push $(if $(filter 0,$(DEBUG)),--quiet) $(DOCKER_REGISTRY)/$(DOCKER_AGENT_REPO):$(DEPLOY_TIMESTAMP)
	docker push $(if $(filter 0,$(DEBUG)),--quiet) $(DOCKER_AGENT_LATEST_TAG)
	docker push $(if $(filter 0,$(DEBUG)),--quiet) $(DOCKER_REGISTRY)/$(DOCKER_RSYNC_REPO):$(DEPLOY_TIMESTAMP)
	docker push $(if $(filter 0,$(DEBUG)),--quiet) $(DOCKER_RSYNC_LATEST_TAG)
	
	@echo "Deploying to Kubernetes..."
	KUBECONFIG=$(KUBECONFIG) helm upgrade --install $(HELM_RELEASE_NAME) charts/dr-syncer \
		--namespace $(HELM_NAMESPACE) \
		--create-namespace \
		--values $(HELM_VALUES) \
		--set crds.install=false \
		--set image.repository=$(DOCKER_REGISTRY)/$(DOCKER_REPO) \
		--set image.tag=$(DEPLOY_TIMESTAMP) \
		--set agent.image.repository=$(DOCKER_REGISTRY)/$(DOCKER_AGENT_REPO) \
		--set agent.image.tag=$(DEPLOY_TIMESTAMP) \
		--set rsyncPod.image.repository=$(DOCKER_REGISTRY)/$(DOCKER_RSYNC_REPO) \
		--set rsyncPod.image.tag=$(DEPLOY_TIMESTAMP) \
		--set version=$(VERSION) \
		--set gitCommit=$(GIT_COMMIT) \
		--set buildDate=$(BUILD_DATE) \
		--wait \
		$(if $(filter 1,$(DEBUG)),--debug)
	
	@echo "✓ Deployment complete"
	@echo "  Image: $(DOCKER_REGISTRY)/$(DOCKER_REPO):$(DEPLOY_TIMESTAMP)"
	@echo "  Version: $(VERSION)"
	@echo "  Namespace: $(HELM_NAMESPACE)"
	@echo "  Release: $(HELM_RELEASE_NAME)"
	@echo "  Kubeconfig: $(KUBECONFIG)"

.PHONY: deploy-dr
deploy-dr: ## Deploy to DR cluster
	@echo "Deploying to DR cluster..."
	$(MAKE) deploy-local KUBECONFIG=$(DR_KUBECONFIG)

.PHONY: deploy-prod
deploy-prod: ## Deploy to Production cluster
	@echo "Deploying to Production cluster..."
	$(MAKE) deploy-local KUBECONFIG=$(PROD_KUBECONFIG)

.PHONY: deploy-all
deploy-all: docker-build docker-push deploy-local ## Build, push, and deploy in one command

##@ Development

.PHONY: fmt
fmt: ## Run go fmt against code
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code
	go vet ./...

.PHONY: test
test: fmt vet ## Run tests
	go test ./... $(if $(filter 1,$(DEBUG)),-v) -coverprofile cover.out

##@ Build

.PHONY: build
build: fmt vet ## Build dr-syncer binary
	go build -o bin/dr-syncer main.go

.PHONY: run
run: fmt vet ## Run against the configured Kubernetes cluster in ~/.kube/config
	go run ./main.go

.PHONY: docker-build-agent
docker-build-agent: check-docker ## Build and push agent docker image with caching
	@echo "Building agent image with version $(VERSION)..."
	docker build \
		$(if $(filter 0,$(DEBUG)),--quiet) \
		--cache-from=$(DOCKER_AGENT_BUILD_CACHE_FROM) \
		--cache-to=$(DOCKER_BUILD_CACHE_TO) \
		--build-arg VERSION=$(VERSION) \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-f build/Dockerfile.agent \
		-t ${AGENT_IMG} \
		-t $(DOCKER_AGENT_LATEST_TAG) .
	
	@echo "Pushing agent images..."
	docker push $(if $(filter 0,$(DEBUG)),--quiet) ${AGENT_IMG}
	docker push $(if $(filter 0,$(DEBUG)),--quiet) $(DOCKER_AGENT_LATEST_TAG)
	
	@echo "✓ Agent build complete"
	@echo "  Image: ${AGENT_IMG}"
	@echo "  Version: $(VERSION)"
	@if [ $(DEBUG) -eq 1 ]; then \
		echo "  Git commit: $(GIT_COMMIT)"; \
	fi

.PHONY: docker-build-rsync
docker-build-rsync: check-docker ## Build and push rsync docker image with caching
	@echo "Building rsync image with version $(VERSION)..."
	docker build \
		$(if $(filter 0,$(DEBUG)),--quiet) \
		--cache-from=$(DOCKER_RSYNC_BUILD_CACHE_FROM) \
		--cache-to=$(DOCKER_BUILD_CACHE_TO) \
		--build-arg VERSION=$(VERSION) \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-f build/Dockerfile.rsync \
		-t ${RSYNC_IMG} \
		-t $(DOCKER_RSYNC_LATEST_TAG) .
	
	@echo "Pushing rsync images..."
	docker push $(if $(filter 0,$(DEBUG)),--quiet) ${RSYNC_IMG}
	docker push $(if $(filter 0,$(DEBUG)),--quiet) $(DOCKER_RSYNC_LATEST_TAG)
	
	@echo "✓ Rsync build complete"
	@echo "  Image: ${RSYNC_IMG}"
	@echo "  Version: $(VERSION)"
	@if [ $(DEBUG) -eq 1 ]; then \
		echo "  Git commit: $(GIT_COMMIT)"; \
	fi

.PHONY: docker-build-all
docker-build-all: docker-build docker-build-agent docker-build-rsync ## Build all images

.PHONY: docker-build
docker-build: check-docker ## Build and push controller docker image with caching
	@echo "Building image with version $(VERSION)..."
	docker build \
		$(if $(filter 0,$(DEBUG)),--quiet) \
		--cache-from=$(DOCKER_BUILD_CACHE_FROM) \
		--cache-to=$(DOCKER_BUILD_CACHE_TO) \
		--build-arg VERSION=$(VERSION) \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-f build/Dockerfile \
		-t ${IMG} \
		-t $(DOCKER_LATEST_TAG) .
	
	@echo "Pushing images..."
	docker push $(if $(filter 0,$(DEBUG)),--quiet) ${IMG}
	docker push $(if $(filter 0,$(DEBUG)),--quiet) $(DOCKER_LATEST_TAG)
	
	@echo "✓ Build complete"
	@echo "  Image: ${IMG}"
	@echo "  Version: $(VERSION)"
	@if [ $(DEBUG) -eq 1 ]; then \
		echo "  Git commit: $(GIT_COMMIT)"; \
	fi

.PHONY: docker-build-nocache
docker-build-nocache: check-docker ## Build docker image without cache
	@echo "Building image without cache..."
	docker build --no-cache \
		$(if $(filter 0,$(DEBUG)),--quiet) \
		--build-arg VERSION=$(VERSION) \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-f build/Dockerfile \
		-t ${IMG} \
		-t $(DOCKER_LATEST_TAG) .
	
	@echo "✓ Build complete"
	@echo "  Image: ${IMG}"
	@echo "  Version: $(VERSION)"
	@if [ $(DEBUG) -eq 1 ]; then \
		echo "  Git commit: $(GIT_COMMIT)"; \
	fi

.PHONY: clean-cache
clean-cache: ## Clean Docker build cache
	docker builder prune --filter type=exec.cachemount --force

.PHONY: prune-all
prune-all: ## Prune all Docker build cache and unused objects
	docker system prune -a --volumes

##@ Deployment

.PHONY: install
install: ## Install CRDs into the K8s cluster specified in ~/.kube/config
	KUBECONFIG=$(KUBECONFIG) kubectl apply -f config/crd/bases/

.PHONY: uninstall
uninstall: ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config
	KUBECONFIG=$(KUBECONFIG) kubectl delete -f config/crd/bases/

.PHONY: deploy
deploy: ## Deploy controller to the K8s cluster with Helm
	KUBECONFIG=$(KUBECONFIG) helm upgrade --install dr-syncer charts/dr-syncer \
		--namespace dr-syncer \
		--create-namespace \
		$(if $(filter 1,$(DEBUG)),--debug)

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster with Helm
	@echo "Undeploying from Kubernetes..."
	KUBECONFIG=$(KUBECONFIG) helm uninstall $(HELM_RELEASE_NAME) \
		--namespace $(HELM_NAMESPACE) \
		$(if $(filter 1,$(DEBUG)),--debug)
	@echo "✓ Undeployment complete"

##@ Generate

.PHONY: build-crds
build-crds: controller-gen ## Generate CRDs from Go types
	$(CONTROLLER_GEN) crd paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: test-crds
test-crds: build-crds ## Test CRDs for validity
	@echo "Validating CRDs..."
	@for f in config/crd/bases/*.yaml; do \
		if [ $(DEBUG) -eq 1 ]; then \
			echo "Validating $$f"; \
		fi; \
		KUBECONFIG=$(KUBECONFIG) kubectl apply --dry-run=client -f $$f > /dev/null || exit 1; \
	done
	@echo "✓ CRDs validated successfully"

.PHONY: install-crds
install-crds: build-crds ## Install CRDs directly to the cluster
	@echo "Installing CRDs directly to the cluster..."
	KUBECONFIG=$(KUBECONFIG) kubectl apply -f config/crd/bases/
	@echo "✓ CRDs installed successfully"

.PHONY: manifests
manifests: build-crds ## Generate CRDs and sync to Helm chart
	# Sync CRDs to Helm chart (without Helm templating)
	for f in config/crd/bases/*.yaml; do \
		if [ $(DEBUG) -eq 1 ]; then \
			echo "Copying $$f to charts/dr-syncer/crds/$$(basename $$f)"; \
		fi; \
		cp $$f charts/dr-syncer/crds/$$(basename $$f); \
	done

.PHONY: generate
generate: controller-gen ## Generate code
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

##@ Build Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
HELM ?= $(LOCALBIN)/helm

## Tool Versions
CONTROLLER_TOOLS_VERSION ?= v0.14.0

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
