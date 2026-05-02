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

/** Image metadata from the Pyro images API.
 *
 * Mirrors `internal/sandbox.ImageInfo`. Most fields are `omitzero` on the
 * server, so empty values stay defaulted for in-flight or legacy entries.
 */
export interface ImageInfo {
  name: string;
  status: string;
  source: string;
  digest: string;
  error: string;
  rootfsPath: string;
  kernelPath: string;
  size: number;
  labels: Record<string, string>;
  createdAt: string;
}

/** Options for `images.create()`. Supply exactly one of `source` or `dockerfile`. */
export interface CreateImageOptions {
  name: string;
  source?: string;
  dockerfile?: string;
  force?: boolean;
}

/** Options for `images.createAndWait()`. */
export interface CreateAndWaitImageOptions extends CreateImageOptions {
  /** Wait timeout in ms. */
  timeout?: number;
}

/** Options for `images.ensure()`. */
export interface EnsureImageOptions {
  name: string;
  source?: string;
  dockerfile?: string;
  /** Wait timeout in ms. */
  timeout?: number;
}
