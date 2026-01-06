#!/usr/bin/env bash

# Script to generate Python SDK from CRD OpenAPI schemas
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
CRD_DIR="${PROJECT_ROOT}/config/crd"
OPENAPI_DIR="${PROJECT_ROOT}/openapi"
SDK_DIR="${PROJECT_ROOT}/sdk/python/arl-client"
TEMPLATE_FILE="${OPENAPI_DIR}/template.yaml"

echo "==> Generating Python SDK from CRD OpenAPI schemas"

# Create openapi directory if it doesn't exist
mkdir -p "${OPENAPI_DIR}"

# Check if Python3 is available
if ! command -v python3 &> /dev/null; then
    echo "Error: Python3 is required but not installed."
    exit 1
fi

# Check if PyYAML is available
if ! python3 -c "import yaml" &> /dev/null; then
    echo "Error: PyYAML is required. Install it with: pip install pyyaml"
    exit 1
fi

# Automatically extract and merge CRD schemas into OpenAPI spec
echo "==> Auto-generating OpenAPI spec from CRDs"
python3 "${SCRIPT_DIR}/merge-openapi.py"


# Generate Python client using openapi-generator (Docker)
echo "==> Generating Python SDK using openapi-generator"

# Check if Docker is available
if ! command -v docker &> /dev/null; then
    echo "Error: Docker is required but not installed."
    exit 1
fi

# Create SDK directory
mkdir -p "${SDK_DIR}"

echo "Generating Python client..."

docker run --rm \
    --user "$(id -u):$(id -g)" \
    -v "${OPENAPI_DIR}:/openapi" \
    -v "${SDK_DIR}:/output" \
    openapitools/openapi-generator-cli:v7.12.0 generate \
    -i "/openapi/arl-api.yaml" \
    -g python \
    -o "/output" \
    --package-name "arl_client" \
    --additional-properties=packageVersion=0.1.0,projectName=arl-client,library=urllib3

echo "==> Post-processing generated files"

# Fix pyproject.toml to use modern [project] table format (for uv compatibility)
python3 "${SCRIPT_DIR}/fix-arl-client-pyproject.py" "${SDK_DIR}/pyproject.toml"

echo "==> Python SDK generation complete"
echo "    Output: ${SDK_DIR}"
echo ""
echo "Note: Custom wrapper code is in sdk/python/arl-wrapper/"
echo "      Auto-generated client code is in sdk/python/arl-client/"
