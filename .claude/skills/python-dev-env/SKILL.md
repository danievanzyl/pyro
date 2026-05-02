---
name: python-dev-env
description: >
  Bootstrap the Python SDK dev env (sdk/python) using uv. Use whenever
  running, editing, or testing pyro_sdk — pytest, respx, pytest-asyncio,
  or imports of pyro_sdk in a fresh worktree. Triggers: "set up python
  env", "install python deps", "run python tests", "uv venv", or any
  ImportError on httpx/respx/pytest_asyncio.
---

# Python dev env via uv

Single source of truth for spinning up `sdk/python` in any worktree.
Replaces ad-hoc `python3 -m venv` + `pip install` flows.

## TL;DR

```bash
cd sdk/python
uv venv                       # creates .venv (Python from pyproject)
uv pip install -e ".[dev]"    # editable + dev extras (pytest, respx, ...)
uv run pytest tests/test_images.py -x   # run tests in venv
```

## Why uv (not pip directly)

- macOS Homebrew Python is PEP 668 externally-managed → bare `pip install`
  errors with "externally-managed-environment". uv sidesteps this.
- `uv venv` is ~10x faster than `python -m venv`.
- `uv pip` resolves and installs in one pass.
- `uv run` auto-activates the venv per command — no `source` dance.

## Bootstrap (first run)

Check uv is on PATH:

```bash
command -v uv || curl -LsSf https://astral.sh/uv/install.sh | sh
```

Homebrew alt: `brew install uv`.

## Standard layout

The SDK lives at `sdk/python/`. pyproject.toml declares:

- `dependencies = ["httpx>=0.25.0"]`
- `[project.optional-dependencies] dev = ["pytest", "pytest-asyncio", "respx"]`

Editable install picks up `src/pyro_sdk/` automatically (hatchling
build target already configured).

## Common ops

```bash
# Fresh venv
cd sdk/python && uv venv && uv pip install -e ".[dev]"

# Run unit tests (skip live-server integration suite)
cd sdk/python && uv run pytest tests/ --ignore=tests/test_integration.py -x

# Run integration tests (needs PYRO_API_KEY + PYRO_BASE_URL)
cd sdk/python && uv run pytest tests/test_integration.py -x

# Quick import smoke
cd sdk/python && uv run python -c "from pyro_sdk import Pyro; print('ok')"

# Add a new dev dep — edit pyproject.toml then:
cd sdk/python && uv pip install -e ".[dev]"
```

## Trip-wires (failures already hit, do not repeat)

1. **`pip install -e "sdk/python[dev]"` from repo root → "not a valid
   editable requirement"**. Path-with-extras is brittle. Always `cd
   sdk/python` then `-e ".[dev]"`.

2. **Bare `pip install` outside venv → PEP 668 error**. Use
   `uv venv` first, never `--break-system-packages`.

3. **zsh eats `[dev]` extras unquoted**. Always wrap in double quotes:
   `uv pip install -e ".[dev]"`.

4. **Tests fail with `ModuleNotFoundError: No module named 'pyro_sdk'`**
   → editable install missing. Re-run `uv pip install -e ".[dev]"`
   inside `sdk/python`.

5. **`.venv` is gitignored at repo root** (`sdk/python/.venv/`). Never
   commit it. `__pycache__` also gitignored.

## Where files live

```
sdk/python/
├── pyproject.toml          # deps + dev extras
├── src/pyro_sdk/           # importable package
│   ├── client.py           # Pyro class, namespace wiring
│   ├── images.py           # Images + PullOperation
│   ├── sandbox.py          # Sandbox class
│   ├── models.py           # ImageInfo, SandboxInfo, ExecResult
│   └── errors.py           # PyroError + subclasses
├── tests/
│   ├── test_images.py      # respx-backed unit tests
│   └── test_integration.py # live-server, gated on env vars
└── .venv/                  # uv venv (gitignored)
```

## Test conventions

- respx for httpx mocking — already in dev extras.
- `pytest-asyncio` strict mode (`asyncio_mode = "strict"` in
  pyproject) — every async test needs `@pytest.mark.asyncio`.
- Monkey-patch instance methods to inject test seams (e.g.
  `pyro._sse_image_events = _fake_sse`) — don't mock imports.
- Use `BASE = "http://test.invalid"` for fake-server URLs.

## When pyproject changes

Editable installs do NOT auto-pick-up new dev deps. After editing
`[project.optional-dependencies]` re-run:

```bash
cd sdk/python && uv pip install -e ".[dev]"
```

For runtime deps (in `[project] dependencies`) the same command also
suffices — uv resolves the full graph.
