import { defineConfig, devices } from "@playwright/test";
import { fileURLToPath } from "node:url";
import { defineBddConfig } from "playwright-bdd";

const port = process.env.DV_E2E_PORT ?? "18080";
const baseURL = `http://127.0.0.1:${port}`;
// File roots must be absolute (FR-011 AC1).
const samples = fileURLToPath(new URL("../deploy/samples", import.meta.url));

const testDir = defineBddConfig({
  features: "../features/ui/*.feature",
  featuresRoot: "../features/ui",
  steps: "steps/*.ts",
  outputDir: ".features-gen",
});

export default defineConfig({
  testDir,
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  retries: 0,
  reporter: process.env.CI ? [["list"], ["html", { open: "never" }]] : "list",
  use: {
    baseURL,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  // Serves the built SPA from the real Go binary (`make build` first).
  webServer: {
    command: "../bin/dv",
    url: `${baseURL}/readyz`,
    reuseExistingServer: !process.env.CI,
    timeout: 30_000,
    env: {
      DV_HTTP_ADDR: `127.0.0.1:${port}`,
      DV_WEB_DIR: "../web/dist",
      DV_LOG_FORMAT: "text",
      DV_LOG_LEVEL: "warn",
      DV_RATE_LIMIT_RPS: "500",
      DV_RATE_LIMIT_BURST: "1000",
      DV_FILE_ROOTS: `samples=${samples}`,
    },
  },
});
