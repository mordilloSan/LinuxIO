import { useEffect, useEffectEvent } from "react";

import { useClearSelectionOnEscape } from "@/hooks/useClearSelectionOnEscape";

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

  useClearSelectionOnEscape(hasFocus, onClear);

  return focused;
}
