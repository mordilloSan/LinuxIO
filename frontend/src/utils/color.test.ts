import { describe, expect, it } from "vitest";

import {
  alpha,
  darken,
  fromHsl,
  interpolateHsl,
  lighten,
  toHexColor,
  toHsl,
} from "@/utils/color";

describe("color utilities", () => {
  it("applies alpha to supported CSS color formats", () => {
    expect(alpha("#abc", 0.5)).toBe("rgba(170, 187, 204, 0.5)");
    expect(alpha("#001122", 2)).toBe("rgba(0, 17, 34, 1)");
    expect(alpha("rgb(1, 2, 3)", -1)).toBe("rgba(1, 2, 3, 0)");
    expect(alpha("rgba(4, 5, 6, 0.2)", 0.7)).toBe("rgba(4, 5, 6, 0.7)");
    expect(alpha("hsl(10, 20%, 30%)", 0.4)).toBe("hsla(10, 20%, 30%, 0.4)");
  });

  it("lightens and darkens parsed colors", () => {
    expect(lighten("#000000", 0.5)).toBe("rgb(128, 128, 128)");
    expect(darken("rgb(100, 150, 200)", 0.5)).toBe("rgb(50, 75, 100)");
  });

  it("reads colors as HSL", () => {
    expect(toHsl("#ffffff")).toEqual({ h: 0, s: 0, l: 1 });
    expect(toHsl("#808080")).toEqual({ h: 0, s: 0, l: 128 / 255 });

    const accent = toHsl("#2196f3");
    expect(accent?.h).toBeCloseTo(207, 0);
    expect(accent?.s).toBeCloseTo(0.9, 1);
    expect(accent?.l).toBeCloseTo(0.54, 2);
  });

  it("round-trips a color through HSL", () => {
    const hsl = toHsl("#2196f3");
    expect(hsl).not.toBeNull();
    expect(fromHsl(hsl!.h, hsl!.s, hsl!.l)).toBe("rgb(33, 150, 243)");
  });

  it("formats picker-compatible hex colors", () => {
    expect(toHexColor("#abc")).toBe("#aabbcc");
    expect(toHexColor("rgb(33, 150, 243)")).toBe("#2196f3");
    expect(toHexColor("var(--brand)")).toBeNull();
  });

  it("interpolates colors along the shortest HSL path", () => {
    expect(interpolateHsl("#ff0000", "#0000ff", 0)).toBe("rgb(255, 0, 0)");
    expect(interpolateHsl("#ff0000", "#0000ff", 0.5)).toBe("rgb(255, 0, 255)");
    expect(interpolateHsl("#ff0000", "#0000ff", 1)).toBe("rgb(0, 0, 255)");
  });

  it("wraps and clamps HSL components", () => {
    // The dock adds a signed hue offset to an accent, so both ends of the
    // circle have to land on the same color as the unrotated value.
    expect(fromHsl(390, 0.5, 0.5)).toBe(fromHsl(30, 0.5, 0.5));
    expect(fromHsl(-30, 0.5, 0.5)).toBe(fromHsl(330, 0.5, 0.5));
    expect(fromHsl(0, 2, 2)).toBe("rgb(255, 255, 255)");
    expect(fromHsl(0, -1, -1)).toBe("rgb(0, 0, 0)");
  });

  it("returns null for colors it cannot parse as HSL", () => {
    expect(toHsl("var(--brand)")).toBeNull();
    expect(toHsl("currentColor")).toBeNull();
  });

  it("falls back to color-mix for unparsed colors", () => {
    expect(lighten("var(--brand)", 0.25)).toBe(
      "color-mix(in srgb, var(--brand) 75%, white 25%)",
    );
    expect(darken("currentColor", 0.1)).toBe(
      "color-mix(in srgb, currentColor 90%, black 10%)",
    );
  });
});
