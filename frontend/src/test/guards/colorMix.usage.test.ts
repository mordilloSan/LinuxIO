import { describe, expect, it } from "vitest";

import {
  readSource,
  relativeToSrc,
  sourceFiles,
} from "@/test/guards/sourceFiles";

/*
 * Transparency mixes are written one way everywhere: colour first, percentage
 * on `transparent` — `color-mix(in srgb, X, transparent 88%)` — the shape
 * the colorUtils alpha() fallback emits. The inverted shape
 * (`X 12%, transparent`) computes the identical colour, but its percentage
 * means the opposite thing, so with both shapes in the tree every hand edit
 * is a chance to silently invert an opacity. Two-colour mixes are exempt:
 * both components are named, so there is nothing to misread.
 */
const invertedTransparencyMix =
  /color-mix\(in srgb, [^,]+ [\d.]+%, ?transparent\)/g;

function lineNumberForIndex(source: string, index: number) {
  return source.slice(0, index).split("\n").length;
}

const sourcePaths = sourceFiles(undefined, {
  extensions: [".css", ".ts", ".tsx"],
}).map(relativeToSrc);

describe("color-mix transparency shape", () => {
  it("keeps the percentage on transparent at every site", () => {
    const violations = sourcePaths.flatMap((relativePath) => {
      const source = readSource(relativePath);
      return [...source.matchAll(invertedTransparencyMix)].map(
        (match) =>
          `${relativePath}:${lineNumberForIndex(source, match.index ?? 0)}`,
      );
    });

    expect(violations).toEqual([]);
  });
});
