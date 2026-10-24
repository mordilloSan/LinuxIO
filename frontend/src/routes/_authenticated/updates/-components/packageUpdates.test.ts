import { describe, expect, it } from "vitest";

import type { Update } from "@/api";

import {
  isDeferredUpdate,
  partitionUpdatesByAvailability,
} from "./packageUpdates";

const update = (info_enum: number, package_id: string): Update => ({
  changelog: "",
  cve: [],
  info_enum,
  issued: "",
  package_id,
  restart: 0,
  state: 0,
  summary: package_id,
  version: "1.0",
});

describe("update availability", () => {
  it("treats PackageKit Blocked and Unavailable entries as deferred", () => {
    expect(isDeferredUpdate(update(9, "blocked"))).toBe(true);
    expect(isDeferredUpdate(update(25, "unavailable"))).toBe(true);
    expect(isDeferredUpdate(update(11, "updating"))).toBe(false);
  });

  it("partitions updates so Update All can submit only actionable IDs", () => {
    const { actionable, deferred } = partitionUpdatesByAvailability([
      update(11, "ready"),
      update(9, "later"),
      update(25, "also-later"),
    ]);

    expect(actionable.map((item) => item.package_id)).toEqual(["ready"]);
    expect(deferred.map((item) => item.package_id)).toEqual([
      "later",
      "also-later",
    ]);
  });
});
