import React from "react";

interface UseIntentPreloadOptions {
  delayMs?: number;
  disabled?: boolean;
  preload?: () => Promise<unknown>;
}

export function useIntentPreload({
  delayMs = 150,
  disabled = false,
  preload,
}: UseIntentPreloadOptions) {
  const preloadRequestedRef = React.useRef(false);
  const preloadTimerRef = React.useRef<number | undefined>(undefined);

  const cancel = React.useCallback(() => {
    if (preloadTimerRef.current === undefined) return;
    window.clearTimeout(preloadTimerRef.current);
    preloadTimerRef.current = undefined;
  }, []);

  React.useEffect(() => {
    preloadRequestedRef.current = false;
    cancel();
  }, [cancel, preload]);

  React.useEffect(() => cancel, [cancel]);

  const run = React.useCallback(() => {
    if (disabled || !preload || preloadRequestedRef.current) return;

    cancel();
    preloadRequestedRef.current = true;
    void preload().catch(() => {
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
