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

"""Serve MkDocs documentation locally with live reload."""

import argparse
import subprocess
import sys
from pathlib import Path


def serve_docs(project_dir: Path, port: int = 8000, host: str = "127.0.0.1") -> int:
    """Serve documentation with live reload."""
    mkdocs_yml = project_dir / "mkdocs.yml"
    if not mkdocs_yml.exists():
        print(f"Error: mkdocs.yml not found at {mkdocs_yml}")
        print("Run 'init_docs.py' first to initialize the documentation.")
        return 1

    print(f"Serving documentation at http://{host}:{port}")
    print("Press Ctrl+C to stop")

    cmd = ["mkdocs", "serve", "-a", f"{host}:{port}", "-f", str(mkdocs_yml)]
    result = subprocess.run(cmd, cwd=project_dir)
    return result.returncode


def main() -> int:
    parser = argparse.ArgumentParser(description="Serve MkDocs documentation locally")
    parser.add_argument(
        "project_dir",
        type=Path,
        nargs="?",
        default=Path.cwd(),
        help="Project directory (default: current directory)",
    )
    parser.add_argument(
        "--port", "-p", type=int, default=8000, help="Port to serve on (default: 8000)"
    )
    parser.add_argument(
        "--host",
        type=str,
        default="127.0.0.1",
        help="Host to serve on (default: 127.0.0.1)",
    )
    args = parser.parse_args()

    return serve_docs(args.project_dir, args.port, args.host)


if __name__ == "__main__":
    sys.exit(main())
