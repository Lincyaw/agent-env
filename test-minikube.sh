#!/bin/bash

# ARL-Infra Minikube Test Script
# This script tests the complete deployment flow

set -e

echo "=== ARL-Infra Minikube Test ==="
echo ""

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Helper functions
print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

print_info() {
    echo -e "${YELLOW}ℹ $1${NC}"
}

# Check prerequisites
echo "Step 1: Checking prerequisites..."
command -v docker >/dev/null 2>&1 || { print_error "docker is required but not installed"; exit 1; }
command -v kubectl >/dev/null 2>&1 || { print_error "kubectl is required but not installed"; exit 1; }
command -v minikube >/dev/null 2>&1 || { print_error "minikube is required but not installed"; exit 1; }
print_success "All prerequisites met"
echo ""

# Build images
echo "Step 2: Building Docker images..."
make docker-build || { print_error "Failed to build images"; exit 1; }
print_success "Images built successfully"
echo ""

# Start minikube
echo "Step 3: Starting minikube..."
minikube status >/dev/null 2>&1 && {
    print_info "Minikube already running"
} || {
    minikube start --driver=docker --cpus=4 --memory=8192 || { print_error "Failed to start minikube"; exit 1; }
    print_success "Minikube started"
}
echo ""

# Load images
echo "Step 4: Loading images into minikube..."
minikube image load arl-operator:latest
minikube image load arl-sidecar:latest
print_success "Images loaded into minikube"
echo ""

# Install CRDs
echo "Step 5: Installing CRDs..."
kubectl apply -f config/crd/ || { print_error "Failed to install CRDs"; exit 1; }
sleep 2
print_success "CRDs installed"
echo ""

# Deploy operator
echo "Step 6: Deploying operator..."
kubectl apply -f config/operator/deployment.yaml || { print_error "Failed to deploy operator"; exit 1; }
print_success "Operator deployed"
echo ""

# Wait for operator to be ready
echo "Step 7: Waiting for operator to be ready..."
kubectl wait --for=condition=available --timeout=120s deployment/arl-operator -n arl-system || {
    print_error "Operator failed to become ready"
    kubectl logs -n arl-system -l app=arl-operator --tail=50
    exit 1
}
print_success "Operator is ready"
echo ""

# Deploy WarmPool
echo "Step 8: Creating WarmPool..."
kubectl apply -f config/samples/warmpool.yaml || { print_error "Failed to create WarmPool"; exit 1; }
print_success "WarmPool created"
echo ""

# Wait for warm pool pods
echo "Step 9: Waiting for warm pool pods..."
sleep 10
kubectl get pods -l arl.infra.io/pool=python-3.9-std
print_info "Warm pool pods are being created"
echo ""

# Deploy Sandbox
echo "Step 10: Creating Sandbox..."
kubectl apply -f config/samples/sandbox.yaml || { print_error "Failed to create Sandbox"; exit 1; }
print_success "Sandbox created"
sleep 5
echo ""

# Check Sandbox status
echo "Step 11: Checking Sandbox status..."
kubectl get sandboxes
SANDBOX_PHASE=$(kubectl get sandbox session-agent-001 -o jsonpath='{.status.phase}')
print_info "Sandbox phase: $SANDBOX_PHASE"
echo ""

# Deploy Task
echo "Step 12: Creating and executing Task..."
kubectl apply -f config/samples/task.yaml || { print_error "Failed to create Task"; exit 1; }
print_success "Task created"
sleep 10
echo ""

# Check Task status
echo "Step 13: Checking Task execution results..."
kubectl get tasks
TASK_STATE=$(kubectl get task task-test-feature-x -o jsonpath='{.status.state}' 2>/dev/null || echo "NotFound")
TASK_EXIT_CODE=$(kubectl get task task-test-feature-x -o jsonpath='{.status.exitCode}' 2>/dev/null || echo "")

print_info "Task state: $TASK_STATE"
if [ "$TASK_EXIT_CODE" != "" ]; then
    print_info "Task exit code: $TASK_EXIT_CODE"
fi
echo ""

# Show detailed status
echo "Step 14: Detailed status..."
echo ""
echo "=== WarmPools ==="
kubectl get warmpools -o wide
echo ""
echo "=== Sandboxes ==="
kubectl get sandboxes -o wide
echo ""
echo "=== Tasks ==="
kubectl get tasks -o wide
echo ""
echo "=== Pods ==="
kubectl get pods -o wide
echo ""

# Show Task output if available
echo "Step 15: Task execution output..."
TASK_STDOUT=$(kubectl get task task-test-feature-x -o jsonpath='{.status.stdout}' 2>/dev/null || echo "")
TASK_STDERR=$(kubectl get task task-test-feature-x -o jsonpath='{.status.stderr}' 2>/dev/null || echo "")

if [ "$TASK_STDOUT" != "" ]; then
    echo "STDOUT:"
    echo "$TASK_STDOUT"
fi

if [ "$TASK_STDERR" != "" ]; then
    echo "STDERR:"
    echo "$TASK_STDERR"
fi
echo ""

# Summary
echo "=== Test Summary ==="
if [ "$TASK_STATE" = "Succeeded" ]; then
    print_success "All tests passed! ARL-Infra is working correctly."
    exit 0
elif [ "$TASK_STATE" = "Running" ] || [ "$TASK_STATE" = "Pending" ]; then
    print_info "Task is still running. Manual verification needed."
    exit 0
else
    print_error "Tests incomplete or failed. Please check the logs above."
    echo ""
    echo "Useful debug commands:"
    echo "  kubectl describe warmpool python-3.9-std"
    echo "  kubectl describe sandbox session-agent-001"
    echo "  kubectl describe task task-test-feature-x"
    echo "  kubectl logs -n arl-system -l app=arl-operator"
    exit 1
fi
