# Makefile for ARL-Infra

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

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

.PHONY: dev
dev: ## Start development mode with auto-rebuild (minikube)
	skaffold dev --profile=dev

.PHONY: run
run: ## Build and deploy once (minikube)
	skaffold run --profile=with-samples

.PHONY: k8s-run
k8s-run: ## Build, push and deploy to standard K8s with samples
	skaffold run --profile=k8s-with-samples

.PHONY: delete
delete: ## Delete deployed resources
	skaffold delete || true

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
generate: proto-go manifests deepcopy sdk-python ## Generate all code (proto, CRDs, deepcopy, Python SDK)

.PHONY: proto-go
proto-go: ## Generate Go gRPC code from proto files
	@mkdir -p pkg/pb
	protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		proto/agent.proto
	@mv proto/*.pb.go pkg/pb/ 2>/dev/null || true



.PHONY: manifests
manifests: ## Generate CRD manifests
	go run sigs.k8s.io/controller-tools/cmd/controller-gen crd:maxDescLen=0 paths="./api/..." output:crd:artifacts:config=config/crd

.PHONY: deepcopy
deepcopy: ## Generate deepcopy code
	go run sigs.k8s.io/controller-tools/cmd/controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./api/..."

.PHONY: sdk-python
sdk-python: ## Generate Python SDK from CRDs
	./hack/generate-sdk.sh
	uv run ruff format .
	uv run ruff check --fix . --unsafe-fixes

##@ Python SDK

.PHONY: build-sdk
build-sdk: sdk-python ## Build Python SDK package
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
	rm -rf sdk/python/arl/arl/arl_client

##@ Architecture

.PHONY: arch-check
arch-check: ## Validate architecture documentation
	uv run python hack/arch-lint.py validate

##@ Utilities

.PHONY: logs
logs: ## Show operator logs
	kubectl logs -n arl-system -l app=arl-operator --tail=100 -f

