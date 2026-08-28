import fs from "node:fs";
import path from "node:path";

import { describe, expect, it } from "vitest";

const srcRoot = path.resolve(process.cwd(), "src");

interface ReviewedException {
  file: string;
  tag?: "button" | "input";
  /** Attribute text that must appear somewhere in the element's opening tag. */
  attribute: string;
  reason: string;
  protects: string;
}

// Native controls that are themselves the specialized interaction (file/color
// inputs, rename fields, table sort, tabs). An exception is keyed on the file,
// the tag, and one distinguishing attribute, so reordering or adding props does
// not break it. A new exception must name its rationale and protected behavior
// rather than increasing an opaque per-file count.
const nativeControlExceptions: ReviewedException[] = [
  {
    file: "components/cards/FileCard.tsx",
    tag: "input",
    attribute: 'className="file-card-rename-input"',
    reason: "inline file rename input",
    protects: "rename editing, commit, cancel, and propagation isolation",
  },
  {
    file: "components/filebrowser/Breadcrumbs.tsx",
    tag: "input",
    attribute: 'className="linuxio-range-input"',
    reason: "native gallery-size range control",
    protects: "continuous gallery-size adjustment",
  },
  {
    file: "components/filebrowser/FileBrowserPanels.tsx",
    tag: "input",
    attribute: 'type="file"',
    reason: "hidden native file and directory inputs",
    protects: "browser file and directory picker behavior",
  },
  {
    file: "components/filebrowser/FileListRow.tsx",
    tag: "input",
    attribute: 'className="file-row-rename-input"',
    reason: "inline file rename input",
    protects: "rename editing, commit, cancel, and propagation isolation",
  },
  {
    file: "components/tabbar/TabSelector.tsx",
    tag: "button",
    attribute: "aria-selected=",
    reason: "native backing for a tablist",
    protects: "tab semantics and selected state",
  },
  {
    file: "components/tables/tableShared.tsx",
    tag: "button",
    attribute: 'className="app-dt__sort-button"',
    reason: "table-library sort control",
    protects: "column sorting and table header semantics",
  },
  {
    file: "routes/_authenticated/docker/-components/ContainerTable.tsx",
    tag: "button",
    attribute: 'className="container-table__stack-toggle"',
    reason: 'the ports/volumes "+N more" count line is itself the row expander',
    protects:
      "caption-sized inline text and chevron that must sit in the stack's line box, which no shared button offers",
  },
  {
    file: "routes/_authenticated/-components/navbar/ThemeColorsSection.tsx",
    tag: "input",
    attribute: "Hex color for",
    reason: "compact hexadecimal color editor",
    protects: "direct keyboard color editing and validation",
  },
  {
    file: "routes/_authenticated/-components/navbar/ThemeColorsSection.tsx",
    tag: "input",
    attribute: 'type="color"',
    reason: "hidden native color picker input",
    protects: "native color-picker integration",
  },
  {
    file: "routes/_authenticated/settings/-components/DockAccentGradientEditor.tsx",
    tag: "input",
    attribute: 'type="color"',
    reason: "visible dock gradient endpoint picker",
    protects: "native color selection with a direct visual swatch",
  },
  {
    file: "routes/_authenticated/settings/-components/DockAccentGradientEditor.tsx",
    tag: "button",
    attribute: "aria-pressed=",
    reason: "dock palette range tile",
    protects:
      "each gradient stop is a pressable swatch whose entire face is the color being chosen, which no shared button offers",
  },
];

const buttonRoleExceptions: ReviewedException[] = [
  {
    file: "routes/_authenticated/-components/navbar/NavbarNotificationsDropdown.tsx",
    attribute: 'role={isIndexer ? "button" : undefined}',
    reason: "specialized notification list item",
    protects: "existing tab, Enter/Space, focus, and indexer-detail behavior",
  },
];

// Negative guards: audited spots that used to be clickable non-interactive
// elements and must stay migrated to shared controls.
const knownClickableNonInteractive: { file: string; pattern: RegExp }[] = [
  {
    file: "routes/_authenticated/-dashboard/SystemOverview.tsx",
    pattern: /<div\b[^>]*\bonClick=\{onEdit\}/,
  },
  {
    file: "components/docker/DockerComposeProgress.tsx",
    pattern: /<div\b[^>]*\bonClick=\{hasLayers \? onToggle : undefined\}/,
  },
  {
    file: "components/cards/DashboardCard.tsx",
    pattern: /<div\b[^>]*\bonClick=\{[\s\S]{0,200}?setIconTextMenuAnchor/,
  },
  {
    file: "routes/_authenticated/-components/navbar/NavbarNotificationsDropdown.tsx",
    pattern:
      /<div\b[^>]*className="app-navbar-notifications__peek"[^>]*onClick=\{handlePeekClick\}/,
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

// The opening tag that contains `index`: from the `<` at or before it to the
// first `>` outside of JSX expression braces.
function openingTagAt(source: string, index: number) {
  const start = source.lastIndexOf("<", index);
  let depth = 0;
  for (let i = start; i < source.length; i++) {
    const ch = source[i];
    if (ch === "{") depth++;
    else if (ch === "}") depth--;
    else if (ch === ">" && depth === 0) return source.slice(start, i + 1);
  }
  return source.slice(start);
}

function isReviewedException(
  file: string,
  source: string,
  index: number,
  exceptions: ReviewedException[],
) {
  const tag = openingTagAt(source, index);
  return exceptions.some(
    (exception) =>
      exception.file === file &&
      (!exception.tag || tag.startsWith(`<${exception.tag}`)) &&
      tag.includes(exception.attribute),
  );
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
      if (isReviewedException(file, source, match.index ?? 0, exceptions)) {
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
        "uses an unreviewed native control; use a shared control or add a documented exception",
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

  it("does not nest AppButton inside links", () => {
    expect(
      collectUnreviewed(
        /<a\b(?:(?!<\/a>)[\s\S])*?<AppButton\b|<Link\b(?:(?!<\/Link>)[\s\S])*?<AppButton\b/g,
        [],
        "nests an AppButton inside a link; use AppLinkButton or AppRouterLinkButton",
      ),
    ).toEqual([]);
  });
});
