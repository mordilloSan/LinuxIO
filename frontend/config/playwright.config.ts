import path from "node:path";
import { fileURLToPath } from "node:url";

import { defineConfig, devices } from "@playwright/test";

const configDirectory = path.dirname(fileURLToPath(import.meta.url));
const frontendRoot = path.resolve(configDirectory, "..");
const repositoryRoot = path.resolve(frontendRoot, "..");
const baseURL = "http://127.0.0.1:4174";

export default defineConfig({
  expect: {
    timeout: 5_000,
    // Same Chromium, same fonts, same machine class: anything past a few
    // anti-aliasing pixels is a real change.
    toHaveScreenshot: { maxDiffPixelRatio: 0.001 },
  },
  forbidOnly: Boolean(process.env.CI),
  fullyParallel: false,
  outputDir: path.join(repositoryRoot, ".cache/playwright/test-results"),
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
  reporter: "list",
  snapshotPathTemplate:
    "{testDir}/__screenshots__/{testFileName}/{arg}-{projectName}-{platform}{ext}",
  retries: process.env.CI ? 1 : 0,
  testDir: path.join(frontendRoot, "src/test/browser"),
  timeout: 20_000,
  use: {
    baseURL,
    screenshot: "only-on-failure",
    trace: "retain-on-failure",
  },
  webServer: {
    command: "node scripts/run-browser-fixture.mjs",
    cwd: frontendRoot,
    gracefulShutdown: {
      signal: "SIGTERM",
      timeout: 5_000,
    },
    reuseExistingServer: false,
    stderr: "pipe",
    stdout: "ignore",
    timeout: 120_000,
    url: `${baseURL}/accounts`,
  },
  workers: 1,
});
