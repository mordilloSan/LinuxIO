import { act, fireEvent, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import AppTooltip from "@/components/ui/AppTooltip";
import { render } from "@/test/render";

const rect = (left: number, top: number, width: number, height: number) =>
  ({
    bottom: top + height,
    height,
    left,
    right: left + width,
    top,
    width,
  }) as DOMRect;

describe("AppTooltip", () => {
  it("keeps a tooltip anchored near the right edge inside the viewport", async () => {
    vi.useFakeTimers();
    Object.defineProperty(window, "innerWidth", {
      configurable: true,
      value: 160,
    });
    Object.defineProperty(window, "innerHeight", {
      configurable: true,
      value: 100,
    });
    const boundingRect = vi
      .spyOn(HTMLElement.prototype, "getBoundingClientRect")
      .mockImplementation(function getBoundingClientRect(this: HTMLElement) {
        if (this.classList.contains("app-tooltip")) {
          const center = Number.parseFloat(this.style.left);
          return rect(center - 50, 42, 100, 30);
        }

        if (this.getAttribute("data-testid") === "trigger") {
          return rect(132, 10, 20, 20);
        }

        return rect(0, 0, 0, 0);
      });

    render(
      <AppTooltip title="Update All (15)">
        <button data-testid="trigger" type="button">
          Update all
        </button>
      </AppTooltip>,
    );

    fireEvent.mouseEnter(screen.getByTestId("trigger"));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(100);
    });

    const tooltip = screen.getByRole("tooltip");
    expect(tooltip).toHaveStyle({ left: "102px" });
    expect(tooltip.getBoundingClientRect().right).toBe(152);

    boundingRect.mockRestore();
  });
});
