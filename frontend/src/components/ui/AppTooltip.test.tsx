import { act, fireEvent, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import AppTooltip from "@/components/ui/AppTooltip";
import { render } from "@/test/render";
import { installInputModalityTracking } from "@/utils/inputModality";

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

  it("does not re-show after the pointer leaves when an enter raced a focus", async () => {
    vi.useFakeTimers();
    render(
      <AppTooltip title="Collapse row">
        <button data-testid="chevron" type="button">
          chevron
        </button>
      </AppTooltip>,
    );
    const chevron = screen.getByTestId("chevron");

    // Entering and then clicking within the same 100ms delay calls show()
    // twice. The second call used to overwrite the pending timer handle, so the
    // first timer survived hide() and put the bubble back on an empty page.
    fireEvent.mouseEnter(chevron);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30);
    });
    fireEvent.focus(chevron);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(20);
    });
    fireEvent.mouseLeave(chevron);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300);
    });

    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
    vi.useRealTimers();
  });

  it("summons the bubble on keyboard focus but not on pointer focus", async () => {
    vi.useFakeTimers();
    const uninstall = installInputModalityTracking();
    render(
      <AppTooltip title="Collapse row">
        <button data-testid="chevron" type="button">
          chevron
        </button>
      </AppTooltip>,
    );
    const chevron = screen.getByTestId("chevron");

    // A press fires focusin with the pointer on the trigger — but the pointer
    // may be gone by the time the bubble would appear, and a focus that a
    // dialog restores on close has no pointer near it at all.
    fireEvent.pointerDown(chevron);
    fireEvent.focus(chevron);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300);
    });
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();

    // Keyboard focus keeps its label: it is the one focus with a blur coming.
    fireEvent.keyDown(document, { key: "Tab" });
    fireEvent.focus(chevron);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300);
    });
    expect(screen.getByRole("tooltip")).toBeInTheDocument();

    uninstall();
    vi.useRealTimers();
  });
});
