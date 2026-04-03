"""ConfigEnv models for WarmPool configuration payloads."""

from __future__ import annotations

from typing import Any

from pydantic import BaseModel, ConfigDict, Field


class VolumeInjection(BaseModel):
    """File-system injection target for a managed config resource."""

    container: str | None = None
    mount_path: str = Field(alias="mountPath")
    read_only: bool | None = Field(default=None, alias="readOnly")
    sub_path: str | None = Field(default=None, alias="subPath")

    model_config = ConfigDict(populate_by_name=True)


class SecretEnvVarRef(BaseModel):
    """Reference to a Secret key exposed as an environment variable."""

    name: str
    key: str


class SecretInjection(BaseModel):
    """How a Secret should be injected into the WarmPool pod."""

    volume: VolumeInjection | None = None
    as_env: list[SecretEnvVarRef] = Field(default_factory=list, alias="asEnv")

    model_config = ConfigDict(populate_by_name=True)


class ConfigMapTemplate(BaseModel):
    """A ConfigMap payload and its injection policy."""

    metadata: dict[str, Any] = Field(default_factory=dict)
    data: dict[str, str] = Field(default_factory=dict)
    binary_data: dict[str, str] = Field(default_factory=dict, alias="binaryData")
    inject: VolumeInjection | None = None

    model_config = ConfigDict(populate_by_name=True)


class SecretTemplate(BaseModel):
    """A Secret payload and its injection policy."""

    metadata: dict[str, Any] = Field(default_factory=dict)
    type: str = "Opaque"
    data: dict[str, str] = Field(default_factory=dict)
    string_data: dict[str, str] = Field(default_factory=dict, alias="stringData")
    inject: SecretInjection | None = None

    model_config = ConfigDict(populate_by_name=True)


class ConfigEnvSpec(BaseModel):
    """WarmPool environment configuration payload."""

    vars: dict[str, str] = Field(default_factory=dict)
    config_maps: list[ConfigMapTemplate | dict[str, Any]] = Field(
        default_factory=list, alias="configMaps"
    )
    secrets: list[SecretTemplate | dict[str, Any]] = Field(default_factory=list)
    env_vars: list[dict[str, Any]] = Field(default_factory=list, alias="envVars")

    model_config = ConfigDict(populate_by_name=True)

    @classmethod
    def from_k8s_resources(cls, **kwargs: Any) -> "ConfigEnvSpec":
        """Build a ConfigEnv payload from raw Kubernetes-style objects."""
        return cls(**kwargs)

    def to_request_payload(self) -> dict[str, Any]:
        """Serialize to the JSON shape expected by the gateway."""
        return self.model_dump(by_alias=True, exclude_none=True)
