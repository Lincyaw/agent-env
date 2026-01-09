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
Verify a Kubernetes deployment is healthy and ready.
Checks deployment status, pod readiness, and container health.
"""

import json
import subprocess
import sys
import time


def run_kubectl(args: list[str]) -> str:
    """Run kubectl command and return output."""
    try:
        result = subprocess.run(["kubectl"] + args, capture_output=True, text=True, check=True)
        return result.stdout
    except subprocess.CalledProcessError as e:
        print(f"Error running kubectl: {e.stderr}", file=sys.stderr)
        raise


def get_deployment_status(name: str, namespace: str = "default") -> dict:
    """Get deployment status as JSON."""
    output = run_kubectl(["get", "deployment", name, "-n", namespace, "-o", "json"])
    return json.loads(output)


def get_pods_for_deployment(name: str, namespace: str = "default") -> list[dict]:
    """Get all pods belonging to a deployment."""
    output = run_kubectl(["get", "pods", "-n", namespace, "-l", f"app={name}", "-o", "json"])
    data = json.loads(output)
    return data.get("items", [])


def verify_deployment(name: str, namespace: str = "default", timeout: int = 300) -> bool:
    """
    Verify deployment is healthy and all pods are ready.

    Args:
        name: Deployment name
        namespace: Kubernetes namespace
        timeout: Maximum time to wait in seconds

    Returns:
        True if deployment is healthy, False otherwise
    """
    print(f"🔍 Verifying deployment '{name}' in namespace '{namespace}'...")

    start_time = time.time()

    while time.time() - start_time < timeout:
        try:
            # Get deployment status
            deployment = get_deployment_status(name, namespace)
            status = deployment.get("status", {})

            desired = status.get("replicas", 0)
            ready = status.get("readyReplicas", 0)
            updated = status.get("updatedReplicas", 0)
            available = status.get("availableReplicas", 0)

            print(
                f"  Replicas - Desired: {desired}, Ready: {ready}, Updated: {updated}, Available: {available}"
            )

            # Check if deployment is ready
            if desired > 0 and ready == desired and updated == desired and available == desired:
                # Verify individual pods
                pods = get_pods_for_deployment(name, namespace)
                all_healthy = True

                for pod in pods:
                    pod_name = pod["metadata"]["name"]
                    phase = pod["status"].get("phase", "Unknown")

                    container_statuses = pod["status"].get("containerStatuses", [])
                    containers_ready = all(cs.get("ready", False) for cs in container_statuses)

                    if phase != "Running" or not containers_ready:
                        print(f"  ⚠️  Pod {pod_name}: phase={phase}, ready={containers_ready}")
                        all_healthy = False
                    else:
                        print(f"  ✅ Pod {pod_name}: healthy")

                if all_healthy:
                    print(f"✅ Deployment '{name}' is healthy and all pods are ready!")
                    return True

            time.sleep(5)

        except Exception as e:
            print(f"Error during verification: {e}", file=sys.stderr)
            time.sleep(5)

    print(f"❌ Timeout waiting for deployment '{name}' to be ready", file=sys.stderr)
    return False


if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("Usage: verify_deployment.py <deployment-name> [namespace] [timeout]")
        sys.exit(1)

    deployment_name = sys.argv[1]
    namespace = sys.argv[2] if len(sys.argv) > 2 else "default"
    timeout = int(sys.argv[3]) if len(sys.argv) > 3 else 300

    success = verify_deployment(deployment_name, namespace, timeout)
    sys.exit(0 if success else 1)
