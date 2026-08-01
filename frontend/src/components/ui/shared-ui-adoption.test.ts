import fs from "node:fs";
import path from "node:path";

import { describe, expect, it } from "vitest";

const srcRoot = path.resolve(process.cwd(), "src");

/**
 * These are the current, reviewed exceptions. Counts make the guard fail when
 * a new native control is added to an exception file without coupling the test
 * to line numbers.
 */
const allowedNativeControls: Record<string, number> = {
  "components/cards/FileCard.tsx": 1,
  "components/filebrowser/Breadcrumbs.tsx": 1,
  "components/filebrowser/FileBrowserPanels.tsx": 2,
  "components/filebrowser/FileListRow.tsx": 1,
  "components/tabbar/TabSelector.tsx": 1,
  "components/tables/AppDataTable.tsx": 1,
  "components/tables/AppVirtualDataTable.tsx": 1,
  "routes/_authenticated/-components/navbar/ThemeColorsSection.tsx": 2,
};

const allowedButtonRoles: Record<string, number> = {
  "components/cards/FilesystemCard.tsx": 1,
  "components/cards/UserCard.tsx": 1,
  "routes/_authenticated/-components/navbar/NavbarNotificationsDropdown.tsx": 2,
};

function listProductionTsxFiles(dir: string): string[] {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const entryPath = path.join(dir, entry.name);
    if (entry.isDirectory()) return listProductionTsxFiles(entryPath);
    if (!entry.name.endsWith(".tsx")) return [];
    if (
      entry.name.endsWith(".test.tsx") ||
      entry.name.endsWith(".stories.tsx")
    ) {
      return [];
    }
    return [entryPath];
  });
}

function lineNumberForIndex(source: string, index: number) {
  return source.slice(0, index).split("\n").length;
}

function collectViolations(
  allowed: Record<string, number>,
  pattern: RegExp,
  violation: string,
  suggestion: string,
) {
  return listProductionTsxFiles(srcRoot).flatMap((filePath) => {
    const relativePath = path
      .relative(srcRoot, filePath)
      .replaceAll(path.sep, "/");
    // Shared controls are the one legitimate place for their native backing
    // elements; all other production code should compose those controls.
    if (relativePath.startsWith("components/ui/")) return [];

    const source = fs.readFileSync(filePath, "utf8");
    const matches = [...source.matchAll(pattern)];
    const extraMatches = matches.slice(allowed[relativePath] ?? 0);
    return extraMatches.map((match) => {
      const line = lineNumberForIndex(source, match.index ?? 0);
      return `${relativePath}:${line} ${violation}; use ${suggestion}`;
    });
  });
}

describe("shared UI adoption", () => {
  it("does not add native form controls outside shared UI components", () => {
    const violations = collectViolations(
      allowedNativeControls,
      /<(?:button|input|select|textarea)\b/g,
      "uses a native control",
      "AppButton, AppTextField, AppSelect, or another shared UI component",
    );

    expect(violations).toEqual([]);
  });

  it("does not add role=button outside shared UI components", () => {
    const violations = collectViolations(
      allowedButtonRoles,
      /\brole\s*=\s*(?:["']button["']|\{[^}\n]*["']button["'][^}\n]*\})/g,
      "emulates a button with ARIA",
      "AppButton or AppIconButton (and a keyboard-accessible shared primitive)",
    );

    expect(violations).toEqual([]);
  });
});
