# Docker settings
DOCKER_REGISTRY ?= harbor.support.tools
DOCKER_REPO ?= dr-syncer/controller
TIMESTAMP ?= $(shell date +%Y%m%d%H%M%S)
IMG ?= $(DOCKER_REGISTRY)/$(DOCKER_REPO):$(TIMESTAMP)
DOCKER_LATEST_TAG ?= $(DOCKER_REGISTRY)/$(DOCKER_REPO):latest
DOCKER_BUILD_CACHE_FROM ?= type=registry,ref=$(DOCKER_LATEST_TAG)
DOCKER_BUILD_CACHE_TO ?= type=inline

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
KUBECONFIG ?= $(HOME)/.kube/config

.PHONY: check-docker
check-docker:
	@echo "Checking Docker registry access..."
	@docker info >/dev/null 2>&1 || (echo "Error: Docker daemon not running" && exit 1)
	@docker pull $(DOCKER_LATEST_TAG) >/dev/null 2>&1 || (echo "Warning: Could not pull latest image for cache. Continuing without cache." && exit 0)

.PHONY: create-registry-secret
create-registry-secret: ## Create Harbor registry secret in Kubernetes
	@echo "Creating Harbor registry secret..."
	@if [ -z "$(HARBOR_USER)" ] || [ -z "$(HARBOR_PASSWORD)" ]; then \
		echo "Error: HARBOR_USER and HARBOR_PASSWORD must be set"; \
		exit 1; \
	fi
	kubectl create namespace $(HELM_NAMESPACE) --dry-run=client -o yaml | kubectl apply -f -
	kubectl create secret docker-registry harbor-registry \
		--namespace $(HELM_NAMESPACE) \
		--docker-server=$(DOCKER_REGISTRY) \
		--docker-username=$(HARBOR_USER) \
		--docker-password=$(HARBOR_PASSWORD) \
		--dry-run=client -o yaml | kubectl apply -f -
	@echo "✓ Registry secret created"

.PHONY: deploy-local
deploy-local: check-docker create-registry-secret manifests ## Build, push image, install CRDs, and deploy to current cluster
	@echo "Starting local deployment..."
	@if [ ! -f "$(KUBECONFIG)" ]; then \
		echo "Error: Kubeconfig not found at $(KUBECONFIG)"; \
		exit 1; \
	fi
	@echo "Using kubeconfig: $(KUBECONFIG)"
	
	@echo "Building image with version $(VERSION)..."
	$(eval DEPLOY_TIMESTAMP := $(shell date +%Y%m%d%H%M%S))
	docker build \
		--cache-from=$(DOCKER_BUILD_CACHE_FROM) \
		--cache-to=$(DOCKER_BUILD_CACHE_TO) \
		--build-arg VERSION=$(VERSION) \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-t $(DOCKER_REGISTRY)/$(DOCKER_REPO):$(DEPLOY_TIMESTAMP) \
		-t $(DOCKER_LATEST_TAG) .
	
	@echo "Pushing images..."
	docker push $(DOCKER_REGISTRY)/$(DOCKER_REPO):$(DEPLOY_TIMESTAMP)
	docker push $(DOCKER_LATEST_TAG)
	
	@echo "Deploying to Kubernetes..."
	KUBECONFIG=$(KUBECONFIG) helm upgrade --install $(HELM_RELEASE_NAME) charts/dr-syncer \
		--namespace $(HELM_NAMESPACE) \
		--create-namespace \
		--values $(HELM_VALUES) \
		--set crds.install=true \
		--set image.repository=$(DOCKER_REGISTRY)/$(DOCKER_REPO) \
		--set image.tag=$(DEPLOY_TIMESTAMP) \
		--set version=$(VERSION) \
		--set gitCommit=$(GIT_COMMIT) \
		--set buildDate=$(BUILD_DATE) \
		--wait \
		--debug
	
	@echo "✓ Deployment complete"
	@echo "  Image: $(DOCKER_REGISTRY)/$(DOCKER_REPO):$(DEPLOY_TIMESTAMP)"
	@echo "  Version: $(VERSION)"
	@echo "  Namespace: $(HELM_NAMESPACE)"
	@echo "  Release: $(HELM_RELEASE_NAME)"

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
	go test ./... -coverprofile cover.out

##@ Build

.PHONY: build
build: fmt vet ## Build dr-syncer binary
	go build -o bin/dr-syncer main.go

.PHONY: run
run: fmt vet ## Run against the configured Kubernetes cluster in ~/.kube/config
	go run ./main.go

.PHONY: docker-build
docker-build: check-docker ## Build and push docker image with caching
	@echo "Building image with version $(VERSION)..."
	docker build \
		--cache-from=$(DOCKER_BUILD_CACHE_FROM) \
		--cache-to=$(DOCKER_BUILD_CACHE_TO) \
		--build-arg VERSION=$(VERSION) \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-t ${IMG} \
		-t $(DOCKER_LATEST_TAG) .
	
	@echo "Pushing images..."
	docker push ${IMG}
	docker push $(DOCKER_LATEST_TAG)
	
	@echo "✓ Build complete"
	@echo "  Image: ${IMG}"
	@echo "  Version: $(VERSION)"
	@echo "  Git commit: $(GIT_COMMIT)"

.PHONY: docker-build-nocache
docker-build-nocache: check-docker ## Build docker image without cache
	@echo "Building image without cache..."
	docker build --no-cache \
		--build-arg VERSION=$(VERSION) \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-t ${IMG} \
		-t $(DOCKER_LATEST_TAG) .
	
	@echo "✓ Build complete"
	@echo "  Image: ${IMG}"
	@echo "  Version: $(VERSION)"
	@echo "  Git commit: $(GIT_COMMIT)"

.PHONY: clean-cache
clean-cache: ## Clean Docker build cache
	docker builder prune --filter type=exec.cachemount --force

.PHONY: prune-all
prune-all: ## Prune all Docker build cache and unused objects
	docker system prune -a --volumes

##@ Deployment

.PHONY: install
install: ## Install CRDs into the K8s cluster specified in ~/.kube/config
	kubectl apply -f config/crd/bases/

.PHONY: uninstall
uninstall: ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config
	kubectl delete -f config/crd/bases/

.PHONY: deploy
deploy: ## Deploy controller to the K8s cluster with Helm
	KUBECONFIG=$(KUBECONFIG) helm upgrade --install dr-syncer charts/dr-syncer \
		--namespace dr-syncer \
		--create-namespace \
		--debug

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster with Helm
	@echo "Undeploying from Kubernetes..."
	KUBECONFIG=$(KUBECONFIG) helm uninstall $(HELM_RELEASE_NAME) \
		--namespace $(HELM_NAMESPACE) \
		--debug
	@echo "✓ Undeployment complete"

##@ Generate

.PHONY: manifests
manifests: controller-gen ## Generate CRDs and sync to Helm chart
	$(CONTROLLER_GEN) crd paths="./..." output:crd:artifacts:config=config/crd/bases
	# Sync CRDs to Helm chart
	for f in config/crd/bases/*.yaml; do \
		echo '{{- if .Values.crds.install }}' > charts/dr-syncer/crds/$$(basename $$f); \
		cat $$f >> charts/dr-syncer/crds/$$(basename $$f); \
		echo '{{- end }}' >> charts/dr-syncer/crds/$$(basename $$f); \
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
CONTROLLER_TOOLS_VERSION ?= v0.13.0

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
