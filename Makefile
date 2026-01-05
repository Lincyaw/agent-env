# Makefile for ARL-Infra

# Image names
OPERATOR_IMG ?= arl-operator:latest
SIDECAR_IMG ?= arl-sidecar:latest

# Minikube context
MINIKUBE_PROFILE ?= minikube

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

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
docker-build-operator: ## Build operator Docker image
	docker build -t $(OPERATOR_IMG) -f Dockerfile.operator .

.PHONY: docker-build-sidecar
docker-build-sidecar: ## Build sidecar Docker image
	docker build -t $(SIDECAR_IMG) -f Dockerfile.sidecar .

.PHONY: docker-build
docker-build: docker-build-operator docker-build-sidecar ## Build all Docker images

##@ Minikube

.PHONY: minikube-start
minikube-start: ## Start minikube
	minikube start --profile=$(MINIKUBE_PROFILE) --driver=docker --cpus=4 --memory=8192

.PHONY: minikube-load-images
minikube-load-images: ## Load images into minikube
	minikube -p $(MINIKUBE_PROFILE) image load $(OPERATOR_IMG)
	minikube -p $(MINIKUBE_PROFILE) image load $(SIDECAR_IMG)

.PHONY: minikube-stop
minikube-stop: ## Stop minikube
	minikube stop --profile=$(MINIKUBE_PROFILE)

.PHONY: minikube-delete
minikube-delete: ## Delete minikube cluster
	minikube delete --profile=$(MINIKUBE_PROFILE)

##@ Deployment

.PHONY: install-crds
install-crds: ## Install CRDs
	kubectl apply -f config/crd/

.PHONY: uninstall-crds
uninstall-crds: ## Uninstall CRDs
	kubectl delete -f config/crd/ --ignore-not-found=true

.PHONY: deploy-operator
deploy-operator: ## Deploy operator
	kubectl apply -f config/operator/deployment.yaml

.PHONY: undeploy-operator
undeploy-operator: ## Remove operator
	kubectl delete -f config/operator/deployment.yaml --ignore-not-found=true

.PHONY: deploy-samples
deploy-samples: ## Deploy sample resources
	kubectl apply -f config/samples/

.PHONY: undeploy-samples
undeploy-samples: ## Remove sample resources
	kubectl delete -f config/samples/ --ignore-not-found=true

.PHONY: deploy
deploy: install-crds deploy-operator ## Deploy CRDs and operator

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
all: tidy build docker-build ## Build everything

.PHONY: quickstart
quickstart: docker-build minikube-load-images deploy ## Quick start: build, load, and deploy
	@echo "ARL-Infra deployed successfully!"
	@echo "Try: kubectl apply -f config/samples/"
