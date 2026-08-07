import { type RefObject, useLayoutEffect, useRef } from "react";

/**
 * Ref that always holds the latest value. Callbacks read current state
 * through it instead of listing the state in their deps, keeping their
 * identity stable across renders. The background-jobs cancel actions rely on
 * this: `BackgroundJobsActionsContext` must never change identity after
 * mount, or every progress frame rerenders all of its consumers.
 */
export function useLatestRef<T>(value: T): RefObject<T> {
  const ref = useRef(value);

  // Update after commit, before the browser can paint or dispatch an event.
  // Writing refs during render is unsafe because React may replay or discard
  // that render; a layout effect preserves the same post-commit freshness
  // without mutating during render.
  useLayoutEffect(() => {
    ref.current = value;
  }, [value]);

  return ref;
}
