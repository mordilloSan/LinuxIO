import type { BtrfsSnapshot } from "@/api";

/** How a day older than yesterday is named: "Tuesday 20 October 2026" in en-GB. */
export const SNAPSHOT_DAY_FORMAT: Intl.DateTimeFormatOptions = {
  weekday: "long",
  day: "numeric",
  month: "long",
  year: "numeric",
};

export interface SnapshotDay {
  key: string;
  /** "Today", "Yesterday", or the day in the browser's own format. */
  label: string;
  /** Today or yesterday, where an age ("2h ago") says more than a date. */
  recent: boolean;
  items: BtrfsSnapshot[];
}

const dayKey = (date: Date) =>
  `${date.getFullYear()}-${date.getMonth()}-${date.getDate()}`;

/**
 * Snapshots grouped by the local day they were taken on, newest day first
 * and newest snapshot first within a day.
 */
export const groupSnapshotsByDay = (
  snapshots: BtrfsSnapshot[],
  now: Date = new Date(),
): SnapshotDay[] => {
  const today = dayKey(now);
  const yesterday = dayKey(
    new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1),
  );
  const format = new Intl.DateTimeFormat(undefined, SNAPSHOT_DAY_FORMAT);
  // An unreadable date sorts last, so it never splits a day in two.
  const time = (snapshot: BtrfsSnapshot) => {
    const ms = Date.parse(snapshot.createdAt);
    return Number.isNaN(ms) ? -Infinity : ms;
  };
  const sorted = [...snapshots].sort((a, b) =>
    time(a) === time(b) ? 0 : time(b) > time(a) ? 1 : -1,
  );
  const days: SnapshotDay[] = [];
  for (const snapshot of sorted) {
    const date = new Date(snapshot.createdAt);
    const valid = !Number.isNaN(date.getTime());
    const key = valid ? dayKey(date) : "unknown";
    let day = days.at(-1);
    if (day?.key !== key) {
      day = {
        key,
        label:
          key === today
            ? "Today"
            : key === yesterday
              ? "Yesterday"
              : valid
                ? format.format(date)
                : "Unknown date",
        recent: key === today || key === yesterday,
        items: [],
      };
      days.push(day);
    }
    day.items.push(snapshot);
  }
  return days;
};
