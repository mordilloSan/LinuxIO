import { useEffect, useEffectEvent } from "react";

import { OVERLAY_ROOT_SELECTOR } from "@/components/ui/AppDialog";

/** Clear page selection after overlays and closer keyboard handlers have priority. */
export function useClearSelectionOnEscape(
  hasSelection: boolean,
  onClear: () => void,
) {
  const clear = useEffectEvent(onClear);

  useEffect(() => {
    if (!hasSelection) return;
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
  }, [hasSelection]);
}
