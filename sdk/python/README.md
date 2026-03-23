# pyro-sdk

Python SDK for [Pyro](https://github.com/danievanzyl/pyro) — the open-source sandbox platform for AI agents.

## Install

```bash
pip install pyro-sdk
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
