import { defineConfig, type PluginOption } from "vite";
import react, { reactCompilerPreset } from "@vitejs/plugin-react";
import babel from "@rolldown/plugin-babel";
import { compression } from "vite-plugin-compression2";
import { analyzer } from "vite-bundle-analyzer";

export default defineConfig(async ({ command }) => {
  const { generateIcons } = await import("./scripts/generate-icons.mjs");
  generateIcons();

  const isBuild = command === "build";
  const shouldAnalyze = isBuild && process.env.BUNDLE_ANALYZE === "true";

  const proxyTarget = process.env.PROXY_TARGET || "https://localhost:8090";
  const devPort = Number(process.env.VITE_DEV_PORT || 3000);

  const plugins: PluginOption[] = [react()];
  if (isBuild) {
    plugins.push(
      (await babel({
        presets: [reactCompilerPreset()],
      })) as unknown as PluginOption,
    );
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
      chunkSizeWarningLimit: 2000,
      manifest: true,
      outDir: "../backend/webserver/web/frontend",
      emptyOutDir: true,
    },
  };
});
