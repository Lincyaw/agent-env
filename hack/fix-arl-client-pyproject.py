#!/usr/bin/env python3
"""Fix auto-generated arl-client pyproject.toml to use modern [project] table format.

This script runs after OpenAPI Generator to convert the generated Poetry-based
pyproject.toml to the modern PEP 621 format that uv and other tools expect.
"""

import re
import sys
from pathlib import Path


def fix_pyproject(file_path: Path) -> None:
    """Convert Poetry format to modern [project] table format."""
    content = file_path.read_text()
    
    # Check if already using [project] table
    if "[project]" in content:
        print(f"✓ {file_path} already uses [project] table, skipping")
        return
    
    # Extract Poetry metadata
    name_match = re.search(r'name\s*=\s*"([^"]+)"', content)
    version_match = re.search(r'version\s*=\s*"([^"]+)"', content)
    desc_match = re.search(r'description\s*=\s*"([^"]*)"', content)
    
    if not (name_match and version_match):
        print(f"✗ Could not extract required metadata from {file_path}", file=sys.stderr)
        sys.exit(1)
    
    name = name_match.group(1).replace("_", "-")  # Convert arl_client to arl-client
    version = version_match.group(1)
    description = desc_match.group(1) if desc_match else "ARL Infrastructure API"
    
    # Build new [project] section
    new_content = f"""[project]
name = "{name}"
version = "{version}"
description = "{description} - Auto-generated OpenAPI client"
readme = "README.md"
requires-python = ">=3.8"
license = {{text = "Apache-2.0"}}
authors = [
    {{name = "ARL Infrastructure", email = "team@openapitools.org"}}
]
keywords = ["OpenAPI", "OpenAPI-Generator", "ARL Infrastructure API"]
dependencies = [
    "urllib3>=1.25.3,<3.0.0",
    "python-dateutil>=2.8.2",
    "pydantic>=2",
    "typing-extensions>=4.7.1",
]

[project.optional-dependencies]
dev = [
    "pytest>=7.2.1",
    "pytest-cov>=2.8.1",
    "types-python-dateutil>=2.8.19.14",
    "mypy>=1.5",
]
"""
    
    # Remove [tool.poetry] and [tool.poetry.dependencies] and [tool.poetry.dev-dependencies] sections
    # Keep everything else (build-system, tool.mypy, etc.)
    lines = content.split('\n')
    filtered_lines = []
    skip_section = False
    
    for line in lines:
        # Start of Poetry sections to skip
        if line.strip().startswith('[tool.poetry]') or \
           line.strip().startswith('[tool.poetry.dependencies]') or \
           line.strip().startswith('[tool.poetry.dev-dependencies]'):
            skip_section = True
            continue
        
        # Start of a new section - stop skipping
        if line.strip().startswith('[') and skip_section:
            skip_section = False
        
        # Skip lines in Poetry sections
        if skip_section:
            continue
        
        filtered_lines.append(line)
    
    # Combine new [project] section with remaining content
    remaining_content = '\n'.join(filtered_lines).strip()
    
    final_content = new_content + '\n' + remaining_content + '\n'
    
    # Write back
    file_path.write_text(final_content)
    print(f"✓ Fixed {file_path} to use [project] table")


def main() -> None:
    """Main entry point."""
    if len(sys.argv) != 2:
        print("Usage: fix-arl-client-pyproject.py <pyproject.toml>", file=sys.stderr)
        sys.exit(1)
    
    file_path = Path(sys.argv[1])
    
    if not file_path.exists():
        print(f"✗ File not found: {file_path}", file=sys.stderr)
        sys.exit(1)
    
    fix_pyproject(file_path)


if __name__ == "__main__":
    main()
