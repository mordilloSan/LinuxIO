function isTextEntryElement(node: EventTarget | null): boolean {
  return (
    node instanceof HTMLInputElement ||
    node instanceof HTMLTextAreaElement ||
    node instanceof HTMLSelectElement ||
    (node instanceof HTMLElement && node.isContentEditable)
  );
}

/**
 * Whether a keydown belongs to text entry, where a shortcut like Ctrl/Cmd-A or
 * Ctrl/Cmd-C must keep its native meaning instead of being hijacked.
 *
 * Both `document.activeElement` and the event target are checked: a retargeted
 * or synthetic event can name one without the other.
 */
export function isTypingTarget(event: { target: EventTarget | null }): boolean {
  return (
    isTextEntryElement(document.activeElement) ||
    isTextEntryElement(event.target)
  );
}
