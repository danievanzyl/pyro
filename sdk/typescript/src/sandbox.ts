import type { Pyro } from "./client.js";
import type { ExecOptions, ExecResult, SandboxInfo } from "./types.js";

/** A running Pyro sandbox. Use pyro.sandbox.create() to get one. */
export class Sandbox {
  readonly id: string;
  info: SandboxInfo;
  private client: Pyro;

  constructor(client: Pyro, info: SandboxInfo) {
    this.client = client;
    this.info = info;
    this.id = info.id;
  }

  /** Execute a command in the sandbox. */
  async exec(command: string[], options?: ExecOptions): Promise<ExecResult> {
    const body: Record<string, unknown> = { command };
    if (options?.env) body.env = options.env;
    if (options?.workdir) body.workdir = options.workdir;
    if (options?.timeout) body.timeout = options.timeout;

    const data = await this.client.request<Record<string, unknown>>(
      "POST",
      `/sandboxes/${this.id}/exec`,
      body,
    );
    return {
      exitCode: data.exit_code as number,
      stdout: (data.stdout as string) ?? "",
      stderr: (data.stderr as string) ?? "",
    };
  }

  /** Run code in the sandbox. Infers interpreter from image. */
  async run(code: string, language?: string): Promise<ExecResult> {
    const lang = language ?? this.inferLanguage();
    if (lang === "python") {
      return this.exec(["python3", "-c", code]);
    } else if (lang === "node") {
      return this.exec(["node", "-e", code]);
    }
    return this.exec(["sh", "-c", code]);
  }

  /** Write a file into the sandbox. */
  async writeFile(path: string, content: string | Uint8Array): Promise<void> {
    const isText = typeof content === "string";
    const resp = await fetch(
      `${this.client.baseUrl}/api/sandboxes/${this.id}/files${path}`,
      {
        method: "PUT",
        headers: {
          ...this.client.headers(),
          "Content-Type": isText ? "text/plain" : "application/octet-stream",
        },
        body: isText ? (content as string) : Buffer.from(content as Uint8Array),
      },
    );
    this.client.checkResponse(resp);
  }

  /** Read a file from the sandbox. */
  async readFile(path: string): Promise<Uint8Array> {
    const resp = await fetch(
      `${this.client.baseUrl}/api/sandboxes/${this.id}/files${path}`,
      { headers: this.client.headers() },
    );
    this.client.checkResponse(resp);
    return new Uint8Array(await resp.arrayBuffer());
  }

  /** Destroy this sandbox. */
  async stop(): Promise<void> {
    await this.client.request("DELETE", `/sandboxes/${this.id}`);
  }

  /** Get current sandbox status. */
  async status(): Promise<SandboxInfo> {
    const data = await this.client.request<Record<string, unknown>>(
      "GET",
      `/sandboxes/${this.id}`,
    );
    this.info = parseSandboxInfo(data);
    return this.info;
  }

  private inferLanguage(): string {
    const image = this.info.image.toLowerCase();
    if (image.includes("python")) return "python";
    if (image.includes("node")) return "node";
    return "shell";
  }
}

export function parseSandboxInfo(data: Record<string, unknown>): SandboxInfo {
  return {
    id: data.id as string,
    image: data.image as string,
    state: data.state as string,
    pid: data.pid as number,
    vsockCid: data.vsock_cid as number,
    createdAt: data.created_at as string,
    expiresAt: data.expires_at as string,
    apiKeyId: data.api_key_id as string | undefined,
    vcpu: data.vcpu as number | undefined,
    memMib: data.mem_mib as number | undefined,
  };
}
