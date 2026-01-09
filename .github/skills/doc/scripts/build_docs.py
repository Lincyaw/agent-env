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

"""Build MkDocs documentation for production."""

import argparse
import subprocess
import sys
from pathlib import Path


def build_docs(project_dir: Path, strict: bool = False, clean: bool = True) -> int:
    """Build documentation for production."""
    mkdocs_yml = project_dir / "mkdocs.yml"
    if not mkdocs_yml.exists():
        print(f"Error: mkdocs.yml not found at {mkdocs_yml}")
        print("Run 'init_docs.py' first to initialize the documentation.")
        return 1

    cmd = ["mkdocs", "build", "-f", str(mkdocs_yml)]
    if strict:
        cmd.append("--strict")
    if clean:
        cmd.append("--clean")

    print("Building documentation...")
    result = subprocess.run(cmd, cwd=project_dir)

    if result.returncode == 0:
        site_dir = project_dir / "site"
        print(f"\n✓ Documentation built successfully at {site_dir}")
        print("\nNext steps:")
        print("  - Deploy to GitHub Pages with 'deploy_docs.py'")
        print(f"  - Or manually deploy the contents of {site_dir}")

    return result.returncode


def main() -> int:
    parser = argparse.ArgumentParser(description="Build MkDocs documentation")
    parser.add_argument(
        "project_dir",
        type=Path,
        nargs="?",
        default=Path.cwd(),
        help="Project directory (default: current directory)",
    )
    parser.add_argument(
        "--strict",
        action="store_true",
        help="Enable strict mode (warnings treated as errors)",
    )
    parser.add_argument(
        "--no-clean",
        dest="clean",
        action="store_false",
        help="Don't clean the site directory before building",
    )
    args = parser.parse_args()

    return build_docs(args.project_dir, args.strict, args.clean)


if __name__ == "__main__":
    sys.exit(main())
