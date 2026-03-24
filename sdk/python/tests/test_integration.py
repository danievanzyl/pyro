"""Integration tests for pyrovm-sdk against a live Pyro server.

Requires PYRO_API_KEY and PYRO_BASE_URL env vars.
"""

import os
import pytest
import pytest_asyncio

from pyro_sdk import Pyro, AuthError, SandboxNotFoundError


BASE_URL = os.environ.get("PYRO_BASE_URL", "http://localhost:8080")
API_KEY = os.environ.get("PYRO_API_KEY", "")


@pytest_asyncio.fixture
async def client():
    c = Pyro(api_key=API_KEY, base_url=BASE_URL, timeout=60.0)
    yield c
    await c.close()


@pytest.mark.asyncio
async def test_health(client):
    h = await client.health()
    assert h["status"] == "ok"


@pytest.mark.asyncio
async def test_auth_error():
    c = Pyro(api_key="pk_invalid", base_url=BASE_URL)
    with pytest.raises(AuthError):
        await c.sandbox.list()
    await c.close()


@pytest.mark.asyncio
async def test_sandbox_lifecycle(client):
    # Create
    sb = await client.sandbox.create(image="minimal", timeout=120)
    assert sb.id
    assert sb.info.state == "running"

    # List
    sandboxes = await client.sandbox.list()
    ids = [s.id for s in sandboxes]
    assert sb.id in ids

    # Get
    fetched = await client.sandbox.get(sb.id)
    assert fetched.id == sb.id

    # Exec
    result = await sb.exec(["echo", "hello"])
    assert result.exit_code == 0
    assert "hello" in result.stdout

    # Run (shell)
    result = await sb.run("echo world")
    assert result.exit_code == 0
    assert "world" in result.stdout

    # File write/read
    await sb.write_file("/tmp/test.txt", "pyro test")
    data = await sb.read_file("/tmp/test.txt")
    assert data == b"pyro test"

    # Status
    info = await sb.status()
    assert info.state == "running"

    # Stop
    await sb.stop()


@pytest.mark.asyncio
async def test_sandbox_not_found(client):
    with pytest.raises(SandboxNotFoundError):
        await client.sandbox.get("nonexistent-id-12345")


@pytest.mark.asyncio
async def test_context_manager(client):
    async with await client.sandbox.create(image="minimal", timeout=60) as sb:
        result = await sb.exec(["hostname"])
        assert result.exit_code == 0
        assert "pyro" in result.stdout
    # sandbox should be destroyed after exiting context
