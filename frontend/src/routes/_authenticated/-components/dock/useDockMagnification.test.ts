import { describe, expect, it } from "vitest";

import {
  calculateDockRenderScale,
  calculateDockTargets,
  DOCK_TILE_SIZE,
  DOCK_TILE_SIZE_MAX,
} from "./useDockMagnification";

describe("calculateDockRenderScale", () => {
  it("renders the visible range from the maximum raster size", () => {
    const restScale = calculateDockRenderScale(1);
    const peakScale = calculateDockRenderScale(
      DOCK_TILE_SIZE_MAX / DOCK_TILE_SIZE,
    );

    expect(restScale).toBe(0.625);
    expect(restScale * DOCK_TILE_SIZE_MAX).toBe(DOCK_TILE_SIZE);
    expect(peakScale).toBe(1);
  });
});

describe("calculateDockTargets", () => {
  it("returns rest targets out of range", () => {
    expect(calculateDockTargets([100, 150], Number.POSITIVE_INFINITY)).toEqual([
      { scale: 1, x: 0 },
      { scale: 1, x: 0 },
    ]);
  });

  it("centers cumulative expansion around the original dock", () => {
    const targets = calculateDockTargets([100, 140, 180], 140);
    expect(targets[1].scale).toBe(1.6);
    expect(targets[0].x + targets[2].x).toBeCloseTo(0);
  });

  it("reconstructs the same centers as cumulative flex expansion", () => {
    const targets = calculateDockTargets([20, 66, 112, 158], 66);
    const expansions = targets.map(({ scale }) => (scale - 1) * 40);
    const totalExpansion = expansions.reduce((sum, value) => sum + value, 0);
    let precedingExpansion = 0;

    targets.forEach(({ x }, index) => {
      expect(x).toBeCloseTo(
        precedingExpansion + expansions[index] / 2 - totalExpansion / 2,
      );
      precedingExpansion += expansions[index];
    });
  });
});
