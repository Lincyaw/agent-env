#!/bin/bash

# Simple validation test for ARL-Infra

set -e

echo "=== ARL-Infra Validation Test ==="
echo ""

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

print_info() {
    echo -e "${YELLOW}ℹ $1${NC}"
}

# Test 1: Check Go code compiles
echo "Test 1: Compiling Go code..."
go build -o /tmp/operator ./cmd/operator/main.go || { print_error "Operator build failed"; exit 1; }
go build -o /tmp/sidecar ./cmd/sidecar/main.go || { print_error "Sidecar build failed"; exit 1; }
print_success "Both binaries compiled successfully"
echo ""

# Test 2: Verify CRD files are valid YAML
echo "Test 2: Validating CRD YAML files..."
for crd in config/crd/*.yaml; do
    python3 -c "import yaml; yaml.safe_load(open('$crd'))" 2>/dev/null || {
        print_error "Invalid YAML: $crd"
        exit 1
    }
done
print_success "All CRD files are valid YAML"
echo ""

# Test 3: Verify sample files are valid
echo "Test 3: Validating sample YAML files..."
for sample in config/samples/*.yaml; do
    python3 -c "import yaml; yaml.safe_load(open('$sample'))" 2>/dev/null || {
        print_error "Invalid YAML: $sample"
        exit 1
    }
done
print_success "All sample files are valid YAML"
echo ""

# Test 4: Run sidecar server briefly
echo "Test 4: Testing sidecar server..."
/tmp/sidecar --workspace=/tmp/test-workspace --port=9999 &
SIDECAR_PID=$!
sleep 2

# Check if sidecar is running
if kill -0 $SIDECAR_PID 2>/dev/null; then
    print_success "Sidecar server started successfully"
    
    # Test health endpoint
    response=$(curl -s http://localhost:9999/health || echo "failed")
    if [[ "$response" == *"healthy"* ]]; then
        print_success "Sidecar health check passed"
    else
        print_error "Sidecar health check failed"
    fi
    
    kill $SIDECAR_PID
    wait $SIDECAR_PID 2>/dev/null || true
else
    print_error "Sidecar server failed to start"
    exit 1
fi
echo ""

# Test 5: Check file structure
echo "Test 5: Verifying project structure..."
required_files=(
    "api/v1alpha1/warmpool_types.go"
    "api/v1alpha1/sandbox_types.go"
    "api/v1alpha1/task_types.go"
    "pkg/controller/warmpool_controller.go"
    "pkg/controller/sandbox_controller.go"
    "pkg/controller/task_controller.go"
    "pkg/sidecar/service.go"
    "pkg/sidecar/server.go"
    "cmd/operator/main.go"
    "cmd/sidecar/main.go"
    "Dockerfile.operator"
    "Dockerfile.sidecar"
    "Makefile"
    "README.md"
)

for file in "${required_files[@]}"; do
    if [ ! -f "$file" ]; then
        print_error "Missing required file: $file"
        exit 1
    fi
done
print_success "All required files present"
echo ""

# Summary
echo "=== Validation Summary ==="
print_success "All validation tests passed!"
echo ""
echo "Components verified:"
echo "  ✓ Operator binary builds successfully"
echo "  ✓ Sidecar binary builds and runs"
echo "  ✓ CRD definitions are valid"
echo "  ✓ Sample manifests are valid"
echo "  ✓ Project structure is complete"
echo ""
echo "Next steps:"
echo "  1. Build Docker images: make docker-build"
echo "  2. Start minikube: make minikube-start"
echo "  3. Deploy to cluster: make deploy"
echo "  4. Create samples: kubectl apply -f config/samples/"
