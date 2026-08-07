import { defineConfig, type PluginOption } from "vite";
import react from "@vitejs/plugin-react";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import { transform as oxcTransform } from "oxc-transform";
import { compression } from "vite-plugin-compression2";
import { analyzer } from "vite-bundle-analyzer";

// React Compiler via the oxc Rust port. oxc-transform is pinned to 0.136.0 —
// the last release shipping `reactCompiler` — because Rolldown/Vite withdrew
// the integration in 0.137.0 over napi binary size while a leaner
// implementation is upstreamed. Unpin once it returns to a current release.
// The port passed 100% of the upstream conformance fixtures at that point.
function oxcReactCompiler(): PluginOption {
  return {
    name: "oxc-react-compiler",
    // "pre" is load-bearing: the compiler must see the original TSX, and
    // rolldown's native TS/JSX lowering runs before normal-phase JS plugins.
    enforce: "pre",
    async transform(code, id) {
      const filename = id.split("?")[0];
      if (!/\.tsx?$/.test(filename) || filename.includes("/node_modules/")) {
        return null;
      }
      const result = await oxcTransform(filename, code, {
        // Defaults mirror reactCompilerPreset(): compilationMode "infer",
        // panicThreshold "none" — components the compiler cannot memoize are
        // skipped, not failed (oxlint's react/react-compiler rule lists them).
        reactCompiler: { target: "19" },
        target: "es2022",
        // The production build emits no sourcemaps (build.sourcemap defaults
        // to false), so generating them here is wasted work.
        sourcemap: false,
      });
      const hardErrors = result.errors.filter(
        (e) =>
          e.severity === "Error" && !e.message.startsWith("[ReactCompiler]"),
      );
      if (hardErrors.length > 0) {
        throw new Error(
          `oxc-react-compiler: ${filename}\n` +
            hardErrors.map((e) => e.message).join("\n"),
        );
      }
      // On "[ReactCompiler] ..." errors (compiler invariant panics) the
      // transform emits no code. Fall back to Vite's normal pipeline so the
      // file ships unmemoized — the same skip semantics the Babel plugin had.
      // oxlint's react/react-compiler rule surfaces these at lint time.
      if (result.code.length === 0) {
        return null;
      }
      // Output is fully lowered (TS stripped, JSX compiled), leaving nothing
      // for Vite's own transform to do on these modules.
      return { code: result.code, map: result.map ?? null };
    },
  };
}

export default defineConfig(async ({ command }) => {
  const { generateIcons } = await import("../scripts/generate-icons.mjs");
  generateIcons();

  const isBuild = command === "build";
  const shouldAnalyze = isBuild && process.env.BUNDLE_ANALYZE === "true";

  const proxyTarget = process.env.PROXY_TARGET || "https://localhost:8090";
  const devPort = Number(process.env.VITE_DEV_PORT || 3000);

  const plugins: PluginOption[] = [
    tanstackRouter({ autoCodeSplitting: true, target: "react" }),
    react(),
  ];
  if (isBuild) {
    plugins.push(oxcReactCompiler());
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
      // No sourcemaps in the production bundle: it ships embedded in the Go
      // webserver, there is no error-tracking service to consume maps, and we
      // avoid publishing original source. Debug with sourcemaps via `dev`
      // instead. If you flip this on, also flip `sourcemap` in
      // oxcReactCompiler() above so compiled files keep complete maps.
      sourcemap: false,
      chunkSizeWarningLimit: 2000,
      manifest: true,
      outDir: "../backend/webserver/web/frontend",
      emptyOutDir: true,
    },
  };
});
