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

const callsUseEffectEvent = (node: ts.Node): boolean => {
  let found = false;
  const visit = (child: ts.Node) => {
    if (found) return;
    if (
      ts.isCallExpression(child) &&
      ts.isIdentifier(child.expression) &&
      child.expression.text === "useEffectEvent"
    ) {
      found = true;
      return;
    }
    ts.forEachChild(child, visit);
  };
  ts.forEachChild(node, visit);
  return found;
};

/**
 * React 19.2's useEffectEvent does not track props or state inside a
 * forwardRef render function: the event keeps the values it closed over on the
 * first render forever. It fails silently — the component still renders, it
 * just acts on stale data (a live chart that appended the same sample every
 * tick, drawing a flat line, is how this was found).
 *
 * forwardRef is deprecated in React 19 anyway; take `ref` as a normal prop.
 */
describe("useEffectEvent and forwardRef", () => {
  it("is never used inside a forwardRef render function", () => {
    const offenders: string[] = [];

    for (const file of sourceFiles(sourceRoot)) {
      const source = readFileSync(file, "utf8");
      if (
        !source.includes("forwardRef") ||
        !source.includes("useEffectEvent")
      ) {
        continue;
      }

      const sourceFile = ts.createSourceFile(
        file,
        source,
        ts.ScriptTarget.Latest,
        true,
        ts.ScriptKind.TSX,
      );

      const visit = (node: ts.Node) => {
        if (
          ts.isCallExpression(node) &&
          ts.isIdentifier(node.expression) &&
          node.expression.text === "forwardRef" &&
          node.arguments.some(callsUseEffectEvent)
        ) {
          const { line } = sourceFile.getLineAndCharacterOfPosition(
            node.getStart(sourceFile),
          );
          offenders.push(`${path.relative(sourceRoot, file)}:${line + 1}`);
        }
        ts.forEachChild(node, visit);
      };
      visit(sourceFile);
    }

    expect(offenders).toEqual([]);
  });
});
