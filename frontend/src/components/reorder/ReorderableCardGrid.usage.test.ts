import fs from "node:fs";
import path from "node:path";

import { describe, expect, it } from "vitest";

const srcRoot = path.resolve(process.cwd(), "src");
const gridTagPattern = /<ReorderableCardGrid\b[\s\S]*?\/>/g;

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

describe("ReorderableCardGrid usage", () => {
  /*
   * Whether a card grid scrolls itself or scrolls the page is the difference
   * between a route whose chrome stays put and one whose tab strip slides away
   * under the header. It is invisible in the JSX until you go looking, which is
   * how the two halves of the same route — the card view and the table it
   * toggles with — drifted apart in the first place. So every call site says
   * which it is, `fillAvailable` for a route's whole surface and
   * `fillAvailable={false}` for one section stacked among others.
   */
  it("declares the viewport strategy at every call site", () => {
    const violations = listTsxFiles(srcRoot).flatMap((filePath) => {
      const source = fs.readFileSync(filePath, "utf8");
      return [...source.matchAll(gridTagPattern)]
        .filter((match) => !/\bfillAvailable\b/.test(match[0]))
        .map((match) => {
          const relativePath = path.relative(srcRoot, filePath);
          return `${relativePath}:${lineNumberForIndex(source, match.index ?? 0)}`;
        });
    });

    expect(violations).toEqual([]);
  });
});
