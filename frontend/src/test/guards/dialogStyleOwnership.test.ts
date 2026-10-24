import { readFileSync } from "node:fs";

import ts from "typescript";
import { expect, it } from "vitest";

import { relativeToSrc, sourceFiles } from "./sourceFiles";

it("keeps dialog chrome in the shared components", () => {
  const violations: string[] = [];
  for (const file of sourceFiles()) {
    const relative = relativeToSrc(file);
    if (relative.startsWith("components/ui/") || relative.startsWith("test/"))
      continue;
    const source = readFileSync(file, "utf8");
    if (!/Dialog|\b(confirm|alert|prompt)\s*\(/.test(source)) continue;
    const tree = ts.createSourceFile(
      file,
      source,
      ts.ScriptTarget.Latest,
      true,
      ts.ScriptKind.TSX,
    );
    const visit = (node: ts.Node) => {
      if (ts.isCallExpression(node)) {
        const expression = node.expression;
        const name = ts.isIdentifier(expression)
          ? expression.text
          : ts.isPropertyAccessExpression(expression) &&
              expression.expression.getText(tree) === "window"
            ? expression.name.text
            : "";
        if (["confirm", "alert", "prompt"].includes(name)) {
          const line =
            tree.getLineAndCharacterOfPosition(node.getStart(tree)).line + 1;
          violations.push(`${relative}:${line} native ${name} dialog`);
        }
      }
      if (ts.isJsxOpeningElement(node) || ts.isJsxSelfClosingElement(node)) {
        const tag = node.tagName.getText(tree);
        const isChrome = tag === "AppDialogTitle" || tag === "AppDialogActions";
        const isContent = tag === "AppDialogContent";
        const isPaper = [
          "GeneralDialog",
          "FileBrowserDialog",
          "AppDialog",
        ].includes(tag);
        for (const attribute of node.attributes.properties) {
          if (!ts.isJsxAttribute(attribute)) continue;
          const attributeName = attribute.name.getText(tree);
          if (
            !(
              (isPaper && attributeName === "paperStyle") ||
              ((isChrome || isContent) && attributeName === "style")
            )
          )
            continue;
          const initializer = attribute.initializer;
          if (
            !initializer ||
            !ts.isJsxExpression(initializer) ||
            !initializer.expression ||
            !ts.isObjectLiteralExpression(initializer.expression)
          )
            continue;
          for (const property of initializer.expression.properties) {
            if (!ts.isPropertyAssignment(property)) continue;
            const name = property.name.getText(tree).replaceAll(/["']/g, "");
            const padding = /^padding/.test(name);
            const paint =
              /^(background|border|boxShadow|fontSize|fontWeight)/.test(name);
            if (
              (isContent && padding) ||
              ((isChrome || isPaper) && (padding || paint))
            ) {
              const line =
                tree.getLineAndCharacterOfPosition(property.getStart(tree))
                  .line + 1;
              violations.push(
                `${relative}:${line} ${tag}.${attributeName}.${name}`,
              );
            }
          }
        }
      }
      ts.forEachChild(node, visit);
    };
    visit(tree);
  }
  expect(violations).toEqual([]);
});
