"""Harbor BaseEnvironment backed by ARL sandbox sessions.

Usage::

    harbor run -d snorkel-ai/senior-swe-bench-v2026.06 \\
        --environment-import-path arl.harbor:ArlEnvironment \\
        -a mini-swe-agent -m anthropic/claude-sonnet-4-6 -n 5 \\
        --ek gateway_url=http://118.145.253.105:8080

The environment creates an ARL sandbox session from the task's
``docker_image`` (or builds via Dockerfile), maps Harbor's exec / upload /
download operations to ARL gRPC calls, and tears the session down on stop.
"""

from __future__ import annotations

import io
import ipaddress
import logging
import os
import shlex
import tarfile
from dataclasses import dataclass, field
from pathlib import Path, PurePosixPath
from typing import Any

from harbor.environments.base import BaseEnvironment, ExecResult, NetworkPolicy
from harbor.environments.capabilities import (
    EnvironmentCapabilities,
    EnvironmentResourceCapabilities,
)
from harbor.environments.definition import (
    DOCKERFILE_NAME,
    effective_exec_cwd,
    environment_content_hash,
    parse_dockerfile_workdir,
    require_agent_environment_definition,
)
from harbor.models.task.config import EnvironmentConfig, NetworkMode
from harbor.models.trial.paths import TrialPaths

from arl.async_client import AsyncGatewayClient
from arl.configenv import ConfigEnvSpec
from arl.types import ExecuteResponse, StepRequest


@dataclass
class ArlStepSnapshot:
    """Record of an exec step with its ARL checkpoint metadata."""

    step_index: int
    snapshot_id: str
    name: str
    exit_code: int
    duration_ms: int


@dataclass
class ArlSessionInfo:
    """ARL session metadata exposed to agents.

    Agents can read ``environment.arl`` to discover the session ID,
    gateway URL, and per-step checkpoint snapshots for fork/resume.
    """

    session_id: str = ""
    gateway_url: str = ""
    parent_session_id: str = ""
    fork_step: int = 0
    steps: list[ArlStepSnapshot] = field(default_factory=list)

    @property
    def last_snapshot_id(self) -> str:
        return self.steps[-1].snapshot_id if self.steps else ""

    @property
    def step_count(self) -> int:
        return len(self.steps)


class ArlEnvironment(BaseEnvironment):
    """ARL-backed Harbor environment.

    Constructor kwargs (passed via ``--ek key=value``):

    * ``gateway_url`` -- ARL gateway address (default: ``$ARL_GATEWAY_URL``
      or ``http://localhost:8080``).
    * ``idle_timeout_seconds`` -- sandbox idle timeout (default 7200).
    * ``allocation_timeout_seconds`` -- pod allocation timeout (default 600).
    * ``experiment_id`` -- optional ARL experiment grouping tag.
    * ``profile`` -- ARL session profile (default ``"default"``).
    * ``build_registry`` -- registry to push images built from Dockerfile
      (default: ``$ARL_BUILD_REGISTRY``).  Falls back to ``image_registry``.
    * ``build_timeout`` -- timeout in seconds for image builds (default 900).
    * ``fork_from`` -- source session ID for forking from a checkpoint.
    * ``fork_step`` -- step number to fork from (default 0).
    * ``resume_from`` -- suspended session ID to resume.

    Agents can access ``environment.arl`` (an :class:`ArlSessionInfo`) to
    read the session ID, gateway URL, and per-step snapshot history.
    """

    def __init__(
        self,
        environment_dir: Path,
        environment_name: str,
        session_id: str,
        trial_paths: TrialPaths,
        task_env_config: EnvironmentConfig,
        *,
        gateway_url: str = "",
        idle_timeout_seconds: int = 7200,
        allocation_timeout_seconds: int = 600,
        experiment_id: str = "",
        profile: str = "default",
        image_registry: str = "",
        image_prefix: str = "",
        image_tag: str = "v1",
        build_registry: str = "",
        build_timeout: int = 900,
        fork_from: str = "",
        fork_step: int = 0,
        resume_from: str = "",
        logger: logging.Logger | None = None,
        **kwargs: Any,
    ) -> None:
        self._gateway_url = (
            gateway_url or os.environ.get("ARL_GATEWAY_URL", "") or "http://localhost:8080"
        )
        self._idle_timeout = idle_timeout_seconds
        self._allocation_timeout = allocation_timeout_seconds
        self._experiment_id = experiment_id
        self._profile = profile
        self._image_registry = image_registry or os.environ.get("ARL_IMAGE_REGISTRY", "")
        self._image_prefix = image_prefix or os.environ.get("ARL_IMAGE_PREFIX", "")
        self._image_tag = image_tag
        self._build_registry = build_registry or os.environ.get("ARL_BUILD_REGISTRY", "")
        self._build_timeout = build_timeout
        self._fork_from = fork_from or os.environ.get("ARL_FORK_FROM", "")
        self._fork_step = int(fork_step or os.environ.get("ARL_FORK_STEP", "0"))
        self._resume_from = resume_from or os.environ.get("ARL_RESUME_FROM", "")

        self._client: AsyncGatewayClient | None = None
        self._arl_session_id: str | None = None
        self.arl = ArlSessionInfo(gateway_url=self._gateway_url)

        # Cache Dockerfile WORKDIR for exec cwd resolution
        dockerfile_path = environment_dir / DOCKERFILE_NAME
        self._dockerfile_workdir: str | None = parse_dockerfile_workdir(dockerfile_path)

        super().__init__(
            environment_dir=environment_dir,
            environment_name=environment_name,
            session_id=session_id,
            trial_paths=trial_paths,
            task_env_config=task_env_config,
            logger=logger,
            **kwargs,
        )

    @staticmethod
    def type() -> str:
        return "arl"

    @property
    def capabilities(self) -> EnvironmentCapabilities:
        return EnvironmentCapabilities(
            gpus=True,
            disable_internet=True,
            network_allowlist=True,
            network_allowlist_hostnames=True,
            network_allowlist_wildcard_hostnames=True,
            network_allowlist_ipv4_addresses=True,
            network_allowlist_ipv6_addresses=True,
            network_allowlist_ipv4_cidrs=True,
            network_allowlist_ipv6_cidrs=True,
            dynamic_network_policy=True,
        )

    @staticmethod
    def resource_capabilities() -> EnvironmentResourceCapabilities:
        return EnvironmentResourceCapabilities(
            cpu_limit=True,
            cpu_request=True,
            memory_limit=True,
            memory_request=True,
        )

    async def _apply_network_policy(self, network_policy: NetworkPolicy) -> None:
        if not self._arl_session_id:
            raise RuntimeError("ARL session not started.")
        client = self._get_client()

        try:
            if network_policy.network_mode == NetworkMode.PUBLIC:
                await client.update_network_policy(self._arl_session_id, allow_internet=True)
                return

            egress_cidrs: list[str] = []
            for host in network_policy.allowed_hosts:
                cidr = _host_to_cidr(host)
                if cidr is None:
                    self.logger.warning(
                        "Ignoring non-CIDR allowlist host %r: dynamic network "
                        "policy can only enforce IP/CIDR egress rules",
                        host,
                    )
                    continue
                egress_cidrs.append(cidr)

            await client.update_network_policy(
                self._arl_session_id,
                allow_internet=False,
                egress_cidrs=egress_cidrs,
            )
        except Exception as exc:
            self.logger.warning("Dynamic network policy not applied: %s", exc)

    def _validate_definition(self) -> None:
        if self.task_env_config.docker_image:
            return
        require_agent_environment_definition(
            self.environment_dir,
            docker_image=self.task_env_config.docker_image,
        )

    @classmethod
    def preflight(cls) -> None:
        try:
            import arl as _arl  # noqa: F401
        except ImportError as exc:
            raise SystemExit(
                "ARL environment requires the 'arl-env' package. Install with: pip install arl-env"
            ) from exc

    def _get_client(self) -> AsyncGatewayClient:
        if self._client is None:
            self._client = AsyncGatewayClient(
                base_url=self._gateway_url,
            )
        return self._client

    def _resolve_image(self) -> str | None:
        """Resolve a pre-built image reference, or None if unavailable."""
        if self.task_env_config.docker_image:
            return self.task_env_config.docker_image
        if self._image_registry:
            name = self.environment_name
            prefix = self._image_prefix
            if prefix:
                image_name = f"{prefix}-{name}" if not name.startswith(prefix) else name
            else:
                image_name = name
            return f"{self._image_registry}/{image_name}:{self._image_tag}"
        return None

    async def _resolve_image_or_build(self, force_build: bool) -> str:
        """Return the container image, building from Dockerfile if needed."""
        pre_built = self._resolve_image()
        if not force_build and pre_built is not None:
            return pre_built

        dockerfile_path = self.environment_dir / DOCKERFILE_NAME
        if not dockerfile_path.exists():
            if pre_built is not None:
                return pre_built
            raise ValueError(
                "ARL environment requires docker_image in task.toml, "
                "--ek image_registry=<registry>/<org>, or a Dockerfile "
                "in the environment directory."
            )

        # Single pass: tar the context and hash its contents for the image tag.
        context_buf, content_hash = self._package_build_context()
        image_name = self.environment_name.replace("/", "-").replace(":", "-")

        registry = self._build_registry or self._image_registry
        if registry:
            target_image = f"{registry}/{image_name}:{content_hash}"
        else:
            target_image = f"{image_name}:{content_hash}"

        self.logger.info("Building image %s from %s", target_image, self.environment_dir)
        client = self._get_client()

        result = await client.build_image(
            target_image,
            context_buf,
            timeout=self._build_timeout,
            cache=True,
        )
        self.logger.info(
            "Image built: %s (digest=%s)",
            result.image,
            result.digest,
        )
        return result.image

    def _package_build_context(self) -> tuple[io.BytesIO, str]:
        """Package environment_dir as tar.gz and compute content hash.

        Returns (tar_buffer, content_hash). Uses the upstream
        environment_content_hash for collision-safe hashing with proper
        .git/__pycache__ filtering.
        """
        content_hash = environment_content_hash(self.environment_dir, truncate=12)
        buf = io.BytesIO()
        with tarfile.open(fileobj=buf, mode="w:gz") as tar:
            tar.add(str(self.environment_dir), arcname=".")
        buf.seek(0)
        return buf, content_hash

    def _initial_allow_internet(self) -> bool | None:
        """Map the startup network policy to the gateway's allowInternet flag."""
        if self._network_policy.network_mode == NetworkMode.NO_NETWORK:
            return False
        if self._network_policy.network_mode == NetworkMode.PUBLIC:
            return True
        # ALLOWLIST mode: start with internet disabled; the gateway's
        # egressAllowCIDRs at pool/session level would handle allowlisting,
        # but those are set at creation time, not dynamically.
        return False

    async def start(self, force_build: bool) -> None:
        client = self._get_client()

        if self._resume_from:
            await client.resume_session(self._resume_from)
            info = await client.get_session(self._resume_from)
            self._arl_session_id = info.id
            self.arl.session_id = info.id
            self.arl.parent_session_id = info.parent_session_id
            self.arl.fork_step = info.fork_step
            self.logger.info(
                "ARL session %s resumed",
                self._arl_session_id,
            )
            return

        if self._fork_from:
            fork_resp = await client.fork_session(self._fork_from, self._fork_step)
            self._arl_session_id = fork_resp.session.id
            self.arl.session_id = fork_resp.session.id
            self.arl.parent_session_id = fork_resp.parent_id
            self.arl.fork_step = fork_resp.fork_step
            self.logger.info(
                "ARL session %s forked from %s at step %d",
                self._arl_session_id,
                self._fork_from,
                self._fork_step,
            )
            await self.ensure_dirs(self._mount_targets(writable_only=True))
            return

        image = await self._resolve_image_or_build(force_build)

        startup_env = self._startup_env()
        config_env: ConfigEnvSpec | None = None
        if startup_env:
            config_env = ConfigEnvSpec(vars=startup_env)

        allow_internet = self._initial_allow_internet()

        info = await client.create_session(
            image,
            profile=self._profile,
            idle_timeout_seconds=self._idle_timeout,
            allocation_timeout_seconds=self._allocation_timeout,
            config_env=config_env,
            allow_internet=allow_internet,
        )
        self._arl_session_id = info.id
        self.arl.session_id = info.id
        self.logger.info(
            "ARL session %s created (image=%s)",
            self._arl_session_id,
            image,
        )

        await self.ensure_dirs(self._mount_targets(writable_only=True))
        await self._upload_environment_dir_after_start()

    async def stop(self, delete: bool) -> None:
        if self._arl_session_id:
            client = self._get_client()
            if delete:
                try:
                    await client.delete_session(self._arl_session_id)
                    self.logger.info("ARL session %s deleted", self._arl_session_id)
                except Exception as exc:
                    self.logger.warning(
                        "Failed to delete ARL session %s: %s",
                        self._arl_session_id,
                        exc,
                    )
            else:
                # Suspend to preserve the session for debugging while
                # reducing resource consumption
                try:
                    await client.suspend_session(self._arl_session_id)
                    self.logger.info("ARL session %s suspended", self._arl_session_id)
                except Exception as exc:
                    self.logger.warning(
                        "Failed to suspend ARL session %s: %s",
                        self._arl_session_id,
                        exc,
                    )
        self._arl_session_id = None
        if self._client is not None:
            await self._client.aclose()
            self._client = None

    async def exec(
        self,
        command: str,
        cwd: str | None = None,
        env: dict[str, str] | None = None,
        timeout_sec: int | None = None,
        user: str | int | None = None,
    ) -> ExecResult:
        if not self._arl_session_id:
            raise RuntimeError("ARL session not started.")
        client = self._get_client()

        merged_env = self._merge_env(env)
        work_dir = effective_exec_cwd(cwd, self.task_env_config.workdir, self._dockerfile_workdir)

        shell_cmd = command
        user = self._resolve_user(user)
        if user is not None and str(user) != "root":
            shell_cmd = f"su -s /bin/bash {shlex.quote(str(user))} -c {shlex.quote(shell_cmd)}"

        step = StepRequest(
            name="exec",
            command=["bash", "-c", shell_cmd],
            workDir=work_dir or None,
            timeoutSeconds=timeout_sec or None,
            env=merged_env or None,
        )

        resp: ExecuteResponse = await client.execute(
            self._arl_session_id,
            [step.model_dump(by_alias=True, exclude_none=True)],
            recover_timeout=(timeout_sec or 300) + 120,
        )

        result = resp.results[0] if resp.results else None
        if result is None:
            return ExecResult(stdout="", stderr="", return_code=-1)

        self.arl.steps.append(ArlStepSnapshot(
            step_index=result.index,
            snapshot_id=result.snapshot_id,
            name=result.name,
            exit_code=result.output.exit_code,
            duration_ms=result.duration_ms,
        ))

        return ExecResult(
            stdout=result.output.stdout,
            stderr=result.output.stderr,
            return_code=result.output.exit_code,
        )

    async def upload_file(self, source_path: Path | str, target_path: str) -> None:
        if not self._arl_session_id:
            raise RuntimeError("ARL session not started.")
        if not target_path.startswith("/"):
            target_path = f"/app/{target_path}"
        client = self._get_client()
        await client.upload_path(self._arl_session_id, source_path, target_path)

    async def upload_dir(self, source_dir: Path | str, target_dir: str) -> None:
        if not self._arl_session_id:
            raise RuntimeError("ARL session not started.")
        source = Path(source_dir)
        for file_path in source.rglob("*"):
            if not file_path.is_file():
                continue
            rel = file_path.relative_to(source).as_posix()
            remote = str(PurePosixPath(target_dir) / rel)
            if not remote.startswith("/"):
                remote = f"/{remote}"
            await self.upload_file(file_path, remote)

    async def download_file(self, source_path: str, target_path: Path | str) -> None:
        if not self._arl_session_id:
            raise RuntimeError("ARL session not started.")
        client = self._get_client()
        await client.download_path(self._arl_session_id, source_path, target_path)

    async def download_dir(self, source_dir: str, target_dir: Path | str) -> None:
        await self.download_dir_with_exclusions(
            source_dir=source_dir,
            target_dir=target_dir,
            exclude=[],
        )


def _host_to_cidr(host: str) -> str | None:
    """Normalize an allowlist host to CIDR notation, or None if not an IP/CIDR.

    Bare IPs become single-host CIDRs (``/32`` or ``/128``); hostnames return
    None since Kubernetes NetworkPolicy egress rules cannot match them.
    """
    host = host.strip()
    if not host:
        return None
    if "/" in host:
        try:
            return str(ipaddress.ip_network(host, strict=False))
        except ValueError:
            return None
    try:
        ip = ipaddress.ip_address(host)
    except ValueError:
        return None
    return f"{host}/32" if ip.version == 4 else f"{host}/128"
