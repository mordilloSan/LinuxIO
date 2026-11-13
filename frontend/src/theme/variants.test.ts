import { readFileSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

import variants from "@/theme/variants";

// Vitest runs from frontend/.
const THEME_COLORS_GO = join(
  process.cwd(),
  "../backend/bridge/internal/userconfig/themecolors.go",
);

type Variant = (typeof variants)[number];

// Where each backend theme colour key lives in a variant. The keys the
// backend leaves empty (the UI derives them) are not listed.
const VARIANT_FIELD: Record<string, (v: Variant) => string> = {
  backgroundDefault: (v) => v.palette.background.default,
  backgroundPaper: (v) => v.palette.background.paper,
  headerBackground: (v) => v.header.background,
  footerBackground: (v) => v.footer.background,
  sidebarBackground: (v) => v.sidebar.background,
  cardBackground: (v) => v.card.background,
  dialogBorder: (v) => v.dialog.border,
  dialogGlow: (v) => v.dialog.glow,
  dialogBackdrop: (v) => v.dialog.backdrop,
  codeBackground: (v) => v.codeBlock.background,
  codeText: (v) => v.codeBlock.color,
  chartRx: (v) => v.chart.rx,
  chartTx: (v) => v.chart.tx,
  chartNeutral: (v) => v.chart.neutral,
  fileBrowserSurface: (v) => v.fileBrowser.surface,
  fileBrowserChrome: (v) => v.fileBrowser.chrome,
  fileBrowserBreadcrumbBackground: (v) => v.fileBrowser.breadcrumbBackground,
  fileBrowserBreadcrumbText: (v) => v.fileBrowser.breadcrumbText,
};

function backendDefaults(): { key: string; light: string; dark: string }[] {
  const source = readFileSync(THEME_COLORS_GO, "utf8");
  return [...source.matchAll(/^\s*\{"(\w+)", "([^"]*)", "([^"]*)"\},$/gm)].map(
    ([, key, light, dark]) => ({ key, light, dark }),
  );
}

describe("theme variants", () => {
  it("start from the colours the backend defaults to", () => {
    const light = variants.find((v) => v.palette.mode === "light")!;
    const dark = variants.find((v) => v.palette.mode === "dark")!;
    const defaults = backendDefaults();
    expect(defaults.length).toBeGreaterThan(10);

    for (const { key, light: lightColor, dark: darkColor } of defaults) {
      if (!lightColor && !darkColor) continue;
      const field = VARIANT_FIELD[key];
      expect(field, `variants.ts has no field for ${key}`).toBeDefined();
      expect(field(light).toUpperCase(), `${key} light`).toBe(
        lightColor.toUpperCase(),
      );
      expect(field(dark).toUpperCase(), `${key} dark`).toBe(
        darkColor.toUpperCase(),
      );
    }
  });
});
