import { describe, expect, it } from "vitest";

import { batchFailures, describeBatchFailures } from "./batchFailures";

describe("batchFailures", () => {
  it("returns the failed items or an empty list for results without any", () => {
    const failed = [{ error: "denied", path: "/a" }];

    expect(batchFailures({ failed })).toBe(failed);
    expect(batchFailures(undefined)).toEqual([]);
    expect(batchFailures(null)).toEqual([]);
    expect(batchFailures({ total: 1 })).toEqual([]);
  });

  it("describes failures by name and truncates with a remainder", () => {
    const failed = Array.from({ length: 7 }, (_, i) => ({
      error: "destination already exists",
      path: `/srv/file-${i}.txt`,
    }));

    expect(describeBatchFailures(failed.slice(0, 1))).toBe(
      "file-0.txt: destination already exists",
    );
    expect(describeBatchFailures(failed).split("\n")).toEqual([
      ...failed.slice(0, 5).map((f, i) => `file-${i}.txt: ${f.error}`),
      "and 2 more",
    ]);
  });
});
