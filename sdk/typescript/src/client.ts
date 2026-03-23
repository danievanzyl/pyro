import {
  AuthError,
  PyroError,
  QuotaError,
  SandboxNotFoundError,
  ServerError,
} from "./errors.js";
import { Sandbox, parseSandboxInfo } from "./sandbox.js";
import type { CreateSandboxOptions, PyroConfig, SandboxInfo } from "./types.js";

class SandboxNamespace {
  private client: Pyro;
  constructor(client: Pyro) {
    this.client = client;
  }

  /** Create a new sandbox. */
  async create(options?: CreateSandboxOptions): Promise<Sandbox> {
    const body: Record<string, unknown> = {
      ttl: options?.timeout ?? 3600,
      image: options?.image ?? "default",
    };
    if (options?.vcpu && options.vcpu > 0) body.vcpu = options.vcpu;
    if (options?.memMib && options.memMib > 0) body.mem_mib = options.memMib;

    const data = await this.client.request<Record<string, unknown>>(
      "POST",
      "/sandboxes",
      body,
    );
    return new Sandbox(this.client, parseSandboxInfo(data));
  }

  /** List all active sandboxes. */
  async list(): Promise<Sandbox[]> {
    const data = await this.client.request<Record<string, unknown>[]>(
      "GET",
      "/sandboxes",
    );
    return data.map(
      (sb) => new Sandbox(this.client, parseSandboxInfo(sb)),
    );
  }

  /** Get a sandbox by ID. */
  async get(sandboxId: string): Promise<Sandbox> {
    const data = await this.client.request<Record<string, unknown>>(
      "GET",
      `/sandboxes/${sandboxId}`,
    );
    return new Sandbox(this.client, parseSandboxInfo(data));
  }
}

/**
 * Pyro SDK client.
 *
 * @example
 * ```typescript
 * import { Pyro } from '@pyrovm/sdk'
 *
 * const pyro = new Pyro({ apiKey: 'pk_...', baseUrl: 'http://localhost:8080' })
 * const sandbox = await pyro.sandbox.create({ image: 'python' })
 * const result = await sandbox.run('print("hello")')
 * console.log(result.stdout)
 * await sandbox.stop()
 * ```
 */
export class Pyro {
  readonly baseUrl: string;
  readonly sandbox: SandboxNamespace;
  private apiKey: string;
  private timeout: number;

  constructor(config?: PyroConfig) {
    this.apiKey =
      config?.apiKey ?? process.env.PYRO_API_KEY ?? "";
    this.baseUrl = (
      config?.baseUrl ??
      process.env.PYRO_BASE_URL ??
      "http://localhost:8080"
    ).replace(/\/+$/, "");
    this.timeout = config?.timeout ?? 30_000;
    this.sandbox = new SandboxNamespace(this);
  }

  /** @internal */
  headers(): Record<string, string> {
    return {
      Authorization: `Bearer ${this.apiKey}`,
      "Content-Type": "application/json",
    };
  }

  /** @internal */
  async request<T = unknown>(
    method: string,
    path: string,
    body?: unknown,
  ): Promise<T> {
    const url = `${this.baseUrl}/api${path}`;
    const init: RequestInit = {
      method,
      headers: this.headers(),
      signal: AbortSignal.timeout(this.timeout),
    };
    if (body !== undefined) {
      init.body = JSON.stringify(body);
    }

    const resp = await fetch(url, init);
    this.checkResponse(resp);
    if (resp.status === 204) return undefined as T;
    return resp.json() as Promise<T>;
  }

  /** @internal */
  checkResponse(resp: Response): void {
    if (resp.ok) return;

    const status = resp.status;
    // We can't await resp.json() here synchronously, so use statusText
    const message = resp.statusText || `HTTP ${status}`;

    if (status === 401) throw new AuthError(message);
    if (status === 404) throw new SandboxNotFoundError(message);
    if (status === 429) throw new QuotaError(message);
    if (status >= 500) throw new ServerError(message);
    throw new PyroError(message, status);
  }

  /** Check server health. */
  async health(): Promise<Record<string, unknown>> {
    const resp = await fetch(`${this.baseUrl}/api/health`);
    return resp.json() as Promise<Record<string, unknown>>;
  }
}
