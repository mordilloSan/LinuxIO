import { describe, expect, it } from "vitest";

import {
  readSource,
  relativeToSrc,
  sourceFiles,
} from "@/test/guards/sourceFiles";

const gridTagPattern = /<AppVirtualGrid\b[\s\S]*?\/>/g;

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

const tsxFiles = sourceFiles(undefined, {
  extensions: [".tsx"],
  includeTests: true,
}).map(relativeToSrc);

describe("AppVirtualGrid usage", () => {
  it("declares the viewport strategy at every call site", () => {
    const violations = tsxFiles.flatMap((relativePath) => {
      const source = readSource(relativePath);
      return [...source.matchAll(gridTagPattern)]
        .filter((match) => !hasExplicitViewportStrategy(match[0]))
        .map(
          (match) =>
            `${relativePath}:${lineNumberForIndex(source, match.index ?? 0)}`,
        );
    });

    expect(violations).toEqual([]);
  });
});
