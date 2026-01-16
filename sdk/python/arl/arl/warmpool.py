"""WarmPool management for ARL."""

import time
from typing import Any, cast

from kubernetes import client, config


class WarmPoolManager:
    """Manager for creating and managing WarmPools.

    Provides high-level API for WarmPool lifecycle management without
    requiring users to understand Kubernetes YAML configurations.

    Examples:
        Create a WarmPool with default Python image:

        >>> manager = WarmPoolManager(namespace="default")
        >>> manager.create_warmpool(
        ...     name="python-39-std",
        ...     image="python:3.9-slim",
        ...     replicas=2
        ... )

        Create a WarmPool for SWE-bench:

        >>> manager = WarmPoolManager(namespace="default")
        >>> manager.create_warmpool(
        ...     name="swebench-emotion",
        ...     image="swebench/swesmith.x86_64.emotion_1776_js-emotion.b882bcba",
        ...     replicas=2,
        ...     sidecar_image="10.10.10.240/library/arl-sidecar:latest"
        ... )
    """

    def __init__(
        self,
        namespace: str = "default",
        timeout: int = 300,
    ) -> None:
        """Initialize WarmPool manager.

        Args:
            namespace: Kubernetes namespace (default: "default")
            timeout: Maximum seconds to wait for operations (default: 300)
        """
        self.namespace = namespace
        self.timeout = timeout
        self._custom_api: client.CustomObjectsApi | None = None

    @property
    def custom_api(self) -> client.CustomObjectsApi:
        """Get Kubernetes custom objects API client (lazy initialization)."""
        if self._custom_api is None:
            try:
                config.load_incluster_config()
            except config.ConfigException:
                config.load_kube_config()
            self._custom_api = client.CustomObjectsApi()
        return self._custom_api

    def create_warmpool(
        self,
        name: str,
        image: str,
        sidecar_image: str,
        replicas: int = 2,
        command: list[str] | None = None,
        workspace_path: str = "/workspace",
        testbed_path: str | None = None,
    ) -> dict[str, Any]:
        """Create a new WarmPool.

        Args:
            name: Name of the WarmPool
            image: Container image for the executor (e.g., "python:3.9-slim")
            replicas: Number of warm pods to maintain (default: 2)
            sidecar_image: Sidecar agent image (default: ARL sidecar)
            command: Command to run in executor container (default: ["sh", "-c", "sleep infinity"])
            workspace_path: Path to mount workspace volume (default: "/workspace")
            testbed_path: Optional path to mount testbed volume (for SWE-bench scenarios)

        Returns:
            WarmPool resource dictionary

        Raises:
            RuntimeError: If WarmPool creation fails

        Examples:
            Create a basic Python pool:

            >>> manager = WarmPoolManager()
            >>> manager.create_warmpool("python-39", "python:3.9-slim")

            Create a SWE-bench pool with testbed:

            >>> manager = WarmPoolManager()
            >>> manager.create_warmpool(
            ...     name="swebench-emotion",
            ...     image="swebench/swesmith.x86_64.emotion_1776_js-emotion.b882bcba",
            ...     testbed_path="/testbed"
            ... )
        """
        if command is None:
            command = ["/bin/sh", "-c", "sleep infinity"]

        # Build volumes configuration
        volumes: list[dict[str, Any]] = [
            {"name": "workspace", "emptyDir": {}},
        ]

        # Build volume mounts for executor
        executor_volume_mounts: list[dict[str, Any]] = [
            {"name": "workspace", "mountPath": workspace_path},
        ]

        # Build volume mounts for sidecar
        sidecar_volume_mounts: list[dict[str, Any]] = [
            {"name": "workspace", "mountPath": workspace_path},
        ]

        # Add testbed volume if specified
        if testbed_path:
            volumes.append({"name": "testbed", "emptyDir": {}})
            executor_volume_mounts.append({"name": "testbed", "mountPath": testbed_path})
            sidecar_volume_mounts.append({"name": "testbed", "mountPath": testbed_path})

        # Build WarmPool specification
        warmpool_body: dict[str, Any] = {
            "apiVersion": "arl.infra.io/v1alpha1",
            "kind": "WarmPool",
            "metadata": {
                "name": name,
                "namespace": self.namespace,
            },
            "spec": {
                "replicas": replicas,
                "template": {
                    "spec": {
                        "containers": [
                            {
                                "name": "executor",
                                "image": image,
                                "command": command,
                                "volumeMounts": executor_volume_mounts,
                            },
                            {
                                "name": "sidecar",
                                "image": sidecar_image,
                                "imagePullPolicy": "Always",
                                "ports": [
                                    {
                                        "name": "http",
                                        "containerPort": 8080,
                                        "protocol": "TCP",
                                    },
                                    {
                                        "name": "grpc",
                                        "containerPort": 9090,
                                        "protocol": "TCP",
                                    },
                                ],
                                "volumeMounts": sidecar_volume_mounts,
                            },
                        ],
                        "volumes": volumes,
                    }
                },
            },
        }

        # Create WarmPool resource
        warmpool = self.custom_api.create_namespaced_custom_object(
            group="arl.infra.io",
            version="v1alpha1",
            namespace=self.namespace,
            plural="warmpools",
            body=warmpool_body,
        )

        return cast(dict[str, Any], warmpool)

    def get_warmpool(self, name: str) -> dict[str, Any]:
        """Get WarmPool status.

        Args:
            name: Name of the WarmPool

        Returns:
            WarmPool resource dictionary

        Raises:
            client.ApiException: If WarmPool not found
        """
        warmpool_obj: object = self.custom_api.get_namespaced_custom_object(
            group="arl.infra.io",
            version="v1alpha1",
            namespace=self.namespace,
            plural="warmpools",
            name=name,
        )
        return cast(dict[str, Any], warmpool_obj)

    def delete_warmpool(self, name: str) -> None:
        """Delete a WarmPool.

        Args:
            name: Name of the WarmPool to delete

        Raises:
            client.ApiException: If deletion fails
        """
        self.custom_api.delete_namespaced_custom_object(
            group="arl.infra.io",
            version="v1alpha1",
            namespace=self.namespace,
            plural="warmpools",
            name=name,
        )

    def wait_for_warmpool_ready(self, name: str, poll_interval: float = 2.0) -> None:
        """Wait for WarmPool to have ready replicas.

        Args:
            name: Name of the WarmPool
            poll_interval: Seconds between status checks (default: 2.0)

        Raises:
            RuntimeError: If WarmPool doesn't become ready within timeout
        """
        start_time: float = time.time()
        last_ready_count: int = -1

        print(f"⏳ Waiting for WarmPool '{name}' to be ready...")

        while time.time() - start_time < self.timeout:
            try:
                warmpool = self.get_warmpool(name)
                status: dict[str, Any] = cast(dict[str, Any], warmpool.get("status", {}))
                ready_replicas: int = status.get("readyReplicas", 0)
                desired_replicas: int = warmpool.get("spec", {}).get("replicas", 0)

                # Show progress updates
                if ready_replicas != last_ready_count:
                    if ready_replicas > 0 or desired_replicas > 0:
                        print(f"   {ready_replicas}/{desired_replicas} pods ready")
                    last_ready_count = ready_replicas

                if ready_replicas >= desired_replicas and desired_replicas > 0:
                    print(f"✓ WarmPool '{name}' is ready with {ready_replicas} pods")
                    return

            except client.ApiException as e:
                if e.status == 404:
                    raise RuntimeError(
                        f"WarmPool '{name}' not found in namespace '{self.namespace}'"
                    ) from e
                if e.status != 404:
                    raise RuntimeError(f"Failed to get WarmPool status: {e.reason}") from e

            time.sleep(poll_interval)

        raise RuntimeError(
            f"WarmPool '{name}' not ready after {self.timeout}s. "
            f"Ready pods: {last_ready_count}. "
            f"Check 'kubectl describe warmpool {name} -n {self.namespace}' for status."
        )
