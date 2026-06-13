import path from "node:path";
import { fileURLToPath } from "node:url";

import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "src"),
    },
  },
  test: {
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
        "vite.config.ts",
        "vitest.config.ts",
      ],
    },
  },
});
