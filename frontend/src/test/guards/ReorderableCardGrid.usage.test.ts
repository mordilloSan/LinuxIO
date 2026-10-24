import { describe, expect, it } from "vitest";

import {
  readSource,
  relativeToSrc,
  sourceFiles,
} from "@/test/guards/sourceFiles";

const gridTagPattern = /<ReorderableCardGrid\b[\s\S]*?\/>/g;

function lineNumberForIndex(source: string, index: number) {
  return source.slice(0, index).split("\n").length;
}

const tsxFiles = sourceFiles(undefined, {
  extensions: [".tsx"],
  includeTests: true,
}).map(relativeToSrc);

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
    const violations = tsxFiles.flatMap((relativePath) => {
      const source = readSource(relativePath);
      return [...source.matchAll(gridTagPattern)]
        .filter((match) => !/\bfillAvailable\b/.test(match[0]))
        .map(
          (match) =>
            `${relativePath}:${lineNumberForIndex(source, match.index ?? 0)}`,
        );
    });

    expect(violations).toEqual([]);
  });
});
