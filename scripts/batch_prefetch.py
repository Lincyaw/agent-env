#!/usr/bin/env python3
"""Batch prefetch WarmPool images for SWE RL training.

Reads both R2E-Gym and SWE-Bench Verified parquet datasets, derives pool
names (matching SWEEnv._derive_pool_ref convention), maps docker images to
the mirror registry, and creates WarmPools via the ARL Gateway API to
pre-cache container images on K8s nodes.

Uses a sliding-window concurrency model: up to `--concurrency` pools are
warmed simultaneously. As soon as one finishes (ready → scale down), the
next starts — no batch barriers, no fast pools waiting for slow ones.

Usage:
    # Dry-run (preview what would be created)
    python scripts/batch_prefetch.py --dry-run

    # Create all pools (uses ARL_GATEWAY_URL env var or --gateway)
    python scripts/batch_prefetch.py

    # Specific dataset only
    python scripts/batch_prefetch.py --dataset r2egym
    python scripts/batch_prefetch.py --dataset swebench

    # Custom gateway and concurrency
    python scripts/batch_prefetch.py --gateway http://14.103.184.145:8080 --concurrency 20

    # Keep pools running after image pull (don't scale down)
    python scripts/batch_prefetch.py --no-scale-down-after

    # Limit number of pools (for testing)
    python scripts/batch_prefetch.py --limit 5

    # Delete all pools instead of creating
    python scripts/batch_prefetch.py --delete
"""

from __future__ import annotations

import argparse
import json
import logging
import os
import re
import sys
import threading
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from pathlib import Path
from typing import Any, cast

from arl.gateway_client import GatewayError
from arl.warmpool import WarmPoolManager

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(message)s",
    datefmt="%H:%M:%S",
)
logger = logging.getLogger(__name__)

# Suppress httpx's per-request INFO logs (e.g. "HTTP Request: POST ... 500")
# so they don't drown out real errors. Genuine failures are logged by our code.
logging.getLogger("httpx").setLevel(logging.WARNING)

# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------

REPO_ROOT = Path(__file__).resolve().parent.parent
DATA_DIR = REPO_ROOT / "data" / "swe"

MIRROR_REGISTRY = "pair-diag-cn-guangzhou.cr.volces.com"
MIRROR_NAMESPACE = "code"
MAX_K8S_NAME_LEN = 63
RETRYABLE_STATUS_CODES = {429, 500, 502, 503, 504}
CHECKPOINT_DEFAULT = REPO_ROOT / "scripts" / ".prefetch_checkpoint.jsonl"
COMPLETED_CHECKPOINT_STATES = {"already_ready", "ok"}
CHECKPOINT_WRITE_LOCK = threading.Lock()

PARQUET_FILES = {
    "r2egym": DATA_DIR / "R2E_Gym_Subset.parquet",
    "swebench": DATA_DIR / "SWE_Bench_Verified.parquet",
}

HF_DATASET_NAMES = {
    "r2egym": "R2E-Gym/R2E-Gym-Subset",
    "swebench": "R2E-Gym/SWE-Bench-Verified",
}


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _ensure_parquet(ds_name: str, path: Path) -> bool:
    """Download parquet from HuggingFace if not already present. Returns True on success."""
    hf_name = HF_DATASET_NAMES.get(ds_name)
    if not hf_name:
        logger.warning(f"No HuggingFace dataset name configured for '{ds_name}'")
        return False
    try:
        from datasets import load_dataset
    except ImportError:
        sys.exit("datasets is required: pip install datasets")

    logger.info(f"[{ds_name}] Downloading {hf_name} ...")
    try:
        splits = load_dataset(hf_name)
    except Exception as e:
        logger.error(f"Failed to download {hf_name}: {e}")
        return False

    split_data = splits.get("train") or splits.get("test")
    if split_data is None:
        logger.error(f"No 'train' or 'test' split found in {hf_name}")
        return False

    path.parent.mkdir(parents=True, exist_ok=True)
    split_data_df = cast("Any", split_data.to_pandas())
    split_data_df.to_parquet(path)
    logger.info(f"[{ds_name}] Saved {len(split_data)} rows to {path}")
    return True


def sanitize_pool_name(repo_name: str, commit_hash: str) -> str:
    """Create a valid K8s DNS label from repo_name + commit_hash.

    Identical to rllm.environments.swe.swe._derive_pool_ref so that the
    pool names created here match what SWEEnv looks up at training time.
    """
    safe_repo = re.sub(r"[^a-z0-9]", "-", repo_name.lower()).strip("-")
    hash_prefix = commit_hash[:8].lower()
    name = f"{safe_repo}-{hash_prefix}"
    return name[:MAX_K8S_NAME_LEN].rstrip("-")


def mirror_image(docker_image: str) -> str:
    """Convert a docker image to the mirror registry format.

    Rewrites the namespace (first path segment) to MIRROR_NAMESPACE ('code'),
    consistent with scripts/mirror_images.py rewrite_namespace().

    Examples:
        namanjain12/numpy_final:abc123
          -> pair-diag-cn-guangzhou.cr.volces.com/code/numpy_final:abc123
        slimshetty/swebench-verified:sweb.eval.x86_64.tag
          -> pair-diag-cn-guangzhou.cr.volces.com/code/swebench-verified:sweb.eval.x86_64.tag
    """
    parts = docker_image.split("/", 1)
    image_path = parts[1] if len(parts) == 2 else docker_image
    return f"{MIRROR_REGISTRY}/{MIRROR_NAMESPACE}/{image_path}"


def load_pool_specs(parquet_path: Path) -> list[dict[str, str]]:
    """Load a parquet dataset and extract unique (pool_name, image) pairs.

    Handles both top-level columns and fields nested inside extra_info JSON.
    Deduplicates by pool_name (same repo+commit -> same pool).
    """
    try:
        import pandas as pd
    except ImportError:
        sys.exit("pandas is required: pip install pandas pyarrow")

    df = pd.read_parquet(parquet_path)

    # Expand extra_info JSON column into additional columns (top-level takes precedence)
    if "extra_info" in df.columns:

        def _parse(v) -> dict:
            if isinstance(v, str):
                try:
                    return json.loads(v)
                except Exception:
                    return {}
            return v if isinstance(v, dict) else {}

        parsed_extra = cast("Any", df["extra_info"].apply(_parse))
        extra = pd.json_normalize(parsed_extra)
        for col in extra.columns:
            if col not in df.columns:
                df[col] = extra[col].values

    def _col(*keys: str) -> pd.Series:
        """Return first non-empty series found among candidate column names."""
        for k in keys:
            if k in df.columns:
                s = df[k].fillna("").astype(str).str.strip()
                if s.ne("").any():
                    return s
        return pd.Series("", index=df.index)

    result = pd.DataFrame(
        {
            "repo": _col("repo_name", "repo"),
            "commit": _col("commit_hash", "base_commit"),
            "image": _col("docker_image", "image_name"),
        }
    )
    result = result[result["repo"].ne("") & result["commit"].ne("") & result["image"].ne("")]
    result = result.copy()
    result["pool_name"] = result.apply(lambda r: sanitize_pool_name(r["repo"], r["commit"]), axis=1)
    result["mirrored_image"] = result["image"].apply(mirror_image)
    result = result.drop_duplicates(subset="pool_name")

    return [
        {
            "name": r["pool_name"],
            "image": r["mirrored_image"],
            "repo": r["repo"],
            "hash": r["commit"],
            "original_image": r["image"],
        }
        for r in result.to_dict("records")
    ]


def load_checkpoint_states(path: Path) -> dict[str, str]:
    """Load latest checkpoint status by pool name from JSONL."""
    if not path.exists():
        return {}

    states: dict[str, str] = {}
    for line in path.read_text(encoding="utf-8").splitlines():
        if not line.strip():
            continue
        try:
            item = json.loads(line)
        except json.JSONDecodeError:
            continue
        name = str(item.get("name", "")).strip()
        status = str(item.get("status", "")).strip()
        if name and status:
            states[name] = status
    return states


def append_checkpoint(path: Path, name: str, image: str, status: str) -> None:
    """Append one checkpoint event to JSONL file."""
    path.parent.mkdir(parents=True, exist_ok=True)
    event = {
        "ts": time.time(),
        "name": name,
        "image": image,
        "status": status,
    }
    with CHECKPOINT_WRITE_LOCK:
        with path.open("a", encoding="utf-8") as f:
            f.write(json.dumps(event, ensure_ascii=False) + "\n")


def create_warmpool_with_retry(
    manager: WarmPoolManager,
    pool: dict[str, str],
    replicas: int,
    create_retries: int,
    create_backoff_base: float,
) -> tuple[str, bool]:
    """Create pool with retry.

    Returns:
        (status, created_new)
        status in {"created", "exists", "failed"}
    """
    name = pool["name"]

    for attempt in range(create_retries + 1):
        try:
            manager.create_warmpool(
                name=name,
                image=pool["image"],
                replicas=replicas,
            )
            return "created", True
        except GatewayError as e:
            if e.status_code == 409 or "already exists" in str(e):
                return "exists", False

            if e.status_code in RETRYABLE_STATUS_CODES and attempt < create_retries:
                delay = create_backoff_base * (2**attempt)
                logger.warning(
                    f"Create retry {attempt + 1}/{create_retries} for {name} after {delay:.1f}s: {e}"
                )
                time.sleep(delay)
                continue

            logger.error(f"Create failed for {name}: {e}")
            return "failed", False

    return "failed", False


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------


def main():
    parser = argparse.ArgumentParser(
        description="Batch prefetch WarmPool images for SWE RL training.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument(
        "--gateway",
        type=str,
        default=os.environ.get("ARL_GATEWAY_URL", "http://localhost:8080"),
        help="ARL Gateway URL (default: $ARL_GATEWAY_URL or http://localhost:8080)",
    )
    parser.add_argument(
        "--namespace",
        type=str,
        default="default",
        help="Kubernetes namespace (default: default)",
    )
    parser.add_argument(
        "--dataset",
        choices=["r2egym", "swebench", "all"],
        default="all",
        help="Which dataset(s) to prefetch (default: all)",
    )
    parser.add_argument(
        "--replicas",
        type=int,
        default=1,
        help="Replicas per pool — 1 is enough for image prefetch (default: 1)",
    )
    parser.add_argument(
        "--concurrency",
        type=int,
        default=10,
        help="Max pools warming simultaneously — sliding window, no batch barriers (default: 10)",
    )
    parser.add_argument(
        "--pool-timeout",
        type=float,
        default=600.0,
        help="Max seconds to wait for a single pool to become ready (default: 600)",
    )
    parser.add_argument(
        "--poll-interval",
        type=float,
        default=10.0,
        help="Seconds between readiness polls (default: 10)",
    )
    parser.add_argument(
        "--limit",
        type=int,
        default=0,
        help="Max number of pools to create, 0 = all (default: 0)",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Print plan without creating pools",
    )
    parser.add_argument(
        "--skip-wait",
        action="store_true",
        help="Don't wait for pools to become ready",
    )
    parser.add_argument(
        "--scale-down-after",
        action=argparse.BooleanOptionalAction,
        default=True,
        help="Scale pools to 0 after image pull (default: True). Use --no-scale-down-after to keep running.",
    )
    parser.add_argument(
        "--delete",
        action="store_true",
        help="Delete all matching pools instead of creating them",
    )
    parser.add_argument(
        "--create-retries",
        type=int,
        default=3,
        help="Retries for transient create failures (default: 3)",
    )
    parser.add_argument(
        "--create-backoff-base",
        type=float,
        default=2.0,
        help="Base backoff seconds for create retries (default: 2.0)",
    )
    parser.add_argument(
        "--checkpoint-file",
        type=Path,
        default=CHECKPOINT_DEFAULT,
        help=f"Checkpoint JSONL path (default: {CHECKPOINT_DEFAULT})",
    )
    parser.add_argument(
        "--no-checkpoint",
        action="store_true",
        help="Disable checkpoint read/write",
    )
    parser.add_argument(
        "--reset-checkpoint",
        action="store_true",
        help="Clear checkpoint file before run",
    )
    args = parser.parse_args()

    if args.concurrency < 1:
        parser.error("--concurrency must be >= 1")
    if args.create_retries < 0:
        parser.error("--create-retries must be >= 0")
    if args.create_backoff_base <= 0:
        parser.error("--create-backoff-base must be > 0")

    checkpoint_states: dict[str, str] = {}
    if not args.no_checkpoint:
        checkpoint_path = args.checkpoint_file.expanduser().resolve()
        if args.reset_checkpoint and checkpoint_path.exists():
            checkpoint_path.unlink()
            logger.info(f"Checkpoint reset: {checkpoint_path}")
        checkpoint_states = load_checkpoint_states(checkpoint_path)
    else:
        checkpoint_path = args.checkpoint_file.expanduser().resolve()

    # --- Load pool specs from datasets ---
    datasets = list(PARQUET_FILES.keys()) if args.dataset == "all" else [args.dataset]
    all_pools: dict[str, dict[str, str]] = {}

    for ds_name in datasets:
        path = PARQUET_FILES[ds_name]
        if not path.exists():
            if not _ensure_parquet(ds_name, path):
                logger.warning(f"Parquet file not found and download failed, skipping: {path}")
                continue
        specs = load_pool_specs(path)
        logger.info(f"[{ds_name}] {len(specs)} unique pools from {path.name}")
        for spec in specs:
            all_pools.setdefault(spec["name"], spec)

    pools = sorted(all_pools.values(), key=lambda p: p["name"])

    if not args.no_checkpoint and checkpoint_states:
        original_count = len(pools)
        pools = [
            p
            for p in pools
            if checkpoint_states.get(p["name"], "") not in COMPLETED_CHECKPOINT_STATES
        ]
        resumed = original_count - len(pools)
        if resumed > 0:
            logger.info(f"Checkpoint skip: {resumed} pools already completed")

    if args.limit > 0:
        pools = pools[: args.limit]
        logger.info(f"Limited to {len(pools)} pools")

    logger.info(f"Total unique pools to prefetch: {len(pools)}")

    if not pools:
        logger.warning("No pools found. Exiting.")
        return

    # --- Dry-run ---
    if args.dry_run:
        print(f"\n=== DRY RUN: {len(pools)} pools would be created ===\n")
        for p in pools[:30]:
            print(f"  pool:  {p['name']}")
            print(f"  image: {p['image']}")
            print(f"  repo:  {p['repo']}  commit: {p['hash'][:12]}")
            print()
        if len(pools) > 30:
            print(f"  ... and {len(pools) - 30} more\n")
        return

    # --- Import ARL (only needed for non-dry-run) ---
    # --- Delete mode ---
    if args.delete:
        _lock = threading.Lock()
        counters = {"deleted": 0, "skipped": 0, "failed": 0}

        def _delete_one(pool: dict[str, str]) -> None:
            mgr = WarmPoolManager(namespace=args.namespace, gateway_url=args.gateway)
            try:
                mgr.delete_warmpool(pool["name"])
                with _lock:
                    counters["deleted"] += 1
                logger.info(f"Deleted: {pool['name']}")
            except GatewayError as e:
                with _lock:
                    if e.status_code == 404:
                        counters["skipped"] += 1
                    else:
                        counters["failed"] += 1
                        logger.error(f"Failed to delete {pool['name']}: {e}")
            finally:
                mgr.close()

        with ThreadPoolExecutor(max_workers=args.concurrency) as executor:
            list(executor.map(_delete_one, pools))

        logger.info(
            f"Delete done: {counters['deleted']} deleted, "
            f"{counters['skipped']} not found, {counters['failed']} failed"
        )
        return

    # --- Create mode (sliding window: create → wait → scale_down per worker) ---
    #
    # Each worker handles ONE pool's full lifecycle. This ensures at most
    # `concurrency` pods are pulling images at any time — preventing
    # registry overload from thousands of simultaneous pulls.
    logger.info(f"Prefetching {len(pools)} WarmPools with concurrency={args.concurrency}")
    logger.info(f"Gateway: {args.gateway}, Namespace: {args.namespace}")
    if args.scale_down_after:
        logger.info("Scale-down-after enabled: pools scaled to 0 replicas after image pull")

    _lock = threading.Lock()
    counters = {"created": 0, "skipped": 0, "failed": 0, "done": 0}
    total = len(pools)

    def _prefetch_one(pool: dict[str, str]) -> tuple[str, bool, str]:
        """Full lifecycle for one pool: create/scale-up → wait → scale_down."""
        name = pool["name"]
        mgr = WarmPoolManager(
            namespace=args.namespace,
            gateway_url=args.gateway,
            timeout=args.pool_timeout,
        )
        try:
            # 1. Create or scale up existing pool
            create_status, created_new = create_warmpool_with_retry(
                manager=mgr,
                pool=pool,
                replicas=args.replicas,
                create_retries=args.create_retries,
                create_backoff_base=args.create_backoff_base,
            )

            if create_status == "failed":
                raise RuntimeError("create_failed")

            if create_status == "exists":
                # Pool CRD exists — check if it already has ready replicas
                try:
                    info = mgr.get_warmpool(name)
                    if info.ready_replicas >= args.replicas:
                        # Already warm, just scale down if needed
                        if args.scale_down_after:
                            mgr.scale_warmpool(name, 0)
                        with _lock:
                            counters["skipped"] += 1
                            counters["done"] += 1
                            done = counters["done"]
                        if not args.no_checkpoint:
                            append_checkpoint(checkpoint_path, name, pool["image"], "already_ready")
                        logger.info(f"  [{done}/{total}] SKIP (ready): {name}")
                        return name, True, "already_ready"
                except Exception as e:
                    logger.warning(
                        f"Could not read existing pool status for {name}, proceeding scale-up: {e}"
                    )
                # Not ready — scale up to trigger image pull
                mgr.scale_warmpool(name, args.replicas)
            elif created_new and not args.no_checkpoint:
                append_checkpoint(checkpoint_path, name, pool["image"], "created")

            # 2. Wait for ready
            if not args.skip_wait:
                mgr.wait_for_ready(
                    name,
                    timeout=args.pool_timeout,
                    poll_interval=args.poll_interval,
                )

            # 3. Scale down (release pod, image stays cached on node)
            if args.scale_down_after and not args.skip_wait:
                mgr.scale_warmpool(name, 0)

            with _lock:
                counters["created"] += 1
                counters["done"] += 1
                done = counters["done"]
            if not args.no_checkpoint:
                append_checkpoint(checkpoint_path, name, pool["image"], "ok")
            suffix = " → scaled to 0" if args.scale_down_after else ""
            logger.info(f"  [{done}/{total}] OK: {name}{suffix}")
            return name, True, "ok"

        except Exception as e:
            with _lock:
                counters["failed"] += 1
                counters["done"] += 1
                done = counters["done"]
            if not args.no_checkpoint:
                append_checkpoint(checkpoint_path, name, pool["image"], "failed")
            logger.error(f"  [{done}/{total}] FAIL: {name}: {e}")
            return name, False, str(e)
        finally:
            mgr.close()

    failed_names: list[str] = []
    try:
        with ThreadPoolExecutor(max_workers=args.concurrency) as executor:
            futures = {executor.submit(_prefetch_one, p): p for p in pools}
            for future in as_completed(futures):
                name, ok, _ = future.result()
                if not ok:
                    failed_names.append(name)
    except KeyboardInterrupt:
        logger.warning("\nInterrupted by user")

    # --- Summary ---
    logger.info("=" * 50)
    logger.info(
        f"Done: created={counters['created']}, skipped={counters['skipped']}, "
        f"failed={counters['failed']}"
    )

    if failed_names:
        failed_path = REPO_ROOT / "scripts" / ".prefetch_failed.log"
        failed_path.write_text("\n".join(failed_names) + "\n")
        logger.error(f"Failed pools written to {failed_path}")

    if counters["failed"] > 0:
        sys.exit(1)


if __name__ == "__main__":
    main()
