import { readFileSync } from "node:fs";
import path from "node:path";
import { runInNewContext } from "node:vm";

import { afterEach, describe, expect, it } from "vitest";

import { buildAppTheme } from "@/theme";
import {
  BOOT_THEME_HINT_KEY,
  clearBootThemeHint,
  writeBootThemeHint,
} from "@/theme/bootThemeHint";

const indexHtml = readFileSync(path.join(process.cwd(), "index.html"), "utf8");
const bootScript = /<script>([\s\S]*?)<\/script>/.exec(indexHtml)?.[1] ?? "";

const BOOT_VARIABLES = [
  "--app-color-scheme",
  "--app-palette-background-default",
  "--app-palette-text-primary",
  "--app-palette-primary-main",
];

// Runs the inline script of index.html as the browser does before first paint.
function bootAt(pathname: string) {
  window.history.replaceState({}, "", pathname);
  runInNewContext(bootScript, {
    window,
    document,
    location: window.location,
    localStorage: window.localStorage,
  });
  const style = document.documentElement.style;
  return {
    scheme: style.getPropertyValue("--app-color-scheme"),
    bg: style.getPropertyValue("--app-palette-background-default"),
    text: style.getPropertyValue("--app-palette-text-primary"),
    primary: style.getPropertyValue("--app-palette-primary-main"),
  };
}

function storeHint(hint: unknown) {
  window.localStorage.setItem(BOOT_THEME_HINT_KEY, JSON.stringify(hint));
}

function coloursOf(theme: ReturnType<typeof buildAppTheme>) {
  return {
    scheme: theme.colorScheme,
    bg: theme.palette.background.default,
    text: theme.palette.text.primary,
    primary: theme.palette.primary.main,
  };
}

describe("boot theme hint", () => {
  afterEach(() => {
    for (const name of BOOT_VARIABLES) {
      document.documentElement.style.removeProperty(name);
    }
    window.history.replaceState({}, "", "/");
  });

  it("is read by the page's inline boot script under the same key", () => {
    expect(bootScript).toContain(`"${BOOT_THEME_HINT_KEY}"`);
  });

  it("paints the theme the app last used", () => {
    const light = buildAppTheme("LIGHT");
    writeBootThemeHint(light);

    expect(bootAt("/")).toEqual(coloursOf(light));
  });

  it("falls back to the dark theme's own colours without a hint", () => {
    writeBootThemeHint(buildAppTheme("LIGHT"));
    clearBootThemeHint();

    expect(bootAt("/")).toEqual(coloursOf(buildAppTheme("DARK")));
  });

  it("keeps the sign-in page dark whatever the hint says", () => {
    writeBootThemeHint(buildAppTheme("LIGHT"));

    expect(bootAt("/sign-in")).toEqual(coloursOf(buildAppTheme("DARK")));
  });

  it("ignores a hint that is not valid JSON or has no known scheme", () => {
    window.localStorage.setItem(BOOT_THEME_HINT_KEY, "{not json");
    expect(bootAt("/").scheme).toBe("dark");

    storeHint({ scheme: "sepia", bg: "#ffffff" });
    expect(bootAt("/").scheme).toBe("dark");
  });

  it("replaces anything that is not a plain colour with the theme's own colour", () => {
    storeHint({
      scheme: "light",
      bg: "url(https://example.invalid/x)",
      text: "red; background: url(x)",
      primary: "var(--anything)",
    });

    expect(bootAt("/")).toEqual(coloursOf(buildAppTheme("LIGHT")));
  });

  it("accepts hex, rgb and hsl colours", () => {
    const hint = {
      scheme: "dark",
      bg: "#102030",
      text: "rgba(255, 255, 255, 0.9)",
      primary: "hsl(210 60% 50% / 0.8)",
    };
    storeHint(hint);

    expect(bootAt("/")).toEqual(hint);
  });
});
