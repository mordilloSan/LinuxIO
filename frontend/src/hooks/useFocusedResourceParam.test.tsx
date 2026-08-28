import { fireEvent } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { useFocusedResourceParam } from "@/hooks/useFocusedResourceParam";
import { renderHook } from "@/test/render";

type Item = { id: string };
const items: Item[] = [{ id: "a" }, { id: "b" }];
const getId = (item: Item) => item.id;

describe("useFocusedResourceParam", () => {
  afterEach(() => {
    document.body.innerHTML = "";
  });

  it("returns the focused item and clears when it leaves the list", () => {
    const onClear = vi.fn();
    const { result, rerender } = renderHook(
      ({ list }: { list: Item[] }) =>
        useFocusedResourceParam({
          focusedId: "b",
          getId,
          items: list,
          onClear,
        }),
      { initialProps: { list: items } },
    );

    expect(result.current).toEqual({ id: "b" });
    expect(onClear).not.toHaveBeenCalled();

    rerender({ list: [{ id: "a" }] });

    expect(result.current).toBeNull();
    expect(onClear).toHaveBeenCalledTimes(1);
  });

  it("clears on Escape only while focused and no dialog is open", () => {
    const onClear = vi.fn();
    const { rerender } = renderHook(
      ({ focusedId }: { focusedId: string | undefined }) =>
        useFocusedResourceParam({ focusedId, getId, items, onClear }),
      { initialProps: { focusedId: undefined as string | undefined } },
    );

    fireEvent.keyDown(window, { key: "Escape" });
    expect(onClear).not.toHaveBeenCalled();

    rerender({ focusedId: "a" });

    const dialog = document.createElement("div");
    dialog.className = "app-dialog-root";
    document.body.append(dialog);
    fireEvent.keyDown(window, { key: "Escape" });
    expect(onClear).not.toHaveBeenCalled();
    dialog.remove();

    fireEvent.keyDown(window, { key: "Escape" });
    expect(onClear).toHaveBeenCalledTimes(1);
  });
});
