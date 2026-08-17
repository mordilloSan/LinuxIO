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
  it("restores the trigger without a ring after a pointer-driven close", () => {
    mountFocusedTrigger();
    const { rerender } = render(<AppDialog open>Dialog</AppDialog>);

    document.dispatchEvent(new Event("pointerdown"));
    rerender(<AppDialog open={false}>Dialog</AppDialog>);

    expect(focusSpy).toHaveBeenCalledWith({ focusVisible: false });
  });

  it("restores the trigger with a ring after a keyboard-driven close", () => {
    mountFocusedTrigger();
    const { rerender } = render(<AppDialog open>Dialog</AppDialog>);

    document.dispatchEvent(new Event("pointerdown"));
    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter" }));
    rerender(<AppDialog open={false}>Dialog</AppDialog>);

    expect(focusSpy).toHaveBeenCalledWith({ focusVisible: true });
  });

  it("does not count bare modifier presses as keyboard input", () => {
    mountFocusedTrigger();
    const { rerender } = render(<AppDialog open>Dialog</AppDialog>);

    document.dispatchEvent(new Event("pointerdown"));
    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Control" }));
    rerender(<AppDialog open={false}>Dialog</AppDialog>);

    expect(focusSpy).toHaveBeenCalledWith({ focusVisible: false });
  });

  it("keeps the ring when the dialog saw no input at all", () => {
    mountFocusedTrigger();
    const { rerender } = render(<AppDialog open>Dialog</AppDialog>);

    rerender(<AppDialog open={false}>Dialog</AppDialog>);

    expect(focusSpy).toHaveBeenCalledWith({ focusVisible: true });
  });

  it("restores on unmount while open", () => {
    mountFocusedTrigger();
    const { unmount } = render(<AppDialog open>Dialog</AppDialog>);

    document.dispatchEvent(new Event("pointerdown"));
    unmount();

    expect(focusSpy).toHaveBeenCalledWith({ focusVisible: false });
  });

  it("does not focus a trigger that left the document", () => {
    mountFocusedTrigger();
    const { rerender } = render(<AppDialog open>Dialog</AppDialog>);

    trigger.remove();
    rerender(<AppDialog open={false}>Dialog</AppDialog>);

    expect(focusSpy).not.toHaveBeenCalled();
  });
});
