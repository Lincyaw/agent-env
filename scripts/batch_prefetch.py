"""Batch pre-fetch WarmPool images from R2E-Gym dataset.

Reads the R2E-Gym-Subset parquet dataset, transforms docker image names
to use the mirror registry, and creates WarmPools via the Gateway API
to trigger image pre-fetching on the cluster.

Usage:
    # Dry-run (print what would be created)
    uv run --group prefetch python scripts/batch_prefetch.py --dry-run --limit 5

    # Create 10 pools for testing
    uv run --group prefetch python scripts/batch_prefetch.py --limit 10

    # Create all pools with custom batch size
    uv run --group prefetch python scripts/batch_prefetch.py --batch-size 20
"""

from __future__ import annotations

import re
from typing import Annotated

import polars as pl
import typer
from arl.gateway_client import GatewayError, PoolNotReadyError
from arl.warmpool import WarmPoolManager

MIRROR_REGISTRY = "aibrix-docker-mirror-cn-beijing.cr.volces.com"
HF_DATASET = "hf://datasets/R2E-Gym/R2E-Gym-Subset/data/train-*.parquet"
COLUMNS = ["repo_name", "docker_image", "commit_hash"]
MAX_K8S_NAME_LEN = 63

app = typer.Typer(help="Batch pre-fetch WarmPool images from R2E-Gym dataset.")


def mirror_image(docker_image: str) -> str:
    """Convert docker hub image to mirror registry format."""
    return f"{MIRROR_REGISTRY}/{docker_image}"


def sanitize_pool_name(repo_name: str, commit_hash: str) -> str:
    """Create a valid Kubernetes DNS label from repo_name and commit_hash."""
    safe_repo = re.sub(r"[^a-z0-9]", "-", repo_name.lower()).strip("-")
    hash_prefix = commit_hash[:12].lower()
    name = f"{safe_repo}-{hash_prefix}"
    return name[:MAX_K8S_NAME_LEN].rstrip("-")


def load_dataset() -> pl.DataFrame:
    """Load and deduplicate the R2E-Gym dataset."""
    typer.echo(f"Loading dataset from {HF_DATASET}...")
    df = pl.read_parquet(HF_DATASET, columns=COLUMNS)
    total = len(df)
    df = df.unique(subset=["docker_image"])
    typer.echo(f"Loaded {total} rows, {deduped} unique images" if (deduped := len(df)) else "")
    return df


@app.command()
def main(
    gateway: Annotated[str, typer.Option(help="Gateway base URL")] = "http://localhost:8080",
    namespace: Annotated[str, typer.Option(help="Kubernetes namespace")] = "default",
    replicas: Annotated[int, typer.Option(help="Replicas per pool (1 for pre-fetching)")] = 1,
    limit: Annotated[int, typer.Option(help="Max number of pools to create (0 = all)")] = 0,
    dry_run: Annotated[bool, typer.Option("--dry-run", help="Print plan without creating")] = False,
    batch_size: Annotated[
        int, typer.Option(help="Pools per batch (waits for batch to complete)")
    ] = 10,
    batch_timeout: Annotated[float, typer.Option(help="Max seconds to wait per batch")] = 600.0,
    poll_interval: Annotated[float, typer.Option(help="Seconds between readiness polls")] = 5.0,
    skip_wait: Annotated[
        bool, typer.Option("--skip-wait", help="Don't wait for batches to be ready")
    ] = False,
) -> None:
    """Create WarmPools from R2E-Gym dataset to pre-fetch images."""
    df = load_dataset()

    if limit > 0:
        df = df.head(limit)
        typer.echo(f"Limited to {len(df)} images")

    # Build pool specs
    pools: list[dict[str, str]] = []
    for row in df.iter_rows(named=True):
        name = sanitize_pool_name(row["repo_name"], row["commit_hash"])
        image = mirror_image(row["docker_image"])
        pools.append(
            {"name": name, "image": image, "repo": row["repo_name"], "hash": row["commit_hash"]}
        )

    if dry_run:
        typer.echo(f"\n=== DRY RUN: {len(pools)} pools would be created ===\n")
        for p in pools[:20]:
            typer.echo(f"  name={p['name']}")
            typer.echo(f"  image={p['image']}")
            typer.echo()
        if len(pools) > 20:
            typer.echo(f"  ... and {len(pools) - 20} more")
        return

    # Split into batches
    batches: list[list[dict[str, str]]] = [
        pools[i : i + batch_size] for i in range(0, len(pools), batch_size)
    ]
    total_batches = len(batches)

    typer.echo(f"\nCreating {len(pools)} WarmPools in {total_batches} batches of {batch_size}")
    typer.echo(f"Gateway: {gateway}, Namespace: {namespace}\n")

    manager = WarmPoolManager(namespace=namespace, gateway_url=gateway, timeout=batch_timeout)
    total_created = 0
    total_skipped = 0
    total_failed = 0

    for batch_idx, batch in enumerate(batches):
        batch_num = batch_idx + 1
        typer.echo(f"=== Batch {batch_num}/{total_batches} ({len(batch)} pools) ===")

        batch_created_names: list[str] = []
        batch_failed = 0

        for pool in batch:
            try:
                manager.create_warmpool(
                    name=pool["name"],
                    image=pool["image"],
                    replicas=replicas,
                )
                total_created += 1
                batch_created_names.append(pool["name"])
            except GatewayError as e:
                if e.status_code == 409:
                    total_skipped += 1
                else:
                    batch_failed += 1
                    total_failed += 1
                    typer.echo(f"  FAILED [{pool['name']}]: {e}", err=True)

        typer.echo(
            f"  Submitted: {len(batch_created_names)} new, "
            f"{len(batch) - len(batch_created_names) - batch_failed} skipped, "
            f"{batch_failed} failed"
        )

        # Wait for this batch to be ready before proceeding
        if batch_created_names and not skip_wait:
            typer.echo(f"  Waiting for {len(batch_created_names)} pools to become ready...")
            ready_count = 0
            fail_count = 0
            for name in batch_created_names:
                try:
                    manager.wait_for_ready(name, timeout=batch_timeout, poll_interval=poll_interval)
                    ready_count += 1
                except PoolNotReadyError as e:
                    fail_count += 1
                    typer.echo(f"    FAIL [{name}]: {e}", err=True)
                except TimeoutError as e:
                    fail_count += 1
                    typer.echo(f"    TIMEOUT [{name}]: {e}", err=True)

            if fail_count:
                typer.echo(f"  Batch done: {ready_count} ready, {fail_count} failed", err=True)
            else:
                typer.echo(f"  All {ready_count} pools ready")

        typer.echo()

    typer.echo("=" * 50)
    typer.echo(f"Done: created={total_created}, skipped={total_skipped}, failed={total_failed}")

    if total_failed > 0:
        raise typer.Exit(code=1)


if __name__ == "__main__":
    app()
