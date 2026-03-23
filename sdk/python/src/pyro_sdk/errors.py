"""Error types for the Pyro SDK."""


class PyroError(Exception):
    """Base error for all Pyro SDK errors."""

    def __init__(self, message: str, status_code: int | None = None):
        super().__init__(message)
        self.status_code = status_code


class AuthError(PyroError):
    """Raised on 401 — invalid or missing API key."""

    def __init__(self, message: str = "Invalid or missing API key"):
        super().__init__(message, status_code=401)


class QuotaError(PyroError):
    """Raised on 429 — rate limit or max sandboxes exceeded."""

    def __init__(self, message: str = "Quota exceeded"):
        super().__init__(message, status_code=429)


class SandboxNotFoundError(PyroError):
    """Raised on 404 — sandbox expired or invalid ID."""

    def __init__(self, sandbox_id: str):
        super().__init__(f"Sandbox not found: {sandbox_id}", status_code=404)
        self.sandbox_id = sandbox_id


class SandboxTimeoutError(PyroError):
    """Raised when waiting for a sandbox times out."""

    def __init__(self, message: str = "Sandbox operation timed out"):
        super().__init__(message)


class ExecError(PyroError):
    """Raised when a command exits with non-zero status."""

    def __init__(self, exit_code: int, stdout: str, stderr: str):
        super().__init__(f"Command exited with code {exit_code}")
        self.exit_code = exit_code
        self.stdout = stdout
        self.stderr = stderr


class ServerError(PyroError):
    """Raised on 500 — unexpected server error."""

    def __init__(self, message: str = "Internal server error"):
        super().__init__(message, status_code=500)
