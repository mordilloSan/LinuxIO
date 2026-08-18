import { act, fireEvent, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

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

afterEach(() => {
  document.documentElement.removeAttribute("data-tab-navigation");
  vi.useRealTimers();
});

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

  it("shows on focus only while Tab navigation is active", async () => {
    vi.useFakeTimers();
    render(
      <AppTooltip title="Keyboard tooltip">
        <button type="button">Target</button>
      </AppTooltip>,
    );
    const target = screen.getByRole("button", { name: "Target" });

    fireEvent.focus(target);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(150);
    });
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();

    fireEvent.blur(target);
    document.documentElement.setAttribute("data-tab-navigation", "true");
    fireEvent.focus(target);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(150);
    });

    expect(screen.getByRole("tooltip")).toHaveTextContent("Keyboard tooltip");
  });
});
