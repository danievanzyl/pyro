# @pyrovm/sdk

TypeScript SDK for [Pyro](https://github.com/danievanzyl/pyro) — the open-source sandbox platform for AI agents.

## Install

```bash
npm install @pyrovm/sdk
```

## Quickstart

```typescript
import { Pyro } from '@pyrovm/sdk'

const pyro = new Pyro({ apiKey: 'pk_...', baseUrl: 'http://localhost:8080' })

const sandbox = await pyro.sandbox.create({ image: 'python', timeout: 300 })
const result = await sandbox.run('print("Hello from Pyro!")')
console.log(result.stdout) // "Hello from Pyro!\n"
await sandbox.stop()
```

## Configuration

| Env var | Description |
|---------|-------------|
| `PYRO_API_KEY` | API key (or pass `apiKey` to constructor) |
| `PYRO_BASE_URL` | Server URL (default: `http://localhost:8080`) |

## API

### `new Pyro(config?)`

Create a client. Config fields: `apiKey`, `baseUrl`, `timeout` (ms, default 30000).

### `pyro.sandbox.create(options?)`

Create a sandbox. Options: `image`, `timeout` (seconds), `vcpu`, `memMib`. Returns `Sandbox`.

### `pyro.sandbox.list()`

List active sandboxes. Returns `Sandbox[]`.

### `pyro.sandbox.get(id)`

Get a sandbox by ID. Returns `Sandbox`.

### `sandbox.exec(command, options?)`

Execute a command. Returns `ExecResult { exitCode, stdout, stderr }`.

### `sandbox.run(code, language?)`

Run code. Auto-detects language from image name.

### `sandbox.writeFile(path, content)`

Write a string or `Uint8Array` into the sandbox.

### `sandbox.readFile(path)`

Read a file from the sandbox. Returns `Uint8Array`.

### `sandbox.stop()`

Destroy the sandbox.

### `pyro.health()`

Check server health. Returns `{ status, active_sandboxes }`.
