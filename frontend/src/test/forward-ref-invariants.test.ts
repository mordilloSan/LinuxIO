import { readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";

import ts from "typescript";
import { describe, expect, it } from "vitest";

const sourceRoot = path.resolve(process.cwd(), "src");

const sourceFiles = (directory: string): string[] => {
  const entries = readdirSync(directory);
  const files: string[] = [];
  for (const entry of entries) {
    const full = path.join(directory, entry);
    if (statSync(full).isDirectory()) {
      if (entry === "node_modules" || entry === "generated") continue;
      files.push(...sourceFiles(full));
      continue;
    }
    if (!/\.tsx?$/.test(entry) || /\.(?:test|spec)\.tsx?$/.test(entry))
      continue;
    files.push(full);
  }
  return files;
};

const isForwardRefProperty = (node: ts.Node): boolean => {
  if (ts.isPropertyAccessExpression(node)) {
    return node.name.text === "forwardRef";
  }
  return (
    ts.isElementAccessExpression(node) &&
    ts.isStringLiteral(node.argumentExpression) &&
    node.argumentExpression.text === "forwardRef"
  );
};

const findForwardRef = (sourceFile: ts.SourceFile): ts.Node | undefined => {
  let offender: ts.Node | undefined;

  const visit = (node: ts.Node) => {
    if (offender) return;

    if (
      ts.isImportSpecifier(node) &&
      (node.propertyName?.text ?? node.name.text) === "forwardRef"
    ) {
      offender = node;
      return;
    }

    if (
      ts.isCallExpression(node) &&
      (ts.isIdentifier(node.expression)
        ? node.expression.text === "forwardRef"
        : isForwardRefProperty(node.expression))
    ) {
      offender = node;
      return;
    }

    ts.forEachChild(node, visit);
  };

  visit(sourceFile);
  return offender;
};

describe("React ref APIs", () => {
  it("does not use forwardRef in production source", () => {
    const offenders: string[] = [];

    for (const file of sourceFiles(sourceRoot)) {
      const source = readFileSync(file, "utf8");
      const sourceFile = ts.createSourceFile(
        file,
        source,
        ts.ScriptTarget.Latest,
        true,
        ts.ScriptKind.TSX,
      );

      const offender = findForwardRef(sourceFile);
      if (offender) {
        const { line } = sourceFile.getLineAndCharacterOfPosition(
          offender.getStart(sourceFile),
        );
        offenders.push(`${path.relative(sourceRoot, file)}:${line + 1}`);
      }
    }

    expect(offenders).toEqual([]);
  });
});
