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

  const devApi = process.env.VITE_API_URL;
  const devPort = Number(process.env.VITE_DEV_PORT || 3000);

  const plugins = [reactPlugin, tsconfigPaths()];
  if (isBuild) {
    plugins.push(
      compression({
        algorithm: "gzip",
        deleteOriginFile: false,
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
        "/navigator": { target: devApi, changeOrigin: true },
        "/ws":        { target: devApi, changeOrigin: true, ws: true },
      },
    },
    build: {
      target: "es2017",
      chunkSizeWarningLimit: 2000,
      manifest: true,
      outDir: "../backend/server/web/frontend",
      emptyOutDir: true,
      minify: "esbuild",
    },
  };
});
