import fs from "node:fs";
import path from "node:path";

import { describe, expect, it } from "vitest";

const srcRoot = path.resolve(process.cwd(), "src");
const sourceExtensions = [".css", ".ts", ".tsx"];

/*
 * Transparency mixes are written one way everywhere: colour first, percentage
 * on `transparent` — `color-mix(in srgb, X, transparent 88%)` — the shape
 * mixWithTransparency (theme/surfaces.ts) emits. The inverted shape
 * (`X 12%, transparent`) computes the identical colour, but its percentage
 * means the opposite thing, so with both shapes in the tree every hand edit
 * is a chance to silently invert an opacity. Two-colour mixes are exempt:
 * both components are named, so there is nothing to misread.
 */
const invertedTransparencyMix =
  /color-mix\(in srgb, [^,]+ [\d.]+%, ?transparent\)/g;

function listSourceFiles(dir: string): string[] {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const entryPath = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      return listSourceFiles(entryPath);
    }
    if (entry.name.includes(".test.")) {
      return [];
    }
    return sourceExtensions.some((ext) => entry.name.endsWith(ext))
      ? [entryPath]
      : [];
  });
}

function lineNumberForIndex(source: string, index: number) {
  return source.slice(0, index).split("\n").length;
}

describe("color-mix transparency shape", () => {
  it("keeps the percentage on transparent at every site", () => {
    const violations = listSourceFiles(srcRoot).flatMap((filePath) => {
      const source = fs.readFileSync(filePath, "utf8");
      return [...source.matchAll(invertedTransparencyMix)].map((match) => {
        const relativePath = path.relative(srcRoot, filePath);
        return `${relativePath}:${lineNumberForIndex(source, match.index ?? 0)}`;
      });
    });

    expect(violations).toEqual([]);
  });
});
