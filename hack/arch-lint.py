#!/usr/bin/env python3
# Copyright 2024 ARL-Infra Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Architecture validation and dependency query tool.

This tool validates the architecture YAML files and provides dependency analysis.

Usage:
    uv run python hack/arch-lint.py validate
    uv run python hack/arch-lint.py query --component sandbox-controller
    uv run python hack/arch-lint.py query --component sandbox-crd --reverse
"""

from __future__ import annotations

import argparse
import sys
from pathlib import Path
from typing import Any

import yaml


def load_yaml(path: Path) -> dict[str, Any]:
    """Load and parse a YAML file."""
    with path.open() as f:
        return yaml.safe_load(f)


def validate_yaml_syntax(arch_dir: Path) -> list[str]:
    """Validate YAML syntax for all architecture files."""
    errors: list[str] = []
    yaml_files = ["components.yaml", "dependencies.yaml", "propagation-rules.yaml"]

    for filename in yaml_files:
        filepath = arch_dir / filename
        if not filepath.exists():
            errors.append(f"Missing required file: {filepath}")
            continue

        try:
            load_yaml(filepath)
        except yaml.YAMLError as e:
            errors.append(f"YAML syntax error in {filepath}: {e}")

    return errors


def validate_file_paths(arch_dir: Path, repo_root: Path) -> list[str]:
    """Validate that file paths referenced in components exist."""
    errors: list[str] = []
    components_file = arch_dir / "components.yaml"

    if not components_file.exists():
        return errors

    try:
        data = load_yaml(components_file)
    except yaml.YAMLError:
        return errors  # Syntax error reported elsewhere

    components = data.get("components", [])
    for component in components:
        name = component.get("name", "unknown")
        paths = component.get("paths", [])

        for path_str in paths:
            # Handle directory paths (ending with /)
            path = repo_root / path_str.rstrip("/")
            if not path.exists():
                errors.append(f"Component '{name}': path does not exist: {path_str}")

    return errors


def validate_component_references(arch_dir: Path) -> list[str]:
    """Validate that dependencies reference existing components."""
    errors: list[str] = []
    components_file = arch_dir / "components.yaml"
    deps_file = arch_dir / "dependencies.yaml"

    if not components_file.exists() or not deps_file.exists():
        return errors

    try:
        components_data = load_yaml(components_file)
        deps_data = load_yaml(deps_file)
    except yaml.YAMLError:
        return errors

    component_names = {c.get("name") for c in components_data.get("components", [])}
    dependencies = deps_data.get("dependencies", [])

    for dep in dependencies:
        from_comp = dep.get("from")
        to_comp = dep.get("to")

        if from_comp and from_comp not in component_names:
            errors.append(f"Dependency references unknown component: {from_comp}")
        if to_comp and to_comp not in component_names:
            errors.append(f"Dependency references unknown component: {to_comp}")

    return errors


def validate_propagation_rules(arch_dir: Path) -> list[str]:
    """Validate propagation rules structure."""
    errors: list[str] = []
    rules_file = arch_dir / "propagation-rules.yaml"

    if not rules_file.exists():
        return errors

    try:
        data = load_yaml(rules_file)
    except yaml.YAMLError:
        return errors

    rules = data.get("rules", [])
    for rule in rules:
        name = rule.get("name", "unknown")
        if "trigger" not in rule:
            errors.append(f"Rule '{name}': missing 'trigger' field")
        if "actions" not in rule and "validation" not in rule:
            errors.append(f"Rule '{name}': must have 'actions' or 'validation'")

    return errors


def run_validate(arch_dir: Path, repo_root: Path) -> int:
    """Run all validations and report results."""
    all_errors: list[str] = []

    print("Validating architecture files...")
    print()

    # YAML syntax
    print("  Checking YAML syntax...")
    errors = validate_yaml_syntax(arch_dir)
    all_errors.extend(errors)
    if errors:
        for err in errors:
            print(f"    ✗ {err}")
    else:
        print("    ✓ All YAML files are valid")

    # File paths
    print("  Checking file paths...")
    errors = validate_file_paths(arch_dir, repo_root)
    all_errors.extend(errors)
    if errors:
        for err in errors:
            print(f"    ✗ {err}")
    else:
        print("    ✓ All referenced paths exist")

    # Component references
    print("  Checking component references...")
    errors = validate_component_references(arch_dir)
    all_errors.extend(errors)
    if errors:
        for err in errors:
            print(f"    ✗ {err}")
    else:
        print("    ✓ All component references are valid")

    # Propagation rules
    print("  Checking propagation rules...")
    errors = validate_propagation_rules(arch_dir)
    all_errors.extend(errors)
    if errors:
        for err in errors:
            print(f"    ✗ {err}")
    else:
        print("    ✓ All propagation rules are valid")

    print()
    if all_errors:
        print(f"Validation failed with {len(all_errors)} error(s)")
        return 1

    print("Validation passed!")
    return 0


def query_dependencies(
    arch_dir: Path, component: str, *, reverse: bool = False
) -> list[dict[str, Any]]:
    """Query dependencies for a component.

    Args:
        arch_dir: Path to architecture directory
        component: Component name to query
        reverse: If True, find what depends on this component

    Returns:
        List of dependency records
    """
    deps_file = arch_dir / "dependencies.yaml"
    if not deps_file.exists():
        return []

    try:
        data = load_yaml(deps_file)
    except yaml.YAMLError:
        return []

    dependencies = data.get("dependencies", [])
    results: list[dict[str, str]] = []

    for dep in dependencies:
        if reverse:
            if dep.get("to") == component:
                results.append(dep)
        else:
            if dep.get("from") == component:
                results.append(dep)

    return results


def run_query(arch_dir: Path, component: str, *, reverse: bool = False) -> int:
    """Query and display dependencies for a component."""
    deps = query_dependencies(arch_dir, component, reverse=reverse)

    if reverse:
        print(f"Components that depend on '{component}':")
    else:
        print(f"Dependencies of '{component}':")

    print()

    if not deps:
        print("  (none)")
        return 0

    for dep in deps:
        from_comp = dep.get("from", "?")
        to_comp = dep.get("to", "?")
        dep_type = dep.get("type", "unknown")
        description = dep.get("description", "")

        if reverse:
            print(f"  ← {from_comp}")
        else:
            print(f"  → {to_comp}")
        print(f"    Type: {dep_type}")
        if description:
            print(f"    Description: {description}")
        print()

    return 0


def list_components(arch_dir: Path) -> int:
    """List all components."""
    components_file = arch_dir / "components.yaml"
    if not components_file.exists():
        print(f"Components file not found: {components_file}")
        return 1

    try:
        data = load_yaml(components_file)
    except yaml.YAMLError as e:
        print(f"Error parsing components file: {e}")
        return 1

    components = data.get("components", [])

    print("Components:")
    print()

    # Group by type
    by_type: dict[str, list[dict]] = {}
    for comp in components:
        comp_type = comp.get("type", "unknown")
        by_type.setdefault(comp_type, []).append(comp)

    for comp_type in sorted(by_type.keys()):
        print(f"  [{comp_type}]")
        for comp in by_type[comp_type]:
            name = comp.get("name", "unknown")
            print(f"    - {name}")
        print()

    return 0


def main() -> int:
    """Main entry point."""
    parser = argparse.ArgumentParser(description="Architecture validation and query tool")
    parser.add_argument(
        "--arch-dir",
        type=Path,
        default=None,
        help="Path to architecture directory (default: auto-detect)",
    )

    subparsers = parser.add_subparsers(dest="command", help="Available commands")

    # validate command
    subparsers.add_parser("validate", help="Validate architecture files")

    # query command
    query_parser = subparsers.add_parser("query", help="Query component dependencies")
    query_parser.add_argument("--component", "-c", required=True, help="Component name to query")
    query_parser.add_argument(
        "--reverse", "-r", action="store_true", help="Show what depends on this component"
    )

    # list command
    subparsers.add_parser("list", help="List all components")

    args = parser.parse_args()

    # Auto-detect paths
    if args.arch_dir:
        arch_dir = args.arch_dir
        repo_root = arch_dir.parent
    else:
        # Try to find architecture directory
        script_path = Path(__file__).resolve()
        repo_root = script_path.parent.parent
        arch_dir = repo_root / "architecture"

    if not arch_dir.exists():
        print(f"Architecture directory not found: {arch_dir}")
        return 1

    if args.command == "validate":
        return run_validate(arch_dir, repo_root)
    elif args.command == "query":
        return run_query(arch_dir, args.component, reverse=args.reverse)
    elif args.command == "list":
        return list_components(arch_dir)
    else:
        parser.print_help()
        return 1


if __name__ == "__main__":
    sys.exit(main())
