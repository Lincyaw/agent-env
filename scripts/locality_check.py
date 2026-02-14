"""Diagnose image-locality scheduling distribution.

Queries the Kubernetes cluster for WarmPool pods and verifies that the
Rendezvous (HRW) hashing algorithm is correctly guiding pod placement.

For each WarmPool, shows:
- Preferred nodes (computed via HRW, same algorithm as the operator)
- Actual node placement of each pod
- Whether pods landed on preferred nodes

Usage:
    # Show all pools
    uv run --group prefetch python scripts/locality_check.py

    # Filter by pool name prefix
    uv run --group prefetch python scripts/locality_check.py --pool-prefix pandas

    # Specific namespace
    uv run --group prefetch python scripts/locality_check.py --namespace arl

    # Show detailed per-pod info
    uv run --group prefetch python scripts/locality_check.py --verbose
"""

from __future__ import annotations

import hashlib
import struct
from collections import Counter
from typing import Annotated

import typer
from kubernetes import client, config

app = typer.Typer(help="Diagnose image-locality scheduling distribution.")

POOL_LABEL = "arl.infra.io/pool"
STATUS_LABEL = "arl.infra.io/status"


# ---------- HRW hashing (mirrors pkg/scheduler/rendezvous.go) ----------


def hrw_score(image: str, node: str) -> int:
    """Compute HRW score for (image, node) pair. Mirrors Go implementation."""
    h = hashlib.sha256()
    h.update(image.encode())
    h.update(b"\x00")
    h.update(node.encode())
    return struct.unpack(">Q", h.digest()[:8])[0]


def compute_top_k(image: str, nodes: list[str], k: int) -> list[str]:
    """Return top-k preferred nodes for image via HRW. Mirrors Go implementation."""
    if not nodes or k <= 0:
        return []
    k = min(k, len(nodes))

    scored = [(n, hrw_score(image, n)) for n in nodes]
    # Sort descending by score, alphabetical tie-break
    scored.sort(key=lambda x: (-x[1], x[0]))
    return [name for name, _ in scored[:k]]


# ---------- Kubernetes helpers ----------


def get_schedulable_nodes(v1: client.CoreV1Api) -> list[str]:
    """Get names of Ready, non-cordoned nodes."""
    nodes = v1.list_node().items
    schedulable = []
    for node in nodes:
        if node.spec.unschedulable:
            continue
        for cond in node.status.conditions or []:
            if cond.type == "Ready" and cond.status == "True":
                schedulable.append(node.metadata.name)
                break
    return sorted(schedulable)


def get_warmpools(custom: client.CustomObjectsApi, namespace: str) -> list[dict]:
    """List WarmPool custom resources."""
    result = custom.list_namespaced_custom_object(
        group="arl.infra.io",
        version="v1alpha1",
        namespace=namespace,
        plural="warmpools",
    )
    return result.get("items", [])


def get_pool_pods(v1: client.CoreV1Api, namespace: str, pool_name: str) -> list:
    """Get pods belonging to a specific WarmPool."""
    return v1.list_namespaced_pod(
        namespace=namespace,
        label_selector=f"{POOL_LABEL}={pool_name}",
    ).items


# ---------- Analysis ----------


def extract_image(warmpool: dict) -> str:
    """Extract the primary (non-sidecar) container image from a WarmPool template."""
    containers = warmpool.get("spec", {}).get("template", {}).get("spec", {}).get("containers", [])
    for c in containers:
        if c.get("name") != "sidecar":
            return c.get("image", "")
    return containers[0].get("image", "") if containers else ""


def extract_locality_config(warmpool: dict) -> dict:
    """Extract ImageLocality config from WarmPool spec."""
    locality = warmpool.get("spec", {}).get("imageLocality") or {}
    return {
        "enabled": locality.get("enabled", True),
        "spreadFactor": locality.get("spreadFactor", 1.0),
        "weight": locality.get("weight", 80),
    }


def extract_pod_affinity_nodes(pod) -> list[str]:
    """Extract preferred node names from pod's NodeAffinity (set by operator)."""
    affinity = pod.spec.affinity
    if not affinity or not affinity.node_affinity:
        return []
    preferred = affinity.node_affinity.preferred_during_scheduling_ignored_during_execution or []
    nodes = []
    for term in preferred:
        for expr in term.preference.match_expressions or []:
            if expr.key == "kubernetes.io/hostname" and expr.operator == "In":
                nodes.extend(expr.values)
    return nodes


@app.command()
def main(
    namespace: Annotated[str, typer.Option(help="Kubernetes namespace")] = "default",
    pool_prefix: Annotated[str, typer.Option(help="Filter pools by name prefix")] = "",
    verbose: Annotated[bool, typer.Option("--verbose", "-v", help="Show per-pod details")] = False,
    kubeconfig: Annotated[str, typer.Option(help="Path to kubeconfig")] = "",
) -> None:
    """Check image-locality scheduling distribution across nodes."""
    if kubeconfig:
        config.load_kube_config(config_file=kubeconfig)
    else:
        try:
            config.load_incluster_config()
        except config.ConfigException:
            config.load_kube_config()

    v1 = client.CoreV1Api()
    custom = client.CustomObjectsApi()

    # 1. Get schedulable nodes
    nodes = get_schedulable_nodes(v1)
    typer.echo(f"Schedulable nodes ({len(nodes)}): {', '.join(nodes)}\n")

    if not nodes:
        typer.echo("No schedulable nodes found.", err=True)
        raise typer.Exit(code=1)

    # 2. Get WarmPools
    warmpools = get_warmpools(custom, namespace)
    if pool_prefix:
        warmpools = [wp for wp in warmpools if wp["metadata"]["name"].startswith(pool_prefix)]

    typer.echo(f"WarmPools found: {len(warmpools)}\n")

    if not warmpools:
        typer.echo("No WarmPools matched.", err=True)
        raise typer.Exit(code=1)

    # 3. Analyze each pool
    global_hit = 0
    global_miss = 0
    global_pending = 0
    node_load: Counter[str] = Counter()  # actual pod count per node

    for wp in warmpools:
        pool_name = wp["metadata"]["name"]
        replicas = wp["spec"].get("replicas", 2)
        image = extract_image(wp)
        locality_cfg = extract_locality_config(wp)

        if not locality_cfg["enabled"]:
            if verbose:
                typer.echo(f"[{pool_name}] locality disabled, skipping")
            continue

        # Compute expected preferred nodes (replicate HRW)
        k = max(1, int(replicas * locality_cfg["spreadFactor"] + 0.999999))
        expected_preferred = compute_top_k(image, nodes, k)

        # Get actual pods
        pods = get_pool_pods(v1, namespace, pool_name)

        hits = 0
        misses = 0
        pending = 0

        for pod in pods:
            actual_node = pod.spec.node_name or ""
            affinity_nodes = extract_pod_affinity_nodes(pod)

            if not actual_node:
                pending += 1
                if verbose:
                    typer.echo(f"  pod={pod.metadata.name}  node=<pending>")
                continue

            node_load[actual_node] += 1
            on_preferred = actual_node in expected_preferred

            if on_preferred:
                hits += 1
            else:
                misses += 1

            if verbose:
                marker = "OK" if on_preferred else "MISS"
                affinity_match = (
                    "match" if set(affinity_nodes) == set(expected_preferred) else "MISMATCH"
                )
                typer.echo(
                    f"  pod={pod.metadata.name}  node={actual_node}  "
                    f"[{marker}]  affinity={affinity_match}"
                )

        total = hits + misses
        hit_rate = f"{hits / total * 100:.0f}%" if total else "N/A"

        typer.echo(
            f"[{pool_name}] image={image[:60]}  replicas={replicas}  k={k}  "
            f"preferred={expected_preferred}  "
            f"hit={hits} miss={misses} pending={pending} rate={hit_rate}"
        )

        global_hit += hits
        global_miss += misses
        global_pending += pending

    # 4. Summary
    total = global_hit + global_miss
    typer.echo("\n" + "=" * 60)
    typer.echo("SUMMARY")
    typer.echo("=" * 60)

    if total:
        typer.echo(f"Locality hit rate: {global_hit}/{total} ({global_hit / total * 100:.1f}%)")
    else:
        typer.echo("No scheduled pods to analyze.")

    if global_pending:
        typer.echo(f"Pending pods (not yet scheduled): {global_pending}")

    typer.echo("\nPod distribution across nodes:")
    for node in sorted(node_load, key=node_load.get, reverse=True):  # type: ignore[arg-type]
        bar = "#" * min(node_load[node], 50)
        typer.echo(f"  {node:40s} {node_load[node]:4d}  {bar}")

    if not node_load:
        typer.echo("  (no pods scheduled)")


if __name__ == "__main__":
    app()
