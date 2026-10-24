import { act, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { render } from "@/test/render";

import StatusLine from "./StatusLine";

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

describe("StatusLine", () => {
  it("shows a spinner, a verb and the elapsed time", () => {
    render(<StatusLine turnStartedAt={Date.now()} />);
    expect(screen.getByRole("progressbar")).toBeInTheDocument();
    expect(screen.getByText(/^Thinking…/)).toBeInTheDocument();
    expect(screen.getByText("0:00")).toBeInTheDocument();
    act(() => {
      vi.advanceTimersByTime(61_000);
    });
    expect(screen.getByText("1:01")).toBeInTheDocument();
  });

  it("cycles to the next verb every 4 seconds and wraps around", () => {
    render(<StatusLine turnStartedAt={Date.now()} />);
    act(() => {
      vi.advanceTimersByTime(3_999);
    });
    expect(screen.getByText(/^Thinking…/)).toBeInTheDocument();
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(screen.getByText(/^Working…/)).toBeInTheDocument();
    act(() => {
      vi.advanceTimersByTime(4_000 * 5);
    });
    expect(screen.getByText(/^Thinking…/)).toBeInTheDocument();
  });

  it("clears its intervals on unmount", () => {
    const setIntervalSpy = vi.spyOn(globalThis, "setInterval");
    const clearIntervalSpy = vi.spyOn(globalThis, "clearInterval");
    const { unmount } = render(<StatusLine turnStartedAt={Date.now()} />);
    expect(setIntervalSpy).toHaveBeenCalledTimes(2);
    const ids = setIntervalSpy.mock.results.map((result) => result.value);
    unmount();
    for (const id of ids) expect(clearIntervalSpy).toHaveBeenCalledWith(id);
  });
});
