"""Sandbox class — represents a running Pyro sandbox."""

from __future__ import annotations

from typing import TYPE_CHECKING, AsyncIterator

import httpx

from pyro_sdk.errors import ExecError, PyroError, SandboxNotFoundError
from pyro_sdk.models import ExecResult, SandboxInfo

if TYPE_CHECKING:
    from pyro_sdk.client import Pyro


class Sandbox:
    """A running Pyro sandbox. Use Pyro.sandbox.create() to get one."""

    def __init__(self, client: Pyro, info: SandboxInfo):
        self._client = client
        self.info = info
        self.id = info.id

    async def exec(
        self,
        command: list[str],
        *,
        env: dict[str, str] | None = None,
        workdir: str | None = None,
        timeout: int | None = None,
    ) -> ExecResult:
        """Execute a command in the sandbox."""
        body: dict = {"command": command}
        if env:
            body["env"] = env
        if workdir:
            body["workdir"] = workdir
        if timeout:
            body["timeout"] = timeout

        data = await self._client._request(
            "POST", f"/sandboxes/{self.id}/exec", json=body
        )
        return ExecResult(
            exit_code=data["exit_code"],
            stdout=data.get("stdout", ""),
            stderr=data.get("stderr", ""),
        )

    async def run(self, code: str, *, language: str | None = None) -> ExecResult:
        """Run code in the sandbox. Infers interpreter from image if not specified."""
        lang = language or self._infer_language()
        if lang == "python":
            return await self.exec(["python3", "-c", code])
        elif lang == "node":
            return await self.exec(["node", "-e", code])
        else:
            return await self.exec(["sh", "-c", code])

    async def write_file(self, path: str, content: str | bytes) -> int:
        """Write a file into the sandbox. Returns bytes written."""
        headers = {}
        if isinstance(content, bytes):
            headers["Content-Type"] = "application/octet-stream"
        else:
            headers["Content-Type"] = "text/plain"
            content = content.encode()

        resp = await self._client._http.put(
            f"{self._client._base_url}/api/sandboxes/{self.id}/files{path}",
            content=content,
            headers={**self._client._headers(), **headers},
        )
        self._client._check_response(resp)
        data = resp.json()
        return data.get("bytes_written", len(content))

    async def read_file(self, path: str) -> bytes:
        """Read a file from the sandbox."""
        resp = await self._client._http.get(
            f"{self._client._base_url}/api/sandboxes/{self.id}/files{path}",
            headers=self._client._headers(),
        )
        self._client._check_response(resp)
        return resp.content

    async def stop(self) -> None:
        """Destroy this sandbox."""
        await self._client._request("DELETE", f"/sandboxes/{self.id}")

    async def status(self) -> SandboxInfo:
        """Get current sandbox status."""
        data = await self._client._request("GET", f"/sandboxes/{self.id}")
        self.info = SandboxInfo.from_dict(data)
        return self.info

    def _infer_language(self) -> str:
        image = self.info.image.lower()
        if "python" in image:
            return "python"
        if "node" in image:
            return "node"
        return "shell"

    async def __aenter__(self):
        return self

    async def __aexit__(self, *exc):
        await self.stop()

    def __repr__(self) -> str:
        return f"Sandbox(id={self.id!r}, image={self.info.image!r}, state={self.info.state!r})"
