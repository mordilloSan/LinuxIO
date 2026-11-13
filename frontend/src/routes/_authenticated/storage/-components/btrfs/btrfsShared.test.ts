import { readFileSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

import {
  BTRFS_PROFILES,
  metadataProfiles,
  recommendedMetadata,
  usableBytes,
} from "./btrfsShared";

// Vitest runs from frontend/.
const PROFILES_GO = join(process.cwd(), "../backend/common/btrfs/devices.go");

/** The allowed entries of the Go `profiles` table, as BTRFS_PROFILES has them. */
function backendProfiles() {
  const source = readFileSync(PROFILES_GO, "utf8");
  const table = source.match(
    /^var profiles = map\[string\]profileSpec\{\n([\s\S]*?)^\}/m,
  );
  if (!table) throw new Error(`no profiles table in ${PROFILES_GO}`);
  const allowed: Record<
    string,
    { minDevices: number; copies: number; rank: number }
  > = {};
  let entries = 0;
  for (const line of table[1].split("\n")) {
    const entry = line.match(/^\t"(\w+)":\s*\{(.*)\},$/);
    if (!entry) continue;
    entries++;
    const fields = Object.fromEntries(
      entry[2].split(",").map((field) => {
        const [key, value] = field.split(":").map((part) => part.trim());
        return [key, value];
      }),
    );
    if (fields.allowed !== "true") continue;
    allowed[entry[1]] = {
      minDevices: Number(fields.minDevices ?? 0),
      copies: Number(fields.copies ?? 0),
      rank: Number(fields.rank ?? 0),
    };
  }
  return { allowed, entries };
}

describe("profile table", () => {
  it("has the allowed profiles of the Go table, with the same values", () => {
    const { allowed, entries } = backendProfiles();
    // raid5 and raid6 are listed in Go but never allowed.
    expect(entries).toBeGreaterThan(Object.keys(allowed).length);
    expect(BTRFS_PROFILES).toEqual(allowed);
  });
});

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
