import { describe, expect, it } from "vitest";

import {
  parseScheduleTiming,
  scheduleCalendar,
  scheduleSummary,
} from "./scheduleTiming";

describe("friendly schedule controls", () => {
  it.each([
    ["daily", "daily", "00:00", "*-*-* 00:00:00"],
    ["weekly", "weekly", "00:00", "Mon *-*-* 00:00:00"],
    ["*-*-* 03:00:00", "daily", "03:00", "*-*-* 03:00:00"],
    ["Sat *-*-* 07:00:00", "weekly", "07:00", "Sat *-*-* 07:00:00"],
    ["hourly", "hourly", "00:00", "hourly"],
    ["*-*-* *:*:*", "secondly", "00:00", "*-*-* *:*:*"],
    ["minutely", "minutely", "00:00", "*-*-* *:*:00"],
  ])(
    "edits %s without changing its timing",
    (value, frequency, time, expected) => {
      const parsed = parseScheduleTiming(value);
      expect(parsed).toMatchObject({ frequency, time });
      expect(scheduleCalendar(parsed)).toBe(expected);
    },
  );

  it.each([
    "2030-01-01 12:00:00..09",
    "Mon..Fri *-*-* 07:00:00",
    "*-*-* 07:00:30",
    "monthly",
  ])("preserves advanced expression %s", (value) => {
    const parsed = parseScheduleTiming(value);
    expect(parsed.frequency).toBe("custom");
    expect(scheduleCalendar(parsed)).toBe(value);
  });

  it("does not submit an empty time and describes weekly schedules", () => {
    expect(
      scheduleCalendar({ ...parseScheduleTiming("daily"), time: "" }),
    ).toBe("");
    expect(scheduleSummary("Sat *-*-* 07:00:00")).toContain(
      "Every Saturday at ",
    );
  });
});
