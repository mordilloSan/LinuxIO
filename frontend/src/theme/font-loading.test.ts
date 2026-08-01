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
});
