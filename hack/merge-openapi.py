#!/usr/bin/env python3
"""
Merge CRD OpenAPI schemas into unified OpenAPI specification.
This script extracts schemas from CRD YAML files and merges them with the template.
"""

import sys
from pathlib import Path
from typing import Any, Dict

import yaml


def load_yaml(file_path: Path) -> Dict[str, Any]:
    """Load YAML file."""
    with open(file_path, "r") as f:
        return yaml.safe_load(f)


def save_yaml(data: Dict[str, Any], file_path: Path):
    """Save data as YAML file."""
    with open(file_path, "w") as f:
        yaml.dump(data, f, default_flow_style=False, sort_keys=False)


def extract_crd_schema(crd_data: Dict[str, Any], resource_name: str) -> Dict[str, Any]:
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


def clean_schema(schema: Dict[str, Any]):
    """Remove Kubernetes-specific fields from schema."""
    # Remove x-kubernetes-* extensions
    keys_to_remove = [k for k in schema.keys() if k.startswith("x-kubernetes-")]
    for key in keys_to_remove:
        del schema[key]

    # Recursively clean nested schemas
    if "properties" in schema:
        for prop in schema["properties"].values():
            if isinstance(prop, dict):
                clean_schema(prop)

    if "items" in schema and isinstance(schema["items"], dict):
        clean_schema(schema["items"])

    if "additionalProperties" in schema and isinstance(
        schema["additionalProperties"], dict
    ):
        clean_schema(schema["additionalProperties"])


def extract_nested_schemas(
    spec_schema: Dict[str, Any],
    schemas: Dict[str, Any],
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
        if prop_schema.get("type") == "array" and isinstance(
            prop_schema.get("items"), dict
        ):
            items = prop_schema["items"]
            if items.get("type") == "object" and "properties" in items:
                # Determine schema name from property name
                # Special case: "steps" -> "TaskStep" (singular)
                if prop_name == "steps":
                    schema_name = "TaskStep"
                else:
                    # Convert snake_case or camelCase to PascalCase, remove trailing 's'
                    schema_name = "".join(
                        word.capitalize() for word in prop_name.split("_")
                    )
                    if schema_name.endswith("s") and len(schema_name) > 1:
                        schema_name = schema_name[:-1]

                if schema_name not in schemas:
                    schema_copy = items.copy()
                    clean_schema(schema_copy)
                    schemas[schema_name] = schema_copy

                    # Replace with reference
                    prop_schema["items"] = {
                        "$ref": f"#/components/schemas/{schema_name}"
                    }

            # Don't recurse too deep to avoid extracting too many Kubernetes internal schemas
            extract_nested_schemas(prop_schema, schemas, prefix)


def merge_schemas(
    template: Dict[str, Any], crd_schemas: Dict[str, Dict[str, Any]]
) -> Dict[str, Any]:
    """Merge CRD schemas into OpenAPI template."""
    result = template.copy()

    # Get or create components/schemas section
    if "components" not in result:
        result["components"] = {}
    if "schemas" not in result["components"]:
        result["components"]["schemas"] = {}

    # Merge all CRD schemas
    for resource_type, schemas in crd_schemas.items():
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
