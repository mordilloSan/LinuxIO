import path from "node:path";
import { fileURLToPath } from "node:url";

import { defineConfig, devices } from "@playwright/test";

// The specs in src/test/vm run against LinuxIO installed in the test VMs.
// `make test-vm-browser` and `make test-vm-browser-rerun` list the VMs'
// addresses in LINUXIO_VM_URLS and set their login. There is one worker for each VM; a worker takes the next spec
// file and runs it against its own VM. Playwright loads this file in every
// worker process, with the worker's parallel index in TEST_PARALLEL_INDEX,
// so baseURL below is that worker's VM (src/test/vm/vm.ts does the same for
// the rig commands).
const vmUrls = (process.env.LINUXIO_VM_URLS ?? "")
  .trim()
  .split(/\s+/)
  .filter(Boolean);
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
  // Names the VM of every result and draws what each VM runs (vm-reporter.ts).
  reporter: [[path.join(configDirectory, "vm-reporter.ts")]],
  // A spec file runs whole on one VM on purpose: its tests build on each other.
  reportSlowTests: null,
  testDir: path.join(frontendRoot, "src/test/vm"),
  timeout: 60_000,
  use: {
    baseURL: vmUrls[Number(process.env.TEST_PARALLEL_INDEX ?? 0)],
    // The VM serves LinuxIO's self-signed certificate.
    ignoreHTTPSErrors: true,
    screenshot: "only-on-failure",
    trace: "retain-on-failure",
  },
  workers: Math.max(1, vmUrls.length),
});
