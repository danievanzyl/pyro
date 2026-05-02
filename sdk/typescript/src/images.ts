/** Image registration surface for the Pyro SDK.
 *
 * Three caller personas:
 *
 * - notebook / script — `pyro.images.ensure({ name, source })`
 * - CI / build       — `pyro.images.createAndWait(...)`
 * - service          — `const op = await pyro.images.create(...); await op.wait()`
 *
 * `PullOperation.wait()` prefers the SSE `/events` stream and falls back to
 * interval polling on `GET /images/{name}` if SSE is unreachable.
 */
import type { Pyro } from "./client.js";
import {
  ImageConflictError,
  ImageNotFoundError,
  ImageRegistrationError,
  ImageTooLargeError,
  PyroError,
  ServerError,
  TimeoutError,
} from "./errors.js";
import type {
  CreateAndWaitImageOptions,
  CreateImageOptions,
  EnsureImageOptions,
  ImageInfo,
} from "./types.js";

// Polling cadence: 1s start, exponential to 5s ceiling.
const POLL_INITIAL_MS = 1000;
const POLL_CEILING_MS = 5000;
const POLL_FACTOR = 1.5;

/** Yielded by SSE generators. */
export interface ImageEvent {
  eventType: string;
  data: Record<string, unknown>;
}

/** Handle for an in-flight (or already-ready) image registration.
 *
 * Returned by `Images.create()`. Call `await op.wait()` to block until the
 * pull terminates.
 */
export class PullOperation {
  readonly name: string;
  private client: Pyro;
  private _info: ImageInfo;

  constructor(client: Pyro, info: ImageInfo) {
    this.client = client;
    this._info = info;
    this.name = info.name;
  }

  get status(): string {
    return this._info.status;
  }

  get info(): ImageInfo {
    return this._info;
  }

  /** Block until the image is ready or the pull fails.
   *
   * Tries the SSE `/events` stream first. On any SSE error, falls back
   * to interval polling on `GET /images/{name}`. Throws
   * `ImageRegistrationError` on terminal failure, `TimeoutError` on
   * deadline.
   *
   * @param timeout milliseconds; omit for no timeout.
   */
  async wait(timeout?: number): Promise<ImageInfo> {
    if (this._info.status === "ready") return this._info;
    if (this._info.status === "failed") {
      throw new ImageRegistrationError(
        this.name,
        this._info.error || "unknown error",
      );
    }

    const deadline =
      timeout === undefined ? undefined : Date.now() + timeout;

    let info: ImageInfo | null = null;
    try {
      info = await this.waitViaSse(deadline);
    } catch (e) {
      if (e instanceof ImageRegistrationError || e instanceof TimeoutError) {
        throw e;
      }
      info = null;
    }
    if (info !== null) {
      this._info = info;
      return info;
    }

    info = await this.waitViaPolling(deadline);
    this._info = info;
    return info;
  }

  private async waitViaSse(
    deadline: number | undefined,
  ): Promise<ImageInfo | null> {
    const remaining =
      deadline === undefined ? undefined : Math.max(deadline - Date.now(), 100);
    const iter = this.client.sseImageEvents(this.name, remaining);
    for await (const ev of iter) {
      if (ev.data.name !== this.name) continue;
      if (ev.eventType === "image.ready") {
        return await this.client.imageGet(this.name);
      }
      if (ev.eventType === "image.failed") {
        throw new ImageRegistrationError(
          this.name,
          (ev.data.error as string | undefined) || "unknown error",
        );
      }
      if (deadline !== undefined && Date.now() >= deadline) {
        throw new TimeoutError(`timed out waiting for image '${this.name}'`);
      }
    }
    return null;
  }

  private async waitViaPolling(
    deadline: number | undefined,
  ): Promise<ImageInfo> {
    let sleep = POLL_INITIAL_MS;
    while (true) {
      const info = await this.client.imageGet(this.name);
      if (info.status === "ready") return info;
      if (info.status === "failed") {
        throw new ImageRegistrationError(
          this.name,
          info.error || "unknown error",
        );
      }
      if (deadline !== undefined) {
        const remaining = deadline - Date.now();
        if (remaining <= 0) {
          throw new TimeoutError(`timed out waiting for image '${this.name}'`);
        }
        sleep = Math.min(sleep, remaining);
      }
      await new Promise((r) => setTimeout(r, sleep));
      sleep = Math.min(sleep * POLL_FACTOR, POLL_CEILING_MS);
    }
  }
}

/** Namespace for image operations: `pyro.images.create()`, etc. */
export class ImagesNamespace {
  private client: Pyro;

  constructor(client: Pyro) {
    this.client = client;
  }

  /** Fetch current ImageInfo. Throws ImageNotFoundError on 404. */
  async get(name: string): Promise<ImageInfo> {
    return await this.client.imageGet(name);
  }

  /** Start a registration. Returns a PullOperation immediately.
   *
   * Use `op.wait()` to block until the pull settles. Maps server errors:
   * 409 → ImageConflictError, 413 → ImageTooLargeError, 4xx → PyroError,
   * 5xx → ServerError.
   */
  async create(opts: CreateImageOptions): Promise<PullOperation> {
    const hasSource = opts.source !== undefined;
    const hasDockerfile = opts.dockerfile !== undefined;
    if (hasSource === hasDockerfile) {
      throw new Error("exactly one of source or dockerfile required");
    }
    const body: Record<string, unknown> = { name: opts.name };
    if (hasSource) body.source = opts.source;
    if (hasDockerfile) body.dockerfile = opts.dockerfile;
    if (opts.force) body.force = true;

    const data = await this.client.imagePost(body);
    return new PullOperation(this.client, parseImageInfo(data));
  }

  /** Start a registration and block until ready. Surfaces failures. */
  async createAndWait(
    opts: CreateAndWaitImageOptions,
  ): Promise<ImageInfo> {
    const { timeout, ...createOpts } = opts;
    const op = await this.create(createOpts);
    return await op.wait(timeout);
  }

  /** Idempotent register. Attaches to in-flight pulls if any.
   *
   * - ready + same source → returns existing (no pull)
   * - ready + different source → ImageConflictError
   * - pulling/extracting → poll/SSE until terminal
   * - failed or 404 → start a fresh pull and wait
   */
  async ensure(opts: EnsureImageOptions): Promise<ImageInfo> {
    const hasSource = opts.source !== undefined;
    const hasDockerfile = opts.dockerfile !== undefined;
    if (hasSource === hasDockerfile) {
      throw new Error("exactly one of source or dockerfile required");
    }

    let existing: ImageInfo | null = null;
    try {
      existing = await this.client.imageGet(opts.name);
    } catch (e) {
      if (!(e instanceof ImageNotFoundError)) throw e;
    }

    if (existing !== null) {
      if (existing.status === "ready") {
        if (
          hasSource &&
          existing.source &&
          existing.source !== opts.source
        ) {
          throw new ImageConflictError(
            opts.name,
            existing.source,
            opts.source!,
          );
        }
        return existing;
      }
      if (existing.status === "pulling" || existing.status === "extracting") {
        const op = new PullOperation(this.client, existing);
        return await op.wait(opts.timeout);
      }
      // status === "failed" → fall through and re-pull.
    }

    const op = await this.create({
      name: opts.name,
      source: opts.source,
      dockerfile: opts.dockerfile,
    });
    return await op.wait(opts.timeout);
  }
}

/** Parse server JSON (snake_case) into camelCase ImageInfo. */
export function parseImageInfo(data: Record<string, unknown>): ImageInfo {
  const labels = (data.labels as Record<string, string> | undefined) ?? {};
  return {
    name: (data.name as string) ?? "",
    status: (data.status as string) ?? "",
    source: (data.source as string) ?? "",
    digest: (data.digest as string) ?? "",
    error: (data.error as string) ?? "",
    rootfsPath: (data.rootfs_path as string) ?? "",
    kernelPath: (data.kernel_path as string) ?? "",
    size: (data.size as number) ?? 0,
    labels: { ...labels },
    createdAt: (data.created_at as string) ?? "",
  };
}

// ---------------------------------------------------------------------------
// Pyro client wiring helpers (kept here so client.ts stays focused on
// cross-cutting HTTP plumbing).
// ---------------------------------------------------------------------------

/** GET /images/{name} → ImageInfo, mapping 404 → ImageNotFoundError. */
export async function imageGetRequest(
  client: Pyro,
  name: string,
): Promise<ImageInfo> {
  const url = `${client.baseUrl}/api/images/${encodeURIComponent(name)}`;
  const resp = await fetch(url, { headers: client.headers() });
  if (resp.status === 404) throw new ImageNotFoundError(name);
  if (!resp.ok) {
    client.checkResponse(resp);
  }
  const data = (await resp.json()) as Record<string, unknown>;
  return parseImageInfo(data);
}

/** POST /images with image-specific error mapping (409, 413). */
export async function imagePostRequest(
  client: Pyro,
  body: Record<string, unknown>,
): Promise<Record<string, unknown>> {
  const url = `${client.baseUrl}/api/images`;
  const resp = await fetch(url, {
    method: "POST",
    headers: client.headers(),
    body: JSON.stringify(body),
  });
  if (resp.ok) {
    return (await resp.json()) as Record<string, unknown>;
  }

  let errBody: Record<string, unknown> = {};
  try {
    errBody = (await resp.json()) as Record<string, unknown>;
  } catch {
    /* empty body */
  }

  if (resp.status === 409) {
    throw new ImageConflictError(
      (body.name as string) || "",
      "<in-flight>",
      (body.source as string) || "",
    );
  }
  if (resp.status === 413) {
    throw new ImageTooLargeError(
      (body.name as string) || "",
      Number(errBody.limit_mb ?? 0),
      Number(errBody.estimated_mb ?? 0),
    );
  }
  // Defer to shared mapper for 401/404/429/5xx.
  if (resp.status >= 500) throw new ServerError(resp.statusText);
  client.checkResponse(resp);
  throw new PyroError(
    (errBody.error as string) || resp.statusText,
    resp.status,
  );
}

/** Stream image lifecycle events from /events filtered to known types.
 *
 * Yields `{ eventType, data }`. Throws on connection failure — the caller
 * is expected to catch and fall back to polling.
 */
export async function* sseImageEventsImpl(
  client: Pyro,
  name: string,
  timeoutMs?: number,
): AsyncIterable<ImageEvent> {
  if (!client.apiKeyForSse) return;
  const url =
    `${client.baseUrl}/api/events?api_key=` +
    encodeURIComponent(client.apiKeyForSse);

  const controller = new AbortController();
  const timer =
    timeoutMs !== undefined
      ? setTimeout(() => controller.abort(), timeoutMs)
      : undefined;

  const imageEventTypes = new Set([
    "image.pulling",
    "image.extracting",
    "image.ready",
    "image.failed",
    "image.layer_progress",
    "image.force_replaced",
  ]);

  try {
    const resp = await fetch(url, {
      headers: { Accept: "text/event-stream" },
      signal: controller.signal,
    });
    if (!resp.ok || !resp.body) {
      throw new Error(`SSE failed: ${resp.status}`);
    }
    const reader = resp.body.getReader();
    const decoder = new TextDecoder();
    let buf = "";
    let eventType: string | null = null;
    let dataLines: string[] = [];

    while (true) {
      const { value, done } = await reader.read();
      if (done) break;
      buf += decoder.decode(value, { stream: true });

      let nl: number;
      while ((nl = buf.indexOf("\n")) >= 0) {
        const line = buf.slice(0, nl).replace(/\r$/, "");
        buf = buf.slice(nl + 1);

        if (line === "") {
          if (eventType && dataLines.length > 0) {
            let payload: Record<string, unknown> = {};
            try {
              payload = JSON.parse(dataLines.join("\n")) as Record<
                string,
                unknown
              >;
            } catch {
              payload = {};
            }
            if (imageEventTypes.has(eventType)) {
              const inner =
                (payload.data as Record<string, unknown> | undefined) ?? {};
              if (inner.name === name) {
                yield { eventType, data: inner };
                if (eventType === "image.ready" || eventType === "image.failed") {
                  return;
                }
              }
            }
          }
          eventType = null;
          dataLines = [];
          continue;
        }
        if (line.startsWith(":")) continue;
        if (line.startsWith("event:")) {
          eventType = line.slice("event:".length).trim();
        } else if (line.startsWith("data:")) {
          dataLines.push(line.slice("data:".length).replace(/^ /, ""));
        }
      }
    }
  } finally {
    if (timer !== undefined) clearTimeout(timer);
  }
}
