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

"""
Initialize a new Python project with best practices.
Sets up project structure, configuration, and development tools.
"""

import sys
from pathlib import Path

PYPROJECT_TEMPLATE = """[project]
name = "{project_name}"
version = "0.1.0"
description = "Add description here"
readme = "README.md"
requires-python = ">=3.12"
dependencies = []

[project.optional-dependencies]
dev = [
    "ruff>=0.8.0",
    "mypy>=1.13.0",
    "pytest>=8.3.0",
    "pytest-cov>=6.0.0",
    "bandit>=1.8.0",
]

[build-system]
requires = ["hatchling"]
build-backend = "hatchling.build"

[tool.ruff]
line-length = 100
target-version = "py312"

[tool.ruff.lint]
select = [
    "E",      # pycodestyle errors
    "W",      # pycodestyle warnings
    "F",      # pyflakes
    "I",      # isort
    "N",      # pep8-naming
    "UP",     # pyupgrade
    "B",      # flake8-bugbear
    "A",      # flake8-builtins
    "C4",     # flake8-comprehensions
    "SIM",    # flake8-simplify
    "TCH",    # flake8-type-checking
    "RUF",    # ruff-specific rules
]
ignore = ["E501"]  # Line too long (handled by formatter)

[tool.ruff.lint.isort]
known-first-party = ["{project_name}"]

[tool.mypy]
python_version = "3.12"
warn_return_any = true
warn_unused_configs = true
disallow_untyped_defs = true
disallow_incomplete_defs = true
check_untyped_defs = true
no_implicit_optional = true
warn_redundant_casts = true
warn_unused_ignores = true
strict_equality = true

[tool.pytest.ini_options]
testpaths = ["tests"]
python_files = ["test_*.py"]
python_classes = ["Test*"]
python_functions = ["test_*"]
addopts = "--strict-markers --cov={project_name} --cov-report=term-missing"

[tool.coverage.run]
source = ["{project_name}"]
omit = ["*/tests/*", "*/__pycache__/*"]

[tool.coverage.report]
exclude_lines = [
    "pragma: no cover",
    "def __repr__",
    "raise AssertionError",
    "raise NotImplementedError",
    "if __name__ == .__main__.:",
    "if TYPE_CHECKING:",
]
"""

README_TEMPLATE = """# {project_name}

Add project description here.

## Installation

```bash
uv sync
```

## Development

Run quality checks:
```bash
uv run python scripts/check_quality.py
```

Auto-fix issues:
```bash
uv run python scripts/autofix.py
```

Run tests:
```bash
uv run pytest
```

## Usage

```python
from {project_name} import example

# Add usage examples
```
"""

GITIGNORE_TEMPLATE = """# Python
__pycache__/
*.py[cod]
*$py.class
*.so
.Python
build/
develop-eggs/
dist/
downloads/
eggs/
.eggs/
lib/
lib64/
parts/
sdist/
var/
wheels/
*.egg-info/
.installed.cfg
*.egg

# Virtual environments
venv/
ENV/
env/
.venv

# Testing
.pytest_cache/
.coverage
htmlcov/
.tox/

# IDE
.vscode/
.idea/
*.swp
*.swo
*~

# MyPy
.mypy_cache/
.dmypy.json
dmypy.json

# Ruff
.ruff_cache/

# OS
.DS_Store
Thumbs.db
"""


def create_file(path: Path, content: str) -> None:
    """Create a file with given content."""
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content)
    print(f"✅ Created: {path}")


def create_project(project_name: str, path: str | None = None) -> None:
    """Create a new Python project with best practices."""
    project_dir = Path(path) if path else Path.cwd() / project_name

    print(f"🚀 Creating Python project: {project_name}")
    print(f"📁 Location: {project_dir}\n")

    # Create directory structure
    src_dir = project_dir / "src" / project_name
    tests_dir = project_dir / "tests"
    scripts_dir = project_dir / "scripts"

    for directory in [src_dir, tests_dir, scripts_dir]:
        directory.mkdir(parents=True, exist_ok=True)

    # Create configuration files
    create_file(
        project_dir / "pyproject.toml",
        PYPROJECT_TEMPLATE.format(project_name=project_name),
    )

    create_file(
        project_dir / "README.md",
        README_TEMPLATE.format(project_name=project_name),
    )

    create_file(
        project_dir / ".gitignore",
        GITIGNORE_TEMPLATE,
    )

    # Create source files
    create_file(
        src_dir / "__init__.py",
        f'"""The {project_name} package."""\n\n__version__ = "0.1.0"\n',
    )

    create_file(
        src_dir / "core.py",
        '"""Core functionality."""\n\n\ndef example() -> str:\n    """Example function."""\n    return "Hello, World!"\n',
    )

    # Create test files
    create_file(
        tests_dir / "__init__.py",
        "",
    )

    create_file(
        tests_dir / "test_core.py",
        f'''"""Tests for core functionality."""

from {project_name}.core import example


def test_example() -> None:
    """Test example function."""
    assert example() == "Hello, World!"
''',
    )

    # Copy quality check scripts (placeholder - in real use, copy from skill)
    create_file(
        scripts_dir / "check_quality.py",
        "# Copy check_quality.py from python skill\n",
    )

    create_file(
        scripts_dir / "autofix.py",
        "# Copy autofix.py from python skill\n",
    )

    print(f"\n✅ Project '{project_name}' created successfully!")
    print("\n📋 Next steps:")
    print(f"   cd {project_dir}")
    print("   uv sync")
    print("   uv run pytest")


def main():
    """Main entry point."""
    if len(sys.argv) < 2:
        print("Usage: python init_project.py <project-name> [path]")
        sys.exit(1)

    project_name = sys.argv[1]
    path = sys.argv[2] if len(sys.argv) > 2 else None

    create_project(project_name, path)


if __name__ == "__main__":
    main()
