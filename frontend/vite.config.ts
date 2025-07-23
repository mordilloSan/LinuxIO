import { defineConfig } from "vite";
import tsconfigPaths from "vite-tsconfig-paths";
import react from "@vitejs/plugin-react-swc";
import svgr from "@svgr/rollup";
import fs from "fs";
import path from "path";

function removeIndexHtmlPlugin() {
  return {
    name: "remove-index-html",
    closeBundle() {
      const indexPath = path.resolve(
        __dirname,
        "../backend/frontend/index.html"
      );
      if (fs.existsSync(indexPath)) {
        fs.unlinkSync(indexPath);
        console.log("🧹 Removed index.html from build output");
      }
    },
  };
}

export default defineConfig(({ mode }) => ({
  base: "/",
  clearScreen: false,
  plugins: [
    react(),
    svgr(),
    tsconfigPaths(),
    ...(mode === "production" ? [removeIndexHtmlPlugin()] : []),
  ],
  server: {
    proxy: {
      // Proxy /navigator requests to Go backend
      "/navigator": {
        target: "http://localhost:8080",
        changeOrigin: true,
        // secure: false, // If your Go backend is https with self-signed, enable this
      },
    },
  },
  build: {
    target: "es2017",
    chunkSizeWarningLimit: 2000,
    manifest: true,
    outDir: "../backend/frontend",
    emptyOutDir: true,
    minify: "esbuild",
  },
  resolve: {
    conditions: ["mui-modern", "module", "browser", "development|production"],
  },
}));
