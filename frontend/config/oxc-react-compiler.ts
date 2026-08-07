import { transform as oxcTransform } from "oxc-transform";
import type { PluginOption } from "vite";

interface OxcReactCompilerOptions {
  excludeTests?: boolean;
}

const isTestModule = (filename: string): boolean => {
  const normalized = filename.replaceAll("\\", "/");
  return (
    normalized.includes("/src/test/") ||
    /\.(?:test|spec)\.tsx?$/.test(normalized)
  );
};

// React Compiler via the oxc Rust port. oxc-transform is pinned to 0.136.0 —
// the last release shipping `reactCompiler` — because Rolldown/Vite withdrew
// the integration in 0.137.0 over napi binary size while a leaner
// implementation is upstreamed. Unpin once it returns to a current release.
// The port passed 100% of the upstream conformance fixtures at that point.
export function oxcReactCompiler({
  excludeTests = false,
}: OxcReactCompilerOptions = {}): PluginOption {
  return {
    name: "oxc-react-compiler",
    // "pre" is load-bearing: the compiler must see the original TSX, and
    // Vite's native TS/JSX lowering runs before normal-phase JS plugins.
    enforce: "pre",
    async transform(code, id) {
      const filename = id.split("?")[0];
      if (
        !/\.tsx?$/.test(filename) ||
        filename.includes("/node_modules/") ||
        (excludeTests && isTestModule(filename))
      ) {
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
        (error) =>
          error.severity === "Error" &&
          !error.message.startsWith("[ReactCompiler]"),
      );
      if (hardErrors.length > 0) {
        throw new Error(
          `oxc-react-compiler: ${filename}\n` +
            hardErrors.map((error) => error.message).join("\n"),
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
