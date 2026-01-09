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
Comprehensive Python code quality checker.
Runs multiple linting and checking tools to ensure code quality.
"""

import subprocess
import sys
from pathlib import Path
from typing import NamedTuple


class CheckResult(NamedTuple):
    """Result of a single check."""

    name: str
    passed: bool
    output: str


def run_command(cmd: list[str], name: str) -> CheckResult:
    """Run a command and return the result."""
    try:
        result = subprocess.run(
            cmd,
            capture_output=True,
            text=True,
            check=False,
        )
        passed = result.returncode == 0
        output = result.stdout + result.stderr
        return CheckResult(name, passed, output)
    except FileNotFoundError:
        return CheckResult(
            name,
            False,
            f"❌ {name} not installed. Install with: uv add --dev {name.lower()}",
        )


def check_ruff_lint(target: str = ".") -> CheckResult:
    """Run Ruff linter."""
    return run_command(
        ["uv", "run", "ruff", "check", target],
        "Ruff Lint",
    )


def check_ruff_format(target: str = ".") -> CheckResult:
    """Check Ruff formatting."""
    return run_command(
        ["uv", "run", "ruff", "format", "--check", target],
        "Ruff Format",
    )


def check_mypy(target: str = ".") -> CheckResult:
    """Run MyPy type checker."""
    return run_command(
        ["uv", "run", "mypy", target],
        "MyPy",
    )


def check_pytest(target: str = "tests") -> CheckResult:
    """Run pytest tests."""
    if not Path(target).exists():
        return CheckResult("Pytest", True, "⚠️  No tests directory found")

    return run_command(
        ["uv", "run", "pytest", target, "-v"],
        "Pytest",
    )


def check_bandit(target: str = ".") -> CheckResult:
    """Run Bandit security linter."""
    return run_command(
        ["uv", "run", "bandit", "-r", target, "-ll"],
        "Bandit Security",
    )


def print_separator():
    """Print a separator line."""
    print("=" * 80)


def main():
    """Run all quality checks."""
    target = sys.argv[1] if len(sys.argv) > 1 else "."

    print(f"🔍 Running Python code quality checks on: {target}\n")

    checks = [
        check_ruff_lint(target),
        check_ruff_format(target),
        check_mypy(target),
        check_pytest("tests"),
        check_bandit(target),
    ]

    all_passed = True

    for result in checks:
        print_separator()
        status = "✅ PASSED" if result.passed else "❌ FAILED"
        print(f"{result.name}: {status}")
        print_separator()

        if result.output.strip():
            print(result.output)
            print()

        if not result.passed:
            all_passed = False

    print_separator()
    if all_passed:
        print("✅ All quality checks passed!")
        print_separator()
        return 0
    else:
        print("❌ Some quality checks failed. Please fix the issues above.")
        print_separator()
        return 1


if __name__ == "__main__":
    sys.exit(main())
