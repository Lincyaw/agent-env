#!/usr/bin/env python3
"""Update ARL plugin manifest versions for release packaging."""

from __future__ import annotations

import argparse
import json
import re
from pathlib import Path


VERSION_RE = re.compile(r"^\d+\.\d+\.\d+([.-][0-9A-Za-z.-]+)?$")
MANIFESTS = (
    Path(".claude-plugin/plugin.json"),
    Path(".codex-plugin/plugin.json"),
)
MARKETPLACE = Path(".claude-plugin/marketplace.json")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("version", help="Release version without the leading v")
    parser.add_argument(
        "--plugin-dir",
        type=Path,
        default=Path(__file__).resolve().parents[1],
        help="Plugin root directory",
    )
    return parser.parse_args()


def update_manifest(path: Path, version: str) -> None:
    data = json.loads(path.read_text(encoding="utf-8"))
    old_version = data.get("version")
    if old_version == version:
        print(f"{path}: already {version}")
        return

    data["version"] = version
    path.write_text(
        json.dumps(data, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )
    print(f"{path}: {old_version} -> {version}")


def update_marketplace(path: Path, version: str) -> None:
    if not path.is_file():
        return

    data = json.loads(path.read_text(encoding="utf-8"))
    changed = False
    for plugin in data.get("plugins", []):
        old_version = plugin.get("version")
        if old_version != version:
            plugin["version"] = version
            changed = True
            print(f"{path}: {plugin.get('name', '<unknown>')} {old_version} -> {version}")

    if changed:
        path.write_text(
            json.dumps(data, ensure_ascii=False, indent=2) + "\n",
            encoding="utf-8",
        )
    else:
        print(f"{path}: already {version}")


def main() -> int:
    args = parse_args()
    version = args.version.removeprefix("v")
    if not VERSION_RE.fullmatch(version):
        raise SystemExit(f"Invalid plugin version: {args.version}")

    plugin_dir = args.plugin_dir.resolve()
    for manifest in MANIFESTS:
        path = plugin_dir / manifest
        if not path.is_file():
            raise SystemExit(f"Missing plugin manifest: {path}")
        update_manifest(path, version)
    update_marketplace(plugin_dir / MARKETPLACE, version)

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
