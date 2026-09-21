import { expect, it, vi } from "vitest";

import AppPopover from "@/components/ui/AppPopover";
import { act, render } from "@/test/render";

it("claims Escape when dismissing so the underlying page keeps its selection", () => {
  const onClose = vi.fn();
  render(
    <AppPopover anchorPosition={{ top: 100, left: 100 }} onClose={onClose} open>
      Menu
    </AppPopover>,
  );
  const event = new KeyboardEvent("keydown", {
    key: "Escape",
    cancelable: true,
    bubbles: true,
  });
  act(() => {
    document.body.dispatchEvent(event);
  });
  expect(onClose).toHaveBeenCalledTimes(1);
  expect(event.defaultPrevented).toBe(true);
});
