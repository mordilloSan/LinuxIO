import fs from "node:fs";
import path from "node:path";

import { describe, expect, it } from "vitest";

const srcRoot = path.resolve(process.cwd(), "src");
const gridTagPattern = /<AppVirtualGrid\b[\s\S]*?\/>/g;

function listTsxFiles(dir: string): string[] {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const entryPath = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      return listTsxFiles(entryPath);
    }
    return entry.name.endsWith(".tsx") ? [entryPath] : [];
  });
}

function lineNumberForIndex(source: string, index: number) {
  return source.slice(0, index).split("\n").length;
}

function hasExplicitViewportStrategy(tag: string) {
  if (/\b(height|maxHeight|scrollElementRef)=/.test(tag)) {
    return true;
  }

  const fillAvailable = tag.match(
    /\bfillAvailable(?:\s*=\s*(?:{\s*false\s*}|["']false["']))?/,
  );

  return Boolean(fillAvailable && !/=/.test(fillAvailable[0]));
}

describe("AppVirtualGrid usage", () => {
  it("declares the viewport strategy at every call site", () => {
    const violations = listTsxFiles(srcRoot).flatMap((filePath) => {
      const source = fs.readFileSync(filePath, "utf8");
      return [...source.matchAll(gridTagPattern)]
        .filter((match) => !hasExplicitViewportStrategy(match[0]))
        .map((match) => {
          const relativePath = path.relative(srcRoot, filePath);
          return `${relativePath}:${lineNumberForIndex(source, match.index ?? 0)}`;
        });
    });

    expect(violations).toEqual([]);
  });
});
