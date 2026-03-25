/** Result of executing a command in a sandbox. */
export interface ExecResult {
  exitCode: number;
  stdout: string;
  stderr: string;
}

/** Sandbox metadata from the API. */
export interface SandboxInfo {
  id: string;
  image: string;
  state: string;
  pid: number;
  vsockCid: number;
  createdAt: string;
  expiresAt: string;
  apiKeyId?: string;
  vcpu?: number;
  memMib?: number;
}

/** Options for creating a sandbox. */
export interface CreateSandboxOptions {
  image?: string;
  timeout?: number;
  vcpu?: number;
  memMib?: number;
  /** Ephemeral scratch disk in MiB (0 = none). Mounted at /scratch with overlayfs. */
  scratchSizeMib?: number;
}

/** Options for executing a command. */
export interface ExecOptions {
  env?: Record<string, string>;
  workdir?: string;
  timeout?: number;
}

/** Pyro client configuration. */
export interface PyroConfig {
  apiKey?: string;
  baseUrl?: string;
  timeout?: number;
}
