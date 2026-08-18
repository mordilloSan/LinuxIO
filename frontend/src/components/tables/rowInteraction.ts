/**
 * A clickable row is a container of controls before it is a control itself:
 * action buttons, links, selection checkboxes and copy-on-click cells all
 * bubble up to the row. A click that lands on one of those belongs to the
 * control, and a click that ends a text selection belongs to the selection —
 * neither is a click on the row.
 */
/**
 * How long a single click waits before acting, on a table that binds both a
 * single-click and a double-click row gesture. Without the wait the first click
 * of a double click runs its own action and the second undoes it. Only such
 * tables pay the delay — a table with one gesture acts immediately.
 */
export const ROW_DOUBLE_CLICK_MS = 250;

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

/** Whether an event landed on a control inside the row rather than the row. */
export function targetIsRowControl(target: EventTarget | null): boolean {
  return (
    target instanceof Element && target.closest(ROW_CONTROL_SELECTOR) !== null
  );
}

/** Whether a click on a row should act on the row rather than on its contents. */
export function clickTargetsRowBody(target: EventTarget | null): boolean {
  return !targetIsRowControl(target) && !hasTextSelection();
}

/**
 * Wrap a reorderable row's dnd-kit listeners so a press that starts on one of
 * its controls never arms the drag.
 *
 * Arming is not free: the hold puts the surface into a pending state, which
 * re-renders the page — and a table whose column defs are rebuilt on that
 * render replaces the pressed control's DOM node (see the shared cell
 * memoization in tableShared), so the click never completes and a checkbox
 * never toggles.
 * A press on a control is not a drag anyway.
 */
export function rowBodyDragListeners(
  listeners: Record<string, Function> | undefined,
): Record<string, Function> | undefined {
  if (!listeners) return listeners;

  // Every entry is a sensor activator — onMouseDown / onTouchStart / onKeyDown
  // for this app's sensor set — so all of them get the same guard rather than
  // one hard-coded event name.
  return Object.fromEntries(
    Object.entries(listeners).map(([name, activate]) => [
      name,
      (event: { target: EventTarget | null }) => {
        if (targetIsRowControl(event.target)) return;
        activate(event);
      },
    ]),
  );
}
