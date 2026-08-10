// Dashboard live-chart tuning. Shared by the series store, the hover overlay,
// and the dashboard graphs, so a chart's scroll speed and its backfill window
// stay described in one place rather than in the store consumers reach into.

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
