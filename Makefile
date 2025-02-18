# Image URL to use all building/pushing image targets
TIMESTAMP ?= $(shell date +%Y%m%d%H%M%S)
IMG ?= supporttools/dr-syncer:$(TIMESTAMP)

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes
SHELL = /bin/bash

.PHONY: all
all: build

.PHONY: deploy-local
deploy-local: ## Build, push image, install CRDs, and deploy to current cluster with correct image tag
	@export LOG_LEVEL=debug
	$(eval DEPLOY_TIMESTAMP := $(shell date +%Y%m%d%H%M%S))
	docker build -t supporttools/dr-syncer:$(DEPLOY_TIMESTAMP) .
	docker push supporttools/dr-syncer:$(DEPLOY_TIMESTAMP)
	helm upgrade --install dr-syncer charts/dr-syncer \
		--namespace dr-syncer \
		--create-namespace \
		--set image.repository=supporttools/dr-syncer \
		--set image.tag=$(DEPLOY_TIMESTAMP)
	@echo "Deployed dr-syncer to current cluster with image: $(IMG)"

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
docker-build: ## Build and push docker image
	docker build -t ${IMG} .
	docker push ${IMG}

##@ Deployment

.PHONY: install
install: ## Install CRDs into the K8s cluster specified in ~/.kube/config
	kubectl apply -f config/crd/bases/

.PHONY: uninstall
uninstall: ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config
	kubectl delete -f config/crd/bases/

.PHONY: deploy
deploy: ## Deploy controller to the K8s cluster with Helm
	helm upgrade --install dr-syncer charts/dr-syncer \
		--namespace dr-syncer \
		--create-namespace

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster with Helm
	helm uninstall dr-syncer -n dr-syncer

##@ Generate

.PHONY: manifests
manifests: controller-gen ## Generate CRDs
	$(CONTROLLER_GEN) crd paths="./..." output:crd:artifacts:config=config/crd/bases

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
