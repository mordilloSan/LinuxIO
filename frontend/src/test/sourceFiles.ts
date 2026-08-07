import { readdirSync } from "node:fs";
import { join } from "node:path";

// Vitest runs from frontend/ (the setup-file path in vitest.config depends on it).
export const SRC_ROOT = join(process.cwd(), "src/");

/**
 * Recursively list non-test .ts/.tsx sources under `dir`, skipping generated
 * code. Used by the guard tests that scan the source tree.
 */
export function sourceFiles(
  dir: string = SRC_ROOT,
  files: string[] = [],
): string[] {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (entry.name !== "generated") sourceFiles(full, files);
    } else if (
      /\.(ts|tsx)$/.test(entry.name) &&
      !entry.name.includes(".test.")
    ) {
      files.push(full);
    }
  }
  return files;
}

/** Path of `file` relative to src/, with forward slashes. */
export function relativeToSrc(file: string): string {
  return file.slice(SRC_ROOT.length).replaceAll("\\", "/");
}
