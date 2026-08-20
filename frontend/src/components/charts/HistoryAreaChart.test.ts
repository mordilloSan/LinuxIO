import { describe, expect, it } from "vitest";

import { downsamplePoints } from "./HistoryAreaChart";

describe("downsamplePoints", () => {
  it("preserves unavailable samples as gaps while averaging measured runs", () => {
    const points = Array.from({ length: 7 }, (_, index) => ({
      t: index,
      v: index === 3 ? null : index + 1,
    }));

    expect(downsamplePoints(points, 2)).toEqual([
      { t: 0.5, v: 1.5, detail: undefined },
      { t: 2, v: 3, detail: undefined },
      { t: 3, v: null },
      { t: 4.5, v: 5.5, detail: undefined },
      { t: 6, v: 7, detail: undefined },
    ]);
  });

  it("keeps zero as a measured value", () => {
    expect(
      downsamplePoints(
        [
          { t: 0, v: 0 },
          { t: 1, v: 2 },
          { t: 2, v: null },
          { t: 3, v: 4 },
        ],
        2,
      ),
    ).toEqual([
      { t: 0, v: 0, detail: undefined },
      { t: 1, v: 2, detail: undefined },
      { t: 2, v: null },
      { t: 3, v: 4, detail: undefined },
    ]);
  });
});
