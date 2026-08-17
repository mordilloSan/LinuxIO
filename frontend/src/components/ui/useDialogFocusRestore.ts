import { useEffect, useRef, type RefObject } from "react";

/* Ctrl+click must still read as pointer input, so bare modifier presses never
   count as keyboard activity. */
const MODIFIER_KEYS = new Set(["Alt", "Control", "Meta", "Shift"]);

/**
 * Tracks whether the last user input seen while `active` was a key press, for
 * deciding whether a programmatic focus restore should paint a ring: a
 * keyboard-driven close must show where focus went, a pointer-driven close
 * must not. Read the ref at restore time and pass it as `focusVisible`
 * (honored by Chrome 145+ / Firefox 104+ / Safari 18.4+; older browsers
 * ignore the hint and keep their own heuristic — which rings the trigger
 * whenever the dialog held a text field, even for pure-mouse flows). Resets
 * to true on each activation: with no input seen, the ring is kept, erring
 * toward the keyboard user.
 */
export function useLastInputWasKeyboard(active: boolean): RefObject<boolean> {
  const lastInputWasKeyboard = useRef(true);

  useEffect(() => {
    if (!active) {
      return;
    }

    lastInputWasKeyboard.current = true;
    const handleKeyDown = (event: KeyboardEvent) => {
      if (!MODIFIER_KEYS.has(event.key)) {
        lastInputWasKeyboard.current = true;
      }
    };
    const handlePointerDown = () => {
      lastInputWasKeyboard.current = false;
    };
    document.addEventListener("keydown", handleKeyDown, true);
    document.addEventListener("pointerdown", handlePointerDown, true);
    return () => {
      document.removeEventListener("keydown", handleKeyDown, true);
      document.removeEventListener("pointerdown", handlePointerDown, true);
    };
  }, [active]);

  return lastInputWasKeyboard;
}

/**
 * Captures the element that was focused when a dialog opened and restores
 * focus to it on close (or unmount-while-open), so keyboard users aren't
 * dropped at the top of the document. Whether the restore paints a focus
 * ring follows the last input kind seen while the dialog was open.
 */
export function useDialogFocusRestore(open: boolean) {
  const lastFocusedElement = useRef<HTMLElement | null>(null);
  const lastInputWasKeyboard = useLastInputWasKeyboard(open);

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
        trigger.focus({
          focusVisible: lastInputWasKeyboard.current,
        } as FocusOptions);
      }
    };
  }, [open, lastInputWasKeyboard]);
}
