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


class ImageError(PyroError):
    """Base error for image-management failures."""


class ImageNotFoundError(ImageError):
    """Raised when GET /images/{name} returns 404."""

    def __init__(self, name: str):
        super().__init__(f"image not found: {name}", status_code=404)
        self.image_name = name


class ImageRegistrationError(ImageError):
    """Raised when a pull terminates in `failed` state."""

    def __init__(self, name: str, message: str):
        super().__init__(f"image {name!r} registration failed: {message}")
        self.image_name = name
        self.server_message = message


class ImageConflictError(ImageError):
    """Raised when an existing image's source disagrees with the requested source.

    `ensure()` raises this rather than silently re-pulling. Pass `force=True`
    to `create()` to replace.
    """

    def __init__(
        self, name: str, existing_source: str, requested_source: str
    ):
        super().__init__(
            f"image {name!r} already registered with source "
            f"{existing_source!r}; requested {requested_source!r}",
            status_code=409,
        )
        self.image_name = name
        self.existing_source = existing_source
        self.requested_source = requested_source


class ImageTooLargeError(ImageError):
    """Raised on 413 — pull would exceed the server's disk cap."""

    def __init__(self, name: str, limit_mb: int, estimated_mb: int):
        super().__init__(
            f"image {name!r} too large: "
            f"~{estimated_mb} MB exceeds limit {limit_mb} MB",
            status_code=413,
        )
        self.image_name = name
        self.limit_mb = limit_mb
        self.estimated_mb = estimated_mb
