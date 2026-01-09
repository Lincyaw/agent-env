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

"""Run all examples as tests.

This script imports and runs all example modules,
treating them as integration tests.
"""

import importlib
import sys
import time


def run_example(module_name: str, display_name: str) -> bool:
    """Run a single example module."""
    print("\n" + "=" * 80)
    print(f"Running: {display_name}")
    print("=" * 80)

    try:
        # Import and run the module
        module = importlib.import_module(module_name)
        module.main()
        print(f"\n✓ {display_name} completed successfully")
        return True
    except Exception as e:
        print(f"\n✗ {display_name} failed: {e}")
        import traceback

        traceback.print_exc()
        return False


def main():
    """Run all examples."""
    print("\n" + "=" * 80)
    print("ARL Python SDK - Running All Examples")
    print("=" * 80)

    examples = [
        ("01_basic_execution", "01. Basic Execution"),
        ("02_multi_step_pipeline", "02. Multi-Step Pipeline"),
        ("03_environment_variables", "03. Environment Variables"),
        ("04_working_directory", "04. Working Directory"),
        ("05_error_handling", "05. Error Handling"),
        ("06_long_running_task", "06. Long-Running Task"),
        ("07_sandbox_reuse", "07. Sandbox Reuse"),
        ("08_callback_hooks", "08. Callback Hooks"),
    ]

    results = []
    for module_name, display_name in examples:
        success = run_example(module_name, display_name)
        results.append((display_name, success))
        if success:
            time.sleep(1)  # Brief pause between examples

    # Print summary
    print("\n" + "=" * 80)
    print("Summary")
    print("=" * 80)

    passed = sum(1 for _, success in results if success)
    total = len(results)

    for name, success in results:
        status = "✓ PASSED" if success else "✗ FAILED"
        print(f"{status}: {name}")

    print(f"\nTotal: {passed}/{total} examples passed")

    if passed == total:
        print("\n🎉 All examples ran successfully!")
        return 0
    else:
        print(f"\n⚠️  {total - passed} example(s) failed")
        return 1


if __name__ == "__main__":
    sys.exit(main())
