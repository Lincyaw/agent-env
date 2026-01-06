#!/bin/bash

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}=== ARL-Infra: Deployment ===${NC}\n"

# Navigate to project root
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# Check if minikube is running
if ! minikube status >/dev/null 2>&1; then
    echo -e "${RED}Error: Minikube is not running. Please run ./setup-minikube.sh first.${NC}"
    exit 1
fi

echo -e "${YELLOW}Step 1: Deploying CRDs...${NC}"
minikube kubectl -- apply -f "$PROJECT_ROOT/config/crd/"
echo -e "${GREEN}✓ CRDs deployed${NC}"

echo -e "\n${YELLOW}Step 2: Deploying ARL Operator...${NC}"
minikube kubectl -- apply -f "$PROJECT_ROOT/config/operator/deployment.yaml"
echo -e "${GREEN}✓ Operator deployment created${NC}"

echo -e "\n${YELLOW}Step 3: Waiting for operator to be ready...${NC}"
echo "This may take a minute..."
if minikube kubectl -- wait --for=condition=ready pod -l app=arl-operator -n arl-system --timeout=120s 2>/dev/null; then
    echo -e "${GREEN}✓ Operator is ready${NC}"
else
    echo -e "${YELLOW}Warning: Operator pod not ready yet, continuing anyway...${NC}"
    echo "You can check status with: minikube kubectl -- get pods -n arl-system"
fi

echo -e "\n${YELLOW}Step 4: Deploying WarmPool...${NC}"
minikube kubectl -- apply -f "$SCRIPT_DIR/manifests/warmpool.yaml"
echo -e "${GREEN}✓ WarmPool created${NC}"

echo -e "\n${YELLOW}Step 5: Waiting for warm pool pods to be ready...${NC}"
echo "This may take a minute..."
sleep 5  # Give operator time to react

# Wait for WarmPool to create pods
MAX_WAIT=60
ELAPSED=0
while [ $ELAPSED -lt $MAX_WAIT ]; do
    POD_COUNT=$(minikube kubectl -- get pods -l arl.infra.io/pool=python-39-std -o name 2>/dev/null | wc -l)
    if [ "$POD_COUNT" -ge 1 ]; then
        echo "Found $POD_COUNT warm pool pod(s)"
        break
    fi
    echo "Waiting for warm pool pods to be created... ($ELAPSED/${MAX_WAIT}s)"
    sleep 5
    ELAPSED=$((ELAPSED + 5))
done

if minikube kubectl -- wait --for=condition=ready pod -l arl.infra.io/pool=python-39-std --timeout=120s 2>/dev/null; then
    echo -e "${GREEN}✓ Warm pool pods are ready${NC}"
else
    echo -e "${YELLOW}Warning: Warm pool pods not fully ready yet, continuing anyway...${NC}"
    echo "You can check status with: minikube kubectl -- get pods -l arl.infra.io/pool=python-39-std"
fi

# Verification
echo -e "\n${GREEN}=== Deployment Status ===${NC}"

echo -e "\n${YELLOW}Operator:${NC}"
minikube kubectl -- get pods -n arl-system -l app=arl-operator

echo -e "\n${YELLOW}WarmPool:${NC}"
minikube kubectl -- get warmpools

echo -e "\n${YELLOW}Warm Pool Pods:${NC}"
minikube kubectl -- get pods -l arl.infra.io/pool=python-39-std

echo -e "\n${GREEN}=== Deployment Complete ===${NC}"
echo -e "Next steps:"
echo "  1. Run ./run-examples.sh to test Python examples"
echo "  2. Or manually run examples:"
echo "     cd ../python"
echo "     uv run python 01_basic_execution.py"
echo -e "\nTo check operator logs:"
echo "  minikube kubectl -- logs -n arl-system -l app=arl-operator --tail=50 -f"
echo -e "\nTo describe WarmPool:"
echo "  minikube kubectl -- describe warmpool python-39-std"
