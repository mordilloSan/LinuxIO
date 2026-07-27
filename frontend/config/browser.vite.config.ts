import path from "node:path";
import { fileURLToPath } from "node:url";

import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

const configDirectory = path.dirname(fileURLToPath(import.meta.url));
const frontendRoot = path.resolve(configDirectory, "..");
const browserOutputDirectory = process.env.LINUXIO_BROWSER_OUT_DIR;

if (!browserOutputDirectory) {
  throw new Error(
    "LINUXIO_BROWSER_OUT_DIR must be set by scripts/run-browser-fixture.mjs",
  );
}

export default defineConfig({
  base: "/",
  build: {
    emptyOutDir: true,
    manifest: true,
    outDir: path.resolve(browserOutputDirectory),
    target: "es2022",
  },
  cacheDir: path.join(frontendRoot, "node_modules/.vite/browser"),
  plugins: [react()],
  preview: {
    host: "127.0.0.1",
    port: 4174,
    strictPort: true,
  },
  resolve: {
    alias: {
      "@": path.join(frontendRoot, "src"),
    },
  },
  root: path.join(frontendRoot, "src/test/browser/fixture"),
});
