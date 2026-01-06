# Makefile for ARL-Infra

# Image registry for standard K8s deployment
REGISTRY ?= 10.10.10.240/library

# Image names for minikube (local)
OPERATOR_IMG_LOCAL ?= arl-operator:latest
SIDECAR_IMG_LOCAL ?= arl-sidecar:latest

# Image names for standard K8s (with registry)
OPERATOR_IMG ?= $(REGISTRY)/arl-operator:latest
SIDECAR_IMG ?= $(REGISTRY)/arl-sidecar:latest

# Minikube context
MINIKUBE_PROFILE ?= minikube

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Skaffold (Recommended)

.PHONY: dev
dev: ## Start development mode with auto-rebuild (minikube)
	skaffold dev --profile=dev

.PHONY: run
run: ## Build and deploy once (minikube)
	skaffold run

.PHONY: run-samples
run-samples: ## Build and deploy with samples (minikube)
	skaffold run --profile=with-samples

.PHONY: k8s-run
k8s-run: ## Build, push and deploy to standard K8s
	skaffold run --profile=k8s

.PHONY: k8s-run-samples
k8s-run-samples: ## Build, push and deploy to K8s with samples
	skaffold run --profile=k8s-with-samples

.PHONY: delete
delete: ## Delete deployed resources
	skaffold delete || true

.PHONY: k8s-delete
k8s-delete: ## Delete deployed resources from K8s
	skaffold delete --profile=k8s || true

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

.PHONY: generate
generate: ## Generate deepcopy code
	go run sigs.k8s.io/controller-tools/cmd/controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./api/..."

.PHONY: manifests
manifests: ## Generate CRD manifests
	go run sigs.k8s.io/controller-tools/cmd/controller-gen crd:maxDescLen=0 paths="./api/..." output:crd:artifacts:config=config/crd

.PHONY: sdk-python
sdk-python: manifests ## Generate Python SDK from CRDs
	./hack/generate-sdk.sh

##@ Build

.PHONY: build-operator
build-operator: ## Build operator binary
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/operator cmd/operator/main.go

.PHONY: build-sidecar
build-sidecar: ## Build sidecar binary
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/sidecar cmd/sidecar/main.go

.PHONY: build
build: build-operator build-sidecar ## Build all binaries

##@ Docker

.PHONY: docker-build-operator
docker-build-operator: ## Build operator Docker image for minikube
	docker build -t $(OPERATOR_IMG_LOCAL) -f Dockerfile.operator .

.PHONY: docker-build-sidecar
docker-build-sidecar: ## Build sidecar Docker image for minikube
	docker build -t $(SIDECAR_IMG_LOCAL) -f Dockerfile.sidecar .

.PHONY: docker-build
docker-build: docker-build-operator docker-build-sidecar ## Build all Docker images for minikube

.PHONY: docker-build-k8s
docker-build-k8s: ## Build and tag Docker images for standard K8s
	docker build -t $(OPERATOR_IMG) -f Dockerfile.operator .
	docker build -t $(SIDECAR_IMG) -f Dockerfile.sidecar .

.PHONY: docker-push
docker-push: ## Push Docker images to registry
	docker push $(OPERATOR_IMG)
	docker push $(SIDECAR_IMG)

##@ Minikube

.PHONY: minikube-start
minikube-start: ## Start minikube
	minikube start --profile=$(MINIKUBE_PROFILE) --driver=docker --cpus=4 --memory=8192

.PHONY: minikube-stop
minikube-stop: ## Stop minikube
	minikube stop --profile=$(MINIKUBE_PROFILE)

.PHONY: minikube-delete
minikube-delete: ## Delete minikube cluster
	minikube delete --profile=$(MINIKUBE_PROFILE)

##@ Deployment

.PHONY: install-crds
install-crds: ## Install CRDs
	kubectl apply --server-side=true --force-conflicts -f config/crd/

.PHONY: uninstall-crds
uninstall-crds: ## Uninstall CRDs
	kubectl delete -f config/crd/ --ignore-not-found=true

.PHONY: deploy-operator
deploy-operator: ## Deploy operator
	kubectl apply --server-side=true --force-conflicts -f config/operator/deployment.yaml

.PHONY: undeploy-operator
undeploy-operator: ## Remove operator
	kubectl delete -f config/operator/deployment.yaml --ignore-not-found=true

.PHONY: deploy-samples
deploy-samples: ## Deploy sample resources
	kubectl apply --server-side=true --force-conflicts -f config/samples/

.PHONY: undeploy-samples
undeploy-samples: ## Remove sample resources
	kubectl delete -f config/samples/ --ignore-not-found=true

.PHONY: deploy
deploy: install-crds deploy-operator ## Deploy CRDs and operator

##@ Standard K8s Deployment

.PHONY: k8s-build-push
k8s-build-push: docker-build-k8s docker-push ## Build and push images for K8s

.PHONY: k8s-deploy
k8s-deploy: install-crds ## Deploy to standard K8s cluster
	kubectl apply --server-side=true --force-conflicts -f config/operator/deployment.yaml

.PHONY: k8s-undeploy
k8s-undeploy: ## Remove from standard K8s cluster
	kubectl delete -f config/operator/deployment.yaml --ignore-not-found=true
	$(MAKE) uninstall-crds

.PHONY: undeploy
undeploy: undeploy-samples undeploy-operator uninstall-crds ## Remove all resources

##@ Testing

.PHONY: test-integration
test-integration: ## Run integration tests with minikube
	@echo "Running integration tests..."
	kubectl get warmpools
	kubectl get sandboxes
	kubectl get tasks

.PHONY: logs
logs: ## Show operator logs
	kubectl logs -n arl-system -l app=arl-operator --tail=100 -f

##@ All-in-one

.PHONY: all
all: tidy build docker-build ## Build everything (legacy)

.PHONY: quickstart
quickstart: minikube-start run-samples ## Quick start: start minikube and deploy with samples
	@echo "ARL-Infra deployed successfully!"
	@echo "Check resources: kubectl get warmpools,sandboxes,tasks"

.PHONY: quickstart-dev
quickstart-dev: minikube-start dev ## Quick start in development mode with auto-reload
	@echo "Starting development mode..."
