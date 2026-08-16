/**
 * Which kind of input the user last drove the page with, and a mark on the
 * element that a pointer press left focused.
 *
 * `:focus-visible` is evaluated against the *current* input modality, not the
 * modality that placed the focus. A button the user only clicked draws no ring
 * — until the next keystroke flips the browser to keyboard modality, at which
 * point whatever still holds focus lights up and keeps the ring until focus
 * moves somewhere else. Measured in Chromium: Escape, an arrow key, a letter,
 * Enter, Space and even a bare Shift all flip it; Ctrl/Cmd-A does not.
 *
 * That makes any surface binding Escape a ring generator for its own controls
 * — the table's expand chevron is the visible case, since AppDataTable listens
 * for Escape precisely so it can collapse the row that chevron just opened.
 *
 * Marking the pointer-taken focus lets CSS drop the ring for that one element
 * while it keeps the focus itself. Blurring the control would also remove the
 * ring, but it costs the tab-order position, the assistive-technology cursor,
 * and the `document.activeElement` that AppDialog stores to restore focus on
 * close. Suppressing the paint costs nothing.
 */

export type InputModality = "keyboard" | "pointer";

/**
 * Set on the focused element while the focus came from a pointer press. Paired
 * with the `[data-pointer-focus]:focus-visible` rule in theme/variables.css.
 */
export const POINTER_FOCUS_ATTRIBUTE = "data-pointer-focus";

// Keyboard is the safe default: it is the modality that shows rings and shows
// focus tooltips, so an uninstalled tracker never hides an affordance.
let modality: InputModality = "keyboard";
let uninstall: (() => void) | null = null;

export const getInputModality = (): InputModality => modality;

const unmark = (element: EventTarget | null) => {
  if (element instanceof HTMLElement) {
    element.removeAttribute(POINTER_FOCUS_ATTRIBUTE);
  }
};

/**
 * Starts tracking. Idempotent — the second call returns the first call's
 * teardown rather than double-binding. Returns a teardown so tests can unwind.
 */
export const installInputModalityTracking = (): (() => void) => {
  if (uninstall) return uninstall;
  if (typeof document === "undefined") return () => {};

  const handlePointerDown = () => {
    modality = "pointer";
  };

  const handleKeyDown = (event: KeyboardEvent) => {
    modality = "keyboard";
    // Enter and Space are the keyboard equivalents of the press that took this
    // focus: the user has taken the control over from the pointer, so give the
    // ring back. No other key may clear the mark — Escape clearing it is the
    // whole bug this module exists for.
    if (event.key === "Enter" || event.key === " ") {
      unmark(document.activeElement);
    }
  };

  const handleFocusIn = (event: FocusEvent) => {
    // A pointer press focuses on mousedown, which the browser dispatches after
    // pointerdown — so the modality above is already current by the time this
    // runs, including for a focus a click handler moves programmatically.
    if (modality === "pointer" && event.target instanceof HTMLElement) {
      event.target.setAttribute(POINTER_FOCUS_ATTRIBUTE, "");
    }
  };

  const handleFocusOut = (event: FocusEvent) => {
    unmark(event.target);
  };

  // Capture phase throughout: a handler that stops propagation must not be able
  // to desynchronise the tracker from the modality the user is actually in.
  document.addEventListener("pointerdown", handlePointerDown, true);
  document.addEventListener("keydown", handleKeyDown, true);
  document.addEventListener("focusin", handleFocusIn, true);
  document.addEventListener("focusout", handleFocusOut, true);

  uninstall = () => {
    document.removeEventListener("pointerdown", handlePointerDown, true);
    document.removeEventListener("keydown", handleKeyDown, true);
    document.removeEventListener("focusin", handleFocusIn, true);
    document.removeEventListener("focusout", handleFocusOut, true);
    unmark(document.activeElement);
    modality = "keyboard";
    uninstall = null;
  };

  return uninstall;
};
