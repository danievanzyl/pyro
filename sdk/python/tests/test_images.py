"""Unit tests for the Images surface — fake server via respx."""

from __future__ import annotations

import asyncio
import pytest
import respx
from httpx import Response

from pyro_sdk import (
    ImageConflictError,
    ImageNotFoundError,
    ImageRegistrationError,
    ImageTooLargeError,
    Pyro,
    PullOperation,
)


BASE = "http://test.invalid"


def _client() -> Pyro:
    return Pyro(api_key="pk_test", base_url=BASE)


# ---------------------------------------------------------------------------
# get / 404
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
@respx.mock
async def test_get_returns_image_info():
    respx.get(f"{BASE}/api/images/py312").mock(
        return_value=Response(
            200,
            json={
                "name": "py312",
                "status": "ready",
                "source": "python:3.12",
                "digest": "sha256:abc",
                "size": 12345,
                "labels": {"org.opencontainers.image.source": "x"},
            },
        )
    )
    pyro = _client()
    info = await pyro.images.get("py312")
    assert info.name == "py312"
    assert info.is_ready
    assert info.digest == "sha256:abc"
    assert info.labels == {"org.opencontainers.image.source": "x"}
    await pyro.close()


@pytest.mark.asyncio
@respx.mock
async def test_get_404_raises_image_not_found():
    respx.get(f"{BASE}/api/images/missing").mock(
        return_value=Response(404, json={"error": "image not found"})
    )
    pyro = _client()
    with pytest.raises(ImageNotFoundError):
        await pyro.images.get("missing")
    await pyro.close()


# ---------------------------------------------------------------------------
# create
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
@respx.mock
async def test_create_returns_pulling_operation():
    respx.post(f"{BASE}/api/images").mock(
        return_value=Response(
            202,
            json={
                "name": "py312",
                "status": "pulling",
                "source": "python:3.12",
            },
        )
    )
    pyro = _client()
    op = await pyro.images.create(name="py312", source="python:3.12")
    assert isinstance(op, PullOperation)
    assert op.status == "pulling"
    await pyro.close()


@pytest.mark.asyncio
@respx.mock
async def test_create_413_raises_image_too_large():
    respx.post(f"{BASE}/api/images").mock(
        return_value=Response(
            413,
            json={
                "error": "image too large",
                "limit_mb": 4096,
                "estimated_mb": 9001,
            },
        )
    )
    pyro = _client()
    with pytest.raises(ImageTooLargeError) as exc:
        await pyro.images.create(name="big", source="cuda:devel")
    assert exc.value.limit_mb == 4096
    assert exc.value.estimated_mb == 9001
    await pyro.close()


@pytest.mark.asyncio
@respx.mock
async def test_create_409_raises_image_conflict():
    respx.post(f"{BASE}/api/images").mock(
        return_value=Response(
            409,
            json={"error": "image name in use with a different source"},
        )
    )
    pyro = _client()
    with pytest.raises(ImageConflictError):
        await pyro.images.create(name="py", source="python:3.13")
    await pyro.close()


# ---------------------------------------------------------------------------
# create_and_wait — happy / failure / timeout
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
@respx.mock
async def test_create_and_wait_happy_path(monkeypatch):
    respx.post(f"{BASE}/api/images").mock(
        return_value=Response(
            202,
            json={"name": "py312", "status": "pulling", "source": "python:3.12"},
        )
    )
    respx.get(f"{BASE}/api/images/py312").mock(
        return_value=Response(
            200,
            json={
                "name": "py312",
                "status": "ready",
                "source": "python:3.12",
                "digest": "sha256:abc",
                "size": 100,
            },
        )
    )
    # Force SSE to fail so we exercise the polling path.
    pyro = _client()

    async def _fail(*args, **kwargs):
        raise RuntimeError("sse disabled in this test")
        yield  # pragma: no cover

    monkeypatch.setattr(pyro, "_sse_image_events", _fail)

    info = await pyro.images.create_and_wait(
        name="py312", source="python:3.12"
    )
    assert info.is_ready
    assert info.digest == "sha256:abc"
    await pyro.close()


@pytest.mark.asyncio
@respx.mock
async def test_create_and_wait_failure_path(monkeypatch):
    respx.post(f"{BASE}/api/images").mock(
        return_value=Response(
            202, json={"name": "x", "status": "pulling", "source": "img:latest"}
        )
    )
    respx.get(f"{BASE}/api/images/x").mock(
        return_value=Response(
            200,
            json={
                "name": "x",
                "status": "failed",
                "error": "manifest unknown",
            },
        )
    )
    pyro = _client()

    async def _fail(*args, **kwargs):
        raise RuntimeError("sse off")
        yield  # pragma: no cover

    monkeypatch.setattr(pyro, "_sse_image_events", _fail)

    with pytest.raises(ImageRegistrationError) as exc:
        await pyro.images.create_and_wait(name="x", source="img:latest")
    assert "manifest unknown" in str(exc.value)
    await pyro.close()


@pytest.mark.asyncio
@respx.mock
async def test_create_and_wait_timeout(monkeypatch):
    respx.post(f"{BASE}/api/images").mock(
        return_value=Response(
            202, json={"name": "slow", "status": "pulling", "source": "img:1"}
        )
    )
    # Always pulling — never reaches ready.
    respx.get(f"{BASE}/api/images/slow").mock(
        return_value=Response(
            200, json={"name": "slow", "status": "pulling", "source": "img:1"}
        )
    )
    pyro = _client()

    async def _fail(*args, **kwargs):
        raise RuntimeError("sse off")
        yield  # pragma: no cover

    monkeypatch.setattr(pyro, "_sse_image_events", _fail)

    with pytest.raises(TimeoutError):
        await pyro.images.create_and_wait(
            name="slow", source="img:1", timeout=0.05
        )
    await pyro.close()


# ---------------------------------------------------------------------------
# ensure
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
@respx.mock
async def test_ensure_existing_ready_is_noop():
    """Existing ready image with matching source → no POST, return existing."""
    get_route = respx.get(f"{BASE}/api/images/py312").mock(
        return_value=Response(
            200,
            json={
                "name": "py312",
                "status": "ready",
                "source": "python:3.12",
            },
        )
    )
    post_route = respx.post(f"{BASE}/api/images").mock(
        return_value=Response(500)
    )
    pyro = _client()
    info = await pyro.images.ensure(name="py312", source="python:3.12")
    assert info.is_ready
    assert get_route.call_count == 1
    assert post_route.call_count == 0
    await pyro.close()


@pytest.mark.asyncio
@respx.mock
async def test_ensure_source_mismatch_raises_conflict():
    respx.get(f"{BASE}/api/images/py").mock(
        return_value=Response(
            200,
            json={"name": "py", "status": "ready", "source": "python:3.12"},
        )
    )
    pyro = _client()
    with pytest.raises(ImageConflictError) as exc:
        await pyro.images.ensure(name="py", source="python:3.13")
    assert exc.value.existing_source == "python:3.12"
    assert exc.value.requested_source == "python:3.13"
    await pyro.close()


@pytest.mark.asyncio
@respx.mock
async def test_ensure_fresh_registers_then_waits(monkeypatch):
    """404 from get → POST → wait → ready."""
    respx.get(f"{BASE}/api/images/new").mock(
        side_effect=[
            Response(404, json={"error": "not found"}),
            Response(
                200,
                json={
                    "name": "new",
                    "status": "ready",
                    "source": "x:1",
                },
            ),
        ]
    )
    respx.post(f"{BASE}/api/images").mock(
        return_value=Response(
            202, json={"name": "new", "status": "pulling", "source": "x:1"}
        )
    )
    pyro = _client()

    async def _fail(*args, **kwargs):
        raise RuntimeError("sse off")
        yield  # pragma: no cover

    monkeypatch.setattr(pyro, "_sse_image_events", _fail)

    info = await pyro.images.ensure(name="new", source="x:1")
    assert info.is_ready
    await pyro.close()


# ---------------------------------------------------------------------------
# wait() prefers SSE over polling
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
@respx.mock
async def test_wait_prefers_sse_over_polling():
    """Acceptance: SSE is consulted before any polling fallback."""
    respx.post(f"{BASE}/api/images").mock(
        return_value=Response(
            202, json={"name": "p", "status": "pulling", "source": "py:1"}
        )
    )
    poll_route = respx.get(f"{BASE}/api/images/p").mock(
        return_value=Response(
            200,
            json={"name": "p", "status": "ready", "source": "py:1", "size": 1},
        )
    )

    pyro = _client()
    sse_called = []

    async def _fake_sse(name, timeout=None):
        sse_called.append(name)
        yield "image.ready", {"name": name}

    pyro._sse_image_events = _fake_sse

    op = await pyro.images.create(name="p", source="py:1")
    info = await op.wait()
    assert info.is_ready
    assert sse_called == ["p"]
    # Polling GET is only invoked once — the post-SSE refetch — not as a poll.
    assert poll_route.call_count == 1
    await pyro.close()


@pytest.mark.asyncio
@respx.mock
async def test_wait_sse_failed_event_raises():
    respx.post(f"{BASE}/api/images").mock(
        return_value=Response(
            202, json={"name": "p", "status": "pulling", "source": "x:1"}
        )
    )
    pyro = _client()

    async def _fake_sse(name, timeout=None):
        yield "image.failed", {"name": name, "error": "boom"}

    pyro._sse_image_events = _fake_sse

    op = await pyro.images.create(name="p", source="x:1")
    with pytest.raises(ImageRegistrationError) as exc:
        await op.wait()
    assert "boom" in str(exc.value)
    await pyro.close()
