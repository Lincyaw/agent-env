#!/usr/bin/env python3
"""
Auto-fix Python code quality issues.
Automatically formats code and fixes common linting issues.
"""

import subprocess
import sys


def run_command(cmd: list[str], description: str) -> bool:
    """Run a command and print the result."""
    print(f"🔧 {description}...")
    try:
        result = subprocess.run(
            cmd,
            capture_output=True,
            text=True,
            check=False,
        )

        if result.returncode == 0:
            print(f"   ✅ {description} completed")
            if result.stdout.strip():
                print(f"   {result.stdout}")
            return True
        else:
            print(f"   ⚠️  {description} completed with warnings")
            if result.stderr.strip():
                print(f"   {result.stderr}")
            return False
    except FileNotFoundError as e:
        print(f"   ❌ Tool not found: {e}")
        return False


def main():
    """Run auto-fix tools."""
    target = sys.argv[1] if len(sys.argv) > 1 else "."

    print(f"🚀 Auto-fixing Python code quality issues in: {target}\n")

    # Run Ruff auto-fixes
    run_command(
        ["uv", "run", "ruff", "check", "--fix", target],
        "Ruff auto-fix linting issues",
    )

    # Run Ruff formatting
    run_command(
        ["uv", "run", "ruff", "format", target],
        "Ruff format code",
    )

    # Sort imports (Ruff handles this too, but being explicit)
    run_command(
        ["uv", "run", "ruff", "check", "--select", "I", "--fix", target],
        "Sort imports",
    )

    print("\n✅ Auto-fix completed!")
    print("💡 Run 'uv run python scripts/check_quality.py' to verify all fixes")


if __name__ == "__main__":
    main()
