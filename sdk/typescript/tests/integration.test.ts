/**
 * Integration tests for @pyrovm/sdk against a live Pyro server.
 *
 * Requires PYRO_API_KEY and PYRO_BASE_URL env vars.
 */

import { describe, it, expect } from "vitest";
import { Pyro, AuthError, SandboxNotFoundError } from "../src/index.js";

const BASE_URL = process.env.PYRO_BASE_URL ?? "http://localhost:8080";
const API_KEY = process.env.PYRO_API_KEY ?? "";

const client = new Pyro({ apiKey: API_KEY, baseUrl: BASE_URL, timeout: 30_000 });

describe("health", () => {
  it("returns ok", async () => {
    const h = await client.health();
    expect(h.status).toBe("ok");
  });
});

describe("auth", () => {
  it("rejects invalid key", async () => {
    const bad = new Pyro({ apiKey: "pk_invalid", baseUrl: BASE_URL });
    await expect(bad.sandbox.list()).rejects.toThrow(AuthError);
  });
});

describe("sandbox lifecycle", () => {
  it("create → exec → file → stop", async () => {
    // Create
    const sb = await client.sandbox.create({ image: "minimal", timeout: 120 });
    expect(sb.id).toBeTruthy();
    expect(sb.info.state).toBe("running");

    // List
    const all = await client.sandbox.list();
    expect(all.some((s) => s.id === sb.id)).toBe(true);

    // Get
    const fetched = await client.sandbox.get(sb.id);
    expect(fetched.id).toBe(sb.id);

    // Exec
    const echo = await sb.exec(["echo", "hello"]);
    expect(echo.exitCode).toBe(0);
    expect(echo.stdout).toContain("hello");

    // Run (shell)
    const run = await sb.run("echo world");
    expect(run.exitCode).toBe(0);
    expect(run.stdout).toContain("world");

    // File write/read
    await sb.writeFile("/tmp/test.txt", "pyro test");
    const data = await sb.readFile("/tmp/test.txt");
    expect(new TextDecoder().decode(data)).toBe("pyro test");

    // Status
    const info = await sb.status();
    expect(info.state).toBe("running");

    // Stop
    await sb.stop();
  }, 60_000);
});

describe("errors", () => {
  it("sandbox not found", async () => {
    await expect(client.sandbox.get("nonexistent-id-12345")).rejects.toThrow(
      SandboxNotFoundError,
    );
  });
});
