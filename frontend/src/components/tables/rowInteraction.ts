/**
 * A clickable row is a container of controls before it is a control itself:
 * action buttons, links, selection checkboxes and copy-on-click cells all
 * bubble up to the row. A click that lands on one of those belongs to the
 * control, and a click that ends a text selection belongs to the selection —
 * neither is a click on the row.
 */
const ROW_CONTROL_SELECTOR = [
  "a",
  "button",
  "input",
  "label",
  "select",
  "textarea",
  '[contenteditable="true"]',
  '[role="button"]',
  '[role="checkbox"]',
  '[role="menuitem"]',
  '[role="switch"]',
  ".app-tooltip-trigger--copy",
].join(", ");

function hasTextSelection() {
  if (typeof window === "undefined" || !window.getSelection) return false;
  const selection = window.getSelection();
  return Boolean(selection && !selection.isCollapsed && selection.toString());
}

/** Whether a click on a row should act on the row rather than on its contents. */
export function clickTargetsRowBody(target: EventTarget | null): boolean {
  if (target instanceof Element && target.closest(ROW_CONTROL_SELECTOR)) {
    return false;
  }
  return !hasTextSelection();
}
