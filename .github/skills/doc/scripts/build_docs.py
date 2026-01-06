#!/usr/bin/env python3
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

    print(f"Building documentation...")
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
