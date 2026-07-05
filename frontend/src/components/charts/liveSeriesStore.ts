import { TimeSeries } from "smoothie";

/**
 * Module-level registry of smoothie TimeSeries so the dashboard live charts
 * survive route changes: the buffers outlive the components that render them.
 * A hard refresh still starts empty — that gap is covered by backfilling from
 * go-monitoring history (see backfillLiveSeries).
 */

/**
 * Chart scroll speed: ~18s visible in a ~350px-wide dashboard canvas.
 * Keep this low: smoothie quantizes scrolling to whole pixels (render time is
 * rounded down to millisPerPixel), so high values make the pan visibly step —
 * at 50ms/px the chart advances 20×/s, which still reads as continuous.
 */
export const LIVE_MILLIS_PER_PIXEL = 50;
/** How far back to seed from go-monitoring history (15s samples). */
export const LIVE_BACKFILL_WINDOW_MS = 90_000;
/** Buffers idle longer than this are cleared and re-seeded instead. */
export const LIVE_STALE_AFTER_MS = 20_000;

interface LiveSeriesEntry {
  series: TimeSeries;
  lastAppendMs: number;
}

const registry = new Map<string, LiveSeriesEntry>();

export interface LiveSeriesHandle {
  series: TimeSeries;
  /** True when the buffer has no usable recent data (fresh or stale). */
  needsBackfill: boolean;
}

/**
 * Get (or create) the persistent series for a chart. A buffer whose newest
 * sample is older than staleAfterMs is cleared so the chart doesn't draw a
 * line across the time we weren't sampling; backfill then refills it.
 */
export function acquireLiveSeries(
  id: string,
  staleAfterMs: number,
): LiveSeriesHandle {
  let entry = registry.get(id);
  if (!entry) {
    entry = { series: new TimeSeries(), lastAppendMs: 0 };
    registry.set(id, entry);
  }
  const stale =
    entry.lastAppendMs !== 0 && Date.now() - entry.lastAppendMs > staleAfterMs;
  if (stale) {
    entry.series.clear();
    entry.lastAppendMs = 0;
  }
  return { series: entry.series, needsBackfill: entry.lastAppendMs === 0 };
}

/** Append a live sample and record when the buffer was last fed. */
export function appendLiveSample(id: string, value: number): void {
  const entry = registry.get(id);
  if (!entry) return;
  const now = Date.now();
  entry.series.append(now, value);
  entry.lastAppendMs = now;
}

/**
 * Seed a series with historical points (epoch ms). Points newer than "now"
 * are dropped so a skewed server clock can't put samples in the future;
 * smoothie inserts older-than-last timestamps in order by itself.
 */
export function backfillLiveSeries(
  id: string,
  points: { t: number; v: number }[],
): void {
  const entry = registry.get(id);
  if (!entry) return;
  const cutoff = Date.now();
  for (const point of points) {
    if (point.t < cutoff) {
      entry.series.append(point.t, point.v);
    }
  }
}
