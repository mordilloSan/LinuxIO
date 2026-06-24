import { describe, expect, it } from "vitest";

import { alpha, darken, lighten } from "@/utils/color";

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

  it("falls back to color-mix for unparsed colors", () => {
    expect(lighten("var(--brand)", 0.25)).toBe(
      "color-mix(in srgb, var(--brand) 75%, white 25%)",
    );
    expect(darken("currentColor", 0.1)).toBe(
      "color-mix(in srgb, currentColor 90%, black 10%)",
    );
  });
});
