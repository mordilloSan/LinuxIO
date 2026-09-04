import { TimeSeries } from "smoothie";

/**
 * Module-level registry of smoothie TimeSeries so the dashboard live charts
 * survive route changes: the buffers outlive the components that render them.
 * A hard refresh still starts empty — that gap is covered by backfilling from
 * linuxio-monitoring history (see backfillLiveSeries).
 */

interface LiveSeriesEntry {
  series: TimeSeries;
  lastAppendMs: number;
}

const registry = new Map<string, LiveSeriesEntry>();

/**
 * Get (or create) the persistent series for a chart. Idempotent, so it is
 * safe to call during render; the impure part — clearing stale buffers — is
 * separated into resetStaleLiveSeries, meant for an effect (useLiveSeries).
 */
export function getLiveSeries(id: string): TimeSeries {
  let entry = registry.get(id);
  if (!entry) {
    entry = { series: new TimeSeries(), lastAppendMs: 0 };
    registry.set(id, entry);
  }
  return entry.series;
}

/**
 * Clear a buffer whose newest sample is older than staleAfterMs, so the
 * chart doesn't draw a line across the time we weren't sampling. Returns
 * true when the series has no usable data (fresh or just cleared) and
 * should be backfilled from history.
 */
export function resetStaleLiveSeries(
  id: string,
  staleAfterMs: number,
): boolean {
  const entry = registry.get(id);
  if (!entry) return false;
  const stale =
    entry.lastAppendMs !== 0 && Date.now() - entry.lastAppendMs > staleAfterMs;
  if (stale) {
    entry.series.clear();
    entry.lastAppendMs = 0;
  }
  return entry.lastAppendMs === 0;
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
// same right-edge offset is the same moment in time on every chart. The store
// also carries the clock the overlays render with: while a hover is active a
// single shared ticker refreshes it every second, so tooltip values track the
// samples scrolling underneath the crosshair while renders stay pure
// (components read Date.now() from this snapshot, never directly).

let hoverRightPx: number | null = null;
let hoverNowMs = 0;
let hoverTicker: ReturnType<typeof setInterval> | undefined;
const hoverListeners = new Set<() => void>();

function notifyHoverListeners(): void {
  for (const listener of hoverListeners) listener();
}

function stopHoverTicker(): void {
  if (hoverTicker === undefined) return;
  clearInterval(hoverTicker);
  hoverTicker = undefined;
}

export function setLiveHoverRightPx(px: number | null): void {
  if (px === hoverRightPx) return;
  hoverRightPx = px;
  if (px === null) {
    stopHoverTicker();
  } else {
    hoverNowMs = Date.now();
    hoverTicker ??= setInterval(() => {
      hoverNowMs = Date.now();
      notifyHoverListeners();
    }, 1000);
  }
  notifyHoverListeners();
}

export function getLiveHoverRightPx(): number | null {
  return hoverRightPx;
}

/** Clock snapshot the hover overlays render with; see the section comment. */
export function getLiveHoverNowMs(): number {
  return hoverNowMs;
}

export function subscribeLiveHover(listener: () => void): () => void {
  hoverListeners.add(listener);
  return () => {
    hoverListeners.delete(listener);
    // Last overlay gone (e.g. route change mid-hover, where pointerleave
    // never fires): drop the ticker and the ghost crosshair with it.
    if (hoverListeners.size === 0) {
      stopHoverTicker();
      hoverRightPx = null;
    }
  };
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
  if (
    lo > 0 &&
    Math.abs(data[lo - 1][0] - tMs) <= Math.abs(data[lo][0] - tMs)
  ) {
    lo -= 1;
  }
  return Math.abs(data[lo][0] - tMs) <= toleranceMs ? data[lo][1] : null;
}
