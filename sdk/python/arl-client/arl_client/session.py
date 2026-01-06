"""High-level wrapper for ARL Infrastructure SDK.

This module provides convenient APIs for working with ARL resources.
"""

from contextlib import contextmanager
from typing import Optional, List, Dict, Any
import time

from kubernetes import client, config
from kubernetes.client.rest import ApiException


class SandboxSession:
    """High-level API for managing Sandbox and Task execution.
    
    Provides context manager support and convenient methods for
    creating sandboxes and executing tasks.
    
    Example:
        >>> with SandboxSession("python-3.9-std", namespace="default") as session:
        ...     result = session.execute([
        ...         {"name": "write", "type": "FilePatch", "content": "print('Hello')"},
        ...         {"name": "run", "type": "Command", "command": ["python", "main.py"]}
        ...     ])
        ...     print(result.stdout)
    """
    
    def __init__(
        self,
        pool_ref: str,
        namespace: str = "default",
        keep_alive: bool = False,
        sandbox_name: Optional[str] = None,
        kubeconfig: Optional[str] = None
    ):
        """Initialize SandboxSession.
        
        Args:
            pool_ref: Name of the WarmPool to allocate from
            namespace: Kubernetes namespace (default: "default")
            keep_alive: Keep sandbox alive after tasks complete (default: False)
            sandbox_name: Optional name for sandbox (auto-generated if None)
            kubeconfig: Path to kubeconfig file (uses default if None)
        """
        self.pool_ref = pool_ref
        self.namespace = namespace
        self.keep_alive = keep_alive
        self.sandbox_name = sandbox_name or f"sandbox-{int(time.time())}"
        self.sandbox = None
        
        # Initialize Kubernetes client
        if kubeconfig:
            config.load_kube_config(config_file=kubeconfig)
        else:
            try:
                config.load_incluster_config()
            except config.ConfigException:
                config.load_kube_config()
        
        self.api = client.CustomObjectsApi()
        self.group = "arl.infra.io"
        self.version = "v1alpha1"
    
    def __enter__(self):
        """Create sandbox on context entry."""
        self.create_sandbox()
        return self
    
    def __exit__(self, exc_type, exc_val, exc_tb):
        """Delete sandbox on context exit if not keeping alive."""
        if not self.keep_alive:
            self.delete_sandbox()
        return False
    
    def create_sandbox(self):
        """Create a sandbox from the warm pool."""
        sandbox_spec = {
            "apiVersion": f"{self.group}/{self.version}",
            "kind": "Sandbox",
            "metadata": {
                "name": self.sandbox_name,
                "namespace": self.namespace
            },
            "spec": {
                "poolRef": self.pool_ref,
                "keepAlive": self.keep_alive
            }
        }
        
        try:
            self.sandbox = self.api.create_namespaced_custom_object(
                group=self.group,
                version=self.version,
                namespace=self.namespace,
                plural="sandboxes",
                body=sandbox_spec
            )
            
            # Wait for sandbox to be ready
            self._wait_for_sandbox_ready(timeout=60)
            
        except ApiException as e:
            raise RuntimeError(f"Failed to create sandbox: {e}")
    
    def delete_sandbox(self):
        """Delete the sandbox."""
        if not self.sandbox:
            return
        
        try:
            self.api.delete_namespaced_custom_object(
                group=self.group,
                version=self.version,
                namespace=self.namespace,
                plural="sandboxes",
                name=self.sandbox_name
            )
        except ApiException as e:
            if e.status != 404:
                raise RuntimeError(f"Failed to delete sandbox: {e}")
    
    def _wait_for_sandbox_ready(self, timeout: int = 60):
        """Wait for sandbox to reach Ready phase."""
        start_time = time.time()
        while time.time() - start_time < timeout:
            try:
                sandbox = self.api.get_namespaced_custom_object(
                    group=self.group,
                    version=self.version,
                    namespace=self.namespace,
                    plural="sandboxes",
                    name=self.sandbox_name
                )
                phase = sandbox.get("status", {}).get("phase", "")
                
                if phase == "Ready":
                    self.sandbox = sandbox
                    return
                elif phase == "Failed":
                    raise RuntimeError("Sandbox failed to initialize")
                
                time.sleep(1)
            except ApiException as e:
                raise RuntimeError(f"Failed to check sandbox status: {e}")
        
        raise TimeoutError(f"Sandbox did not become ready within {timeout}s")
    
    def execute(
        self,
        steps: List[Dict[str, Any]],
        timeout: str = "30s",
        wait: bool = True
    ) -> Optional[Dict[str, Any]]:
        """Execute a task in the sandbox.
        
        Args:
            steps: List of task steps (FilePatch or Command)
            timeout: Maximum execution time (e.g., "30s", "5m")
            wait: Wait for task completion (default: True)
        
        Returns:
            Task status if wait=True, None otherwise
            
        Example:
            >>> steps = [
            ...     {"name": "write", "type": "FilePatch", "path": "test.py", 
            ...      "content": "print('test')"},
            ...     {"name": "run", "type": "Command", "command": ["python", "test.py"]}
            ... ]
            >>> result = session.execute(steps)
            >>> print(result['status']['stdout'])
        """
        if not self.sandbox:
            raise RuntimeError("Sandbox not created. Use context manager or call create_sandbox()")
        
        task_name = f"task-{int(time.time())}"
        task_spec = {
            "apiVersion": f"{self.group}/{self.version}",
            "kind": "Task",
            "metadata": {
                "name": task_name,
                "namespace": self.namespace
            },
            "spec": {
                "sandboxRef": self.sandbox_name,
                "timeout": timeout,
                "steps": steps
            }
        }
        
        try:
            task = self.api.create_namespaced_custom_object(
                group=self.group,
                version=self.version,
                namespace=self.namespace,
                plural="tasks",
                body=task_spec
            )
            
            if wait:
                return self._wait_for_task_completion(task_name)
            return task
            
        except ApiException as e:
            raise RuntimeError(f"Failed to create task: {e}")
    
    def _wait_for_task_completion(self, task_name: str, timeout: int = 300) -> Dict[str, Any]:
        """Wait for task to complete and return result."""
        start_time = time.time()
        while time.time() - start_time < timeout:
            try:
                task = self.api.get_namespaced_custom_object(
                    group=self.group,
                    version=self.version,
                    namespace=self.namespace,
                    plural="tasks",
                    name=task_name
                )
                state = task.get("status", {}).get("state", "")
                
                if state in ["Succeeded", "Failed"]:
                    return task
                
                time.sleep(1)
            except ApiException as e:
                raise RuntimeError(f"Failed to check task status: {e}")
        
        raise TimeoutError(f"Task did not complete within {timeout}s")
    
    def get_status(self) -> Dict[str, Any]:
        """Get current sandbox status."""
        if not self.sandbox:
            raise RuntimeError("Sandbox not created")
        
        try:
            return self.api.get_namespaced_custom_object(
                group=self.group,
                version=self.version,
                namespace=self.namespace,
                plural="sandboxes",
                name=self.sandbox_name
            )
        except ApiException as e:
            raise RuntimeError(f"Failed to get sandbox status: {e}")
