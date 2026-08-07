import { spawn } from "node:child_process";
import { mkdtemp, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const scriptDirectory = path.dirname(fileURLToPath(import.meta.url));
const frontendRoot = path.resolve(scriptDirectory, "..");
const viteBinary = path.join(frontendRoot, "node_modules/.bin/vite");
const outputDirectory = await mkdtemp(
  path.join(os.tmpdir(), "linuxio-browser-"),
);
const childEnvironment = {
  ...process.env,
  LINUXIO_BROWSER_OUT_DIR: outputDirectory,
};

let activeChild;
let stopping = false;

const runVite = (args) =>
  new Promise((resolve, reject) => {
    const child = spawn(viteBinary, args, {
      cwd: frontendRoot,
      env: childEnvironment,
      stdio: "inherit",
    });
    activeChild = child;

    child.once("error", reject);
    child.once("exit", (code, signal) => {
      if (activeChild === child) activeChild = undefined;
      if (stopping && signal) {
        resolve();
      } else if (code === 0) {
        resolve();
      } else {
        reject(
          new Error(
            `Vite ${args[0]} failed${signal ? ` with ${signal}` : ` with exit code ${code}`}`,
          ),
        );
      }
    });
  });

const forwardSignal = (signal) => {
  stopping = true;
  activeChild?.kill(signal);
};

process.once("SIGINT", () => forwardSignal("SIGINT"));
process.once("SIGTERM", () => forwardSignal("SIGTERM"));

try {
  await runVite(["build", "--config", "config/browser.vite.config.ts"]);
  if (!stopping) {
    await runVite(["preview", "--config", "config/browser.vite.config.ts"]);
  }
} catch (error) {
  if (!stopping) {
    console.error(error);
    process.exitCode = 1;
  }
} finally {
  await rm(outputDirectory, { force: true, recursive: true });
}
