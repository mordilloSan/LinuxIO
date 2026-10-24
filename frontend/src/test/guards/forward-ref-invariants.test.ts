import ts from "typescript";
import { describe, expect, it } from "vitest";

import {
  readSource,
  relativeToSrc,
  sourceFiles,
} from "@/test/guards/sourceFiles";

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

    for (const file of sourceFiles().map(relativeToSrc)) {
      const source = readSource(file);
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
        offenders.push(`${file}:${line + 1}`);
      }
    }

    expect(offenders).toEqual([]);
  });
});
