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

"""Deploy MkDocs documentation to GitHub Pages."""

import argparse
import subprocess
import sys
from pathlib import Path


def deploy_docs(project_dir: Path, message: str | None = None, force: bool = False) -> int:
    """Deploy documentation to GitHub Pages."""
    mkdocs_yml = project_dir / "mkdocs.yml"
    if not mkdocs_yml.exists():
        print(f"Error: mkdocs.yml not found at {mkdocs_yml}")
        print("Run 'init_docs.py' first to initialize the documentation.")
        return 1

    cmd = ["mkdocs", "gh-deploy", "-f", str(mkdocs_yml)]

    if message:
        cmd.extend(["-m", message])

    if force:
        cmd.append("--force")

    print("Deploying to GitHub Pages...")
    result = subprocess.run(cmd, cwd=project_dir)

    if result.returncode == 0:
        print("\n✓ Documentation deployed successfully to GitHub Pages!")
        print("\nYour documentation will be available at:")
        print("  https://<username>.github.io/<repository>/")
        print("\nNote: It may take a few minutes for changes to appear.")

    return result.returncode


def main() -> int:
    parser = argparse.ArgumentParser(description="Deploy MkDocs documentation to GitHub Pages")
    parser.add_argument(
        "project_dir",
        type=Path,
        nargs="?",
        default=Path.cwd(),
        help="Project directory (default: current directory)",
    )
    parser.add_argument(
        "-m",
        "--message",
        type=str,
        help="Commit message for the deployment (default: 'Deployed {sha} with MkDocs version: {version}')",
    )
    parser.add_argument(
        "--force",
        action="store_true",
        help="Force push to gh-pages branch (use with caution)",
    )
    args = parser.parse_args()

    return deploy_docs(args.project_dir, args.message, args.force)


if __name__ == "__main__":
    sys.exit(main())
