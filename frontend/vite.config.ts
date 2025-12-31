// vite.config.ts
import { defineConfig } from "vite";
import reactBabel from "@vitejs/plugin-react";
import reactSwc from "@vitejs/plugin-react-swc";
import tsconfigPaths from "vite-tsconfig-paths";
import { compression } from 'vite-plugin-compression2'

export default defineConfig(({ command }) => {
  const isBuild = command === "build";
  const reactPlugin = isBuild
    ? reactBabel({ babel: { plugins: ["babel-plugin-react-compiler"] } })
    : reactSwc();

  // PROXY_TARGET is for vite's proxy (not exposed to frontend)
  // VITE_DEV_PORT is for the dev server port
  const proxyTarget = process.env.PROXY_TARGET || "https://localhost:8090";
  const devPort = Number(process.env.VITE_DEV_PORT || 3000);

  const plugins = [reactPlugin, tsconfigPaths()];
  if (isBuild) {
    plugins.push(
      compression({
        algorithms: ["gzip"],
      }),
    );
  }

  return {
    base: "/",
    clearScreen: false,
    plugins,
    server: {
      port: devPort,
      strictPort: false,
      proxy: {
        "/ws": { target: proxyTarget, changeOrigin: true, ws: true, secure: false },
        "/auth": { target: proxyTarget, changeOrigin: true, secure: false },
      },
    },
    build: {
      target: "es2017",
      chunkSizeWarningLimit: 2000,
      manifest: true,
      outDir: "../backend/webserver/web/frontend",
      emptyOutDir: true,
      minify: "esbuild",
    },
  };
});
