/**
 * Shared timestamp formatters so every chart labels time the same way:
 * clock for intra-day ranges, clock with seconds for the live charts, and
 * month/day once a range spans multiple days.
 */

const clock = new Intl.DateTimeFormat(undefined, {
  hour: "2-digit",
  minute: "2-digit",
});

const clockWithSeconds = new Intl.DateTimeFormat(undefined, {
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
});

const monthDay = new Intl.DateTimeFormat(undefined, {
  month: "short",
  day: "numeric",
});

export const formatChartClock = (t: number): string => clock.format(t);

export const formatChartClockWithSeconds = (t: number): string =>
  clockWithSeconds.format(t);

export const formatChartDay = (t: number): string => monthDay.format(t);
