"""Pyro client — main entry point for the SDK."""

from __future__ import annotations

import os
from typing import Any

import httpx

from pyro_sdk.errors import (
    AuthError,
    PyroError,
    QuotaError,
    SandboxNotFoundError,
    ServerError,
)
from pyro_sdk.models import SandboxInfo
from pyro_sdk.sandbox import Sandbox


class _SandboxNamespace:
    """Namespace for sandbox operations: pyro.sandbox.create(), etc."""

    def __init__(self, client: Pyro):
        self._client = client

    async def create(
        self,
        *,
        image: str = "default",
        timeout: int = 3600,
        vcpu: int = 0,
        mem_mib: int = 0,
        scratch_size_mib: int = 0,
    ) -> Sandbox:
        """Create a new sandbox.

        Args:
            image: Base image name (default, ubuntu, python, node, minimal).
            timeout: TTL in seconds (default 3600 = 1 hour).
            vcpu: vCPU count (0 = server default).
            mem_mib: Memory in MiB (0 = server default).
            scratch_size_mib: Ephemeral scratch disk in MiB (0 = none).
                Mounted at /scratch with overlayfs on /usr, /var, /etc
                so package installs write to scratch instead of rootfs.

        Returns:
            A Sandbox instance ready for use.
        """
        body: dict[str, Any] = {"ttl": timeout, "image": image}
        if vcpu > 0:
            body["vcpu"] = vcpu
        if mem_mib > 0:
            body["mem_mib"] = mem_mib
        if scratch_size_mib > 0:
            body["scratch_size_mib"] = scratch_size_mib

        data = await self._client._request("POST", "/sandboxes", json=body)
        info = SandboxInfo.from_dict(data)
        return Sandbox(self._client, info)

    async def list(self) -> list[Sandbox]:
        """List all active sandboxes."""
        data = await self._client._request("GET", "/sandboxes")
        return [
            Sandbox(self._client, SandboxInfo.from_dict(sb)) for sb in data
        ]

    async def get(self, sandbox_id: str) -> Sandbox:
        """Get a sandbox by ID."""
        data = await self._client._request("GET", f"/sandboxes/{sandbox_id}")
        return Sandbox(self._client, SandboxInfo.from_dict(data))


class Pyro:
    """Pyro SDK client.

    Usage::

        from pyro_sdk import Pyro

        pyro = Pyro(api_key="pk_...", base_url="http://localhost:8080")

        async with await pyro.sandbox.create(image="python") as sb:
            result = await sb.run("print('hello')")
            print(result.stdout)
    """

    def __init__(
        self,
        *,
        api_key: str | None = None,
        base_url: str | None = None,
        timeout: float = 30.0,
    ):
        self._api_key = api_key or os.environ.get("PYRO_API_KEY", "")
        self._base_url = (
            base_url or os.environ.get("PYRO_BASE_URL", "http://localhost:8080")
        ).rstrip("/")
        self._http = httpx.AsyncClient(timeout=timeout)
        self.sandbox = _SandboxNamespace(self)

    def _headers(self) -> dict[str, str]:
        return {
            "Authorization": f"Bearer {self._api_key}",
            "Content-Type": "application/json",
        }

    async def _request(
        self, method: str, path: str, **kwargs: Any
    ) -> Any:
        """Make an authenticated API request."""
        url = f"{self._base_url}/api{path}"
        resp = await self._http.request(
            method, url, headers=self._headers(), **kwargs
        )
        self._check_response(resp)
        if resp.status_code == 204:
            return None
        return resp.json()

    def _check_response(self, resp: httpx.Response) -> None:
        """Raise appropriate error for non-2xx responses."""
        if resp.is_success:
            return

        try:
            body = resp.json()
            message = body.get("error", resp.text)
        except Exception:
            message = resp.text

        if resp.status_code == 401:
            raise AuthError(message)
        elif resp.status_code == 404:
            raise SandboxNotFoundError(message)
        elif resp.status_code == 429:
            raise QuotaError(message)
        elif resp.status_code >= 500:
            raise ServerError(message)
        else:
            raise PyroError(message, status_code=resp.status_code)

    async def health(self) -> dict:
        """Check server health."""
        resp = await self._http.get(f"{self._base_url}/api/health")
        return resp.json()

    async def close(self) -> None:
        """Close the HTTP client."""
        await self._http.aclose()

    async def __aenter__(self):
        return self

    async def __aexit__(self, *exc):
        await self.close()
