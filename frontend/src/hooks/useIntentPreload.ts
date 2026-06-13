import React from "react";

/** Options for {@link useIntentPreload}. */
interface UseIntentPreloadOptions {
  /** Debounce before a scheduled preload fires, in ms. Defaults to 150. */
  delayMs?: number;
  /** When true, all triggers are no-ops (e.g. the link is disabled). */
  disabled?: boolean;
  /**
   * The preload callback to run — typically the output of
   * `createRouteIntentPreload` (`@/routing/routeIntentPreload`). Optional so a
   * link with nothing to preload can still use this hook safely.
   */
  preload?: () => Promise<unknown>;
}

/**
 * The DOM-event driver for route intent preloading. Given a route's `preload`
 * callback and debounce, it returns three handlers a navigation link binds to
 * pointer/focus events:
 *
 * - `schedule` — start a debounced preload; bind to hover/focus
 *   (`onPointerEnter`, `onFocus`). Waits `delayMs` so passing the cursor over a
 *   link doesn't fetch its chunk/data.
 * - `cancel` — clear a pending scheduled preload; bind to blur/leave
 *   (`onPointerLeave`, `onBlur`).
 * - `run` — fire the preload immediately, skipping the debounce; bind to commit
 *   gestures (`onMouseDown`, `onTouchStart`) where navigation is imminent.
 *
 * A `preloadRequestedRef` flag dedupes: once a preload has been kicked off it
 * won't run again. The flag resets if `preload()` rejects (so a transient
 * failure can be retried) and whenever the `preload` identity changes. The timer
 * is also cleared on unmount.
 */
export function useIntentPreload({
  delayMs = 150,
  disabled = false,
  preload,
}: UseIntentPreloadOptions) {
  // Whether a preload has already been kicked off (dedupe guard).
  const preloadRequestedRef = React.useRef(false);
  // Pending debounce timer id from `schedule`, if any.
  const preloadTimerRef = React.useRef<number | undefined>(undefined);

  const cancel = React.useCallback(() => {
    if (preloadTimerRef.current === undefined) return;
    window.clearTimeout(preloadTimerRef.current);
    preloadTimerRef.current = undefined;
  }, []);

  // A new preload target means a fresh request is allowed; drop any pending timer.
  React.useEffect(() => {
    preloadRequestedRef.current = false;
    cancel();
  }, [cancel, preload]);

  // Clear any pending timer on unmount.
  React.useEffect(() => cancel, [cancel]);

  const run = React.useCallback(() => {
    if (disabled || !preload || preloadRequestedRef.current) return;

    cancel();
    preloadRequestedRef.current = true;
    void preload().catch(() => {
      // Allow a retry if the speculative preload failed.
      preloadRequestedRef.current = false;
    });
  }, [cancel, disabled, preload]);

  const schedule = React.useCallback(() => {
    if (disabled || !preload || preloadRequestedRef.current) return;

    cancel();
    preloadTimerRef.current = window.setTimeout(run, delayMs);
  }, [cancel, delayMs, disabled, preload, run]);

  return {
    cancel,
    run,
    schedule,
  };
}
