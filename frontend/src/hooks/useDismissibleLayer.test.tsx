import { describe, expect, it, vi } from "vitest";

import { useDismissibleLayer } from "@/hooks/useDismissibleLayer";
import { render, screen } from "@/test/render";

function Layer({ onClose, open }: { onClose: () => void; open: boolean }) {
  const ref = useDismissibleLayer<HTMLDivElement>(open, onClose);
  return (
    <div>
      <div data-testid="inside" ref={ref}>
        inside
      </div>
      <button type="button">outside</button>
    </div>
  );
}

describe("useDismissibleLayer", () => {
  it("closes on outside pointer and Escape, but not inside pointer", () => {
    const onClose = vi.fn();
    render(<Layer onClose={onClose} open />);

    screen
      .getByTestId("inside")
      .dispatchEvent(new MouseEvent("mousedown", { bubbles: true }));
    expect(onClose).not.toHaveBeenCalled();

    screen
      .getByRole("button", { name: "outside" })
      .dispatchEvent(new MouseEvent("mousedown", { bubbles: true }));
    expect(onClose).toHaveBeenCalledTimes(1);

    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    expect(onClose).toHaveBeenCalledTimes(2);
  });

  it("does not attach listeners while closed", () => {
    const onClose = vi.fn();
    render(<Layer onClose={onClose} open={false} />);

    screen
      .getByRole("button", { name: "outside" })
      .dispatchEvent(new MouseEvent("mousedown", { bubbles: true }));
    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));

    expect(onClose).not.toHaveBeenCalled();
  });
});
