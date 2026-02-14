"""ARL Interactive Shell — connect to a remote sandbox terminal.

Usage:
    uv run python examples/python/test_interactive_shell.py
    uv run python examples/python/test_interactive_shell.py --gateway-url http://localhost:8080
    uv run python examples/python/test_interactive_shell.py --pool my-pool --image python:3.11
    uv run python examples/python/test_interactive_shell.py --keep-alive

Environment variables (override defaults):
    ARL_GATEWAY_URL   Gateway base URL
    ARL_POOL_NAME     WarmPool name
    ARL_NAMESPACE     Kubernetes namespace
    ARL_POOL_IMAGE    Container image for auto-created pool
"""

from __future__ import annotations

import argparse
import contextlib
import os
import shutil
import signal
import sys
import threading

from arl import (
    GatewayClient,
    GatewayError,
    InteractiveShellClient,
    PoolNotReadyError,
    SandboxSession,
    WarmPoolManager,
)
from arl.types import SessionInfo
from rich.console import Console
from rich.panel import Panel
from rich.progress import Progress, SpinnerColumn, TextColumn
from rich.table import Table

# ---------------------------------------------------------------------------
# Defaults (overridable via env vars or CLI flags)
# ---------------------------------------------------------------------------
DEFAULT_GATEWAY_URL = "http://localhost:8080"
DEFAULT_POOL_NAME = "test-pool"
DEFAULT_NAMESPACE = "arl"
DEFAULT_POOL_IMAGE = "pair-diag-cn-guangzhou.cr.volces.com/pair/ubuntu:22.04"

console = Console(stderr=True)


# ---------------------------------------------------------------------------
# InteractiveTerminal — full-duplex shell over WebSocket
# ---------------------------------------------------------------------------
class InteractiveTerminal:
    """Full-duplex interactive terminal backed by an ARL sandbox shell.

    Reads output in a background thread and forwards stdin line-by-line.
    Supports Ctrl-C (SIGINT) and Ctrl-D (exit), and sends terminal resize
    events so that the remote PTY stays in sync.
    """

    # ANSI escape: bold green prompt, then reset
    PROMPT = "\033[1;32m$ \033[0m"

    def __init__(self, client: InteractiveShellClient) -> None:
        self._client = client
        self._running = True
        self._reader_thread: threading.Thread | None = None
        self._exit_code: int | None = None
        self._need_prompt = True

    # -- output reader (background) ----------------------------------------

    def _read_loop(self) -> None:
        """Continuously read WebSocket messages and print output."""
        while self._running:
            try:
                msg = self._client.read_message(timeout=0.5)
                if msg is None:
                    continue

                if msg.type == "output" and msg.data:
                    self._need_prompt = True
                    sys.stdout.write(msg.data)
                    sys.stdout.flush()
                elif msg.type == "exit":
                    self._exit_code = msg.exit_code
                    console.print(f"\n[dim]Shell exited with code {msg.exit_code}[/dim]")
                    self._running = False
                elif msg.type == "error":
                    console.print(f"\n[red]Error: {msg.data}[/red]")
            except Exception as e:
                if self._running:
                    console.print(f"\n[red]WebSocket error: {e}[/red]")
                    self._running = False
                break

    # -- resize ------------------------------------------------------------

    def _send_resize(self) -> None:
        """Send current terminal dimensions to the remote PTY."""
        size = shutil.get_terminal_size(fallback=(80, 24))
        with contextlib.suppress(Exception):
            self._client.send_resize(cols=size.columns, rows=size.lines)

    def _install_sigwinch(self) -> None:
        """Install SIGWINCH handler to track terminal resize (Unix only)."""
        if sys.platform == "win32":
            return
        with contextlib.suppress(OSError, ValueError):
            signal.signal(signal.SIGWINCH, lambda *_: self._send_resize())

    # -- main loop ---------------------------------------------------------

    def run(self) -> int:
        """Block until the user exits or the remote shell terminates.

        Returns:
            Remote shell exit code (0 if unknown).
        """
        self._reader_thread = threading.Thread(target=self._read_loop, daemon=True)
        self._reader_thread.start()
        self._install_sigwinch()
        self._send_resize()

        try:
            while self._running:
                try:
                    if self._need_prompt:
                        sys.stdout.write(self.PROMPT)
                        sys.stdout.flush()
                        self._need_prompt = False
                    line = input()
                    if not self._running:
                        break
                    self._client.send_input(line + "\n")
                except EOFError:
                    console.print("\n[dim]Ctrl-D detected, exiting...[/dim]")
                    self._client.send_input("exit\n")
                    break
                except KeyboardInterrupt:
                    sys.stdout.write("^C\n")
                    sys.stdout.flush()
                    try:
                        self._client.send_signal("SIGINT")
                    except Exception:
                        self._running = False
        finally:
            self._running = False
            self._client.close()

        return self._exit_code if self._exit_code is not None else 0

    def close(self) -> None:
        self._running = False
        self._client.close()


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _check_gateway(client: GatewayClient) -> bool:
    """Return True if gateway is healthy, print guidance otherwise."""
    if client.health():
        return True
    console.print(
        Panel(
            "[red bold]Gateway unreachable[/red bold]\n\n"
            "Possible fixes:\n"
            "  1. Verify the gateway pod is running\n"
            "  2. Start a port-forward:\n"
            "     [cyan]kubectl port-forward -n arl "
            "svc/arl-operator-gateway 8080:8080[/cyan]",
            title="Connection Error",
            border_style="red",
        )
    )
    return False


def _ensure_pool(
    pool_mgr: WarmPoolManager,
    name: str,
    image: str,
    replicas: int,
) -> bool:
    """Create the pool if it doesn't exist and wait until ready."""
    try:
        pool_mgr.create_warmpool(name=name, image=image, replicas=replicas)
        console.print(f"  [green]\u2713[/green] Pool [cyan]{name}[/cyan] created")
    except GatewayError as e:
        if "already exists" in str(e):
            console.print(f"  [dim]Pool [cyan]{name}[/cyan] already exists[/dim]")
        else:
            console.print(f"  [red]\u2717 {e}[/red]")
            return False

    with Progress(
        SpinnerColumn(),
        TextColumn("[progress.description]{task.description}"),
        console=console,
    ) as progress:
        progress.add_task("Waiting for pool to be ready...", total=None)
        try:
            info = pool_mgr.wait_for_ready(name, timeout=300.0, poll_interval=5.0)
            console.print(
                f"  [green]\u2713[/green] Pool ready: "
                f"replicas={info.replicas}  "
                f"ready={info.ready_replicas}  "
                f"allocated={info.allocated_replicas}"
            )
            return True
        except PoolNotReadyError as e:
            console.print(f"  [red]\u2717 Pool has failing pods: {e}[/red]")
            return False
        except TimeoutError as e:
            console.print(f"  [red]\u2717 Timeout waiting for pool: {e}[/red]")
            return False


def _print_session_banner(info: SessionInfo) -> None:
    """Print a rich panel with session details."""
    table = Table(show_header=False, box=None, padding=(0, 2))
    table.add_column(style="bold")
    table.add_column()
    table.add_row("Session", info.id)
    table.add_row("Sandbox", info.sandbox_name)
    table.add_row("Pod", info.pod_name or "\u2014")
    table.add_row("Pod IP", info.pod_ip or "\u2014")
    table.add_row("Namespace", info.namespace)
    table.add_row("Pool", info.pool_ref)
    if info.created_at:
        table.add_row("Created", info.created_at.strftime("%Y-%m-%d %H:%M:%S"))

    console.print(
        Panel(
            table,
            title="[bold cyan]ARL Interactive Shell[/bold cyan]",
            subtitle="[dim]Ctrl-C \u2192 SIGINT  |  Ctrl-D \u2192 exit[/dim]",
            border_style="cyan",
        )
    )
    console.print()


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------


def _build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Connect to a remote ARL sandbox via interactive shell.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=(
            "Environment variables:\n"
            "  ARL_GATEWAY_URL   Gateway base URL\n"
            "  ARL_POOL_NAME     WarmPool name\n"
            "  ARL_NAMESPACE     Kubernetes namespace\n"
            "  ARL_POOL_IMAGE    Container image for auto-created pool\n"
        ),
    )
    parser.add_argument(
        "--gateway-url",
        default=os.environ.get("ARL_GATEWAY_URL", DEFAULT_GATEWAY_URL),
        help=f"Gateway URL (default: {DEFAULT_GATEWAY_URL})",
    )
    parser.add_argument(
        "--pool",
        default=os.environ.get("ARL_POOL_NAME", DEFAULT_POOL_NAME),
        help=f"WarmPool name (default: {DEFAULT_POOL_NAME})",
    )
    parser.add_argument(
        "--namespace",
        default=os.environ.get("ARL_NAMESPACE", DEFAULT_NAMESPACE),
        help=f"Kubernetes namespace (default: {DEFAULT_NAMESPACE})",
    )
    parser.add_argument(
        "--image",
        default=os.environ.get("ARL_POOL_IMAGE", DEFAULT_POOL_IMAGE),
        help="Container image for auto-created pool",
    )
    parser.add_argument(
        "--replicas",
        type=int,
        default=1,
        help="Number of warm replicas (default: 1)",
    )
    parser.add_argument(
        "--keep-alive",
        action="store_true",
        help="Keep sandbox alive after disconnecting",
    )
    return parser


def main() -> None:
    args = _build_parser().parse_args()

    gateway_url: str = args.gateway_url
    pool_name: str = args.pool
    namespace: str = args.namespace

    # 1. Gateway health check
    client = GatewayClient(base_url=gateway_url)
    if not _check_gateway(client):
        sys.exit(1)

    # 2. Ensure warm pool is ready
    pool_mgr = WarmPoolManager(namespace=namespace, gateway_url=gateway_url)
    if not _ensure_pool(pool_mgr, pool_name, args.image, args.replicas):
        sys.exit(1)

    # 3. Create sandbox & connect shell
    console.print()
    with Progress(
        SpinnerColumn(),
        TextColumn("[progress.description]{task.description}"),
        console=console,
    ) as progress:
        progress.add_task("Creating sandbox session...", total=None)

    with SandboxSession(
        pool_ref=pool_name,
        namespace=namespace,
        gateway_url=gateway_url,
        keep_alive=args.keep_alive,
    ) as session:
        sid = session.session_id
        if sid is None:
            console.print("[red]Failed to create session[/red]")
            sys.exit(1)

        info = session.session_info
        if info:
            _print_session_banner(info)

        shell_client = InteractiveShellClient(gateway_url=gateway_url)
        try:
            shell_client.connect(sid)
        except ImportError:
            console.print(
                "[red]websockets package required.[/red]\n"
                "Install with: [cyan]uv add 'arl-env[shell]'[/cyan]"
            )
            sys.exit(1)
        except Exception as e:
            console.print(f"[red]Failed to connect shell: {e}[/red]")
            sys.exit(1)

        terminal = InteractiveTerminal(shell_client)
        try:
            exit_code = terminal.run()
        except KeyboardInterrupt:
            console.print("\n[dim]Interrupted[/dim]")
            exit_code = 130
        finally:
            terminal.close()

    console.print("[dim]Session closed.[/dim]")
    sys.exit(exit_code)


if __name__ == "__main__":
    main()
