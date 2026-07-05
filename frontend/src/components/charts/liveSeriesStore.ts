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

// ─── Shared crosshair ────────────────────────────────────────────────────────
// The dashboard live charts share one hover position, measured in pixels from
// the canvas' right edge: they all scroll at LIVE_MILLIS_PER_PIXEL, so the
// same right-edge offset is the same moment in time on every chart.

let hoverRightPx: number | null = null;
const hoverListeners = new Set<() => void>();

export function setLiveHoverRightPx(px: number | null): void {
  if (px === hoverRightPx) return;
  hoverRightPx = px;
  for (const listener of hoverListeners) listener();
}

export function getLiveHoverRightPx(): number | null {
  return hoverRightPx;
}

export function subscribeLiveHover(listener: () => void): () => void {
  hoverListeners.add(listener);
  return () => hoverListeners.delete(listener);
}

/**
 * Nearest sample value at tMs, or null when the series has no point within
 * toleranceMs (e.g. hovering a gap left by a stale-cleared buffer). Reaches
 * into smoothie's untyped-but-stable `data` array of [timestamp, value].
 */
export function sampleLiveSeries(
  series: TimeSeries,
  tMs: number,
  toleranceMs = 10_000,
): number | null {
  const data = (series as unknown as { data: [number, number][] }).data;
  if (!data || data.length === 0) return null;
  let lo = 0;
  let hi = data.length - 1;
  while (lo < hi) {
    const mid = (lo + hi) >> 1;
    if (data[mid][0] < tMs) {
      lo = mid + 1;
    } else {
      hi = mid;
    }
  }
  if (lo > 0 && Math.abs(data[lo - 1][0] - tMs) <= Math.abs(data[lo][0] - tMs)) {
    lo -= 1;
  }
  return Math.abs(data[lo][0] - tMs) <= toleranceMs ? data[lo][1] : null;
}
