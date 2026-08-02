import fs from "node:fs";
import path from "node:path";

import { describe, expect, it } from "vitest";

const srcRoot = path.resolve(process.cwd(), "src");

interface ReviewedException {
  file: string;
  pattern: RegExp;
  reason: string;
  protects: string;
}

// These are exact, stable patterns for native controls that are themselves
// the specialized interaction (file/color inputs, rename fields, table sort,
// and tabs). A new exception must name its file, pattern, rationale, and
// protected behavior rather than increasing an opaque per-file count.
const nativeControlExceptions: ReviewedException[] = [
  {
    file: "components/cards/FileCard.tsx",
    pattern: /<input\s+onBlur=\{handleRenameBlur\}/,
    reason: "inline file rename input",
    protects: "rename editing, commit, cancel, and propagation isolation",
  },
  {
    file: "components/filebrowser/Breadcrumbs.tsx",
    pattern: /<input\s+className="linuxio-range-input"/,
    reason: "native gallery-size range control",
    protects: "continuous gallery-size adjustment",
  },
  {
    file: "components/filebrowser/FileBrowserPanels.tsx",
    pattern: /<input\s+multiple[\s\S]{0,180}?type="file"/g,
    reason: "hidden native file and directory inputs",
    protects: "browser file and directory picker behavior",
  },
  {
    file: "components/filebrowser/FileListRow.tsx",
    pattern: /<input\s+onBlur=\{handleRenameBlur\}/,
    reason: "inline file rename input",
    protects: "rename editing, commit, cancel, and propagation isolation",
  },
  {
    file: "components/tabbar/TabSelector.tsx",
    pattern: /<button\s+aria-selected=\{value === opt\.value\}/,
    reason: "native backing for a tablist",
    protects: "tab semantics and selected state",
  },
  {
    file: "components/tables/AppDataTable.tsx",
    pattern: /<button\s+className="app-vdt__sort-button"/,
    reason: "table-library sort control",
    protects: "column sorting and table header semantics",
  },
  {
    file: "components/tables/AppVirtualDataTable.tsx",
    pattern: /<button\s+className="app-vdt__sort-button"/,
    reason: "table-library sort control",
    protects: "column sorting and table header semantics",
  },
  {
    file: "routes/_authenticated/-components/navbar/ThemeColorsSection.tsx",
    pattern: /<input\s+aria-label=\{`Hex color for \$\{label\}`\}/,
    reason: "compact hexadecimal color editor",
    protects: "direct keyboard color editing and validation",
  },
  {
    file: "routes/_authenticated/-components/navbar/ThemeColorsSection.tsx",
    pattern: /<input\s+aria-hidden="true"[\s\S]{0,500}?type="color"/,
    reason: "hidden native color picker input",
    protects: "native color-picker integration",
  },
];

const buttonRoleExceptions: ReviewedException[] = [
  {
    file: "routes/_authenticated/-components/navbar/NavbarNotificationsDropdown.tsx",
    pattern: /role=\{isIndexer \? "button" : undefined\}/g,
    reason: "specialized notification list item",
    protects: "existing tab, Enter/Space, focus, and indexer-detail behavior",
  },
];

const knownClickableNonInteractive: ReviewedException[] = [
  {
    file: "routes/_authenticated/-dashboard/SystemOverview.tsx",
    pattern: /<div\b[^>]*\bonClick=\{onEdit\}/,
    reason: "audited editable overview row",
    protects: "row edit action must remain a named shared button",
  },
  {
    file: "components/docker/DockerComposeProgress.tsx",
    pattern: /<div\b[^>]*\bonClick=\{hasLayers \? onToggle : undefined\}/,
    reason: "audited compose group disclosure",
    protects: "group expansion must remain a shared disclosure button",
  },
  {
    file: "components/cards/DashboardCard.tsx",
    pattern: /<div\b[^>]*\bonClick=\{[\s\S]{0,200}?setIconTextMenuAnchor/,
    reason: "audited dashboard option trigger",
    protects: "option menu must remain a named shared menu trigger",
  },
  {
    file: "routes/_authenticated/-components/navbar/NavbarNotificationsDropdown.tsx",
    pattern:
      /<div\b[^>]*className="app-navbar-notifications__peek"[^>]*onClick=\{handlePeekClick\}/,
    reason: "audited notification peek",
    protects:
      "peek remains a named shared button with native keyboard behavior",
  },
];

const knownNestedInteractive: ReviewedException[] = [
  {
    file: "routes/_authenticated/-components/update/UpdateBanner.tsx",
    pattern: /<a[\s\S]{0,500}?<AppButton\b/,
    reason: "audited release-notes link/button nesting defect",
    protects: "navigation remains one semantic link without nested controls",
  },
  {
    file: "routes/_authenticated/-components/navbar/NavbarNotificationsDropdown.tsx",
    pattern: /<Link[\s\S]{0,500}?<AppButton\b/,
    reason: "audited recent-notification link/button nesting defect",
    protects: "recent-notification navigation remains one semantic link",
  },
];

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

function relative(filePath: string) {
  return path.relative(srcRoot, filePath).replaceAll(path.sep, "/");
}

function isExactException(
  file: string,
  source: string,
  index: number,
  exceptions: ReviewedException[],
) {
  return exceptions.some((exception) => {
    if (exception.file !== file) return false;
    exception.pattern.lastIndex = 0;
    return exception.pattern.exec(source.slice(index))?.index === 0;
  });
}

function collectUnreviewed(
  pattern: RegExp,
  exceptions: ReviewedException[],
  message: string,
) {
  return listProductionTsxFiles(srcRoot).flatMap((filePath) => {
    const file = relative(filePath);
    if (file.startsWith("components/ui/")) return [];
    const source = fs.readFileSync(filePath, "utf8");
    pattern.lastIndex = 0;
    return [...source.matchAll(pattern)].flatMap((match) => {
      if (isExactException(file, source, match.index ?? 0, exceptions)) {
        return [];
      }
      const line = source.slice(0, match.index ?? 0).split("\n").length;
      return [`${file}:${line} ${message}`];
    });
  });
}

describe("shared UI adoption", () => {
  it("allows only explicitly reviewed native-control exceptions", () => {
    expect(
      collectUnreviewed(
        /<(?:button|input|select|textarea)\b/g,
        nativeControlExceptions,
        "uses an unreviewed native control; use a shared control or add a documented exact exception",
      ),
    ).toEqual([]);
  });

  it("allows only explicitly reviewed button-role exceptions", () => {
    expect(
      collectUnreviewed(
        /\brole\s*=\s*(?:["']button["']|\{[^}\n]*["']button["'][^}\n]*\})/g,
        buttonRoleExceptions,
        "emulates a button with an unreviewed ARIA role; use a shared button",
      ),
    ).toEqual([]);
  });

  it("keeps audited clickable non-interactive patterns migrated", () => {
    expect(
      knownClickableNonInteractive.flatMap(({ file, pattern }) => {
        const source = fs.readFileSync(path.join(srcRoot, file), "utf8");
        pattern.lastIndex = 0;
        return pattern.test(source) ? [`${file} matches ${pattern}`] : [];
      }),
    ).toEqual([]);
  });

  it("does not make FrostedCard own feature interactions", () => {
    expect(
      collectUnreviewed(
        /<FrostedCard\b[^>]*\bonClick=/g,
        [],
        "makes FrostedCard interactive; use a semantic AppButton trigger region",
      ),
    ).toEqual([]);
  });

  it("keeps audited nested interactive patterns migrated", () => {
    expect(
      knownNestedInteractive.flatMap(({ file, pattern }) => {
        const source = fs.readFileSync(path.join(srcRoot, file), "utf8");
        pattern.lastIndex = 0;
        return pattern.test(source) ? [`${file} matches ${pattern}`] : [];
      }),
    ).toEqual([]);
  });
});
