#!/bin/bash

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}=== ARL-Infra: Cleanup ===${NC}\n"

# Navigate to project root
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# Check if minikube is running
if ! minikube status >/dev/null 2>&1; then
    echo -e "${YELLOW}Minikube is not running. Nothing to clean up.${NC}"
    exit 0
fi

# Ask for confirmation
echo -e "${YELLOW}This will delete all ARL resources from the cluster.${NC}"
read -p "Continue? (y/N) " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "Cleanup cancelled."
    exit 0
fi

echo -e "\n${YELLOW}Step 1: Deleting Tasks...${NC}"
minikube kubectl -- delete tasks --all --timeout=30s || echo "No tasks to delete or timeout"
echo -e "${GREEN}✓ Tasks deleted${NC}"

echo -e "\n${YELLOW}Step 2: Deleting Sandboxes...${NC}"
minikube kubectl -- delete sandboxes --all --timeout=30s || echo "No sandboxes to delete or timeout"
echo -e "${GREEN}✓ Sandboxes deleted${NC}"

echo -e "\n${YELLOW}Step 3: Deleting WarmPools...${NC}"
minikube kubectl -- delete warmpools --all --timeout=30s || echo "No warmpools to delete or timeout"
echo -e "${GREEN}✓ WarmPools deleted${NC}"

echo -e "\n${YELLOW}Step 4: Waiting for warm pool pods to terminate...${NC}"
sleep 3
minikube kubectl -- wait --for=delete pod -l arl.infra.io/pool=python-39-std --timeout=60s 2>/dev/null || echo "Pods already gone or timeout"
echo -e "${GREEN}✓ Warm pool pods terminated${NC}"

echo -e "\n${YELLOW}Step 5: Deleting Operator...${NC}"
minikube kubectl -- delete -f "$PROJECT_ROOT/config/operator/deployment.yaml" --timeout=30s || echo "Operator already deleted or timeout"
echo -e "${GREEN}✓ Operator deleted${NC}"

echo -e "\n${YELLOW}Step 6: Deleting CRDs...${NC}"
minikube kubectl -- delete -f "$PROJECT_ROOT/config/crd/" --timeout=30s || echo "CRDs already deleted or timeout"
echo -e "${GREEN}✓ CRDs deleted${NC}"

echo -e "\n${GREEN}=== Cleanup Complete ===${NC}"
echo -e "\nRemaining resources:"
minikube kubectl -- get all -n arl-system 2>/dev/null || echo "  (namespace deleted)"
minikube kubectl -- get warmpools,sandboxes,tasks 2>/dev/null || echo "  (no ARL resources found)"

echo -e "\n${YELLOW}Note:${NC} The minikube cluster is still running."
echo "To stop minikube: minikube stop"
echo "To delete minikube cluster: minikube delete"
