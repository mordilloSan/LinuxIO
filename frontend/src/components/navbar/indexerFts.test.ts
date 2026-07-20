import { describe, expect, it } from "vitest";

import { getFTSStatus } from "@/components/navbar/indexerFts";

describe("getFTSStatus", () => {
  it.each([
    [true, true, "Fast search enabled"],
    [false, false, "Fast indexing enabled"],
    [true, false, "Fast search pending"],
    [false, true, "Fast indexing pending"],
  ])(
    "reports desired=%s and active=%s",
    (ftsSearchEnabled, ftsActive, label) => {
      expect(getFTSStatus(ftsSearchEnabled, ftsActive).label).toBe(label);
    },
  );
});
