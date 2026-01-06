"""Direct sidecar client example.

Demonstrates:
- Using SidecarClient directly (without SandboxSession)
- File operations via gRPC
- Command execution
- Workspace reset
"""

from arl import SidecarClient


def main() -> None:
    """Demonstrate direct sidecar client usage."""
    print("=" * 60)
    print("Example: Direct Sidecar Client (gRPC)")
    print("=" * 60)

    # NOTE: Replace with actual pod IP when running
    # You can get the pod IP from the Sandbox status:
    #   kubectl get sandbox <name> -o jsonpath='{.status.podIP}'
    pod_ip = "10.0.0.1"  # Example - replace with actual pod IP
    grpc_port = 9090

    print(f"\n[Info] Connecting to sidecar at {pod_ip}:{grpc_port}")
    print("[Info] (In real usage, get pod IP from Sandbox status)")

    # This example shows the API but won't run without a real pod
    print("\n--- SidecarClient API demonstration ---")

    print("""
# Connect to sidecar directly
with SidecarClient(f"{pod_ip}:{grpc_port}") as client:
    
    # 1. Update files in workspace
    response = client.update_files(
        base_path="/workspace",
        files={
            "hello.py": "print('Hello, World!')",
            "config.txt": "DEBUG=true",
        }
    )
    print(f"Files updated: {response.success}")
    
    # 2. Execute command (aggregated result)
    result = client.execute(
        command=["python3", "/workspace/hello.py"],
        working_dir="/workspace",
    )
    print(f"Output: {result.stdout}")
    print(f"Exit code: {result.exit_code}")
    
    # 3. Execute with streaming
    for log in client.execute_stream(["ls", "-la"]):
        print(log.stdout, end="")
        if log.done:
            print(f"Exit: {log.exit_code}")
    
    # 4. Send signal to process
    response = client.signal_process(pid=1234, signal="SIGTERM")
    print(f"Signal sent: {response.success}")
    
    # 5. Reset workspace
    response = client.reset(preserve_files=False)
    print(f"Reset: {response.success}")
    
    # 6. Interactive shell
    with client.interactive_shell() as shell:
        shell.send_data("echo hello\\n")
        for output in shell.read_output():
            print(output.data, end="")
            if output.closed:
                break
""")

    print("\n✓ See sdk/python/arl/README.md for complete API documentation")


if __name__ == "__main__":
    main()
