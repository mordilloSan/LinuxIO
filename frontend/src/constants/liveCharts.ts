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
/** How far back to seed from monitoring history (collector interval samples). */
export const LIVE_BACKFILL_WINDOW_MS = 90_000;
/** Buffers idle longer than this are cleared and re-seeded instead. */
export const LIVE_STALE_AFTER_MS = 20_000;

/** Dashboard card polling: live gauges, memory, and slow-changing sensors. */
export const DASHBOARD_REFETCH_FAST_MS = 1000;
export const DASHBOARD_REFETCH_MEMORY_MS = 2000;
export const DASHBOARD_REFETCH_SLOW_MS = 50_000;
