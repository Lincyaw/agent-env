"""WarmPool management for ARL via Gateway API."""

from __future__ import annotations

import time

from arl.gateway_client import GatewayClient, PoolNotReadyError
from arl.types import PoolInfo, ResourceRequirements, ToolsSpec


class WarmPoolManager:
    """Manager for creating and managing WarmPools via the Gateway API.

    Examples:
        >>> manager = WarmPoolManager()
        >>> manager.create_warmpool(
        ...     name="python-39",
        ...     image="python:3.9-slim",
        ...     replicas=2,
        ... )
        >>> info = manager.wait_for_ready("python-39")
        >>> print(f"Ready: {info.ready_replicas}/{info.replicas}")
    """

    def __init__(
        self,
        namespace: str = "default",
        gateway_url: str = "http://localhost:8080",
        timeout: float = 300.0,
    ) -> None:
        self.namespace = namespace
        self._client = GatewayClient(base_url=gateway_url, timeout=timeout)

    def create_warmpool(
        self,
        name: str,
        image: str,
        replicas: int = 2,
        tools: ToolsSpec | None = None,
        resources: ResourceRequirements | None = None,
        workspace_dir: str = "/workspace",
    ) -> None:
        """Create a new WarmPool.

        Args:
            name: Name of the WarmPool.
            image: Container image for the executor.
            replicas: Number of warm pods to maintain.
            tools: Optional tools specification to provision in the executor container.
            resources: Optional resource requirements (CPU/memory requests and limits).
                      If not specified, uses defaults: requests={cpu: 100m, memory: 128Mi},
                      limits={cpu: 1000m, memory: 1Gi}.
            workspace_dir: Workspace directory mount path (default: /workspace).
        """
        self._client.create_pool(
            name=name,
            namespace=self.namespace,
            image=image,
            replicas=replicas,
            tools=tools,
            resources=resources,
            workspace_dir=workspace_dir,
        )

    def get_warmpool(self, name: str) -> PoolInfo:
        """Get WarmPool info.

        Args:
            name: Name of the WarmPool.

        Returns:
            PoolInfo with current pool status.
        """
        return self._client.get_pool(name, namespace=self.namespace)

    def wait_for_ready(
        self,
        name: str,
        timeout: float = 300.0,
        poll_interval: float = 5.0,
    ) -> PoolInfo:
        """Wait for a WarmPool to have ready replicas.

        Polls the pool status and returns when ready replicas are available.
        Raises PoolNotReadyError immediately if pods are failing (e.g.,
        ImagePullBackOff, CrashLoopBackOff) instead of waiting until timeout.

        Args:
            name: Name of the WarmPool.
            timeout: Maximum time to wait in seconds.
            poll_interval: Time between polls in seconds.

        Returns:
            PoolInfo when pool has ready replicas.

        Raises:
            PoolNotReadyError: If pods are failing with no ready replicas.
            TimeoutError: If timeout is exceeded without pool becoming ready.
        """
        deadline = time.monotonic() + timeout
        last_info: PoolInfo | None = None
        consecutive_failures = 0

        while time.monotonic() < deadline:
            info = self.get_warmpool(name)
            last_info = info

            if info.ready_replicas > 0:
                return info

            # Check for failing pods
            for cond in info.conditions:
                if cond.type == "PodsFailing" and cond.status == "True":
                    consecutive_failures += 1
                    # Fail fast after 2 consecutive checks with failures and no ready pods
                    # (gives the system a brief chance to recover)
                    if consecutive_failures >= 2:
                        raise PoolNotReadyError(
                            pool_name=name,
                            message=cond.message or cond.reason,
                            conditions=info.conditions,
                        )
                    break
            else:
                consecutive_failures = 0

            time.sleep(poll_interval)

        # Timeout reached
        diag = ""
        if last_info:
            diag = (
                f"replicas={last_info.replicas} "
                f"ready={last_info.ready_replicas} "
                f"allocated={last_info.allocated_replicas}"
            )
            for cond in last_info.conditions:
                if cond.status == "True" or (cond.type == "Ready" and cond.status == "False"):
                    diag += f" [{cond.type}: {cond.message}]"

        raise TimeoutError(f"Pool '{name}' not ready after {timeout}s: {diag}")

    def delete_warmpool(self, name: str) -> None:
        """Delete a WarmPool.

        Args:
            name: Name of the WarmPool to delete.
        """
        self._client.delete_pool(name, namespace=self.namespace)
