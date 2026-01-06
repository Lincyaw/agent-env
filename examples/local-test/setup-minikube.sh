#!/bin/bash

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}=== ARL-Infra: Minikube Setup ===${NC}\n"

# Check prerequisites
echo -e "${YELLOW}Checking prerequisites...${NC}"

command -v docker >/dev/null 2>&1 || { echo -e "${RED}Error: docker is required but not installed.${NC}" >&2; exit 1; }
command -v kubectl >/dev/null 2>&1 || { echo -e "${RED}Error: kubectl is required but not installed.${NC}" >&2; exit 1; }
command -v minikube >/dev/null 2>&1 || { echo -e "${RED}Error: minikube is required but not installed.${NC}" >&2; exit 1; }

echo -e "${GREEN}✓ All prerequisites found${NC}\n"

# Start minikube
echo -e "${YELLOW}Starting minikube...${NC}"
if minikube status >/dev/null 2>&1; then
    echo -e "${GREEN}✓ Minikube is already running${NC}"
else
    echo "Starting new minikube cluster with 4 CPUs and 8GB RAM..."
    minikube start --cpus=4 --memory=8192 --driver=docker
    echo -e "${GREEN}✓ Minikube started successfully${NC}"
fi

# Enable addons
echo -e "\n${YELLOW}Enabling minikube addons...${NC}"
minikube addons enable metrics-server || echo "Warning: metrics-server addon failed (non-critical)"
echo -e "${GREEN}✓ Addons enabled${NC}"

# Configure to use minikube's Docker daemon
echo -e "\n${YELLOW}Configuring Docker environment...${NC}"
eval $(minikube docker-env)
echo -e "${GREEN}✓ Docker environment configured to use minikube${NC}"

# Navigate to project root
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
cd "$PROJECT_ROOT"

# Build images
echo -e "\n${YELLOW}Building Docker images...${NC}"

echo "Building arl-operator:latest..."
docker build -t arl-operator:latest -f Dockerfile.operator . || {
    echo -e "${RED}Error: Failed to build operator image${NC}"
    exit 1
}
echo -e "${GREEN}✓ Operator image built${NC}"

echo "Building arl-sidecar:latest..."
docker build -t arl-sidecar:latest -f Dockerfile.sidecar . || {
    echo -e "${RED}Error: Failed to build sidecar image${NC}"
    exit 1
}
echo -e "${GREEN}✓ Sidecar image built${NC}"

# Verify images
echo -e "\n${YELLOW}Verifying images...${NC}"
docker images | grep -E "arl-(operator|sidecar)" | grep latest
echo -e "${GREEN}✓ All images verified${NC}"

# Summary
echo -e "\n${GREEN}=== Setup Complete ===${NC}"
echo -e "Minikube cluster: ${GREEN}Ready${NC}"
echo -e "Docker images: ${GREEN}Built${NC}"
echo -e "\nNext steps:"
echo "  1. Run ./deploy.sh to deploy ARL infrastructure"
echo "  2. Run ./run-examples.sh to test Python examples"
echo -e "\nTo verify cluster:"
echo "  kubectl cluster-info"
echo "  kubectl get nodes"
echo -e "\nTo use minikube's Docker daemon in your shell:"
echo "  eval \$(minikube docker-env)"
