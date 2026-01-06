# Makefile for ARL-Infra

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

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
	uv run ruff format .
	uv run ruff check --fix . --unsafe-fixes

.PHONY: generate
generate: ## Generate deepcopy code
	go run sigs.k8s.io/controller-tools/cmd/controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./api/..."

.PHONY: manifests
manifests: ## Generate CRD manifests
	go run sigs.k8s.io/controller-tools/cmd/controller-gen crd:maxDescLen=0 paths="./api/..." output:crd:artifacts:config=config/crd

.PHONY: sdk-python
sdk-python: manifests ## Generate Python SDK from CRDs
	./hack/generate-sdk.sh
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

##@ Utilities

.PHONY: logs
logs: ## Show operator logs
	kubectl logs -n arl-system -l app=arl-operator --tail=100 -f

