import { useEffect, useEffectEvent } from "react";

import { OVERLAY_ROOT_SELECTOR } from "@/components/ui/AppDialog";

interface UseCardSelectionEscapeOptions {
  enabled: boolean;
  isReordering: boolean;
  onClearSelection: () => void;
  onExitReordering: () => void;
}

const blurFocusedSelectableCard = () => {
  const focused = document.activeElement;
  if (
    focused instanceof HTMLButtonElement &&
    focused.classList.contains("selectable-card-button")
  ) {
    focused.blur();
  }
};

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
      // Escape changes the browser to keyboard modality. A card focused by a
      // prior pointer gesture would otherwise acquire a focus-visible outline
      // after the gesture is over, including on the first of the two presses.
      blurFocusedSelectableCard();
      event.preventDefault();
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [enabled, isReordering]);
}
