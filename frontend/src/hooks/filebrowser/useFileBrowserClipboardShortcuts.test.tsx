import { afterEach, describe, expect, it, vi } from "vitest";

import { useFileBrowserClipboardShortcuts } from "@/hooks/filebrowser/useFileBrowserClipboardShortcuts";
import { renderHook } from "@/test/render";

function mountShortcuts(
  overrides: Partial<{
    editingPath: string | null;
    renamingPath: string | null;
  }> = {},
) {
  const handlers = {
    onCopy: vi.fn(),
    onCut: vi.fn(),
    onPaste: vi.fn(),
  };

  const utils = renderHook(() =>
    useFileBrowserClipboardShortcuts({
      editingPath: null,
      renamingPath: null,
      ...overrides,
      ...handlers,
    }),
  );

  return { ...utils, handlers };
}

function dispatchKey(
  key: string,
  init: KeyboardEventInit = {},
  target: EventTarget = document,
) {
  const event = new KeyboardEvent("keydown", {
    bubbles: true,
    cancelable: true,
    key,
    ...init,
  });
  target.dispatchEvent(event);
  return event;
}

describe("useFileBrowserClipboardShortcuts", () => {
  const addedNodes: HTMLElement[] = [];

  function appendNode<T extends HTMLElement>(node: T): T {
    document.body.appendChild(node);
    addedNodes.push(node);
    return node;
  }

  afterEach(() => {
    addedNodes.splice(0).forEach((node) => node.remove());
  });

  it("invokes the matching handler for Ctrl+C/X/V and prevents default", () => {
    const { handlers } = mountShortcuts();

    const copy = dispatchKey("c", { ctrlKey: true });
    expect(handlers.onCopy).toHaveBeenCalledTimes(1);
    expect(copy.defaultPrevented).toBe(true);

    const cut = dispatchKey("x", { ctrlKey: true });
    expect(handlers.onCut).toHaveBeenCalledTimes(1);
    expect(cut.defaultPrevented).toBe(true);

    const paste = dispatchKey("v", { ctrlKey: true });
    expect(handlers.onPaste).toHaveBeenCalledTimes(1);
    expect(paste.defaultPrevented).toBe(true);
  });

  it("treats Cmd (metaKey) the same as Ctrl", () => {
    const { handlers } = mountShortcuts();

    dispatchKey("v", { metaKey: true });

    expect(handlers.onPaste).toHaveBeenCalledTimes(1);
  });

  it("ignores the shortcut key without a Ctrl/Cmd modifier", () => {
    const { handlers } = mountShortcuts();

    const event = dispatchKey("c");

    expect(handlers.onCopy).not.toHaveBeenCalled();
    expect(event.defaultPrevented).toBe(false);
  });

  it("ignores unrelated keys even with a modifier held", () => {
    const { handlers } = mountShortcuts();

    dispatchKey("a", { ctrlKey: true });

    expect(handlers.onCopy).not.toHaveBeenCalled();
    expect(handlers.onCut).not.toHaveBeenCalled();
    expect(handlers.onPaste).not.toHaveBeenCalled();
  });

  it("suppresses shortcuts while inline editing is active", () => {
    const { handlers } = mountShortcuts({ editingPath: "/srv/a.txt" });

    dispatchKey("c", { ctrlKey: true });

    expect(handlers.onCopy).not.toHaveBeenCalled();
  });

  it("suppresses shortcuts while a rename is in progress", () => {
    const { handlers } = mountShortcuts({ renamingPath: "/srv/a.txt" });

    dispatchKey("x", { ctrlKey: true });

    expect(handlers.onCut).not.toHaveBeenCalled();
  });

  it("suppresses shortcuts while a dialog is open", () => {
    appendNode(
      Object.assign(document.createElement("div"), {
        className: "app-dialog-root",
      }),
    );
    const { handlers } = mountShortcuts();

    dispatchKey("v", { ctrlKey: true });

    expect(handlers.onPaste).not.toHaveBeenCalled();
  });

  it("ignores shortcuts when the event target is an input field", () => {
    const input = appendNode(document.createElement("input"));
    const { handlers } = mountShortcuts();

    dispatchKey("c", { ctrlKey: true }, input);

    expect(handlers.onCopy).not.toHaveBeenCalled();
  });

  it("ignores shortcuts when the event target is contentEditable", () => {
    const editable = appendNode(document.createElement("div"));
    Object.defineProperty(editable, "isContentEditable", {
      configurable: true,
      value: true,
    });
    const { handlers } = mountShortcuts();

    dispatchKey("c", { ctrlKey: true }, editable);

    expect(handlers.onCopy).not.toHaveBeenCalled();
  });

  it("ignores shortcuts when the focused element is a textarea", () => {
    const textarea = appendNode(document.createElement("textarea"));
    textarea.focus();
    expect(document.activeElement).toBe(textarea);

    const { handlers } = mountShortcuts();

    dispatchKey("c", { ctrlKey: true });

    expect(handlers.onCopy).not.toHaveBeenCalled();
  });

  it("detaches the keydown listener on unmount", () => {
    const { handlers, unmount } = mountShortcuts();

    unmount();
    dispatchKey("c", { ctrlKey: true });

    expect(handlers.onCopy).not.toHaveBeenCalled();
  });
});
