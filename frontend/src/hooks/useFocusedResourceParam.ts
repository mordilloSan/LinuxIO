import { useEffect, useEffectEvent } from "react";

import { OVERLAY_ROOT_SELECTOR } from "@/components/ui/AppDialog";

interface FocusedResourceParamOptions<T> {
  /** The id read from the route's search param; `undefined` when unset. */
  focusedId: string | undefined;
  getId: (item: T) => string;
  items: readonly T[];
  /** Clears the search param. May be an inline closure. */
  onClear: () => void;
}

/**
 * Docker list pages keep the focused resource in a search param. This derives
 * the focused item from `items`, clears the param when that item leaves the
 * list, and closes the panel on Escape unless a dialog is open or something
 * closer to the key already handled it.
 */
export function useFocusedResourceParam<T>({
  focusedId,
  getId,
  items,
  onClear,
}: FocusedResourceParamOptions<T>): T | null {
  const focused =
    focusedId === undefined
      ? null
      : (items.find((item) => getId(item) === focusedId) ?? null);
  const hasFocus = focused !== null;
  const clear = useEffectEvent(onClear);

  useEffect(() => {
    if (focusedId && !hasFocus) clear();
  }, [focusedId, hasFocus]);

  useEffect(() => {
    if (!hasFocus) return;
    const handleKeyDown = (event: KeyboardEvent) => {
      if (
        (event.key !== "Escape" && event.key !== "Esc") ||
        event.defaultPrevented ||
        document.querySelector(OVERLAY_ROOT_SELECTOR)
      ) {
        return;
      }
      clear();
      event.preventDefault();
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [hasFocus]);

  return focused;
}
