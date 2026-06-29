"""Pluggable authentication for the ARL SDK.

The gateway accepts a bearer credential in the ``Authorization`` header. The
credential can be a long-lived ARL API key (direct access to the ARL gateway)
or, when fronted by an OIDC gateway, a short-lived JWT that must be refreshed.

These are modelled as ``httpx.Auth`` flows so the credential is applied per
request rather than frozen at client construction — a prerequisite for token
refresh. ``ApiKeyAuth`` is the static case used for direct ARL access.
"""

from __future__ import annotations

import threading
import time
from collections.abc import Generator

import httpx


class ApiKeyAuth(httpx.Auth):
    """Static API-key bearer auth.

    Sends ``Authorization: Bearer <api_key>`` on every request. Use this for
    direct access to the ARL gateway with a long-lived API key.
    """

    def __init__(self, api_key: str) -> None:
        if not api_key:
            raise ValueError("api_key must be a non-empty string")
        self._api_key = api_key

    def auth_flow(self, request: httpx.Request) -> Generator[httpx.Request, httpx.Response, None]:
        request.headers["Authorization"] = f"Bearer {self._api_key}"
        yield request


class SsoTokenAuth(httpx.Auth):
    """OIDC client-credentials bearer auth with caching and refresh.

    Exchanges a ``client_id`` / ``client_secret`` at the SSO token endpoint for
    a short-lived JWT, caches it, refreshes proactively before expiry, and
    reactively re-mints on a single upstream ``401``. Use this for the
    front-door path where an OIDC gateway validates SSO-issued JWTs (rather than
    direct ARL access with a long-lived API key).

    The token is fetched lazily on the first request and shared across requests;
    fetching is guarded by a lock so concurrent requests mint at most one token.
    """

    def __init__(
        self,
        token_url: str,
        client_id: str,
        client_secret: str,
        *,
        scope: str | None = None,
        expiry_margin: float = 30.0,
        timeout: float = 30.0,
        verify: bool | str = True,
    ) -> None:
        if not token_url:
            raise ValueError("token_url must be a non-empty string")
        if not client_id:
            raise ValueError("client_id must be a non-empty string")
        if not client_secret:
            raise ValueError("client_secret must be a non-empty string")
        self._token_url = token_url
        self._client_id = client_id
        self._client_secret = client_secret
        self._scope = scope
        self._expiry_margin = expiry_margin
        self._timeout = timeout
        # TLS verification for the token request. The SSO endpoint may sit
        # behind an edge that terminates TLS with an internal/self-signed CA
        # (e.g. https://<edge-eip>:8082/token) — pass a CA bundle path, or
        # False to disable verification on a trusted network.
        self._verify = verify
        self._lock = threading.Lock()
        self._token: str | None = None
        self._expires_at = 0.0

    def _fetch_token(self) -> str:
        data = {
            "grant_type": "client_credentials",
            "client_id": self._client_id,
            "client_secret": self._client_secret,
        }
        if self._scope:
            data["scope"] = self._scope
        resp = httpx.post(self._token_url, data=data, timeout=self._timeout, verify=self._verify)
        resp.raise_for_status()
        payload = resp.json()
        raw_token = payload.get("access_token")
        if not isinstance(raw_token, str) or not raw_token:
            raise ValueError("token endpoint response missing 'access_token'")
        token: str = raw_token
        # Default to a conservative 1h lifetime if the server omits expires_in.
        expires_in = float(payload.get("expires_in", 3600))
        self._token = token
        self._expires_at = time.monotonic() + expires_in
        return token

    def _valid_token(self, *, force_refresh: bool = False) -> str:
        with self._lock:
            stale = time.monotonic() >= self._expires_at - self._expiry_margin
            if force_refresh or self._token is None or stale:
                return self._fetch_token()
            return self._token

    def auth_flow(self, request: httpx.Request) -> Generator[httpx.Request, httpx.Response, None]:
        request.headers["Authorization"] = f"Bearer {self._valid_token()}"
        response = yield request
        if response.status_code == 401:
            # The cached token may have been revoked or rotated upstream; mint a
            # fresh one and replay the request exactly once.
            request.headers["Authorization"] = f"Bearer {self._valid_token(force_refresh=True)}"
            yield request


def resolve_auth(auth: httpx.Auth | None, api_key: str | None) -> httpx.Auth | None:
    """Resolve the auth flow from the explicit ``auth`` or an ``api_key``.

    Exactly one source may be provided. Returns ``None`` (unauthenticated) when
    neither yields a credential, preserving the prior behaviour where an empty
    key means no ``Authorization`` header.
    """
    if auth is not None:
        if api_key:
            raise ValueError("pass either 'auth' or 'api_key', not both")
        return auth
    if api_key:
        return ApiKeyAuth(api_key)
    return None
