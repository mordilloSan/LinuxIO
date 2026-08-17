import { describe, expect, it, vi } from "vitest";

import { renderHook } from "@/test/render";

import { useCardSelectionEscape } from "./useCardSelectionEscape";

const pressEscape = () => {
  const event = new KeyboardEvent("keydown", {
    bubbles: true,
    cancelable: true,
    key: "Escape",
  });
  window.dispatchEvent(event);
  return event;
};

describe("useCardSelectionEscape", () => {
  it("clears selection and claims an unguarded Escape", () => {
    const onClearSelection = vi.fn();
    const onExitReordering = vi.fn();
    renderHook(() =>
      useCardSelectionEscape({
        enabled: true,
        isReordering: false,
        onClearSelection,
        onExitReordering,
      }),
    );

    const card = document.createElement("button");
    card.className = "selectable-card-button";
    document.body.append(card);
    card.focus();

    const event = pressEscape();

    expect(onClearSelection).toHaveBeenCalledOnce();
    expect(onExitReordering).not.toHaveBeenCalled();
    expect(event.defaultPrevented).toBe(true);
    expect(card).not.toHaveFocus();
    card.remove();
  });

  it("leaves Escape to dialogs and handlers that already claimed it", () => {
    const onClearSelection = vi.fn();
    renderHook(() =>
      useCardSelectionEscape({
        enabled: true,
        isReordering: false,
        onClearSelection,
        onExitReordering: vi.fn(),
      }),
    );

    const claimedEvent = new KeyboardEvent("keydown", {
      cancelable: true,
      key: "Escape",
    });
    claimedEvent.preventDefault();
    window.dispatchEvent(claimedEvent);

    const dialog = document.createElement("div");
    dialog.className = "app-dialog-root";
    document.body.append(dialog);
    const dialogEvent = pressEscape();
    dialog.remove();

    expect(onClearSelection).not.toHaveBeenCalled();
    expect(dialogEvent.defaultPrevented).toBe(false);
  });

  it("waits for a second Escape when the first exits reorder mode", () => {
    const onClearSelection = vi.fn();
    const onExitReordering = vi.fn();
    const { rerender } = renderHook(
      ({ isReordering }) =>
        useCardSelectionEscape({
          enabled: true,
          isReordering,
          onClearSelection,
          onExitReordering,
        }),
      { initialProps: { isReordering: true } },
    );

    const card = document.createElement("button");
    card.className = "selectable-card-button";
    document.body.append(card);
    card.focus();

    const firstEscape = pressEscape();
    expect(onClearSelection).not.toHaveBeenCalled();
    expect(onExitReordering).toHaveBeenCalledOnce();
    expect(firstEscape.defaultPrevented).toBe(true);
    expect(card).not.toHaveFocus();

    rerender({ isReordering: false });
    const secondEscape = pressEscape();

    expect(onClearSelection).toHaveBeenCalledOnce();
    expect(secondEscape.defaultPrevented).toBe(true);
    card.remove();
  });
});
