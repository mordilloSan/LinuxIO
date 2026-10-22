import { describe, expect, it } from "vitest";

import { usableBytes } from "./btrfsShared";

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
