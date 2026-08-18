import { describe, expect, it } from "vitest";

import {
  DEFAULT_DOCK_ACCENT_GRADIENT,
  defaultDockAccentColors,
  resolveDockAccentGradient,
  sampleDockAccentColor,
} from "./dockPalette";

describe("dock accent palette", () => {
  it("derives the familiar 60 degree family from the theme accent", () => {
    expect(defaultDockAccentColors("#2196f3")).toEqual([
      "rgb(33, 243, 231)",
      "rgb(33, 45, 243)",
    ]);
  });

  it("keeps empty endpoint values attached to the live theme accent", () => {
    expect(
      resolveDockAccentGradient("#2196f3", DEFAULT_DOCK_ACCENT_GRADIENT),
    ).toEqual({
      startColor: "rgb(33, 243, 231)",
      endColor: "rgb(33, 45, 243)",
      rangeStart: 0,
      rangeEnd: 100,
    });
    expect(
      sampleDockAccentColor("#2196f3", DEFAULT_DOCK_ACCENT_GRADIENT, 0.5),
    ).toBe("rgb(33, 150, 243)");
  });

  it("samples only the selected part of a custom gradient", () => {
    const value = {
      startColor: "#ff0000",
      endColor: "#0000ff",
      rangeStart: 25,
      rangeEnd: 75,
    };

    expect(sampleDockAccentColor("#2196f3", value, 0)).toBe("rgb(255, 0, 128)");
    expect(sampleDockAccentColor("#2196f3", value, 0.5)).toBe(
      "rgb(255, 0, 255)",
    );
    expect(sampleDockAccentColor("#2196f3", value, 1)).toBe("rgb(128, 0, 255)");
  });

  it("keeps the theme-derived fallback when only one edge is customized", () => {
    const value = {
      startColor: "#ff0000",
      rangeStart: 0,
      rangeEnd: 100,
    };

    expect(sampleDockAccentColor("#2196f3", value, 0)).toBe("rgb(255, 0, 0)");
    expect(sampleDockAccentColor("#2196f3", value, 1)).toBe("rgb(33, 45, 243)");
  });

  it("defensively clamps malformed cached ranges", () => {
    expect(
      resolveDockAccentGradient("#2196f3", {
        startColor: "#111111",
        endColor: "#eeeeee",
        rangeStart: 120,
        rangeEnd: -20,
      }),
    ).toMatchObject({ rangeStart: 100, rangeEnd: 100 });
  });
});
