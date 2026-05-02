"""Image registration surface for the Pyro SDK.

Three caller personas:

* notebook / script — `pyro.images.ensure(name=..., source=...)`
* CI / build       — `pyro.images.create_and_wait(...)`
* service          — `op = await pyro.images.create(...); await op.wait()`

`PullOperation.wait()` prefers the SSE `/events` stream and falls back to
interval polling on `GET /images/{name}` if SSE is unreachable.
"""

from __future__ import annotations

import asyncio
import json
import time
from typing import TYPE_CHECKING, Any, AsyncIterator

import httpx

from pyro_sdk.errors import (
    ImageConflictError,
    ImageNotFoundError,
    ImageRegistrationError,
    ImageTooLargeError,
    PyroError,
    SandboxNotFoundError,
)
from pyro_sdk.models import ImageInfo

if TYPE_CHECKING:
    from pyro_sdk.client import Pyro


# Polling cadence: 1s start, exponential to 5s ceiling.
_POLL_INITIAL = 1.0
_POLL_CEILING = 5.0
_POLL_FACTOR = 1.5

# SSE stream is treated as a dependency to be probed; if the connection or
# initial read fails we skip to polling rather than crash the whole wait().
_SSE_CONNECT_TIMEOUT = 5.0


class PullOperation:
    """Handle for an in-flight (or already-ready) image registration.

    Returned by `Images.create()`. Call `await op.wait()` to block until the
    pull terminates.
    """

    def __init__(self, client: Pyro, info: ImageInfo):
        self._client = client
        self._info = info
        self.name = info.name

    @property
    def status(self) -> str:
        return self._info.status

    @property
    def info(self) -> ImageInfo:
        return self._info

    async def wait(self, timeout: float | None = None) -> ImageInfo:
        """Block until the image is ready or the pull fails.

        Tries the SSE `/events` stream first. On any SSE error, falls back
        to interval polling on `GET /images/{name}`. Raises
        `ImageRegistrationError` on terminal failure, `TimeoutError` on
        deadline.
        """
        if self._info.is_ready:
            return self._info
        if self._info.status == "failed":
            raise ImageRegistrationError(
                self.name, self._info.error or "unknown error"
            )

        deadline = None if timeout is None else time.monotonic() + timeout

        # SSE first. _wait_via_sse returns None if the stream is unreachable
        # so the caller falls through to polling.
        try:
            info = await self._wait_via_sse(deadline)
        except (ImageRegistrationError, TimeoutError):
            raise
        except Exception:
            info = None
        if info is not None:
            self._info = info
            return info

        info = await self._wait_via_polling(deadline)
        self._info = info
        return info

    async def _wait_via_sse(
        self, deadline: float | None
    ) -> ImageInfo | None:
        """Stream image lifecycle events and return on terminal state.

        Returns None if SSE is unreachable so the caller can fall back to
        polling. Raises on terminal `failed` or timeout.
        """
        sse_timeout = self._sse_remaining(deadline)
        async for event_type, data in self._client._sse_image_events(
            self.name, timeout=sse_timeout
        ):
            if data.get("name") != self.name:
                continue
            if event_type == "image.ready":
                # Re-fetch full ImageInfo (size/digest/labels).
                return await self._client._image_get(self.name)
            if event_type == "image.failed":
                raise ImageRegistrationError(
                    self.name, data.get("error") or "unknown error"
                )
            if deadline is not None and time.monotonic() >= deadline:
                raise TimeoutError(
                    f"timed out waiting for image {self.name!r}"
                )
        return None

    async def _wait_via_polling(
        self, deadline: float | None
    ) -> ImageInfo:
        sleep = _POLL_INITIAL
        while True:
            info = await self._client._image_get(self.name)
            if info.is_ready:
                return info
            if info.status == "failed":
                raise ImageRegistrationError(
                    self.name, info.error or "unknown error"
                )
            if deadline is not None:
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    raise TimeoutError(
                        f"timed out waiting for image {self.name!r}"
                    )
                sleep = min(sleep, remaining)
            await asyncio.sleep(sleep)
            sleep = min(sleep * _POLL_FACTOR, _POLL_CEILING)

    @staticmethod
    def _sse_remaining(deadline: float | None) -> float | None:
        if deadline is None:
            return None
        return max(deadline - time.monotonic(), 0.1)


class _ImagesNamespace:
    """Namespace for image operations: `pyro.images.create()`, etc."""

    def __init__(self, client: Pyro):
        self._client = client

    async def get(self, name: str) -> ImageInfo:
        """Fetch current ImageInfo. Raises ImageNotFoundError on 404."""
        return await self._client._image_get(name)

    async def create(
        self,
        *,
        name: str,
        source: str | None = None,
        dockerfile: str | None = None,
        force: bool = False,
    ) -> PullOperation:
        """Start a registration. Returns immediately with a PullOperation.

        Use `op.wait()` to block until the pull settles. Maps server errors:
        409 source-mismatch → ImageConflictError, 413 → ImageTooLargeError,
        4xx → PyroError, 5xx → ServerError.
        """
        if (source is None) == (dockerfile is None):
            raise ValueError("exactly one of source or dockerfile required")
        body: dict[str, Any] = {"name": name}
        if source is not None:
            body["source"] = source
        if dockerfile is not None:
            body["dockerfile"] = dockerfile
        if force:
            body["force"] = True

        data = await self._client._image_post(body)
        return PullOperation(self._client, ImageInfo.from_dict(data))

    async def create_and_wait(
        self,
        *,
        name: str,
        source: str | None = None,
        dockerfile: str | None = None,
        force: bool = False,
        timeout: float | None = None,
    ) -> ImageInfo:
        """Start a registration and block until ready. Surfaces failures."""
        op = await self.create(
            name=name, source=source, dockerfile=dockerfile, force=force
        )
        return await op.wait(timeout=timeout)

    async def ensure(
        self,
        *,
        name: str,
        source: str | None = None,
        dockerfile: str | None = None,
        timeout: float | None = None,
    ) -> ImageInfo:
        """Idempotent register. Attaches to in-flight pulls if any.

        - ready + same source → returns existing (no pull)
        - ready + different source → ImageConflictError
        - pulling/extracting → poll/SSE until terminal
        - failed or 404 → start a fresh pull and wait
        """
        if (source is None) == (dockerfile is None):
            raise ValueError("exactly one of source or dockerfile required")

        try:
            existing = await self._client._image_get(name)
        except ImageNotFoundError:
            existing = None

        if existing is not None:
            if existing.is_ready:
                if source is not None and existing.source and existing.source != source:
                    raise ImageConflictError(
                        name, existing.source, source
                    )
                return existing
            if existing.status in ("pulling", "extracting"):
                op = PullOperation(self._client, existing)
                return await op.wait(timeout=timeout)
            # status == "failed" → fall through and re-pull.

        op = await self.create(
            name=name, source=source, dockerfile=dockerfile
        )
        return await op.wait(timeout=timeout)


# ---------------------------------------------------------------------------
# Pyro client wiring helpers (these live here so client.py stays focused on
# the cross-cutting HTTP plumbing).
# ---------------------------------------------------------------------------


async def _image_get(client: Pyro, name: str) -> ImageInfo:
    """GET /images/{name} → ImageInfo, mapping 404 → ImageNotFoundError."""
    try:
        data = await client._request("GET", f"/images/{name}")
    except SandboxNotFoundError:
        # The shared 404 handler raises SandboxNotFoundError regardless of
        # path; rewrap so callers can match on image-specific exception.
        raise ImageNotFoundError(name)
    return ImageInfo.from_dict(data)


async def _image_post(client: Pyro, body: dict[str, Any]) -> dict[str, Any]:
    """POST /images with image-specific error mapping (409, 413).

    The shared `_request` helper has no view of these codes, so we drive the
    HTTP call directly here.
    """
    url = f"{client._base_url}/api/images"
    resp = await client._http.post(url, json=body, headers=client._headers())
    if resp.is_success:
        return resp.json()

    try:
        err_body = resp.json()
    except Exception:
        err_body = {}

    if resp.status_code == 409:
        raise ImageConflictError(
            body.get("name", ""),
            existing_source="<in-flight>",
            requested_source=body.get("source", ""),
        )
    if resp.status_code == 413:
        raise ImageTooLargeError(
            body.get("name", ""),
            limit_mb=int(err_body.get("limit_mb", 0)),
            estimated_mb=int(err_body.get("estimated_mb", 0)),
        )
    # Defer to shared mapper for 401/404/429/5xx.
    client._check_response(resp)
    raise PyroError(
        err_body.get("error", resp.text), status_code=resp.status_code
    )


async def _sse_image_events(
    client: Pyro, name: str, timeout: float | None = None
) -> AsyncIterator[tuple[str, dict[str, Any]]]:
    """Stream image lifecycle events from /events filtered to known types.

    Yields `(event_type, data_dict)`. Raises httpx errors on connection
    failure — the caller is expected to catch and fall back to polling.
    """
    if not client._api_key:
        return
    url = (
        f"{client._base_url}/api/events?api_key="
        f"{httpx.QueryParams({'k': client._api_key})['k']}"
    )

    connect_timeout = httpx.Timeout(_SSE_CONNECT_TIMEOUT, read=timeout)
    image_event_types = {
        "image.pulling",
        "image.extracting",
        "image.ready",
        "image.failed",
        "image.layer_progress",
        "image.force_replaced",
    }

    async with httpx.AsyncClient(timeout=connect_timeout) as sse:
        async with sse.stream("GET", url) as resp:
            resp.raise_for_status()
            event_type: str | None = None
            data_buf: list[str] = []
            async for line in resp.aiter_lines():
                if line == "":
                    if event_type and data_buf:
                        try:
                            payload = json.loads("\n".join(data_buf))
                        except json.JSONDecodeError:
                            payload = {}
                        if event_type in image_event_types:
                            inner = payload.get("data") or {}
                            if inner.get("name") == name:
                                yield event_type, inner
                                if event_type in (
                                    "image.ready",
                                    "image.failed",
                                ):
                                    return
                    event_type = None
                    data_buf = []
                    continue
                if line.startswith(":"):
                    continue
                if line.startswith("event:"):
                    event_type = line[len("event:"):].strip()
                elif line.startswith("data:"):
                    data_buf.append(line[len("data:"):].lstrip())
