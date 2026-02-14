"""Example: Tool provisioning and usage in ARL sandboxes.

Demonstrates:
1. Creating a WarmPool with inline tools
2. Discovering available tools via registry
3. Calling tools with JSON parameters

Prerequisites:
    - ARL operator + gateway deployed
    - Gateway port-forwarded to localhost:8080

Usage:
    kubectl port-forward -n arl svc/arl-operator-gateway 8080:8080
    uv run python examples/python/test_tools.py
"""

from arl import SandboxSession, WarmPoolManager
from arl.types import InlineToolSpec, ToolsSpec

GATEWAY_URL = "http://localhost:8080"
POOL_NAME = "tools-demo"
NAMESPACE = "arl"


def main() -> None:
    pool_mgr = WarmPoolManager(namespace=NAMESPACE, gateway_url=GATEWAY_URL)

    # 1. Create pool with inline tools
    # Tool script uses only POSIX sh — no jq/python dependency
    tools = ToolsSpec(
        inline=[
            InlineToolSpec(
                name="greet",
                description="Return a greeting message",
                parameters={"type": "object", "properties": {"name": {"type": "string"}}},
                runtime="bash",
                entrypoint="run.sh",
                timeout="10s",
                files={
                    "run.sh": (
                        "#!/bin/sh\n"
                        "read input\n"
                        '# Simple extraction without jq: get value of "name" key\n'
                        'name=$(echo "$input" | sed -n \'s/.*"name"[[:space:]]*:[[:space:]]*"\\([^"]*\\)".*/\\1/p\')\n'
                        '[ -z "$name" ] && name="world"\n'
                        'printf \'{"message": "hello %s"}\\n\' "$name"\n'
                    ),
                },
            ),
        ],
    )

    pool_mgr.create_warmpool(
        name=POOL_NAME,
        image="pair-diag-cn-guangzhou.cr.volces.com/pair/ubuntu:22.04",
        replicas=1,
        tools=tools,
    )
    print("Waiting for pool to be ready...")
    pool_mgr.wait_for_ready(POOL_NAME)
    print(f"Pool '{POOL_NAME}' is ready")

    # 2. Use session to discover and call tools
    with SandboxSession(
        pool_ref=POOL_NAME, namespace=NAMESPACE, gateway_url=GATEWAY_URL
    ) as session:
        # Discover tools
        registry = session.list_tools()
        print(f"Available tools: {[t.name for t in registry.tools]}")

        for tool in registry.tools:
            print(f"  - {tool.name}: {tool.description} (runtime={tool.runtime})")

        # Call a tool
        result = session.call_tool("greet", {"name": "ARL"})
        print(f"Tool result: {result.parsed}")
        print(f"Exit code: {result.exit_code}")
        if result.stderr:
            print(f"Stderr: {result.stderr}")

    # Cleanup
    pool_mgr.delete_warmpool(POOL_NAME)
    print("Cleaned up")


if __name__ == "__main__":
    main()
