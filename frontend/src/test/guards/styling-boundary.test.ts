import { describe, expect, it } from "vitest";

import {
  readSource,
  relativeToSrc,
  sourceFiles,
} from "@/test/guards/sourceFiles";

/*
 * Styling flows one way. AppThemeProvider writes the theme to :root as
 * --app-* variables (theme/cssVariables.ts, theme/variables.css); the shared
 * components in components/ui consume them; everything else composes those
 * components or uses the variables directly. Reading the theme in JS,
 * computing colours, restating the type scale inline, or branching on the
 * colour scheme in JS is reserved for code that must hand a browser API a
 * resolved value: canvas strokes, xterm themes, and colour editors.
 * Those files are listed below with their reason. Nothing else may match,
 * and a listed file that stops matching must be removed from the list.
 */
const OWNED_BY_THE_THEME = [
  "components/ui/",
  "theme/",
  "components/charts/",
  "components/terminal/",
  "icons/",
  "test/",
];

const RESOLVED_COLOUR_READERS: { file: string; reason: string }[] = [
  {
    file: "components/docker/TerminalDialog.tsx",
    reason: "xterm's theme option takes resolved colours",
  },
  {
    file: "components/gauge/CircularGauge.tsx",
    reason: "the gauge gradient is interpolated from resolved hex values",
  },
  {
    file: "routes/_authenticated/-components/dock/Dock.tsx",
    reason: "dock tile gradients are colour maths on resolved values",
  },
  {
    file: "routes/_authenticated/-components/dock/dockPalette.ts",
    reason: "dock tile gradients are colour maths on resolved values",
  },
  {
    file: "routes/_authenticated/filebrowser/-components/fileTypeColors.ts",
    reason: "file-type icons use fixed hues that identify a kind of file",
  },
  {
    file: "routes/_authenticated/settings/-components/ThemeColorsSection.tsx",
    reason: "the theme colour editor previews and edits resolved colours",
  },
  {
    file: "components/charts/LiveChart.tsx",
    reason: "smoothie canvas strokes take resolved colours",
  },
  {
    file: "routes/_authenticated/network/-components/NetworkTrafficGraph.tsx",
    reason: "smoothie canvas strokes take resolved colours",
  },
  {
    file: "routes/_authenticated/settings/-components/DockAccentGradientEditor.tsx",
    reason: "a hex colour editor",
  },
  {
    file: "routes/_authenticated/terminal/-components/Terminal.tsx",
    reason: "xterm's theme option and ANSI palette take resolved colours",
  },
];

const PATTERNS: Record<string, RegExp> = {
  "useAppTheme()": /\buseAppTheme\b/,
  "@/theme/colorUtils import": /from "@\/theme\/colorUtils"/,
  "palette.mode branch": /palette\.mode/,
  "hex colour literal":
    /(?<![\w&])#(?:[0-9a-fA-F]{3,4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})\b/,
  "inline fontSize": /\bfontSize:/,
};

const allowed = new Set(RESOLVED_COLOUR_READERS.map(({ file }) => file));
const candidates = sourceFiles()
  .map(relativeToSrc)
  .filter((file) => !OWNED_BY_THE_THEME.some((dir) => file.startsWith(dir)))
  .sort();

describe("styling boundary", () => {
  it.each(Object.entries(PATTERNS))(
    "%s appears only in the listed resolved-colour readers",
    (_name, pattern) => {
      const offenders = candidates.filter(
        (file) => !allowed.has(file) && pattern.test(readSource(file)),
      );
      expect(offenders).toEqual([]);
    },
  );

  it("lists only files that still need a resolved colour", () => {
    const patterns = Object.values(PATTERNS);
    const stale = RESOLVED_COLOUR_READERS.filter(
      ({ file }) => !patterns.some((pattern) => pattern.test(readSource(file))),
    ).map(({ file }) => file);
    expect(stale).toEqual([]);
  });
});
