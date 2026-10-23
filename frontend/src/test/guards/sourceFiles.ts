import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

// Vitest runs from frontend/ (the setup-file path in vitest.config depends on it).
export const SRC_ROOT = join(process.cwd(), "src/");

interface SourceFileOptions {
  extensions?: readonly string[];
  includeTests?: boolean;
}

/**
 * Recursively list source files under `dir`, skipping generated code. Test
 * files are skipped unless `includeTests` is set. Used by the guard tests
 * that scan the source tree.
 */
export function sourceFiles(
  dir: string = SRC_ROOT,
  {
    extensions = [".ts", ".tsx"],
    includeTests = false,
  }: SourceFileOptions = {},
  files: string[] = [],
): string[] {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (entry.name !== "generated") {
        sourceFiles(full, { extensions, includeTests }, files);
      }
    } else if (
      extensions.some((ext) => entry.name.endsWith(ext)) &&
      (includeTests || !entry.name.includes(".test."))
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

/** Contents of a source file given its path relative to src/. */
export function readSource(rel: string): string {
  return readFileSync(join(SRC_ROOT, rel), "utf8");
}
