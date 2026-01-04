// vite.config.ts
import { defineConfig, type Plugin } from "vite";
import reactBabel from "@vitejs/plugin-react";
import reactSwc from "@vitejs/plugin-react-swc";
import tsconfigPaths from "vite-tsconfig-paths";
import { compression } from 'vite-plugin-compression2';
import { analyzer } from 'vite-bundle-analyzer';
import path from 'path';

export default defineConfig(({ command }) => {
  const isBuild = command === "build";
  const reactPlugin = isBuild
    ? reactBabel({ babel: { plugins: ["babel-plugin-react-compiler"] } })
    : reactSwc();

  // PROXY_TARGET is for vite's proxy (not exposed to frontend)
  // VITE_DEV_PORT is for the dev server port
  const proxyTarget = process.env.PROXY_TARGET || "https://localhost:8090";
  const devPort = Number(process.env.VITE_DEV_PORT || 3000);

  const plugins: (Plugin | Plugin[])[] = [reactPlugin, tsconfigPaths()];
  if (isBuild) {
    plugins.push(
      compression({
        algorithms: ["gzip"],
      }),
      analyzer({
        analyzerMode: 'static',
        openAnalyzer: false,
      }),
    );
  }

  return {
    base: "/",
    clearScreen: false,
    plugins,
    resolve: {
      alias: {
        // Map all @/ imports to frontend/src/
        '@/api': path.resolve(__dirname, './src/api'),
        '@/components': path.resolve(__dirname, './src/components'),
        '@/contexts': path.resolve(__dirname, './src/contexts'),
        '@/hooks': path.resolve(__dirname, './src/hooks'),
        '@/layouts': path.resolve(__dirname, './src/layouts'),
        '@/pages': path.resolve(__dirname, './src/pages'),
        '@/theme': path.resolve(__dirname, './src/theme'),
        '@/types': path.resolve(__dirname, './src/types'),
        '@/utils': path.resolve(__dirname, './src/utils'),
        '@/assets': path.resolve(__dirname, './src/assets'),
        '@/services': path.resolve(__dirname, './src/services'),
        '@/constants': path.resolve(__dirname, './src/theme/constants'),
        '@/config': path.resolve(__dirname, './src/config'),
        '@/routes': path.resolve(__dirname, './src/routes'),
      },
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
      },
    },
    build: {
      target: "es2017",
      chunkSizeWarningLimit: 2000,
      manifest: true,
      outDir: "../backend/webserver/web/frontend",
      emptyOutDir: true,
      minify: "esbuild",
      rollupOptions: {
        external: (id) => {
          // Exclude modules directory from build (loaded dynamically at runtime)
          return id.includes('/modules/') && id.includes('/src/index.tsx');
        },
      },
    },
  };
});
