from __future__ import annotations

import json
import sys
import threading

from arl import (
    GatewayClient,
    GatewayError,
    InteractiveShellClient,
    SandboxSession,
    WarmPoolManager,
)

GATEWAY_URL = "http://14.103.184.145:8080"
POOL_NAME = "test-pool"
NAMESPACE = "arl"
DEFAULT_POOL_IMAGE = "pair-diag-cn-guangzhou.cr.volces.com/pair/ubuntu:22.04"


class InteractiveTerminal:
    def __init__(self, session_id: str, gateway_url: str) -> None:
        self._client = InteractiveShellClient(gateway_url=gateway_url)
        self._client.connect(session_id)
        self._running = True
        self._reader_thread: threading.Thread | None = None

    def _output_reader(self) -> None:
        """Background thread that continuously reads and prints output."""
        while self._running:
            try:
                # Read raw WebSocket message to handle all message types
                if self._client._ws is None:  # type: ignore[attr-defined]
                    break

                raw = self._client._ws.recv(timeout=0.5)  # type: ignore[attr-defined,union-attr]
                if isinstance(raw, bytes):
                    raw = raw.decode()
                msg = json.loads(raw)

                if msg["type"] == "output":
                    data = msg.get("data", "")
                    if data:
                        print(data, end="", flush=True)
                elif msg["type"] == "exit":
                    exit_code = msg.get("exit_code", 0)
                    print(f"\n[Shell exited with code {exit_code}]")
                    self._running = False
                elif msg["type"] == "error":
                    error = msg.get("data", "unknown error")
                    print(f"\n[Error: {error}]", file=sys.stderr)

            except TimeoutError:
                continue
            except Exception as e:
                if self._running:
                    print(f"\n[WebSocket error: {e}]", file=sys.stderr)
                    self._running = False
                break

    def start(self) -> None:
        """Start the output reader thread and enter interactive input loop."""
        self._reader_thread = threading.Thread(target=self._output_reader, daemon=True)
        self._reader_thread.start()

        print("Connected to shell. Type commands (Ctrl-C = SIGINT, Ctrl-D = exit)\n")

        try:
            while self._running:
                try:
                    line = input()
                    if not self._running:
                        break
                    self._client.send_input(line + "\n")
                except EOFError:
                    # Ctrl-D pressed
                    print("\n[Exiting...]")
                    self._client.send_input("exit\n")
                    break
                except KeyboardInterrupt:
                    # Ctrl-C pressed
                    print("^C")
                    self._send_signal("SIGINT")
        finally:
            self._running = False
            self._client.close()

    def _send_signal(self, sig: str = "SIGINT") -> None:
        """Send a signal to the shell process."""
        try:
            if self._client._ws is not None:  # type: ignore[attr-defined]
                msg = json.dumps({"type": "signal", "signal": sig})
                self._client._ws.send(msg)  # type: ignore[attr-defined,union-attr]
        except Exception as e:
            print(f"\n[Failed to send signal: {e}]", file=sys.stderr)
            self._running = False

    def close(self) -> None:
        """Close the shell connection."""
        self._running = False
        self._client.close()


def main() -> None:
    print("ARL Interactive Shell Terminal")
    print(f"Gateway: {GATEWAY_URL}  Pool: {POOL_NAME}  Namespace: {NAMESPACE}\n")

    # Check gateway health
    client = GatewayClient(base_url=GATEWAY_URL)
    if not client.health():
        print("Gateway not reachable. Please check:")
        print("  1. Gateway is deployed")
        print(
            "  2. Port-forward is running: "
            "kubectl port-forward -n arl svc/arl-operator-gateway 8080:8080"
        )
        sys.exit(1)

    pool_mgr = WarmPoolManager(namespace=NAMESPACE, gateway_url=GATEWAY_URL)
    try:
        pool_mgr.create_warmpool(name=POOL_NAME, image=DEFAULT_POOL_IMAGE, replicas=1)
    except GatewayError as e:
        print(e)
    print("Creating sandbox session...")
    with SandboxSession(
        pool_ref=POOL_NAME, namespace=NAMESPACE, gateway_url=GATEWAY_URL
    ) as session:
        sid = session.session_id
        assert sid is not None
        info = session.session_info
        pod_name = info.pod_name if info else "unknown"  # type: ignore[union-attr]
        print(f"Session: {sid}")
        print(f"Pod: {pod_name}\n")

        shell = InteractiveTerminal(sid, gateway_url=GATEWAY_URL)
        try:
            shell.start()
        except KeyboardInterrupt:
            print("\n[Interrupted]")
        finally:
            shell.close()

    print("\nSession closed.")


if __name__ == "__main__":
    main()
