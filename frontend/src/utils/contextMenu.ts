export const ALLOW_CONTEXT_MENU_ATTR = "data-allow-context-menu";

export const ALLOW_CONTEXT_MENU_SELECTOR = `[${ALLOW_CONTEXT_MENU_ATTR}='true']`;

// Spread onto an element to let the native context menu open inside it.
export const allowContextMenuProps = {
  [ALLOW_CONTEXT_MENU_ATTR]: "true",
} as const;

export function targetAllowsContextMenu(target: EventTarget | null): boolean {
  return (
    target instanceof Element &&
    target.closest(ALLOW_CONTEXT_MENU_SELECTOR) !== null
  );
}
