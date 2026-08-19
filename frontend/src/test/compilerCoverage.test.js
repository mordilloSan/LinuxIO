import { describe, expect, it } from "vitest";

import {
  analyzeCompilerCoverage,
  formatCompilerCoverageReport,
  isVerboseCompilerCoverageRequested,
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

const classificationSource = `
function ExplicitBoundary() {
  "use no memo";
  return useVirtualizer();
}

const ManualFallback = useCallback(async () => {
  await manualOperation();
  return manualResult();
}, []);

function OrdinaryFunction() {
  ordinaryOperation();
  return ordinaryResult();
}
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
    expect(analysis.recoverableBailouts).toEqual([]);
    expect(analysis.manualMemoFallbacks).toEqual([]);
    expect(analysis.explicitNoMemoBoundaries).toEqual([
      expect.objectContaining({ functionName: "VirtualizedList" }),
      expect.objectContaining({ functionName: "ExplicitBoundary" }),
    ]);
  });

  it("classifies explicit boundaries, useCallback fallbacks, and actionable bailouts", () => {
    const diagnostic = (needle, message) => {
      const start = classificationSource.indexOf(needle);
      return {
        codeframe: `[ReactCompiler] ${message}`,
        labels: [{ end: start + needle.length, message, start }],
        message: `[ReactCompiler] ${message}`,
        severity: "Warning",
      };
    };
    const analysis = analyzeCompilerCoverage(
      "src/classification.tsx",
      classificationSource,
      transformResult({
        errors: [
          diagnostic("useVirtualizer", "explicit boundary reason"),
          diagnostic("manualOperation", "manual reason one"),
          diagnostic("manualResult", "manual reason two"),
          diagnostic("ordinaryOperation", "actionable reason one"),
          diagnostic("ordinaryResult", "actionable reason two"),
        ],
      }),
    );

    expect(analysis.recoverableBailouts).toEqual([
      expect.objectContaining({
        functionName: "OrdinaryFunction",
        reasons: ["actionable reason one", "actionable reason two"],
      }),
    ]);
    expect(analysis.manualMemoFallbacks).toEqual([
      expect.objectContaining({
        functionName: "ManualFallback",
        reasons: ["manual reason one", "manual reason two"],
      }),
    ]);
    expect(analysis.explicitNoMemoBoundaries).toEqual([
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
    const results = [
      { rel: "src/VirtualizedList.tsx", ...analyzed },
      {
        explicitNoMemoBoundaries: [],
        manualMemoFallbacks: [
          {
            column: 4,
            functionName: "manualHandler",
            line: 8,
            reasons: ["manual reason"],
          },
        ],
        recoverableBailouts: [
          {
            column: 6,
            functionName: "actionableHandler",
            line: 9,
            reasons: ["actionable reason"],
          },
        ],
        rel: "src/plain.ts",
        status: "untouched",
      },
    ];
    const report = formatCompilerCoverageReport(results);

    expect(report).toContain("files emitting memoized code: 1");
    expect(report).toContain("files with nothing to memoize: 1");
    expect(report).toContain("actionable recoverable function bailouts: 1");
    expect(report).toContain("actionableHandler — actionable reason");
    expect(report).toContain("manual memo fallback callbacks: 1");
    expect(report).toContain('explicit "use no memo" boundaries: 2');
    expect(report).toContain("COMPILER_COVERAGE_VERBOSE=1");
    expect(report).not.toContain("manualHandler — manual reason");
    expect(report).not.toContain("src/VirtualizedList.tsx:3:3 VirtualizedList");

    const verboseReport = formatCompilerCoverageReport(results, {
      verbose: true,
    });
    expect(verboseReport).toContain("manualHandler — manual reason");
    expect(verboseReport).toContain(
      "src/VirtualizedList.tsx:3:3 VirtualizedList",
    );
  });

  it("recognizes CLI and environment verbose switches", () => {
    expect(
      isVerboseCompilerCoverageRequested({
        argv: ["node", "script"],
        env: {},
      }),
    ).toBe(false);
    expect(
      isVerboseCompilerCoverageRequested({
        argv: ["node", "script", "--verbose"],
        env: {},
      }),
    ).toBe(true);
    for (const value of ["1", "true", "TRUE", "yes", "on"]) {
      expect(
        isVerboseCompilerCoverageRequested({
          argv: ["node", "script"],
          env: { COMPILER_COVERAGE_VERBOSE: value },
        }),
      ).toBe(true);
    }
    expect(
      isVerboseCompilerCoverageRequested({
        argv: ["node", "script"],
        env: { COMPILER_COVERAGE_VERBOSE: "0" },
      }),
    ).toBe(false);
  });
});
