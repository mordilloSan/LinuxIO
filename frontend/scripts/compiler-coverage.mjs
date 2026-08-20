#!/usr/bin/env node
// Reports production-source React Compiler coverage. File-level runtime output,
// recoverable function bailouts, and explicit opt-out directives are separate:
// a file can contain both memoized functions and functions the compiler skips.
import { readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { transform } from "oxc-transform-react";
import ts from "typescript";

const compilerRuntime = "react/compiler-runtime";
const reactCompilerMarker = "[ReactCompiler]";
const scriptPath = fileURLToPath(import.meta.url);
const scriptDirectory = path.dirname(scriptPath);
const frontendRoot = path.resolve(scriptDirectory, "..");
const srcDir = path.join(frontendRoot, "src");

function collectSources(dir) {
  const files = [];
  for (const entry of readdirSync(dir)) {
    const full = path.join(dir, entry);
    if (statSync(full).isDirectory()) {
      files.push(...collectSources(full));
      continue;
    }
    if (!/\.tsx?$/.test(entry)) continue;
    // Test-only code never reaches the production build.
    if (entry.endsWith(".d.ts") || /\.(?:test|spec)\.tsx?$/.test(entry)) {
      continue;
    }
    files.push(full);
  }
  return files;
}

function sourceFileFor(filename, sourceText) {
  const scriptKind = filename.endsWith(".tsx")
    ? ts.ScriptKind.TSX
    : ts.ScriptKind.TS;
  return ts.createSourceFile(
    filename,
    sourceText,
    ts.ScriptTarget.Latest,
    true,
    scriptKind,
  );
}

function functionName(node, sourceFile) {
  if (ts.isConstructorDeclaration(node)) return "constructor";
  if (node.name) return node.name.getText(sourceFile);

  let parent = node.parent;
  while (
    parent &&
    (ts.isParenthesizedExpression(parent) ||
      ts.isAsExpression(parent) ||
      ts.isSatisfiesExpression(parent) ||
      ts.isCallExpression(parent))
  ) {
    parent = parent.parent;
  }

  if (
    parent &&
    (ts.isVariableDeclaration(parent) ||
      ts.isPropertyAssignment(parent) ||
      ts.isPropertyDeclaration(parent))
  ) {
    return parent.name.getText(sourceFile);
  }
  if (
    parent &&
    ts.isBinaryExpression(parent) &&
    parent.operatorToken.kind === ts.SyntaxKind.EqualsToken
  ) {
    return parent.left.getText(sourceFile);
  }
  return "<anonymous>";
}

function sourceLocation(sourceFile, position) {
  const { character, line } = sourceFile.getLineAndCharacterOfPosition(
    Math.max(0, Math.min(position, sourceFile.end)),
  );
  return { column: character + 1, line: line + 1 };
}

function collectFunctionBoundaries(sourceFile) {
  const boundaries = [];
  const visit = (node) => {
    if (ts.isFunctionLike(node) && node.body) {
      boundaries.push({
        end: node.end,
        name: functionName(node, sourceFile),
        node,
        start: node.getStart(sourceFile),
      });
    }
    ts.forEachChild(node, visit);
  };
  visit(sourceFile);
  return boundaries;
}

function containingFunction(boundaries, position) {
  let match;
  for (const boundary of boundaries) {
    if (boundary.start > position || boundary.end < position) continue;
    if (!match || boundary.end - boundary.start < match.end - match.start) {
      match = boundary;
    }
  }
  return match;
}

function directiveIn(statements, directive) {
  for (const statement of statements) {
    if (
      !ts.isExpressionStatement(statement) ||
      !ts.isStringLiteral(statement.expression)
    ) {
      return undefined;
    }
    if (statement.expression.text === directive) return statement;
  }
  return undefined;
}

function collectExplicitNoMemoBoundaries(sourceFile, boundaries) {
  return boundaries
    .map((boundary) => {
      const directive = explicitNoMemoDirective(boundary);
      if (!directive) return undefined;
      const { column, line } = sourceLocation(
        sourceFile,
        directive.getStart(sourceFile),
      );
      return { column, functionName: boundary.name, line };
    })
    .filter(Boolean)
    .sort(
      (left, right) =>
        left.line - right.line ||
        left.column - right.column ||
        left.functionName.localeCompare(right.functionName),
    );
}

function explicitNoMemoDirective(boundary) {
  if (!boundary || !ts.isBlock(boundary.node.body)) return undefined;
  return directiveIn(boundary.node.body.statements, "use no memo");
}

function isTransparentExpression(node) {
  return (
    ts.isParenthesizedExpression(node) ||
    ts.isAsExpression(node) ||
    ts.isSatisfiesExpression(node)
  );
}

function isUseCallbackCall(expression) {
  if (ts.isIdentifier(expression)) return expression.text === "useCallback";
  return (
    ts.isPropertyAccessExpression(expression) &&
    expression.name.text === "useCallback" &&
    ts.isIdentifier(expression.expression) &&
    expression.expression.text === "React"
  );
}

function isUseCallbackBoundary(boundary) {
  if (!boundary) return false;
  let node = boundary.node;
  let parent = node.parent;
  while (parent && isTransparentExpression(parent)) {
    node = parent;
    parent = parent.parent;
  }
  return (
    !!parent &&
    ts.isCallExpression(parent) &&
    parent.arguments[0] === node &&
    isUseCallbackCall(parent.expression)
  );
}

function isRecoverableCompilerDiagnostic(diagnostic) {
  return (
    diagnostic.severity === "Warning" &&
    (diagnostic.message.includes(reactCompilerMarker) ||
      diagnostic.codeframe?.includes(reactCompilerMarker))
  );
}

function diagnosticReason(diagnostic) {
  const summary = diagnostic.message
    .replace(/^\[ReactCompiler\]\s*/, "")
    .trim();
  const detail = diagnostic.labels?.find((label) => label.message)?.message;
  return detail && !summary.includes(detail)
    ? `${summary} — ${detail}`
    : summary;
}

function collectRecoverableBailouts(sourceFile, boundaries, diagnostics) {
  const grouped = new Map();
  for (const diagnostic of diagnostics.filter(
    isRecoverableCompilerDiagnostic,
  )) {
    const label = diagnostic.labels?.find(
      ({ start }) => Number.isInteger(start) && start >= 0,
    );
    const position = label?.start ?? 0;
    const boundary = containingFunction(boundaries, position);
    const location = sourceLocation(sourceFile, position);
    const key = boundary
      ? `function:${boundary.start}:${boundary.end}`
      : `unscoped:${position}`;
    const existing = grouped.get(key) ?? {
      column: location.column,
      functionName: boundary?.name ?? "<module>",
      line: location.line,
      reasons: new Set(),
      boundary,
    };
    if (
      location.line < existing.line ||
      (location.line === existing.line && location.column < existing.column)
    ) {
      existing.line = location.line;
      existing.column = location.column;
    }
    existing.reasons.add(diagnosticReason(diagnostic));
    grouped.set(key, existing);
  }

  return [...grouped.values()]
    .map((bailout) => ({
      ...bailout,
      reasons: [...bailout.reasons].sort((left, right) =>
        left.localeCompare(right),
      ),
    }))
    .sort(
      (left, right) =>
        left.line - right.line ||
        left.column - right.column ||
        left.functionName.localeCompare(right.functionName),
    );
}

function classifyTransformResult(result) {
  if (result.fatal || result.code.length === 0) return "fatal";
  return result.code.includes(compilerRuntime) ? "memoized" : "untouched";
}

export function analyzeCompilerCoverage(filename, sourceText, result) {
  const sourceFile = sourceFileFor(filename, sourceText);
  const boundaries = collectFunctionBoundaries(sourceFile);
  const status = classifyTransformResult(result);
  const recoverableBailouts =
    status === "fatal"
      ? []
      : collectRecoverableBailouts(sourceFile, boundaries, result.errors);
  const actionableBailouts = [];
  const manualMemoFallbacks = [];
  for (const bailout of recoverableBailouts) {
    if (explicitNoMemoDirective(bailout.boundary)) continue;
    if (isUseCallbackBoundary(bailout.boundary)) {
      manualMemoFallbacks.push(bailout);
    } else {
      actionableBailouts.push(bailout);
    }
  }

  const withoutInternalBoundary = (bailout) => {
    const { boundary: _boundary, ...publicBailout } = bailout;
    return publicBailout;
  };
  return {
    explicitNoMemoBoundaries: collectExplicitNoMemoBoundaries(
      sourceFile,
      boundaries,
    ),
    manualMemoFallbacks: manualMemoFallbacks.map(withoutInternalBoundary),
    recoverableBailouts: actionableBailouts.map(withoutInternalBoundary),
    status,
  };
}

function normalizedRelativePath(filename) {
  return filename.split(path.sep).join("/");
}

export function formatCompilerCoverageReport(
  results,
  { verbose = false } = {},
) {
  const ordered = [...results].sort((left, right) =>
    left.rel.localeCompare(right.rel),
  );
  const withStatus = (status) =>
    ordered.filter((result) => result.status === status);
  const fatal = withStatus("fatal");
  const actionable = ordered.flatMap((result) =>
    result.recoverableBailouts.map((bailout) => ({
      ...bailout,
      rel: result.rel,
    })),
  );
  const manual = ordered.flatMap((result) =>
    (result.manualMemoFallbacks ?? []).map((bailout) => ({
      ...bailout,
      rel: result.rel,
    })),
  );
  const explicit = ordered.flatMap((result) =>
    result.explicitNoMemoBoundaries.map((boundary) => ({
      ...boundary,
      rel: result.rel,
    })),
  );

  const lines = [
    "React Compiler coverage (production build, src without tests)",
    `  files emitting memoized code: ${withStatus("memoized").length}`,
    `  files with nothing to memoize: ${withStatus("untouched").length}`,
    `  fatal whole-file failures:     ${fatal.length}`,
  ];
  for (const { rel } of fatal) lines.push(`    - ${rel}`);

  lines.push(
    `  actionable recoverable function bailouts: ${actionable.length}`,
  );
  for (const bailout of actionable) {
    lines.push(
      `    - ${bailout.rel}:${bailout.line}:${bailout.column} ` +
        `${bailout.functionName} — ${bailout.reasons.join("; ")}`,
    );
  }

  lines.push(`  manual memo fallback callbacks: ${manual.length}`);
  if (verbose) {
    for (const bailout of manual) {
      lines.push(
        `    - ${bailout.rel}:${bailout.line}:${bailout.column} ` +
          `${bailout.functionName} — ${bailout.reasons.join("; ")}`,
      );
    }
  }

  lines.push(`  explicit "use no memo" boundaries: ${explicit.length}`);
  if (verbose) {
    for (const boundary of explicit) {
      lines.push(
        `    - ${boundary.rel}:${boundary.line}:${boundary.column} ` +
          boundary.functionName,
      );
    }
  }
  if (!verbose && (manual.length > 0 || explicit.length > 0)) {
    lines.push(
      "  (rerun with COMPILER_COVERAGE_VERBOSE=1 to list accepted boundaries)",
    );
  }
  return lines.join("\n");
}

export function isVerboseCompilerCoverageRequested({
  argv = process.argv,
  env = process.env,
} = {}) {
  const verboseValues = new Set(["1", "true", "yes", "on"]);
  return (
    argv.includes("--verbose") ||
    verboseValues.has(
      String(env.COMPILER_COVERAGE_VERBOSE ?? "")
        .trim()
        .toLowerCase(),
    )
  );
}

async function main() {
  const files = collectSources(srcDir)
    .filter((file) => !file.startsWith(path.join(srcDir, "test") + path.sep))
    .sort((left, right) => left.localeCompare(right));
  const results = await Promise.all(
    files.map(async (file) => {
      const sourceText = readFileSync(file, "utf8");
      const transformResult = await transform(file, sourceText, {
        reactCompiler: {
          target: "19",
        },
        jsx: { runtime: "automatic" },
        sourcemap: false,
      });
      return {
        rel: normalizedRelativePath(path.relative(frontendRoot, file)),
        ...analyzeCompilerCoverage(file, sourceText, transformResult),
      };
    }),
  );
  console.log(
    formatCompilerCoverageReport(results, {
      verbose: isVerboseCompilerCoverageRequested(),
    }),
  );
}

if (process.argv[1] && path.resolve(process.argv[1]) === scriptPath) {
  await main();
}
