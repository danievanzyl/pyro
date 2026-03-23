/** Base error for all Pyro SDK errors. */
export class PyroError extends Error {
  statusCode?: number;
  constructor(message: string, statusCode?: number) {
    super(message);
    this.name = "PyroError";
    this.statusCode = statusCode;
  }
}

/** Raised on 401 — invalid or missing API key. */
export class AuthError extends PyroError {
  constructor(message = "Invalid or missing API key") {
    super(message, 401);
    this.name = "AuthError";
  }
}

/** Raised on 429 — rate limit or max sandboxes exceeded. */
export class QuotaError extends PyroError {
  constructor(message = "Quota exceeded") {
    super(message, 429);
    this.name = "QuotaError";
  }
}

/** Raised on 404 — sandbox expired or invalid ID. */
export class SandboxNotFoundError extends PyroError {
  sandboxId: string;
  constructor(sandboxId: string) {
    super(`Sandbox not found: ${sandboxId}`, 404);
    this.name = "SandboxNotFoundError";
    this.sandboxId = sandboxId;
  }
}

/** Raised when waiting for a sandbox times out. */
export class SandboxTimeoutError extends PyroError {
  constructor(message = "Sandbox operation timed out") {
    super(message);
    this.name = "SandboxTimeoutError";
  }
}

/** Raised when a command exits with non-zero status. */
export class ExecError extends PyroError {
  exitCode: number;
  stdout: string;
  stderr: string;
  constructor(exitCode: number, stdout: string, stderr: string) {
    super(`Command exited with code ${exitCode}`);
    this.name = "ExecError";
    this.exitCode = exitCode;
    this.stdout = stdout;
    this.stderr = stderr;
  }
}

/** Raised on 500 — unexpected server error. */
export class ServerError extends PyroError {
  constructor(message = "Internal server error") {
    super(message, 500);
    this.name = "ServerError";
  }
}
