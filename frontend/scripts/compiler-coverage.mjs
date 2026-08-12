#!/usr/bin/env node
// Reports which src files the React Compiler (oxc-transform reactCompiler)
// memoizes and which it skips. A skipped file compiles fine but ships fully
// unmemoized: on any "[ReactCompiler]" error the pinned oxc-transform emits
// no code and the Vite plugin falls back to the plain pipeline (see
// config/oxc-react-compiler.ts). For per-function reasons run:
//   npx oxlint -A all -W react/react-compiler src
import { readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { transform } from "oxc-transform";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const frontendRoot = path.resolve(__dirname, "..");
const srcDir = path.join(frontendRoot, "src");

function collectSources(dir) {
  const files = [];
  for (const entry of readdirSync(dir)) {
    const full = path.join(dir, entry);
    if (statSync(full).isDirectory()) {
      files.push(...collectSources(full));
      continue;
    }
    if (!/\.tsx?$/.test(entry)) continue;
    // Test-only code never reaches the production build.
    if (entry.endsWith(".d.ts") || /\.test\.tsx?$/.test(entry)) continue;
    files.push(full);
  }
  return files;
}

const files = collectSources(srcDir).filter(
  (f) => !f.startsWith(path.join(srcDir, "test") + path.sep),
);

const results = await Promise.all(
  files.map(async (file) => {
    const result = await transform(file, readFileSync(file, "utf8"), {
      reactCompiler: { target: "19" },
      target: "es2022",
      sourcemap: false,
    });
    const rel = path.relative(frontendRoot, file);
    if (result.code.length === 0) return { rel, status: "skipped" };
    if (result.code.includes("react/compiler-runtime")) {
      return { rel, status: "memoized" };
    }
    return { rel, status: "untouched" };
  }),
);

const byStatus = (status) => results.filter((r) => r.status === status);
const skipped = byStatus("skipped").sort((a, b) => a.rel.localeCompare(b.rel));

console.log("React Compiler coverage (production build, src without tests)");
console.log(`  memoized:            ${byStatus("memoized").length} files`);
console.log(`  nothing to memoize:  ${byStatus("untouched").length} files`);
console.log(`  skipped (unmemoized): ${skipped.length} files`);
for (const { rel } of skipped) {
  console.log(`    - ${rel}`);
}
