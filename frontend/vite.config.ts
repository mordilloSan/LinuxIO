import { defineConfig } from "vite";
import tsconfigPaths from "vite-tsconfig-paths";
import reactBabel from "@vitejs/plugin-react";
import reactSwc from "@vitejs/plugin-react-swc";

export default defineConfig(({ command }) => {
  const isBuild = command === "build";

  const reactPlugin = isBuild
    ? reactBabel({ babel: { plugins: ["babel-plugin-react-compiler"] } })
    : reactSwc(); // fast dev HMR

  return {
    base: "/",
    clearScreen: false,
    plugins: [reactPlugin, tsconfigPaths()],
    server: {
      proxy: {
        "/navigator": { target: "http://localhost:8080", changeOrigin: true },
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
