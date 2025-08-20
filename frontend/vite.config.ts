import { defineConfig } from "vite";
import tsconfigPaths from "vite-tsconfig-paths";
import reactBabel from "@vitejs/plugin-react";
import reactSwc from "@vitejs/plugin-react-swc";
import svgr from "@svgr/rollup";

export default defineConfig(({ command }) => {
  const isBuild = command === "build";

  const reactPlugin = isBuild
    ? reactBabel({
        // React Compiler only in production builds
        babel: { plugins: ["babel-plugin-react-compiler"] },
      })
    : reactSwc(); // HMR in dev

  return {
    base: "/",
    clearScreen: false,
    plugins: [reactPlugin, svgr(), tsconfigPaths()],
    server: {
      proxy: {
        "/navigator": {
          target: "http://localhost:8080",
          changeOrigin: true,
        },
      },
    },
    build: {
      target: "es2017",
      chunkSizeWarningLimit: 2000,
      manifest: true,
      outDir: "../backend/cmd/server/frontend",
      emptyOutDir: true,
      minify: "esbuild",
    },
    resolve: {
      conditions: [
        "mui-modern",
        "module",
        "browser",
        isBuild ? "production" : "development",
      ],
    },
  };
});
