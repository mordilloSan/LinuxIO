import path from "node:path";
import { fileURLToPath } from "node:url";

import { tanstackRouter } from "@tanstack/router-plugin/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

const configDirectory = path.dirname(fileURLToPath(import.meta.url));
const frontendRoot = path.resolve(configDirectory, "..");
const compilerExcludeRE = [
  /\/node_modules\//,
  /\/src\/test\//,
  /\.(?:test|spec)\.[jt]sx?$/,
  // These components deliberately opt out with "use no memo" because
  // TanStack Virtual exposes functions the compiler cannot memoize safely.
  // Compiler coverage checks those boundaries directly; transforming them in
  // Vitest only emits redundant IncompatibleLibrary warnings.
  /\/(?:AppDataTable|AppVirtualGrid|VirtualDirectoryItems)\.tsx$/,
];

export default defineConfig({
  cacheDir: path.join(frontendRoot, "node_modules/.vite"),
  plugins: [
    tanstackRouter({ disableLogging: true, target: "react" }),
    react({ compiler: { target: "19" }, exclude: compilerExcludeRE }),
  ],
  resolve: {
    alias: {
      "@": path.join(frontendRoot, "src"),
    },
  },
  root: frontendRoot,
  test: {
    exclude: ["src/test/browser/**", "node_modules/**"],
    pool: "vmThreads",
    // vmThreads is the fastest pool but retains memory per VM context and
    // defaults to one worker per core. Left unbounded a full run peaked at
    // ~7GB RSS across 16 workers, starving the Go tooling `make test` runs
    // alongside it. Recycle workers past the limit and cap the worker count.
    vmMemoryLimit: "512MB",
    maxWorkers: 8,
    clearMocks: true,
    // Silence intercepted console output from passing tests; failing tests
    // still print their logs for debugging. Keeps `make test` output readable.
    silent: "passed-only",
    environment: "jsdom",
    environmentOptions: {
      jsdom: {
        url: "https://linuxio.test/",
      },
    },
    fakeTimers: {
      toFake: [
        "Date",
        "setTimeout",
        "clearTimeout",
        "setInterval",
        "clearInterval",
      ],
    },
    globals: false,
    restoreMocks: true,
    setupFiles: ["./src/test/setup.ts"],
    coverage: {
      provider: "v8",
      reporter: ["text", "html", "lcov"],
      exclude: [
        "backend/**",
        "dist/**",
        "node_modules/**",
        "src/**/*.d.ts",
        "src/api/generated/**",
        "src/icons/**",
        "src/test/**",
        "src/index.tsx",
        "config/vite.config.ts",
        "config/vitest.config.ts",
      ],
    },
  },
});
