"""Batch task execution example.

This example demonstrates running multiple tasks in parallel
using multiple sandboxes.
"""

from concurrent.futures import ThreadPoolExecutor, as_completed
from arl_client.session import SandboxSession


def execute_task(task_id: int, pool_ref: str) -> dict:
    """Execute a single task in its own sandbox."""
    with SandboxSession(
        pool_ref=pool_ref,
        namespace="default"
    ) as session:
        result = session.execute([
            {
                "name": "write_file",
                "type": "FilePatch",
                "path": f"/workspace/task_{task_id}.py",
                "content": f"print('Task {task_id} completed')"
            },
            {
                "name": "run",
                "type": "Command",
                "command": ["python", f"/workspace/task_{task_id}.py"]
            }
        ])
        
        status = result.get("status", {})
        return {
            "task_id": task_id,
            "state": status.get("state"),
            "stdout": status.get("stdout"),
            "duration": status.get("duration")
        }


def main():
    """Run batch tasks in parallel."""
    pool_ref = "python-3.9-std"
    num_tasks = 5
    
    print(f"Executing {num_tasks} tasks in parallel...")
    
    with ThreadPoolExecutor(max_workers=3) as executor:
        # Submit all tasks
        futures = {
            executor.submit(execute_task, i, pool_ref): i 
            for i in range(num_tasks)
        }
        
        # Collect results as they complete
        for future in as_completed(futures):
            task_id = futures[future]
            try:
                result = future.result()
                print(f"\nTask {result['task_id']}:")
                print(f"  State: {result['state']}")
                print(f"  Output: {result['stdout']}")
                print(f"  Duration: {result['duration']}")
            except Exception as e:
                print(f"Task {task_id} failed: {e}")


if __name__ == "__main__":
    main()
