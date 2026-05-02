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

/** Raised when an image operation times out. */
export class TimeoutError extends PyroError {
  constructor(message = "operation timed out") {
    super(message);
    this.name = "TimeoutError";
  }
}

/** Base error for image-management failures. */
export class ImageError extends PyroError {
  constructor(message: string, statusCode?: number) {
    super(message, statusCode);
    this.name = "ImageError";
  }
}

/** Raised when GET /images/{name} returns 404. */
export class ImageNotFoundError extends ImageError {
  imageName: string;
  constructor(name: string) {
    super(`image not found: ${name}`, 404);
    this.name = "ImageNotFoundError";
    this.imageName = name;
  }
}

/** Raised when a pull terminates in `failed` state. */
export class ImageRegistrationError extends ImageError {
  imageName: string;
  serverMessage: string;
  constructor(name: string, message: string) {
    super(`image '${name}' registration failed: ${message}`);
    this.name = "ImageRegistrationError";
    this.imageName = name;
    this.serverMessage = message;
  }
}

/** Raised when an existing image's source disagrees with the requested source.
 *
 * `ensure()` raises this rather than silently re-pulling. Pass `force: true`
 * to `create()` to replace.
 */
export class ImageConflictError extends ImageError {
  imageName: string;
  existingSource: string;
  requestedSource: string;
  constructor(name: string, existingSource: string, requestedSource: string) {
    super(
      `image '${name}' already registered with source '${existingSource}'; requested '${requestedSource}'`,
      409,
    );
    this.name = "ImageConflictError";
    this.imageName = name;
    this.existingSource = existingSource;
    this.requestedSource = requestedSource;
  }
}

/** Raised on 413 — pull would exceed the server's disk cap. */
export class ImageTooLargeError extends ImageError {
  imageName: string;
  limitMb: number;
  estimatedMb: number;
  constructor(name: string, limitMb: number, estimatedMb: number) {
    super(
      `image '${name}' too large: ~${estimatedMb} MB exceeds limit ${limitMb} MB`,
      413,
    );
    this.name = "ImageTooLargeError";
    this.imageName = name;
    this.limitMb = limitMb;
    this.estimatedMb = estimatedMb;
  }
}
