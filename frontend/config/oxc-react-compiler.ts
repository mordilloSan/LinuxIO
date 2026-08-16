import { transform as oxcTransform, type Severity } from "oxc-transform-react";
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

// React Compiler via Oxc's dedicated Rust binding. Keeping it separate from
// the general transformer avoids adding a framework compiler to every Oxc
// consumer while allowing this integration to follow current Oxc releases.
export function oxcReactCompiler({
  excludeTests = false,
}: OxcReactCompilerOptions = {}): PluginOption {
  return {
    name: "oxc-react-compiler",
    // "pre" is load-bearing: the compiler must see the original TSX, and
    // Vite's native TS/JSX lowering runs before normal-phase JS plugins.
    enforce: "pre",
    transform: {
      // Evaluated natively by Rolldown, so the JS hook below is only invoked
      // for TS/TSX source modules instead of every module in the graph. The
      // handler keeps its own checks for runners that ignore hook filters.
      filter: {
        id: {
          include: /\.tsx?(?:\?|$)/,
          exclude: /node_modules/,
        },
      },
      async handler(code, id) {
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
          reactCompiler: {
            target: "19",
            // 0.144.0 predates the nested-closure correctness fix. Remove this
            // once the pinned release includes oxc-project/oxc#25548.
            environment: { enableFunctionOutlining: false },
          },
          jsx: { runtime: "automatic" },
          // The production build emits no sourcemaps (build.sourcemap defaults
          // to false), so generating them here is wasted work.
          sourcemap: false,
        });
        const hardErrors = result.errors.filter(
          (error) =>
            error.severity === ("Error" as Severity) &&
            !error.message.startsWith("[ReactCompiler]"),
        );
        if (hardErrors.length > 0) {
          throw new Error(
            `oxc-react-compiler: ${filename}\n` +
              hardErrors.map((error) => error.message).join("\n"),
          );
        }
        // On fatal React Compiler errors the transform emits no usable code.
        // Fall back to Vite's normal pipeline so the file ships unmemoized —
        // the same skip semantics the Babel plugin had. Oxlint's
        // react/react-compiler rule surfaces these at lint time.
        if (result.fatal || result.code.length === 0) {
          return null;
        }
        // TypeScript is stripped and JSX is compiled here. Vite retains
        // ownership of the configured JavaScript build target.
        return { code: result.code, map: result.map ?? null };
      },
    },
  };
}
