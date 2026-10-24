import { useEffect, useEffectEvent, type RefObject } from "react";

import { OVERLAY_ROOT_SELECTOR } from "./overlay";

/**
 * Close an open overlay on Escape, but only when it is the topmost overlay in
 * DOM order (see OVERLAY_ROOT_SELECTOR).
 */
export function useOverlayEscape({
  disabled,
  onEscape,
  open,
  rootRef,
}: {
  disabled?: boolean;
  onEscape: (event: KeyboardEvent) => void;
  open: boolean;
  rootRef: RefObject<HTMLElement | null>;
}) {
  const handleDocumentKeyDown = useEffectEvent((event: KeyboardEvent) => {
    if (event.key !== "Escape" || disabled || event.defaultPrevented) {
      return;
    }

    const root = rootRef.current;
    if (!root) {
      return;
    }

    const openOverlays = document.querySelectorAll(OVERLAY_ROOT_SELECTOR);
    if (openOverlays[openOverlays.length - 1] !== root) {
      return;
    }

    event.preventDefault();
    event.stopPropagation();
    onEscape(event);
  });

  useEffect(() => {
    if (!open) {
      return;
    }

    document.addEventListener("keydown", handleDocumentKeyDown);
    return () => {
      document.removeEventListener("keydown", handleDocumentKeyDown);
    };
  }, [open]);
}
