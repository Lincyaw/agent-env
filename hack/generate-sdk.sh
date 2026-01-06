#!/usr/bin/env bash

# Script to generate Python SDK from CRD OpenAPI schemas
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
CRD_DIR="${PROJECT_ROOT}/config/crd"
OPENAPI_DIR="${PROJECT_ROOT}/openapi"
SDK_DIR="${PROJECT_ROOT}/sdk/python/arl-client"

echo "==> Generating Python SDK from CRD OpenAPI schemas"

# Create openapi directory if it doesn't exist
mkdir -p "${OPENAPI_DIR}"

# Extract and create complete OpenAPI schemas
echo "==> Creating OpenAPI specification for ARL resources"

# Create a combined OpenAPI spec
cat > "${OPENAPI_DIR}/arl-api.yaml" <<'EOF'
openapi: 3.0.0
info:
  title: ARL Infrastructure API
  version: v1alpha1
  description: |
    OpenAPI specification for ARL (Agentic RL) Infrastructure custom resources.
    Provides models for WarmPool, Sandbox, and Task resources.
  contact:
    name: ARL Infrastructure
    url: https://github.com/Lincyaw/agent-env

servers: []

paths: {}

components:
  schemas:
EOF

# Extract schemas for each CRD and add to combined spec
for crd in warmpool sandbox task; do
    echo "Extracting schema for ${crd}..."
    
    # Get the resource name (capitalize first letter)
    case $crd in
        warmpool) resource_name="WarmPool" ;;
        sandbox) resource_name="Sandbox" ;;
        task) resource_name="Task" ;;
    esac
    
    # Extract the complete OpenAPI schema and convert to proper YAML
    yq eval ".spec.versions[0].schema.openAPIV3Schema" \
        "${CRD_DIR}/${crd}.yaml" | \
        yq eval "{ \"${resource_name}\": . }" - >> "${OPENAPI_DIR}/arl-api.yaml"
    
    # Also extract Spec and Status types
    yq eval ".spec.versions[0].schema.openAPIV3Schema.properties.spec" \
        "${CRD_DIR}/${crd}.yaml" | \
        yq eval "{ \"${resource_name}Spec\": . }" - >> "${OPENAPI_DIR}/arl-api.yaml"
    
    yq eval ".spec.versions[0].schema.openAPIV3Schema.properties.status" \
        "${CRD_DIR}/${crd}.yaml" | \
        yq eval "{ \"${resource_name}Status\": . }" - >> "${OPENAPI_DIR}/arl-api.yaml"
done

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
    -v "${OPENAPI_DIR}:/openapi" \
    -v "${SDK_DIR}:/output" \
    openapitools/openapi-generator-cli:v7.12.0 generate \
    -i "/openapi/arl-api.yaml" \
    -g python \
    -o "/output" \
    --package-name "arl_client" \
    --additional-properties=packageVersion=0.1.0,projectName=arl-client,library=urllib3 \
    --skip-validate-spec

echo "==> Python SDK generation complete"
echo "    Output: ${SDK_DIR}"
