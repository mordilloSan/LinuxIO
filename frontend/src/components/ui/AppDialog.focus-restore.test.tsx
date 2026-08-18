import { afterEach, describe, expect, it, vi, type MockInstance } from "vitest";

import { AppDialog } from "@/components/ui/AppDialog";
import { render } from "@/test/render";

let trigger: HTMLButtonElement;
let focusSpy: MockInstance;

function mountFocusedTrigger() {
  trigger = document.createElement("button");
  document.body.appendChild(trigger);
  trigger.focus();
  focusSpy = vi.spyOn(trigger, "focus");
}

afterEach(() => {
  trigger.remove();
});

describe("AppDialog focus restore", () => {
  it("restores the connected trigger independently of close input", () => {
    mountFocusedTrigger();
    const { rerender } = render(<AppDialog open>Dialog</AppDialog>);

    document.dispatchEvent(new Event("pointerdown"));
    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    rerender(<AppDialog open={false}>Dialog</AppDialog>);

    expect(focusSpy).toHaveBeenCalledOnce();
    expect(focusSpy).toHaveBeenCalledWith();
  });

  it("restores on unmount while open", () => {
    mountFocusedTrigger();
    const { unmount } = render(<AppDialog open>Dialog</AppDialog>);

    unmount();

    expect(focusSpy).toHaveBeenCalledOnce();
    expect(focusSpy).toHaveBeenCalledWith();
  });

  it("does not focus a trigger that left the document", () => {
    mountFocusedTrigger();
    const { rerender } = render(<AppDialog open>Dialog</AppDialog>);

    trigger.remove();
    rerender(<AppDialog open={false}>Dialog</AppDialog>);

    expect(focusSpy).not.toHaveBeenCalled();
  });
});
