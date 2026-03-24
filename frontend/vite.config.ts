// vite.config.ts
import { defineConfig, type Plugin } from "vite";
import react, { reactCompilerPreset } from "@vitejs/plugin-react";
import babel from "@rolldown/plugin-babel";
import { compression } from 'vite-plugin-compression2';
import { analyzer } from 'vite-bundle-analyzer';

export default defineConfig(async ({ command }) => {
  const { generateIcons } = await import("./scripts/generate-icons.mjs");
  generateIcons();

  const isBuild = command === "build";
  const shouldAnalyze = isBuild && process.env.BUNDLE_ANALYZE === "true";

  // PROXY_TARGET is for vite's proxy (not exposed to frontend)
  // VITE_DEV_PORT is for the dev server port
  const proxyTarget = process.env.PROXY_TARGET || "https://localhost:8090";
  const devPort = Number(process.env.VITE_DEV_PORT || 3000);

  const plugins: (Plugin | Plugin[])[] = [react()];
  if (isBuild) {
    plugins.push(await babel({ presets: [reactCompilerPreset()] } as never) as unknown as Plugin);
    plugins.push(
      compression({
        algorithms: ["gzip"],
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
      fs: {
        // Allow serving files from the modules directory for HMR
        allow: ['..'],
      },
      proxy: {
        "/ws": { target: proxyTarget, changeOrigin: true, ws: true, secure: false },
        "/auth": { target: proxyTarget, changeOrigin: true, secure: false },
        "/api": { target: proxyTarget, changeOrigin: true, secure: false },
      },
    },
    build: {
      target: "es2017",
      chunkSizeWarningLimit: 2000,
      manifest: true,
      outDir: "../backend/webserver/web/frontend",
      emptyOutDir: true,
      rolldownOptions: {
        external: /\/modules\/.*\/src\/index\.tsx/,
      },
    },
  };
});
