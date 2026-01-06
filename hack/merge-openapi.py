#!/usr/bin/env python3
"""
Merge CRD OpenAPI schemas into unified OpenAPI specification.
This script extracts schemas from CRD YAML files and merges them with the template.
"""

import sys
from pathlib import Path
from typing import Any

import yaml


def load_yaml(file_path: Path) -> dict[str, Any]:
    """Load YAML file."""
    with open(file_path) as f:
        return yaml.safe_load(f)


def save_yaml(data: dict[str, Any], file_path: Path):
    """Save data as YAML file."""
    with open(file_path, "w") as f:
        yaml.dump(data, f, default_flow_style=False, sort_keys=False)


def extract_crd_schema(crd_data: dict[str, Any], resource_name: str) -> dict[str, Any]:
    """Extract OpenAPI schema from CRD."""
    try:
        schema = crd_data["spec"]["versions"][0]["schema"]["openAPIV3Schema"]

        # Remove top-level Kubernetes metadata fields we don't want in the model
        properties = schema.get("properties", {})

        # Build the resource schema
        resource_schema = {
            "type": "object",
            "properties": {
                "apiVersion": {
                    "type": "string",
                    "description": "APIVersion defines the versioned schema of this representation",
                },
                "kind": {
                    "type": "string",
                    "description": "Kind is a string value representing the REST resource",
                },
                "metadata": {"$ref": "#/components/schemas/ObjectMeta"},
            },
        }

        # Add spec and status if they exist
        if "spec" in properties:
            resource_schema["properties"]["spec"] = {
                "$ref": f"#/components/schemas/{resource_name}Spec"
            }

        if "status" in properties:
            resource_schema["properties"]["status"] = {
                "$ref": f"#/components/schemas/{resource_name}Status"
            }

        schemas = {resource_name: resource_schema}

        # Extract Spec schema
        if "spec" in properties:
            spec_schema = properties["spec"].copy()
            # Clean up k8s specific fields
            clean_schema(spec_schema)
            schemas[f"{resource_name}Spec"] = spec_schema

            # Extract first-level nested schemas only (e.g., TaskStep from steps[])
            extract_nested_schemas(spec_schema, schemas, max_depth=1)
        # Extract Status schema
        if "status" in properties:
            status_schema = properties["status"].copy()
            clean_schema(status_schema)
            schemas[f"{resource_name}Status"] = status_schema

            # Extract first-level nested schemas only
            extract_nested_schemas(status_schema, schemas, max_depth=1)
        return schemas

    except KeyError as e:
        print(f"Error extracting schema: {e}", file=sys.stderr)
        return {}


def clean_schema(schema: dict[str, Any]):
    """Remove Kubernetes-specific fields from schema."""
    # Remove x-kubernetes-* extensions
    keys_to_remove = [k for k in schema if k.startswith("x-kubernetes-")]
    for key in keys_to_remove:
        del schema[key]

    # Recursively clean nested schemas
    if "properties" in schema:
        for prop in schema["properties"].values():
            if isinstance(prop, dict):
                clean_schema(prop)

    if "items" in schema and isinstance(schema["items"], dict):
        clean_schema(schema["items"])

    if "additionalProperties" in schema and isinstance(schema["additionalProperties"], dict):
        clean_schema(schema["additionalProperties"])


def enhance_task_step_schema(schema: dict[str, Any]):
    """Add enhanced documentation and examples for TaskStep schema."""
    if "properties" not in schema:
        return

    props = schema["properties"]

    # Add descriptions for each field
    if "name" in props:
        props["name"]["description"] = "Step identifier (unique within the task)"
        props["name"]["example"] = "step1_write_file"

    if "type" in props:
        props["type"]["description"] = (
            "Step type - either 'FilePatch' (create/modify files) or 'Command' (execute commands)"
        )
        props["type"]["enum"] = ["FilePatch", "Command"]
        props["type"]["example"] = "Command"

    if "content" in props:
        props["content"]["description"] = (
            "File content to write (required for FilePatch steps, ignored for Command steps)"
        )
        props["content"]["example"] = "print('Hello, World!')"

    if "path" in props:
        props["path"]["description"] = (
            "File path to create or modify (required for FilePatch steps, ignored for Command steps)"
        )
        props["path"]["example"] = "/workspace/script.py"

    if "command" in props:
        props["command"]["description"] = (
            "Command and arguments to execute (required for Command steps, ignored for FilePatch steps)"
        )
        props["command"]["example"] = ["python", "script.py", "--verbose"]

    if "workDir" in props:
        props["workDir"]["description"] = (
            "Working directory for command execution (optional, only for Command steps)"
        )
        props["workDir"]["example"] = "/workspace"

    if "env" in props:
        props["env"]["description"] = (
            "Environment variables as key-value pairs (optional, only for Command steps)"
        )
        props["env"]["example"] = {"DEBUG": "1", "API_KEY": "secret"}

    # Add overall description
    schema["description"] = """Task step definition. Each step can be either:

1. **FilePatch**: Create or modify a file
   - Required: name, type="FilePatch", path, content
   - Example: {"name": "write", "type": "FilePatch", "path": "/workspace/test.py", "content": "print('test')"}

2. **Command**: Execute a command
   - Required: name, type="Command", command
   - Optional: workDir, env
   - Example: {"name": "run", "type": "Command", "command": ["python", "test.py"], "env": {"DEBUG": "1"}}
"""


def extract_nested_schemas(
    spec_schema: dict[str, Any],
    schemas: dict[str, Any],
    prefix: str = "",
    max_depth: int = 1,
    current_depth: int = 0,
):
    """Extract nested object schemas as separate schema definitions.

    Args:
        spec_schema: Schema to extract from
        schemas: Dictionary to store extracted schemas
        prefix: Prefix for schema names
        max_depth: Maximum depth to extract (1 = only direct children)
        current_depth: Current recursion depth
    """
    if "properties" not in spec_schema or current_depth >= max_depth:
        return

    for prop_name, prop_schema in spec_schema["properties"].items():
        if not isinstance(prop_schema, dict):
            continue

        # Check if this is an array of objects
        if prop_schema.get("type") == "array" and isinstance(prop_schema.get("items"), dict):
            items = prop_schema["items"]
            if items.get("type") == "object" and "properties" in items:
                # Determine schema name from property name
                # Special case: "steps" -> "TaskStep" (singular)
                if prop_name == "steps":
                    schema_name = "TaskStep"
                else:
                    # Convert snake_case or camelCase to PascalCase, remove trailing 's'
                    schema_name = "".join(word.capitalize() for word in prop_name.split("_"))
                    if schema_name.endswith("s") and len(schema_name) > 1:
                        schema_name = schema_name[:-1]

                if schema_name not in schemas:
                    schema_copy = items.copy()
                    clean_schema(schema_copy)

                    # Enhance specific schemas
                    if schema_name == "TaskStep":
                        enhance_task_step_schema(schema_copy)

                    schemas[schema_name] = schema_copy

                    # Replace with reference
                    prop_schema["items"] = {"$ref": f"#/components/schemas/{schema_name}"}

            # Don't recurse too deep to avoid extracting too many Kubernetes internal schemas
            extract_nested_schemas(prop_schema, schemas, prefix)


def merge_schemas(
    template: dict[str, Any], crd_schemas: dict[str, dict[str, Any]]
) -> dict[str, Any]:
    """Merge CRD schemas into OpenAPI template."""
    result = template.copy()

    # Get or create components/schemas section
    if "components" not in result:
        result["components"] = {}
    if "schemas" not in result["components"]:
        result["components"]["schemas"] = {}

    # Merge all CRD schemas
    for _resource_type, schemas in crd_schemas.items():
        for schema_name, schema_def in schemas.items():
            # Override placeholder schemas from template
            result["components"]["schemas"][schema_name] = schema_def
            print(f"  Added schema: {schema_name}")

    return result


def main():
    """Main function."""
    # Setup paths
    script_dir = Path(__file__).parent
    project_root = script_dir.parent
    crd_dir = project_root / "config" / "crd"
    openapi_dir = project_root / "openapi"
    template_file = openapi_dir / "template.yaml"
    output_file = openapi_dir / "arl-api.yaml"

    print("==> Extracting schemas from CRDs")

    # CRD file mapping
    crd_files = {
        "Sandbox": crd_dir / "arl.infra.io_sandboxes.yaml",
        "Task": crd_dir / "arl.infra.io_tasks.yaml",
        "WarmPool": crd_dir / "arl.infra.io_warmpools.yaml",
    }

    # Extract schemas from each CRD
    all_schemas = {}
    for resource_name, crd_file in crd_files.items():
        if not crd_file.exists():
            print(f"Warning: CRD file not found: {crd_file}", file=sys.stderr)
            continue

        print(f"  Processing {resource_name} from {crd_file.name}")
        crd_data = load_yaml(crd_file)
        schemas = extract_crd_schema(crd_data, resource_name)
        all_schemas[resource_name] = schemas

    # Load template
    print(f"\n==> Loading template from {template_file}")
    template = load_yaml(template_file)

    # Merge schemas
    print("\n==> Merging schemas into OpenAPI spec")
    merged = merge_schemas(template, all_schemas)

    # Save result
    print(f"\n==> Saving to {output_file}")
    save_yaml(merged, output_file)

    print("\n✓ OpenAPI specification generated successfully")
    print(f"  Output: {output_file}")
    print(f"  Total schemas: {len(merged['components']['schemas'])}")


if __name__ == "__main__":
    main()
