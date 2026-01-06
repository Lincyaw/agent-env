#!/bin/bash

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${GREEN}=== ARL-Infra: Running Python Examples ===${NC}\n"

# Navigate to examples/python directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PYTHON_DIR="$SCRIPT_DIR/../python"

if [ ! -d "$PYTHON_DIR" ]; then
    echo -e "${RED}Error: Python examples directory not found at $PYTHON_DIR${NC}"
    exit 1
fi

cd "$PYTHON_DIR"

# Check if uv is installed
if ! command -v uv >/dev/null 2>&1; then
    echo -e "${RED}Error: uv is required but not installed.${NC}"
    echo "Install uv: curl -LsSf https://astral.sh/uv/install.sh | sh"
    exit 1
fi

# Check if warm pool is ready
echo -e "${YELLOW}Checking cluster status...${NC}"
if ! minikube kubectl -- get warmpool python-39-std >/dev/null 2>&1; then
    echo -e "${RED}Error: WarmPool 'python-39-std' not found. Please run ./deploy.sh first.${NC}"
    exit 1
fi

POD_COUNT=$(minikube kubectl -- get pods -l arl.infra.io/pool=python-39-std --field-selector=status.phase=Running -o name 2>/dev/null | wc -l)
if [ "$POD_COUNT" -lt 1 ]; then
    echo -e "${RED}Error: No running warm pool pods found. Please wait for deployment to complete.${NC}"
    minikube kubectl -- get pods -l arl.infra.io/pool=python-39-std
    exit 1
fi
echo -e "${GREEN}✓ Found $POD_COUNT running warm pool pod(s)${NC}\n"

# Install dependencies if needed
if [ ! -d ".venv" ]; then
    echo -e "${YELLOW}Installing Python dependencies...${NC}"
    uv sync
    echo -e "${GREEN}✓ Dependencies installed${NC}\n"
fi

# List of example scripts
EXAMPLES=(
    "01_basic_execution.py"
    "02_multi_step_pipeline.py"
    "03_environment_variables.py"
    "04_working_directory.py"
    "05_error_handling.py"
    "06_long_running_task.py"
    "07_sandbox_reuse.py"
)

FAILED_EXAMPLES=()
PASSED_EXAMPLES=()

# Run each example
for example in "${EXAMPLES[@]}"; do
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${YELLOW}Running: $example${NC}"
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    
    if uv run python "$example"; then
        echo -e "${GREEN}✓ $example PASSED${NC}\n"
        PASSED_EXAMPLES+=("$example")
    else
        echo -e "${RED}✗ $example FAILED${NC}\n"
        FAILED_EXAMPLES+=("$example")
    fi
    
    # Small delay between examples
    sleep 2
done

# Summary
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${GREEN}=== Test Results ===${NC}"
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"

echo -e "\n${GREEN}Passed: ${#PASSED_EXAMPLES[@]}/${#EXAMPLES[@]}${NC}"
for example in "${PASSED_EXAMPLES[@]}"; do
    echo -e "  ${GREEN}✓${NC} $example"
done

if [ ${#FAILED_EXAMPLES[@]} -gt 0 ]; then
    echo -e "\n${RED}Failed: ${#FAILED_EXAMPLES[@]}/${#EXAMPLES[@]}${NC}"
    for example in "${FAILED_EXAMPLES[@]}"; do
        echo -e "  ${RED}✗${NC} $example"
    done
    echo -e "\n${YELLOW}Check the logs above for error details.${NC}"
    exit 1
else
    echo -e "\n${GREEN}All examples passed successfully! 🎉${NC}"
fi

echo -e "\n${YELLOW}Cleanup:${NC}"
echo "Sandboxes and tasks created by examples are cleaned up automatically."
echo "To verify: minikube kubectl -- get sandboxes,tasks"
echo -e "\nTo completely clean up the cluster:"
echo "  cd $SCRIPT_DIR && ./cleanup.sh"
