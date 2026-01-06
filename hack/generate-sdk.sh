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

# Create OpenAPI specification by converting CRD schemas
echo "==> Creating OpenAPI specification for ARL resources"

# Function to convert CRD schema to OpenAPI schema
convert_crd_to_openapi() {
    local crd_file=$1
    local output_file=$2
    
    # Extract the schema and clean up Kubernetes-specific fields
    yq eval '
        .spec.versions[0].schema.openAPIV3Schema |
        del(.properties.apiVersion) |
        del(.properties.kind) |
        del(.properties.metadata) |
        del(.. | select(has("x-kubernetes-*")))
    ' "$crd_file" > "$output_file"
}

# Convert each CRD
echo "Converting Sandbox CRD..."
convert_crd_to_openapi "${CRD_DIR}/arl.infra.io_sandboxes.yaml" "${OPENAPI_DIR}/sandbox-schema.json"

echo "Converting Task CRD..."
convert_crd_to_openapi "${CRD_DIR}/arl.infra.io_tasks.yaml" "${OPENAPI_DIR}/task-schema.json"

echo "Converting WarmPool CRD..."
convert_crd_to_openapi "${CRD_DIR}/arl.infra.io_warmpools.yaml" "${OPENAPI_DIR}/warmpool-schema.json"

# Create a unified OpenAPI spec
cat > "${OPENAPI_DIR}/arl-api.yaml" <<'EOF'
openapi: 3.0.0
info:
  title: ARL Infrastructure API
  version: v1alpha1
  description: |
    OpenAPI specification for ARL (Agentic RL) Infrastructure custom resources.
    This SDK provides models for WarmPool, Sandbox, and Task Kubernetes custom resources.
  contact:
    name: ARL Infrastructure
    url: https://github.com/Lincyaw/agent-env

servers: []

paths:
  /api/v1alpha1/namespaces/{namespace}/sandboxes:
    get:
      summary: List Sandboxes
      operationId: listSandboxes
      parameters:
        - name: namespace
          in: path
          required: true
          schema:
            type: string
      responses:
        '200':
          description: List of sandboxes
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/SandboxList'
    post:
      summary: Create Sandbox
      operationId: createSandbox
      parameters:
        - name: namespace
          in: path
          required: true
          schema:
            type: string
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/Sandbox'
      responses:
        '201':
          description: Sandbox created
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Sandbox'

  /api/v1alpha1/namespaces/{namespace}/tasks:
    get:
      summary: List Tasks
      operationId: listTasks
      parameters:
        - name: namespace
          in: path
          required: true
          schema:
            type: string
      responses:
        '200':
          description: List of tasks
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/TaskList'
    post:
      summary: Create Task
      operationId: createTask
      parameters:
        - name: namespace
          in: path
          required: true
          schema:
            type: string
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/Task'
      responses:
        '201':
          description: Task created
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Task'

  /api/v1alpha1/namespaces/{namespace}/warmpools:
    get:
      summary: List WarmPools
      operationId: listWarmPools
      parameters:
        - name: namespace
          in: path
          required: true
          schema:
            type: string
      responses:
        '200':
          description: List of warmpools
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/WarmPoolList'
    post:
      summary: Create WarmPool
      operationId: createWarmPool
      parameters:
        - name: namespace
          in: path
          required: true
          schema:
            type: string
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/WarmPool'
      responses:
        '201':
          description: WarmPool created
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/WarmPool'

components:
  schemas:
    Sandbox:
      type: object
      properties:
        apiVersion:
          type: string
          example: arl.infra.io/v1alpha1
        kind:
          type: string
          example: Sandbox
        metadata:
          $ref: '#/components/schemas/ObjectMeta'
        spec:
          $ref: '#/components/schemas/SandboxSpec'
        status:
          $ref: '#/components/schemas/SandboxStatus'
    
    SandboxSpec:
      type: object
      required:
        - poolRef
      properties:
        poolRef:
          type: string
          description: Name of the WarmPool to allocate from
        keepAlive:
          type: boolean
          description: Keep pod alive after task completion
        resources:
          $ref: '#/components/schemas/ResourceRequirements'
    
    SandboxStatus:
      type: object
      properties:
        phase:
          type: string
          enum: [Pending, Bound, Ready, Failed]
        podName:
          type: string
        podIP:
          type: string
        workDir:
          type: string
        conditions:
          type: array
          items:
            $ref: '#/components/schemas/Condition'
    
    Task:
      type: object
      properties:
        apiVersion:
          type: string
          example: arl.infra.io/v1alpha1
        kind:
          type: string
          example: Task
        metadata:
          $ref: '#/components/schemas/ObjectMeta'
        spec:
          $ref: '#/components/schemas/TaskSpec'
        status:
          $ref: '#/components/schemas/TaskStatus'
    
    TaskSpec:
      type: object
      required:
        - sandboxRef
        - steps
      properties:
        sandboxRef:
          type: string
          description: Name of the Sandbox to execute in
        timeout:
          type: string
          description: Maximum execution time (e.g., "30s", "5m")
        steps:
          type: array
          items:
            $ref: '#/components/schemas/TaskStep'
        retries:
          type: integer
          minimum: 0
          maximum: 10
        ttlSecondsAfterFinished:
          type: integer
          minimum: 0
    
    TaskStep:
      type: object
      required:
        - name
        - type
      properties:
        name:
          type: string
        type:
          type: string
          enum: [FilePatch, Command]
        content:
          type: string
          description: Patch content for FilePatch type
        path:
          type: string
          description: File path for FilePatch operations
        command:
          type: array
          items:
            type: string
          description: Command to execute for Command type
        workDir:
          type: string
        env:
          type: object
          additionalProperties:
            type: string
    
    TaskStatus:
      type: object
      properties:
        state:
          type: string
          enum: [Pending, Running, Succeeded, Failed]
        exitCode:
          type: integer
        stdout:
          type: string
        stderr:
          type: string
        duration:
          type: string
        startTime:
          type: string
          format: date-time
        completionTime:
          type: string
          format: date-time
        conditions:
          type: array
          items:
            $ref: '#/components/schemas/Condition'
    
    WarmPool:
      type: object
      properties:
        apiVersion:
          type: string
          example: arl.infra.io/v1alpha1
        kind:
          type: string
          example: WarmPool
        metadata:
          $ref: '#/components/schemas/ObjectMeta'
        spec:
          $ref: '#/components/schemas/WarmPoolSpec'
        status:
          $ref: '#/components/schemas/WarmPoolStatus'
    
    WarmPoolSpec:
      type: object
      required:
        - replicas
        - template
      properties:
        replicas:
          type: integer
          format: int32
          description: Number of idle pods to maintain
        template:
          $ref: '#/components/schemas/PodTemplateSpec'
    
    WarmPoolStatus:
      type: object
      properties:
        readyReplicas:
          type: integer
          format: int32
        allocatedReplicas:
          type: integer
          format: int32
        conditions:
          type: array
          items:
            $ref: '#/components/schemas/Condition'
    
    SandboxList:
      type: object
      properties:
        apiVersion:
          type: string
        kind:
          type: string
        metadata:
          $ref: '#/components/schemas/ListMeta'
        items:
          type: array
          items:
            $ref: '#/components/schemas/Sandbox'
    
    TaskList:
      type: object
      properties:
        apiVersion:
          type: string
        kind:
          type: string
        metadata:
          $ref: '#/components/schemas/ListMeta'
        items:
          type: array
          items:
            $ref: '#/components/schemas/Task'
    
    WarmPoolList:
      type: object
      properties:
        apiVersion:
          type: string
        kind:
          type: string
        metadata:
          $ref: '#/components/schemas/ListMeta'
        items:
          type: array
          items:
            $ref: '#/components/schemas/WarmPool'
    
    ObjectMeta:
      type: object
      properties:
        name:
          type: string
        namespace:
          type: string
        labels:
          type: object
          additionalProperties:
            type: string
        annotations:
          type: object
          additionalProperties:
            type: string
    
    ListMeta:
      type: object
      properties:
        resourceVersion:
          type: string
        continue:
          type: string
    
    Condition:
      type: object
      properties:
        type:
          type: string
        status:
          type: string
        lastTransitionTime:
          type: string
          format: date-time
        reason:
          type: string
        message:
          type: string
    
    ResourceRequirements:
      type: object
      properties:
        limits:
          type: object
          additionalProperties:
            type: string
        requests:
          type: object
          additionalProperties:
            type: string
    
    PodTemplateSpec:
      type: object
      properties:
        metadata:
          $ref: '#/components/schemas/ObjectMeta'
        spec:
          type: object
          description: Pod specification - simplified for SDK
EOF

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
    --additional-properties=packageVersion=0.1.0,projectName=arl-client,library=urllib3

echo "==> Python SDK generation complete"
echo "    Output: ${SDK_DIR}"
