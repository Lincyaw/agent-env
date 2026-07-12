# Makefile for ARL-Infra

SHELL := /bin/bash

REGISTRY ?= pair-diag-cn-guangzhou.cr.volces.com/pair
PLATFORM ?= linux/amd64
DEFAULT_IMAGE_TAG := $(shell git rev-parse --short HEAD)-$(shell date +%Y%m%d%H%M%S)
IMAGE_TAG ?= $(DEFAULT_IMAGE_TAG)
GATEWAY_TAG ?= $(IMAGE_TAG)
SIDECAR_TAG ?= v0.15.6
EXECUTOR_AGENT_TAG ?= v0.15.6
IMAGE_LOCALITY_SCHEDULER_TAG ?= v0.15.6

DEPLOY_RELEASE ?= agent-env
DEPLOY_NAMESPACE ?= arl1
DEPLOY_FULLNAME ?= agent-env
DEPLOY_NAME ?= $(DEPLOY_FULLNAME)
DEPLOY_SECRET_PREFIX ?= $(DEPLOY_FULLNAME)
GATEWAY_SERVICE_TYPE ?= LoadBalancer
GATEWAY_SERVICE_PORT ?= 8080

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z0-9_-]+:.*?##/ { printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Setup

.PHONY: install-tools
install-tools: install-protoc install-go-tools install-python-tools ## Install all development tools

.PHONY: install-protoc
install-protoc: ## Install Protocol Buffers compiler
	@echo "Installing protoc..."
	@if command -v protoc >/dev/null 2>&1; then \
		echo "protoc already installed: $$(protoc --version)"; \
	else \
		PROTOC_VERSION=28.3; \
		PROTOC_ZIP=protoc-$${PROTOC_VERSION}-linux-x86_64.zip; \
		curl -LO https://github.com/protocolbuffers/protobuf/releases/download/v$${PROTOC_VERSION}/$${PROTOC_ZIP}; \
		sudo unzip -o $${PROTOC_ZIP} -d /usr/local bin/protoc; \
		sudo unzip -o $${PROTOC_ZIP} -d /usr/local 'include/*'; \
		rm -f $${PROTOC_ZIP}; \
		echo "protoc installed: $$(protoc --version)"; \
	fi

.PHONY: install-go-tools
install-go-tools: ## Install Go development tools
	@echo "Installing Go tools..."
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	go install sigs.k8s.io/controller-tools/cmd/controller-gen@latest
	@echo "Go tools installed successfully"

.PHONY: install-python-tools
install-python-tools: ## Install Python development tools
	@echo "Installing Python tools..."
	uv sync --all-groups
	uv pip install grpcio-tools
	@echo "Python tools installed successfully"

##@ Deployment (Skaffold)

.PHONY: k8s-setup
k8s-setup: ## Setup prerequisites for a new K8s cluster
	@echo "Setting up K8s cluster prerequisites..."
	@echo "1. Updating Helm dependencies..."
	cd charts/agent-env && helm dependency update
	@echo "Setup complete. Install agent-sandbox first, then run 'skaffold run --profile=dev' to deploy agent-env."

.PHONY: push-gateway-image
push-gateway-image: ## Build and push the gateway image
	docker build --platform $(PLATFORM) -t $(REGISTRY)/arl-gateway:$(GATEWAY_TAG) -f Dockerfile.gateway .
	docker push $(REGISTRY)/arl-gateway:$(GATEWAY_TAG)

.PHONY: push-images
push-images: ## Build and push all deployable images
	docker build --platform $(PLATFORM) -t $(REGISTRY)/arl-sidecar:$(SIDECAR_TAG) -f Dockerfile.sidecar .
	docker push $(REGISTRY)/arl-sidecar:$(SIDECAR_TAG)
	docker build --platform $(PLATFORM) -t $(REGISTRY)/arl-gateway:$(GATEWAY_TAG) -f Dockerfile.gateway .
	docker push $(REGISTRY)/arl-gateway:$(GATEWAY_TAG)
	docker build --platform $(PLATFORM) -t $(REGISTRY)/arl-executor-agent:$(EXECUTOR_AGENT_TAG) -f Dockerfile.executor-agent .
	docker push $(REGISTRY)/arl-executor-agent:$(EXECUTOR_AGENT_TAG)
	docker build --platform $(PLATFORM) -t $(REGISTRY)/arl-image-locality-scheduler:$(IMAGE_LOCALITY_SCHEDULER_TAG) -f Dockerfile.image-locality-scheduler .
	docker push $(REGISTRY)/arl-image-locality-scheduler:$(IMAGE_LOCALITY_SCHEDULER_TAG)

.PHONY: deploy-agent-env
deploy-agent-env: ## Deploy chart using existing namespace secrets
	@set -euo pipefail; \
	tmpdir="$$(mktemp -d)"; \
	cleanup() { rm -rf "$$tmpdir"; }; \
	trap cleanup EXIT; \
	echo "Deploying $(DEPLOY_RELEASE) to namespace $(DEPLOY_NAMESPACE)"; \
	kubectl get secret $(DEPLOY_SECRET_PREFIX)-auth -n $(DEPLOY_NAMESPACE) -o jsonpath='{.data.api-keys}' | base64 -d > "$$tmpdir/auth-keys"; \
	kubectl get secret $(DEPLOY_SECRET_PREFIX)-grafana -n $(DEPLOY_NAMESPACE) -o jsonpath='{.data.admin-password}' | base64 -d > "$$tmpdir/grafana-admin-password"; \
	kubectl get secret $(DEPLOY_SECRET_PREFIX)-clickhouse -n $(DEPLOY_NAMESPACE) -o jsonpath='{.data.password}' | base64 -d > "$$tmpdir/clickhouse-password"; \
	kubectl get secret $(DEPLOY_SECRET_PREFIX)-grpc-token -n $(DEPLOY_NAMESPACE) -o jsonpath='{.data.token}' | base64 -d > "$$tmpdir/grpc-token"; \
	helm template $(DEPLOY_RELEASE) charts/agent-env \
		--namespace $(DEPLOY_NAMESPACE) \
		--set fullnameOverride=$(DEPLOY_FULLNAME) \
		--set nameOverride=$(DEPLOY_NAME) \
		--set auth.enabled=true \
		--set-file auth.apiKeys="$$tmpdir/auth-keys" \
		--set-file auth.grpcToken="$$tmpdir/grpc-token" \
		--set-file grafana.adminPassword="$$tmpdir/grafana-admin-password" \
		--set clickhouse.enabled=true \
		--set-file clickhouse.password="$$tmpdir/clickhouse-password" \
		--set redis.enabled=true \
		--set redis.deploy=true \
		--set gateway.service.type=$(GATEWAY_SERVICE_TYPE) \
		--set-string gateway.service.port=$(GATEWAY_SERVICE_PORT) \
		--set image.injectedPullPolicy=Always \
		--set gateway.image.repository=$(REGISTRY)/arl-gateway \
		--set gateway.image.tag=$(GATEWAY_TAG) \
		--set sidecar.image.repository=$(REGISTRY)/arl-sidecar \
		--set sidecar.image.tag=$(SIDECAR_TAG) \
		--set executorAgent.image.repository=$(REGISTRY)/arl-executor-agent \
		--set executorAgent.image.tag=$(EXECUTOR_AGENT_TAG) \
		--set imageLocalityScheduler.image.repository=$(REGISTRY)/arl-image-locality-scheduler \
		--set imageLocalityScheduler.image.tag=$(IMAGE_LOCALITY_SCHEDULER_TAG) \
		| kubectl apply --server-side=true --force-conflicts -f -

.PHONY: deploy-arl1
deploy-arl1: DEPLOY_NAMESPACE=arl1
deploy-arl1: DEPLOY_RELEASE=agent-env
deploy-arl1: DEPLOY_FULLNAME=agent-env
deploy-arl1: DEPLOY_NAME=agent-env
deploy-arl1: DEPLOY_SECRET_PREFIX=agent-env
deploy-arl1: push-gateway-image deploy-agent-env ## Build gateway and deploy the arl1 stack

.PHONY: deploy-arl1-all
deploy-arl1-all: DEPLOY_NAMESPACE=arl1
deploy-arl1-all: DEPLOY_RELEASE=agent-env
deploy-arl1-all: DEPLOY_FULLNAME=agent-env
deploy-arl1-all: DEPLOY_NAME=agent-env
deploy-arl1-all: DEPLOY_SECRET_PREFIX=agent-env
deploy-arl1-all: SIDECAR_TAG=$(IMAGE_TAG)
deploy-arl1-all: EXECUTOR_AGENT_TAG=$(IMAGE_TAG)
deploy-arl1-all: IMAGE_LOCALITY_SCHEDULER_TAG=$(IMAGE_TAG)
deploy-arl1-all: push-images deploy-agent-env ## Build all images and deploy the arl1 stack


##@ Build

.PHONY: build
build: ## Build all Go binaries
	go build ./...

.PHONY: build-gateway
build-gateway: ## Build gateway binary
	CGO_ENABLED=0 go build -o bin/gateway cmd/gateway/main.go

.PHONY: build-executor-agent
build-executor-agent: ## Build executor agent binary
	CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/executor-agent cmd/executor-agent/main.go

.PHONY: build-sidecar
build-sidecar: ## Build sidecar binary
	CGO_ENABLED=0 go build -o bin/sidecar cmd/sidecar/main.go

.PHONY: build-cli
build-cli: ## Build arl CLI binary
	CGO_ENABLED=0 go build -o bin/arl ./cmd/arl

.PHONY: codex-skills
codex-skills: ## Generate Codex compatibility skills from plugin slash commands
	cd plugin && python3 scripts/build_codex_compat_skills.py --repo-root . --out-dir .codex-generated-skills --clean

.PHONY: install-codex-skills
install-codex-skills: ## Install ARL Codex skills into ~/.codex/skills
	cd plugin && ./install-codex-skills.sh
	
.PHONY: build-image-locality-scheduler
build-image-locality-scheduler: ## Build image locality scheduler binary
	CGO_ENABLED=0 go build -tags=scheduler_plugin -o bin/image-locality-scheduler cmd/image-locality-scheduler/main.go

##@ Development

.PHONY: fmt
fmt: ## Run go fmt
	go fmt ./...

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: tidy
tidy: ## Run go mod tidy
	go mod tidy

.PHONY: check
check: fmt vet tidy ## Run all code quality checks
	uv run ruff check --fix sdk/python/arl/arl/*.py examples/python/*.py --unsafe-fixes
	uv run ruff format sdk/python/arl/arl/*.py examples/python/*.py
	uv run mypy sdk/python/arl/arl examples/python


##@ Code Generation

.PHONY: generate
generate: proto-go proto-executor-v2 ## Generate all code

.PHONY: proto-go
proto-go: ## Generate Go gRPC code from proto files
	@mkdir -p pkg/pb
	protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		proto/agent.proto
	@mv proto/*.pb.go pkg/pb/ 2>/dev/null || true

.PHONY: proto-executor-v2
proto-executor-v2: ## Generate Go code from executor V2 proto
	@mkdir -p pkg/pb/executorv2
	protoc --go_out=. --go_opt=paths=source_relative \
		proto/executor_v2.proto
	@mv proto/executor_v2.pb.go pkg/pb/executorv2/ 2>/dev/null || true

##@ Python SDK

.PHONY: build-sdk
build-sdk: ## Build Python SDK package
	cd sdk/python/arl && uv build

.PHONY: publish-test
publish-test: build-sdk ## Publish to Test PyPI (requires UV_PUBLISH_TOKEN)
	cd sdk/python/arl && uv publish --publish-url https://test.pypi.org/legacy/

.PHONY: publish
publish: build-sdk ## Publish to Production PyPI (requires UV_PUBLISH_TOKEN)
	cd sdk/python/arl && uv publish

.PHONY: clean-sdk
clean-sdk: ## Clean Python SDK build artifacts
	rm -rf sdk/python/arl/dist sdk/python/arl/build sdk/python/arl/*.egg-info

##@ Architecture

.PHONY: arch-check
arch-check: ## Validate architecture documentation
	uv run python hack/arch-lint.py validate

##@ Utilities

.PHONY: logs
logs: ## Show gateway logs
	kubectl logs -n arl -l app.kubernetes.io/component=gateway --tail=100 -f
