import { tanstackRouter } from "@tanstack/router-plugin/vite";
import react from "@vitejs/plugin-react";
import { defineConfig, type PluginOption, type UserConfig } from "vite";
import { analyzer } from "vite-bundle-analyzer";
import { compression } from "vite-plugin-compression2";

export default defineConfig(async ({ command }): Promise<UserConfig> => {
  const { generateIcons } = await import("../scripts/generate-icons.mjs");
  generateIcons();

  const isBuild = command === "build";
  const shouldAnalyze = isBuild && process.env.BUNDLE_ANALYZE === "true";

  const proxyTarget = process.env.PROXY_TARGET || "https://localhost:8090";
  const devPort = Number(process.env.VITE_DEV_PORT || 3000);

  const plugins: PluginOption[] = [
    tanstackRouter({ autoCodeSplitting: true, target: "react" }),
    react(
      isBuild
        ? {
            compiler: { target: "19" },
            exclude: /\/[^/]*Virtual[^/]*\.tsx$/,
          }
        : undefined,
    ),
  ];
  if (isBuild) {
    plugins.push(
      compression({
        algorithms: ["gzip", "brotliCompress"],
      }),
    );
    if (shouldAnalyze) {
      plugins.push(
        analyzer({
          analyzerMode: "static",
          openAnalyzer: false,
        }),
      );
    }
  }

  return {
    base: "/",
    clearScreen: false,
    css: {
      // Rust CSS pipeline; ships inside Vite 8 (no postcss config exists to
      // migrate). Targets are derived from build.target automatically.
      transformer: "lightningcss",
    },
    plugins,
    resolve: {
      tsconfigPaths: true,
    },
    server: {
      port: devPort,
      strictPort: false,
      proxy: {
        "/ws": {
          target: proxyTarget,
          changeOrigin: true,
          ws: true,
          secure: false,
        },
        "/auth": { target: proxyTarget, changeOrigin: true, secure: false },
        "/api": { target: proxyTarget, changeOrigin: true, secure: false },
      },
    },
    build: {
      target: "es2022",
      // No sourcemaps in the production bundle: it ships embedded in the Go
      // webserver, there is no error-tracking service to consume maps, and we
      // avoid publishing original source. Debug with sourcemaps via `dev`
      // instead. The React plugin follows this setting when the production
      // compiler is enabled above.
      sourcemap: false,
      // The compression plugin emits real .gz/.br artifacts below; the extra
      // gzip pass behind this option only feeds the build-log size column.
      reportCompressedSize: false,
      chunkSizeWarningLimit: 2000,
      manifest: true,
      outDir: "../backend/webserver/web/frontend",
      emptyOutDir: true,
    },
  };
});
