import { readFileSync } from "node:fs";
import path from "node:path";

import { describe, expect, it } from "vitest";

import { buildAppTheme } from "@/theme";

const frontendRoot = process.cwd();
const readFrontendFile = (relativePath: string) =>
  readFileSync(path.join(frontendRoot, relativePath), "utf8");

describe("font loading", () => {
  it("keeps the initial page independent of external font services", () => {
    const indexHtml = readFrontendFile("index.html");

    expect(indexHtml).not.toContain("fonts.googleapis.com");
    expect(indexHtml).not.toContain("fonts.gstatic.com");
  });

  it("uses one bundled Inter font contract", () => {
    const theme = buildAppTheme("DARK");
    const entry = readFrontendFile("src/index.tsx");
    const variables = readFrontendFile("src/theme/variables.css");
    const typography = readFrontendFile("src/components/ui/app-typography.css");

    const cssFontFamily = variables
      .match(/--app-font-family:\s*([^;]+);/)?.[1]
      .replace(/\s+/g, "")
      .trim();

    expect(cssFontFamily).toBe(theme.typography.fontFamily.replace(/\s+/g, ""));
    expect(cssFontFamily?.startsWith('"InterVariable",Inter,')).toBe(true);
    expect(entry).toContain('import "@fontsource-variable/inter/wght.css";');
    expect(typography).toContain("font-family: var(--app-font-family);");
  });

  it("declares one mono stack in both the theme and the stylesheet", () => {
    const theme = buildAppTheme("DARK");
    const variables = readFrontendFile("src/theme/variables.css");

    const cssFontMono = variables
      .match(/--app-font-mono:\s*([^;]+);/)?.[1]
      .replace(/\s+/g, "")
      .trim();

    expect(cssFontMono).toBe(
      theme.typography.fontFamilyMono.replace(/\s+/g, ""),
    );
    // Bare `monospace` is the fallback of last resort, never the first choice:
    // it resolves per-OS to a face that does not sit with Inter.
    expect(cssFontMono?.startsWith("ui-monospace,")).toBe(true);
  });
});
