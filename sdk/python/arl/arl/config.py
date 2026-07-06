"""ARL CLI config file reader — shared with Go CLI.

Reads ``~/.config/arl/config.yaml`` (or ``$XDG_CONFIG_HOME/arl/config.yaml``)
which is the same file the ``arl`` Go CLI writes. Provides the active
context's ``gateway_url`` and ``api_key`` so callers that pass neither
constructor arg nor env var still authenticate correctly.

Resolution order (matches Go CLI):
  1. Explicit constructor arg (``api_key=``, ``base_url=``)
  2. Environment variable (``ARL_API_KEY``, ``ARL_GATEWAY_URL``)
  3. Active config-file context
  4. Hard-coded default (``http://localhost:8080``, no auth)
"""

from __future__ import annotations

import os
from contextlib import suppress
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any


@dataclass(frozen=True)
class ContextConfig:
    gateway_url: str = ""
    api_key: str = ""
    api_key_file: str = ""


@dataclass
class ArlConfig:
    current_context: str = ""
    contexts: dict[str, ContextConfig] = field(default_factory=dict)

    def active_context(self, override: str = "") -> ContextConfig:
        name = override or self.current_context or "default"
        return self.contexts.get(name, ContextConfig())


def _config_dir() -> Path | None:
    xdg = os.environ.get("XDG_CONFIG_HOME")
    if xdg:
        return Path(xdg) / "arl"
    home = Path.home()
    return home / ".config" / "arl"


def load_config() -> ArlConfig:
    """Load the ARL config file, returning an empty config on any error."""
    cfg_dir = _config_dir()
    if cfg_dir is None:
        return ArlConfig()
    path = cfg_dir / "config.yaml"
    if not path.is_file():
        return ArlConfig()
    try:
        import yaml

        raw: dict[str, Any] = yaml.safe_load(path.read_text()) or {}
    except Exception:
        return ArlConfig()

    contexts: dict[str, ContextConfig] = {}
    for name, ctx_raw in (raw.get("contexts") or {}).items():
        if not isinstance(ctx_raw, dict):
            continue
        contexts[name] = ContextConfig(
            gateway_url=str(ctx_raw.get("gateway_url", "") or ""),
            api_key=str(ctx_raw.get("api_key", "") or ""),
            api_key_file=str(ctx_raw.get("api_key_file", "") or ""),
        )

    # Migrate legacy flat fields (same logic as Go CLI).
    gw = str(raw.get("gateway_url", "") or "").strip()
    key = str(raw.get("api_key", "") or "").strip()
    key_file = str(raw.get("api_key_file", "") or "").strip()
    if (gw or key or key_file) and "default" not in contexts:
        contexts["default"] = ContextConfig(
            gateway_url=gw,
            api_key=key,
            api_key_file=key_file,
        )

    return ArlConfig(
        current_context=str(raw.get("current_context", "") or ""),
        contexts=contexts,
    )


def resolve_from_config(
    *,
    gateway_url: str = "",
    api_key: str = "",
    context: str = "",
) -> tuple[str, str]:
    """Resolve gateway_url and api_key using the config-file fallback chain.

    Returns (gateway_url, api_key) after applying:
      arg > env var > config-file context > default.
    """
    cfg = load_config()
    ctx_override = context or os.environ.get("ARL_CONTEXT", "")
    ctx = cfg.active_context(ctx_override)

    resolved_url = (
        gateway_url
        or os.environ.get("ARL_GATEWAY_URL", "")
        or ctx.gateway_url
        or "http://localhost:8080"
    )

    resolved_key = api_key or os.environ.get("ARL_API_KEY", "")
    if not resolved_key and ctx.api_key_file:
        with suppress(Exception):
            resolved_key = Path(ctx.api_key_file).read_text().strip()
    if not resolved_key:
        resolved_key = ctx.api_key

    return resolved_url, resolved_key
