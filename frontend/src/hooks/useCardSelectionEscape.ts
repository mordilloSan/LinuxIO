import { useEffect, useEffectEvent } from "react";

import { OVERLAY_ROOT_SELECTOR } from "@/components/ui/AppDialog";

interface UseCardSelectionEscapeOptions {
  enabled: boolean;
  isReordering: boolean;
  onClearSelection: () => void;
  onExitReordering: () => void;
}

/**
 * Clears card selection with the same ownership guards used by data tables.
 * Reorder mode is the card view's preceding Escape layer: its first Escape
 * exits layout mode, then a second Escape clears the selection.
 */
export function useCardSelectionEscape({
  enabled,
  isReordering,
  onClearSelection,
  onExitReordering,
}: UseCardSelectionEscapeOptions) {
  const clearSelection = useEffectEvent(onClearSelection);
  const exitReordering = useEffectEvent(onExitReordering);

  useEffect(() => {
    if (!enabled) return;

    const handleKeyDown = (event: KeyboardEvent) => {
      if (
        event.key !== "Escape" ||
        event.defaultPrevented ||
        document.querySelector(OVERLAY_ROOT_SELECTOR)
      ) {
        return;
      }

      if (isReordering) {
        exitReordering();
      } else {
        clearSelection();
      }
      event.preventDefault();
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [enabled, isReordering]);
}
