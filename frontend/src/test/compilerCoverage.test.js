import { describe, expect, it } from "vitest";

import {
  analyzeCompilerCoverage,
  formatCompilerCoverageReport,
} from "../../scripts/compiler-coverage.mjs";

const source = `
function VirtualizedList() {
  "use no memo";
  const virtualizer = useVirtualizer({ count: 1 });
  return virtualizer.getVirtualItems();
}

const ExplicitBoundary = () => {
  "use no memo";
  return null;
};
`;

function transformResult(overrides = {}) {
  return {
    code: 'import { c as _c } from "react/compiler-runtime";',
    errors: [],
    fatal: false,
    ...overrides,
  };
}

describe("compiler coverage reporting", () => {
  it("separates recoverable function bailouts from explicit boundaries", () => {
    const diagnosticStart = source.indexOf("useVirtualizer");
    const analysis = analyzeCompilerCoverage(
      "src/VirtualizedList.tsx",
      source,
      transformResult({
        errors: [
          {
            codeframe: "[ReactCompiler] IncompatibleLibrary",
            labels: [
              {
                end: diagnosticStart + "useVirtualizer".length,
                message:
                  "The library returns functions that cannot be memoized safely",
                start: diagnosticStart,
              },
            ],
            message:
              "[ReactCompiler] IncompatibleLibrary: Use of incompatible library",
            severity: "Warning",
          },
          {
            codeframe: "unrelated transform warning",
            labels: [{ end: 1, message: null, start: 0 }],
            message: "Unrelated warning",
            severity: "Warning",
          },
        ],
      }),
    );

    expect(analysis.status).toBe("memoized");
    expect(analysis.recoverableBailouts).toEqual([
      expect.objectContaining({
        functionName: "VirtualizedList",
        reasons: [
          "IncompatibleLibrary: Use of incompatible library — " +
            "The library returns functions that cannot be memoized safely",
        ],
      }),
    ]);
    expect(analysis.explicitNoMemoBoundaries).toEqual([
      expect.objectContaining({ functionName: "VirtualizedList" }),
      expect.objectContaining({ functionName: "ExplicitBoundary" }),
    ]);
  });

  it("keeps fatal files and files with no memoized output distinct", () => {
    expect(
      analyzeCompilerCoverage(
        "src/fatal.tsx",
        "export const value = 1;",
        transformResult({ code: "", fatal: true }),
      ).status,
    ).toBe("fatal");
    expect(
      analyzeCompilerCoverage(
        "src/plain.ts",
        "export const value = 1;",
        transformResult({ code: "export const value = 1;" }),
      ).status,
    ).toBe("untouched");
  });

  it("formats each category independently", () => {
    const analyzed = analyzeCompilerCoverage(
      "src/VirtualizedList.tsx",
      source,
      transformResult(),
    );
    const report = formatCompilerCoverageReport([
      { rel: "src/VirtualizedList.tsx", ...analyzed },
      {
        explicitNoMemoBoundaries: [],
        recoverableBailouts: [],
        rel: "src/plain.ts",
        status: "untouched",
      },
    ]);

    expect(report).toContain("files emitting memoized code: 1");
    expect(report).toContain("files with nothing to memoize: 1");
    expect(report).toContain('explicit "use no memo" boundaries: 2');
    expect(report).toContain("src/VirtualizedList.tsx:3:3 VirtualizedList");
  });
});
