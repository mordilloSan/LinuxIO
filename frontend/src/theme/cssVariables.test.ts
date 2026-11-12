import { describe, expect, it } from "vitest";

import { buildAppTheme } from "@/theme/appTheme";
import { darken, toHexColor } from "@/theme/colorUtils";
import { getThemeCssVariables } from "@/theme/cssVariables";

const inset = (theme: ReturnType<typeof buildAppTheme>) =>
  getThemeCssVariables(theme)["--app-code-inset-background"];

describe("getThemeCssVariables", () => {
  it("sets code into the page a shade below the page's own background", () => {
    const dark = buildAppTheme("DARK");
    expect(inset(dark)).toBe(
      toHexColor(darken(dark.palette.background.default, 0.18)),
    );

    const light = buildAppTheme("LIGHT");
    expect(inset(light)).toBe(
      toHexColor(darken(light.palette.background.default, 0.03)),
    );
  });

  it("follows a background colour the user chose, not the code block colour", () => {
    const theme = buildAppTheme("DARK", undefined, {
      dark: { backgroundDefault: "#102030", codeBackground: "#1e1e1e" },
    });
    expect(inset(theme)).toBe(toHexColor(darken("#102030", 0.18)));
  });

  it("uses the colour the user set for it", () => {
    const theme = buildAppTheme("DARK", undefined, {
      dark: { backgroundDefault: "#102030", codeInsetBackground: "#223344" },
    });
    expect(inset(theme)).toBe("#223344");
    // A colour input needs a hex value for the unset default too.
    expect(inset(buildAppTheme("DARK"))).toMatch(/^#[0-9a-f]{6}$/i);
  });
});
