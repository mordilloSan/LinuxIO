// Hold-to-reorder tuning. Shared by the hook that arms the gesture
// (`useReorderableSurface`) and the surfaces that mirror the hold timing into
// CSS (`--reorder-hold-ms`), so the animation and the sensor can never drift.

/** How long a card or row must be held before layout mode opens. */
export const REORDER_HOLD_MS = 2000;
/** How long layout mode survives without any interaction. */
export const REORDER_IDLE_EXIT_MS = 5000;
/**
 * Pointer slack allowed during the hold. Anything larger reads as a scroll or a
 * drag-select and cancels the hold instead of arming it.
 */
export const REORDER_HOLD_TOLERANCE_PX = 8;
