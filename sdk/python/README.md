# pyrovm-sdk

Python SDK for [Pyro](https://github.com/danievanzyl/pyro) — the open-source sandbox platform for AI agents.

## Install

```bash
pip install pyrovm-sdk
```

## Quickstart

```python
import asyncio
from pyro_sdk import Pyro

async def main():
    pyro = Pyro(api_key="pk_...", base_url="http://localhost:8080")

    async with await pyro.sandbox.create(image="python") as sb:
        result = await sb.run("print('Hello from Pyro!')")
        print(result.stdout)  # "Hello from Pyro!\n"

asyncio.run(main())
```

## Configuration

| Env var | Description |
|---------|-------------|
| `PYRO_API_KEY` | API key (or pass `api_key=` to constructor) |
| `PYRO_BASE_URL` | Server URL (default: `http://localhost:8080`) |

## API

### `Pyro(api_key=, base_url=, timeout=)`
Create a client. Reads from env vars if not provided.

### `pyro.sandbox.create(image=, timeout=, vcpu=, mem_mib=)`
Create a sandbox. Returns a `Sandbox` object.

### `sandbox.run(code, language=)`
Run code. Auto-detects language from image name.

### `sandbox.exec(command, env=, workdir=, timeout=)`
Execute a command. Returns `ExecResult(exit_code, stdout, stderr)`.

### `sandbox.write_file(path, content)`
Write a file into the sandbox.

### `sandbox.read_file(path)`
Read a file from the sandbox. Returns `bytes`.

### `sandbox.stop()`
Destroy the sandbox. Called automatically when using `async with`.

## Images

Register a base image once (idempotent), then boot sandboxes from it:

```python
import asyncio
from pyro_sdk import Pyro

async def main():
    pyro = Pyro(api_key="pk_...", base_url="http://localhost:8080")

    # Idempotent: pulls if missing, attaches to in-flight pulls,
    # returns immediately if already ready.
    img = await pyro.images.ensure(name="py312", source="python:3.12")
    print(img.name, img.status, img.digest)

    async with await pyro.sandbox.create(image="py312") as sb:
        result = await sb.run("print('Hello from py312!')")
        print(result.stdout)

asyncio.run(main())
```

### `pyro.images.ensure(name=, source=, timeout=)`
Idempotent register. Same source → no-op. Different source → `ImageConflictError`.

### `pyro.images.create_and_wait(name=, source=, force=, timeout=)`
Block until the pull settles. Failures raise `ImageRegistrationError`.

### `pyro.images.create(name=, source=, force=)`
Kick off a pull, return a `PullOperation`. Use `await op.wait(timeout=...)` later.

### `pyro.images.get(name)`
Return current `ImageInfo`. 404 → `ImageNotFoundError`.
