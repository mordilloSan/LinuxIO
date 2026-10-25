import { describe, expect, it } from "vitest";

import {
  metadataProfiles,
  recommendedMetadata,
  usableBytes,
} from "./btrfsShared";

const T = 1000 ** 4;
// The greedy fill is stepped, so results are exact to within a step.
const near = (got: number, want: number) =>
  expect(Math.abs(got - want)).toBeLessThan(want / 500 + 1);

describe("usableBytes", () => {
  it("matches btrfs for equal disks", () => {
    near(usableBytes("single", [T, T, T]), 3 * T);
    near(usableBytes("dup", [T]), T / 2);
    near(usableBytes("raid0", [T, T]), 2 * T);
    near(usableBytes("raid1", [T, T, T]), 1.5 * T);
    near(usableBytes("raid1c3", [T, T, T]), T);
    near(usableBytes("raid10", [T, T, T, T]), 2 * T);
  });

  it("counts mixed sizes the way btrfs allocates them", () => {
    // raid1 cannot mirror more than the other disks hold.
    near(usableBytes("raid1", [4 * T, T, T]), 2 * T);
    near(usableBytes("raid1", [2 * T, T, T]), 2 * T);
    // raid0 stops when one disk with free space is left.
    near(usableBytes("raid0", [3 * T, T]), 2 * T);
    // Each raid1c3 chunk needs two copies on the small disks: 3T / 2.
    near(usableBytes("raid1c3", [3 * T, T, T, T]), 1.5 * T);
  });

  it("is zero below the profile's device count", () => {
    expect(usableBytes("raid1", [T])).toBe(0);
    expect(usableBytes("raid10", [T, T, T])).toBe(0);
    expect(usableBytes("raid5", [T, T, T])).toBe(0);
  });
});

describe("metadata profiles", () => {
  const all = [
    "single",
    "dup",
    "raid0",
    "raid1",
    "raid1c3",
    "raid1c4",
    "raid10",
  ];

  it("never offers metadata weaker than data", () => {
    expect(metadataProfiles(all, "single")).toEqual(all);
    expect(metadataProfiles(all, "dup")).toEqual(
      all.filter((p) => p !== "single" && p !== "raid0"),
    );
    // dup keeps both copies on one disk, so it cannot follow raid1 data.
    expect(metadataProfiles(all, "raid10")).toEqual([
      "raid1",
      "raid1c3",
      "raid1c4",
      "raid10",
    ]);
    expect(metadataProfiles(all, "raid1c3")).toEqual(["raid1c3", "raid1c4"]);
  });

  it("recommends dup, raid1 or raid1c3 by disk count", () => {
    expect(recommendedMetadata(all, 1)).toBe("dup");
    expect(recommendedMetadata(all, 2)).toBe("raid1");
    expect(recommendedMetadata(all, 4)).toBe("raid1c3");
    expect(recommendedMetadata(["raid1c4"], 4)).toBe("raid1c4");
  });
});
