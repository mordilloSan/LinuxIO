import { describe, expect, it } from "vitest";

import type { BtrfsSnapshot } from "@/api";

import { groupSnapshotsByDay, SNAPSHOT_DAY_FORMAT } from "./snapshotDays";

const taken = (
  month: number,
  day: number,
  hour: number,
  minute: number,
): BtrfsSnapshot => ({
  source: "@",
  path: `linuxio-snapshots/@/${month}-${day}-${hour}-${minute}`,
  kind: "timeline",
  createdAt: new Date(2026, month, day, hour, minute).toISOString(),
  readOnly: true,
  id: month * 10000 + day * 100 + hour,
  uuid: "",
  receivedUuid: "",
});

// 25 November 2026, 05:00 local time.
const now = new Date(2026, 10, 25, 5, 0);

describe("groupSnapshotsByDay", () => {
  it("puts each snapshot under its local day, newest day first", () => {
    const afterMidnight = taken(10, 25, 0, 8);
    const lateYesterday = taken(10, 24, 23, 11);
    const october = taken(9, 20, 7, 41);

    const days = groupSnapshotsByDay(
      [october, afterMidnight, lateYesterday],
      now,
    );

    expect(days.map((day) => day.label)).toEqual([
      "Today",
      "Yesterday",
      new Intl.DateTimeFormat(undefined, SNAPSHOT_DAY_FORMAT).format(
        new Date(2026, 9, 20),
      ),
    ]);
    expect(days.map((day) => day.recent)).toEqual([true, true, false]);
    expect(days.map((day) => day.items)).toEqual([
      [afterMidnight],
      [lateYesterday],
      [october],
    ]);
  });

  it("keeps the newest snapshot of a day first", () => {
    const morning = taken(9, 20, 3, 0);
    const later = taken(9, 20, 7, 41);

    const [day] = groupSnapshotsByDay([morning, later], now);

    expect(day.items).toEqual([later, morning]);
  });

  it("names a snapshot with an unreadable date without throwing", () => {
    const broken = { ...taken(9, 20, 3, 0), createdAt: "not a date" };

    expect(groupSnapshotsByDay([broken], now)[0].label).toBe("Unknown date");
  });

  it("keeps a day in one group when a date is unreadable", () => {
    const first = taken(10, 25, 3, 0);
    const second = taken(10, 25, 1, 0);
    const broken = { ...taken(9, 20, 3, 0), createdAt: "not a date" };

    const days = groupSnapshotsByDay([first, broken, second], now);

    expect(days.map((day) => day.label)).toEqual(["Today", "Unknown date"]);
    expect(days[0].items).toEqual([first, second]);
  });
});
