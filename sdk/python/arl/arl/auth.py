"""Pluggable authentication for the ARL SDK.

The gateway accepts a bearer credential in the ``Authorization`` header. The
credential can be a long-lived ARL API key (direct access to the ARL gateway)
or, when fronted by an OIDC gateway, a short-lived JWT that must be refreshed.

These are modelled as ``httpx.Auth`` flows so the credential is applied per
request rather than frozen at client construction — a prerequisite for token
refresh. ``ApiKeyAuth`` is the static case used for direct ARL access.
"""

from __future__ import annotations

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

    def auth_flow(
        self, request: httpx.Request
    ) -> Generator[httpx.Request, httpx.Response, None]:
        request.headers["Authorization"] = f"Bearer {self._api_key}"
        yield request


def resolve_auth(
    auth: httpx.Auth | None, api_key: str | None
) -> httpx.Auth | None:
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
