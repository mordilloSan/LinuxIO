import { useEffect, useRef } from "react";

/**
 * Captures the element that was focused when a dialog opened and restores
 * focus to it on close (or unmount-while-open), so subsequent interaction
 * keeps a stable owner. Focus-ring presentation is controlled independently by
 * the app-wide Tab-navigation policy.
 */
export function useDialogFocusRestore(open: boolean) {
  const lastFocusedElement = useRef<HTMLElement | null>(null);

  useEffect(() => {
    if (!open) {
      return;
    }

    lastFocusedElement.current = document.activeElement as HTMLElement | null;
    return () => {
      const trigger = lastFocusedElement.current;
      lastFocusedElement.current = null;
      // Runs on unmount as well as on close, so the trigger may be gone: a
      // dialog's own action routinely deletes the row or card it was opened
      // from, and .focus() on a detached node drops focus onto <body> while
      // the ref would keep pinning that node's whole subtree.
      if (trigger?.isConnected) {
        trigger.focus();
      }
    };
  }, [open]);
}
