/**
 * Unit tests for the images surface.
 *
 * Mocks `globalThis.fetch` so no live server is needed. SSE is exercised by
 * monkey-patching `client.sseImageEvents` per-test (mirrors the Python SDK's
 * `_sse_image_events` test seam).
 */

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Pyro } from "../src/client.js";
import {
  ImageConflictError,
  ImageNotFoundError,
  ImageRegistrationError,
  ImageTooLargeError,
  TimeoutError,
} from "../src/errors.js";
import { PullOperation } from "../src/images.js";
import type { ImageInfo } from "../src/types.js";

let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  fetchMock = vi.fn();
  globalThis.fetch = fetchMock as unknown as typeof fetch;
});

afterEach(() => {
  vi.restoreAllMocks();
});

function mkResponse(status: number, body?: unknown): Response {
  const text = body === undefined ? "" : JSON.stringify(body);
  return new Response(text, { status });
}

function newClient(): Pyro {
  return new Pyro({
    apiKey: "pk_test",
    baseUrl: "http://test",
    timeout: 5000,
  });
}

function emptyImageInfo(overrides: Partial<ImageInfo>): ImageInfo {
  return {
    name: "",
    status: "",
    source: "",
    digest: "",
    error: "",
    rootfsPath: "",
    kernelPath: "",
    size: 0,
    labels: {},
    createdAt: "",
    ...overrides,
  };
}

describe("images.get", () => {
  it("returns ImageInfo on 200", async () => {
    fetchMock.mockResolvedValueOnce(
      mkResponse(200, {
        name: "py",
        status: "ready",
        source: "python:3.12",
        digest: "sha256:abc",
        size: 1024,
        labels: { "org.opencontainers.image.source": "github.com/x" },
        rootfs_path: "/var/lib/pyro/py/rootfs.ext4",
        kernel_path: "/var/lib/pyro/kernel",
        created_at: "2026-05-02T00:00:00Z",
      }),
    );
    const info = await newClient().images.get("py");
    expect(info.name).toBe("py");
    expect(info.status).toBe("ready");
    expect(info.digest).toBe("sha256:abc");
    expect(info.rootfsPath).toBe("/var/lib/pyro/py/rootfs.ext4");
    expect(info.kernelPath).toBe("/var/lib/pyro/kernel");
    expect(info.createdAt).toBe("2026-05-02T00:00:00Z");
    expect(info.labels["org.opencontainers.image.source"]).toBe("github.com/x");
  });

  it("throws ImageNotFoundError on 404", async () => {
    fetchMock.mockResolvedValueOnce(mkResponse(404, { error: "not found" }));
    await expect(newClient().images.get("missing")).rejects.toThrow(
      ImageNotFoundError,
    );
  });
});

describe("images.create", () => {
  it("returns PullOperation on 202", async () => {
    fetchMock.mockResolvedValueOnce(
      mkResponse(202, {
        name: "py",
        status: "pulling",
        source: "python:3.12",
      }),
    );
    const op = await newClient().images.create({
      name: "py",
      source: "python:3.12",
    });
    expect(op).toBeInstanceOf(PullOperation);
    expect(op.status).toBe("pulling");
    expect(op.name).toBe("py");
  });

  it("maps 413 → ImageTooLargeError carrying limit/estimate", async () => {
    fetchMock.mockResolvedValueOnce(
      mkResponse(413, {
        error: "image too large",
        limit_mb: 4096,
        estimated_mb: 8000,
      }),
    );
    await expect(
      newClient().images.create({ name: "huge", source: "x" }),
    ).rejects.toMatchObject({
      name: "ImageTooLargeError",
      limitMb: 4096,
      estimatedMb: 8000,
    });
  });

  it("maps 409 → ImageConflictError", async () => {
    fetchMock.mockResolvedValueOnce(
      mkResponse(409, { error: "name in use" }),
    );
    await expect(
      newClient().images.create({ name: "py", source: "python:3.12" }),
    ).rejects.toThrow(ImageConflictError);
  });

  it("rejects when neither source nor dockerfile supplied", async () => {
    await expect(
      newClient().images.create({ name: "py" }),
    ).rejects.toThrow(/exactly one/);
  });
});

describe("images.createAndWait", () => {
  it("happy path: POST 202 + SSE image.ready → ImageInfo", async () => {
    fetchMock
      .mockResolvedValueOnce(
        mkResponse(202, {
          name: "py",
          status: "pulling",
          source: "python:3.12",
        }),
      )
      .mockResolvedValueOnce(
        mkResponse(200, {
          name: "py",
          status: "ready",
          source: "python:3.12",
          digest: "sha256:abc",
          size: 200,
        }),
      );

    const c = newClient();
    c.sseImageEvents = async function* () {
      yield { eventType: "image.ready", data: { name: "py" } };
    };

    const info = await c.images.createAndWait({
      name: "py",
      source: "python:3.12",
    });
    expect(info.status).toBe("ready");
    expect(info.digest).toBe("sha256:abc");
  });

  it("SSE image.failed → ImageRegistrationError", async () => {
    fetchMock.mockResolvedValueOnce(
      mkResponse(202, { name: "py", status: "pulling" }),
    );

    const c = newClient();
    c.sseImageEvents = async function* () {
      yield {
        eventType: "image.failed",
        data: { name: "py", error: "registry timeout" },
      };
    };

    await expect(
      c.images.createAndWait({ name: "py", source: "python:3.12" }),
    ).rejects.toThrow(ImageRegistrationError);
  });

  it("timeout: SSE unavailable, polling drives TimeoutError", async () => {
    fetchMock
      .mockResolvedValueOnce(
        mkResponse(202, { name: "py", status: "pulling" }),
      )
      // Polling indefinitely returns pulling. Use mockImplementation so
      // each poll gets a fresh Response (Body can only be read once).
      .mockImplementation(() =>
        Promise.resolve(mkResponse(200, { name: "py", status: "pulling" })),
      );

    const c = newClient();
    c.sseImageEvents = async function* () {
      throw new Error("sse unavailable");
    };

    await expect(
      c.images.createAndWait({
        name: "py",
        source: "python:3.12",
        timeout: 30,
      }),
    ).rejects.toThrow(TimeoutError);
  });
});

describe("images.ensure", () => {
  it("existing ready: no POST", async () => {
    fetchMock.mockResolvedValueOnce(
      mkResponse(200, {
        name: "py",
        status: "ready",
        source: "python:3.12",
      }),
    );
    const c = newClient();
    const info = await c.images.ensure({
      name: "py",
      source: "python:3.12",
    });
    expect(info.status).toBe("ready");
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const init = (fetchMock.mock.calls[0][1] ?? {}) as RequestInit;
    expect(init.method ?? "GET").toBe("GET");
  });

  it("source mismatch on existing ready → ImageConflictError", async () => {
    fetchMock.mockResolvedValueOnce(
      mkResponse(200, {
        name: "py",
        status: "ready",
        source: "python:3.11",
      }),
    );
    await expect(
      newClient().images.ensure({ name: "py", source: "python:3.12" }),
    ).rejects.toThrow(ImageConflictError);
  });

  it("fresh: 404 → POST → SSE ready → ImageInfo", async () => {
    fetchMock
      .mockResolvedValueOnce(mkResponse(404, { error: "not found" }))
      .mockResolvedValueOnce(
        mkResponse(202, {
          name: "py",
          status: "pulling",
          source: "python:3.12",
        }),
      )
      .mockResolvedValueOnce(
        mkResponse(200, {
          name: "py",
          status: "ready",
          source: "python:3.12",
          digest: "sha256:fresh",
        }),
      );

    const c = newClient();
    c.sseImageEvents = async function* () {
      yield { eventType: "image.ready", data: { name: "py" } };
    };

    const info = await c.images.ensure({
      name: "py",
      source: "python:3.12",
    });
    expect(info.status).toBe("ready");
    expect(info.digest).toBe("sha256:fresh");
    expect(fetchMock).toHaveBeenCalledTimes(3);
  });
});

describe("PullOperation.wait prefers SSE", () => {
  it("monkey-patched SSE yields ready, only the refetch GET hits fetch", async () => {
    fetchMock.mockResolvedValueOnce(
      mkResponse(200, {
        name: "py",
        status: "ready",
        source: "python:3.12",
        digest: "sha256:x",
      }),
    );

    const c = newClient();
    let sseCalls = 0;
    c.sseImageEvents = async function* () {
      sseCalls++;
      yield { eventType: "image.ready", data: { name: "py" } };
    };

    const op = new PullOperation(
      c,
      emptyImageInfo({
        name: "py",
        status: "pulling",
        source: "python:3.12",
      }),
    );
    const info = await op.wait();
    expect(info.status).toBe("ready");
    expect(sseCalls).toBe(1);
    // Only the post-SSE refetch — polling never invoked.
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});
