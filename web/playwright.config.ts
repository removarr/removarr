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

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  reporter: process.env.CI ? "list" : [["list"], ["html", { open: "never" }]],
  use: {
    baseURL: "http://127.0.0.1:7373",
    trace: "retain-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: {
    command: `go run ./internal/e2e/fakeserver -port 7373 -dir ${JSON.stringify(dir)}`,
    cwd: root,
    url: "http://127.0.0.1:7373/api/health",
    reuseExistingServer: false,
    // The first run compiles the server.
    timeout: 180_000,
    stdout: "ignore",
    stderr: "pipe",
  },
});
