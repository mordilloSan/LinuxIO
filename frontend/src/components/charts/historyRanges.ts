/**
 * Time windows offered by every history card. `resolution` picks the agent's
 * pre-rolled bucket size, `windowMs` is the x-axis span, and `refetchMs` is
 * the poll cadence a window of that length deserves.
 */
export const HISTORY_RANGES = [
  {
    id: "1h",
    label: "1 Hour",
    resolution: "1m",
    refetchMs: 15_000,
    windowMs: 3_600_000,
  },
  {
    id: "12h",
    label: "12 Hours",
    resolution: "10m",
    refetchMs: 60_000,
    windowMs: 43_200_000,
  },
  {
    id: "24h",
    label: "24 Hours",
    resolution: "20m",
    refetchMs: 60_000,
    windowMs: 86_400_000,
  },
  {
    id: "7d",
    label: "7 Days",
    resolution: "120m",
    refetchMs: 300_000,
    windowMs: 604_800_000,
  },
  {
    id: "30d",
    label: "30 Days",
    resolution: "480m",
    refetchMs: 300_000,
    windowMs: 2_592_000_000,
  },
] as const;

export type HistoryRange = (typeof HISTORY_RANGES)[number];
export type HistoryRangeId = HistoryRange["id"];
