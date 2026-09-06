import { act, fireEvent, screen } from "@testing-library/react";
import { toast } from "sonner";
import { afterEach, describe, expect, it, vi } from "vitest";

import AppTooltip from "@/components/ui/AppTooltip";
import { render } from "@/test/render";
import { copyToClipboard } from "@/utils/clipboard";

vi.mock("@/utils/clipboard", () => ({
  copyToClipboard: vi.fn().mockResolvedValue(undefined),
}));

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
  vi.unstubAllGlobals();
});

describe("AppTooltip", () => {
  it("defers positioning and listeners until opening and preserves the focused trigger", async () => {
    vi.useFakeTimers();
    const view = render(
      <AppTooltip title="Keyboard tooltip">
        <button type="button">Target</button>
      </AppTooltip>,
    );
    const target = screen.getByRole("button", { name: "Target" });
    const measure = vi.spyOn(target, "getBoundingClientRect");
    const addListener = vi.spyOn(window, "addEventListener");
    const removeListener = vi.spyOn(window, "removeEventListener");

    document.documentElement.setAttribute("data-tab-navigation", "true");
    act(() => target.focus());
    await act(async () => {
      await vi.advanceTimersByTimeAsync(99);
    });
    expect(measure).not.toHaveBeenCalled();
    expect(addListener).not.toHaveBeenCalledWith("resize", expect.anything());
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(screen.getByRole("tooltip")).toHaveTextContent("Keyboard tooltip");
    expect(screen.getByRole("button", { name: "Target" })).toBe(target);
    expect(target).toHaveFocus();
    expect(measure).toHaveBeenCalledTimes(1);
    expect(addListener).toHaveBeenCalledWith("scroll", expect.anything(), true);
    expect(addListener).toHaveBeenCalledWith("resize", expect.anything());

    fireEvent.mouseLeave(target);
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
    expect(target).toHaveFocus();
    expect(removeListener).toHaveBeenCalledWith(
      "scroll",
      expect.anything(),
      true,
    );
    expect(removeListener).toHaveBeenCalledWith("resize", expect.anything());
    fireEvent.resize(window);
    expect(measure).toHaveBeenCalledTimes(1);

    fireEvent.mouseEnter(target);
    view.unmount();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(150);
    });
    expect(measure).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
    measure.mockRestore();
    addListener.mockRestore();
    removeListener.mockRestore();
  });

  it("copies current text without hovering and stops copying when it no longer truncates", async () => {
    const success = vi.spyOn(toast, "success").mockReturnValue("copied");
    vi.mocked(copyToClipboard).mockClear();
    const view = render(
      <AppTooltip copyText="Old text" title="Copy tooltip">
        <span data-testid="trigger">Target</span>
      </AppTooltip>,
    );
    const target = screen.getByTestId("trigger");
    Object.defineProperty(target, "scrollWidth", {
      configurable: true,
      value: 200,
    });
    Object.defineProperty(target, "clientWidth", {
      configurable: true,
      value: 100,
    });
    view.rerender(
      <AppTooltip
        copyText="Current text"
        copySuccessMessage="Current success"
        title="Copy tooltip"
      >
        <span data-testid="trigger">Target</span>
      </AppTooltip>,
    );

    await act(async () => {
      fireEvent.click(target);
    });
    expect(copyToClipboard).toHaveBeenCalledExactlyOnceWith("Current text");
    expect(success).toHaveBeenCalledWith("Current success", undefined);
    expect(target.parentElement).toHaveClass("app-tooltip-trigger--copy");
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();

    Object.defineProperty(target, "scrollWidth", {
      configurable: true,
      value: 100,
    });
    await act(async () => {
      fireEvent.click(target);
    });
    expect(copyToClipboard).toHaveBeenCalledTimes(1);
    expect(target.parentElement).not.toHaveClass("app-tooltip-trigger--copy");
    success.mockRestore();
  });

  it("uses current props when a delayed tooltip opens and rechecks truncation on resize", async () => {
    vi.useFakeTimers();
    const view = render(
      <AppTooltip title="Old title">
        <span data-testid="trigger">Target</span>
      </AppTooltip>,
    );
    const target = screen.getByTestId("trigger");
    Object.defineProperty(target, "scrollWidth", {
      configurable: true,
      value: 200,
    });
    Object.defineProperty(target, "clientWidth", {
      configurable: true,
      value: 100,
    });
    const measure = vi
      .spyOn(target, "getBoundingClientRect")
      .mockReturnValue(rect(20, 10, 40, 20));
    fireEvent.mouseEnter(target);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(50);
    });
    view.rerender(
      <AppTooltip
        copyText="Current copy"
        onlyWhenTruncated
        placement="right"
        title="Current title"
      >
        <span data-testid="trigger">Target</span>
      </AppTooltip>,
    );
    await act(async () => {
      await vi.advanceTimersByTimeAsync(50);
    });
    expect(screen.getByRole("tooltip")).toHaveTextContent("Current title");
    expect(screen.getByRole("tooltip")).toHaveStyle({
      left: "68px",
      top: "20px",
    });
    expect(target.parentElement).toHaveClass("app-tooltip-trigger--copy");

    measure.mockReturnValue(rect(30, 20, 40, 20));
    fireEvent.scroll(window);
    expect(screen.getByRole("tooltip")).toHaveStyle({
      left: "78px",
      top: "30px",
    });
    Object.defineProperty(target, "scrollWidth", {
      configurable: true,
      value: 100,
    });
    fireEvent.resize(window);
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
    expect(target.parentElement).not.toHaveClass("app-tooltip-trigger--copy");
    measure.mockRestore();
  });

  it("measures copy availability on hover instead of observing the trigger", () => {
    let observerCount = 0;
    class CountingResizeObserver {
      disconnect = vi.fn();
      observe = vi.fn();
      unobserve = vi.fn();

      constructor() {
        observerCount += 1;
      }
    }
    vi.stubGlobal("ResizeObserver", CountingResizeObserver);

    const view = render(
      <AppTooltip title="Plain tooltip">
        <button type="button">Target</button>
      </AppTooltip>,
    );
    expect(observerCount).toBe(0);

    // Copy support used to mount a ResizeObserver and a window resize listener
    // per trigger; virtualized rows mount dozens per scroll frame, so the
    // truncation check now runs in show() and on click instead.
    view.rerender(
      <AppTooltip copyText="copy me" title="Copy tooltip">
        <button type="button">Target</button>
      </AppTooltip>,
    );
    expect(observerCount).toBe(0);
  });

  it("drops the copy affordance when copyText is removed while mounted", () => {
    const view = render(
      <AppTooltip copyText="copy me" title="Copy tooltip">
        <span data-testid="trigger">Target</span>
      </AppTooltip>,
    );
    const trigger = screen.getByTestId("trigger");
    Object.defineProperty(trigger, "scrollWidth", {
      configurable: true,
      value: 200,
    });
    Object.defineProperty(trigger, "clientWidth", {
      configurable: true,
      value: 100,
    });
    const wrapper = trigger.parentElement as HTMLElement;
    fireEvent.mouseEnter(wrapper);
    expect(wrapper).toHaveClass("app-tooltip-trigger--copy");

    // No hover or resize follows the prop change, so nothing recomputes
    // canCopy; the class must come from the current copyText instead.
    view.rerender(
      <AppTooltip title="Copy tooltip">
        <span data-testid="trigger">Target</span>
      </AppTooltip>,
    );
    expect(wrapper).not.toHaveClass("app-tooltip-trigger--copy");
  });

  it.each([0, 0.0001])(
    "keeps a tooltip inside the viewport with %s px of layout rounding",
    async (rounding) => {
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
            const center =
              Math.round(Number.parseFloat(this.style.left) * 64) / 64;
            return rect(center - 50 + rounding, 42, 100, 30);
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
      expect(Number.parseFloat(tooltip.style.left)).toBeCloseTo(102);
      expect(tooltip.getBoundingClientRect().right).toBeCloseTo(152);

      boundingRect.mockRestore();
    },
  );

  it("settles when the tooltip is larger than the viewport", async () => {
    vi.useFakeTimers();
    Object.defineProperty(window, "innerWidth", {
      configurable: true,
      value: 160,
    });
    Object.defineProperty(window, "innerHeight", {
      configurable: true,
      value: 80,
    });
    const boundingRect = vi
      .spyOn(HTMLElement.prototype, "getBoundingClientRect")
      .mockImplementation(function getBoundingClientRect(this: HTMLElement) {
        if (this.classList.contains("app-tooltip")) {
          const left = Number.parseFloat(this.style.left);
          const top = Number.parseFloat(this.style.top);
          return rect(left - 160, top, 320, 120);
        }

        if (this.getAttribute("data-testid") === "trigger") {
          return rect(60, 10, 20, 20);
        }

        return rect(0, 0, 0, 0);
      });

    render(
      <AppTooltip title="A tooltip with more content than the viewport can show">
        <button data-testid="trigger" type="button">
          Target
        </button>
      </AppTooltip>,
    );

    fireEvent.mouseEnter(screen.getByTestId("trigger"));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(100);
    });

    const tooltip = screen.getByRole("tooltip");
    expect(tooltip).toHaveStyle({ left: "168px", top: "8px" });
    expect(tooltip.getBoundingClientRect()).toMatchObject({
      left: 8,
      top: 8,
    });

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

    // Entering and then focusing within the same 100ms delay calls show()
    // twice. The second call used to overwrite the pending timer handle, so the
    // first timer survived hide() and put the bubble back on an empty page.
    fireEvent.mouseEnter(chevron);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30);
    });
    document.documentElement.setAttribute("data-tab-navigation", "true");
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
