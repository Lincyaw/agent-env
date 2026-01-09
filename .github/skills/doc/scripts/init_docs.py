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

"""Initialize MkDocs Material documentation project."""

import argparse
import subprocess
import sys
from pathlib import Path


def run_command(cmd: list[str], cwd: Path | None = None) -> int:
    """Run a command and return its exit code."""
    print(f"Running: {' '.join(cmd)}")
    result = subprocess.run(cmd, cwd=cwd)
    return result.returncode


def init_docs(project_dir: Path) -> int:
    """Initialize documentation project."""
    if not project_dir.exists():
        project_dir.mkdir(parents=True)

    docs_dir = project_dir / "docs"
    if docs_dir.exists():
        print(f"Documentation already exists at {docs_dir}")
        return 1

    # Create basic structure
    docs_dir.mkdir()
    (docs_dir / "index.md").write_text(
        """# Welcome to the Documentation

This is the home page of your documentation.

## Getting Started

Add your documentation content here.

## Features

- Modern Material Design theme
- Full-text search
- Mobile responsive
- Dark mode support
- Fast static site generation
"""
    )

    # Create basic mkdocs.yml
    mkdocs_yml = project_dir / "mkdocs.yml"
    mkdocs_yml.write_text(
        """site_name: Documentation
site_description: Project Documentation
site_author: Your Name

theme:
  name: material
  palette:
    # Light mode
    - media: "(prefers-color-scheme: light)"
      scheme: default
      primary: indigo
      accent: indigo
      toggle:
        icon: material/brightness-7
        name: Switch to dark mode
    # Dark mode
    - media: "(prefers-color-scheme: dark)"
      scheme: slate
      primary: indigo
      accent: indigo
      toggle:
        icon: material/brightness-4
        name: Switch to light mode
  features:
    - navigation.tabs
    - navigation.sections
    - navigation.top
    - navigation.tracking
    - search.suggest
    - search.highlight
    - content.tabs.link
    - content.code.annotation
    - content.code.copy
  language: en
  font:
    text: Roboto
    code: Roboto Mono

markdown_extensions:
  - pymdownx.highlight:
      anchor_linenums: true
  - pymdownx.inlinehilite
  - pymdownx.snippets
  - admonition
  - pymdownx.details
  - pymdownx.superfences:
      custom_fences:
        - name: mermaid
          class: mermaid
          format: !!python/name:pymdownx.superfences.fence_code_format
  - pymdownx.tabbed:
      alternate_style: true
  - pymdownx.tasklist:
      custom_checkbox: true
  - attr_list
  - md_in_html
  - pymdownx.emoji:
      emoji_index: !!python/name:material.extensions.emoji.twemoji
      emoji_generator: !!python/name:material.extensions.emoji.to_svg
  - toc:
      permalink: true

plugins:
  - search

nav:
  - Home: index.md

extra:
  social:
    - icon: fontawesome/brands/github
      link: https://github.com/yourusername/yourproject
"""
    )

    print(f"✓ Documentation initialized at {docs_dir}")
    print(f"✓ Configuration created at {mkdocs_yml}")
    print("\nNext steps:")
    print(f"  1. Edit {mkdocs_yml} to customize your site")
    print(f"  2. Add documentation files to {docs_dir}")
    print("  3. Run 'serve_docs.py' to preview locally")
    print("  4. Run 'build_docs.py' to build for production")

    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description="Initialize MkDocs documentation")
    parser.add_argument(
        "project_dir",
        type=Path,
        nargs="?",
        default=Path.cwd(),
        help="Project directory (default: current directory)",
    )
    args = parser.parse_args()

    return init_docs(args.project_dir)


if __name__ == "__main__":
    sys.exit(main())
