import path from "node:path";
import { fileURLToPath } from "node:url";

import { defineConfig, devices } from "@playwright/test";

// The specs in src/test/vm run against LinuxIO installed in the test VM.
// `make test-vm-browser` sets LINUXIO_VM_URL and the VM login.
const configDirectory = path.dirname(fileURLToPath(import.meta.url));
const frontendRoot = path.resolve(configDirectory, "..");
const repositoryRoot = path.resolve(frontendRoot, "..");

export default defineConfig({
  forbidOnly: Boolean(process.env.CI),
  fullyParallel: false,
  outputDir: path.join(repositoryRoot, ".cache/playwright/vm-results"),
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
  reporter: "list",
  testDir: path.join(frontendRoot, "src/test/vm"),
  timeout: 60_000,
  use: {
    baseURL: process.env.LINUXIO_VM_URL,
    // The VM serves LinuxIO's self-signed certificate.
    ignoreHTTPSErrors: true,
    screenshot: "only-on-failure",
    trace: "retain-on-failure",
  },
  workers: 1,
});
