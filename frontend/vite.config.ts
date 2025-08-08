import { defineConfig } from "vite";
import tsconfigPaths from "vite-tsconfig-paths";
import reactBabel from "@vitejs/plugin-react"; // Babel (prod)
import reactSwc from "@vitejs/plugin-react-swc"; // SWC (dev)
import svgr from "@svgr/rollup";
import fs from "fs";
import path from "path";

function removeIndexHtmlPlugin() {
  return {
    name: "remove-index-html",
    closeBundle() {
      const indexPath = path.resolve(
        __dirname,
        "../backend/cmd/server/frontend/index.html"
      );
      if (fs.existsSync(indexPath)) {
        fs.unlinkSync(indexPath);
        console.log("🧹 Removed index.html from build output");
      }
    },
  };
}

export default defineConfig(({ command }) => {
  const isBuild = command === "build";

  const reactPlugin = isBuild
    ? reactBabel({
        // ✅ React Compiler only in production builds
        babel: { plugins: ["babel-plugin-react-compiler"] },
      })
    : reactSwc(); // ⚡ super-fast HMR in dev

  return {
    base: "/",
    clearScreen: false,
    plugins: [
      reactPlugin,
      svgr(),
      tsconfigPaths(),
      ...(isBuild ? [removeIndexHtmlPlugin()] : []),
    ],
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
