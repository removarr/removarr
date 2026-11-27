import { defineConfig, devices } from "@playwright/test";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

// The end-to-end test runs removarr's real API and the embedded UI on fake apps
// (internal/e2e/fakeserver). Build the UI first with `pnpm build`: the server serves the
// embedded build. Every run starts on a fresh database.
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "removarr-e2e-"));
// web/go.mod keeps the go tool out of web/, so the server is run from the repository root.
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
// REMOVARR_E2E_PORT moves the fake server off 7373, for example when one is already running
// there. A server already on the port is never reused: the test needs a fresh database.
const port = Number(process.env.REMOVARR_E2E_PORT || "7373");
if (!Number.isInteger(port) || port < 1 || port > 65535) {
  throw new Error("REMOVARR_E2E_PORT must be a port number");
}
const base = `http://127.0.0.1:${port}`;

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  reporter: process.env.CI ? "list" : [["list"], ["html", { open: "never" }]],
  use: {
    baseURL: base,
    trace: "retain-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: {
    command: `go run ./internal/e2e/fakeserver -port ${port} -dir ${JSON.stringify(dir)}`,
    cwd: root,
    url: `${base}/api/health`,
    reuseExistingServer: false,
    // The first run compiles the server.
    timeout: 180_000,
    stdout: "ignore",
    stderr: "pipe",
  },
});
