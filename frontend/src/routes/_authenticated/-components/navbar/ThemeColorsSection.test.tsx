import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { fireEvent, render } from "@/test/render";

import ThemeColorsSection from "./ThemeColorsSection";

const mocks = vi.hoisted(() => ({
  setThemeColors: vi.fn(),
}));

vi.mock("@/hooks/useConfig", () => ({
  useConfigValue: (key: string) => {
    if (key === "theme") return ["LIGHT", vi.fn()];
    if (key === "primaryColor") return ["#1976d2", vi.fn()];
    return [undefined, mocks.setThemeColors];
  },
}));

// Apply the functional updater `handleChange` hands to useConfigValue's setter
// so the assertion reads the colour that would be saved.
const savedColors = (call: number) =>
  mocks.setThemeColors.mock.calls[call]?.[0](undefined);

const pickerInput = (container: HTMLElement) =>
  container.querySelector<HTMLInputElement>('input[type="color"]')!;

describe("ThemeColorsSection colour picker", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    mocks.setThemeColors.mockReset();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("coalesces a drag into one trailing save and previews the live colour", () => {
    const { container } = render(<ThemeColorsSection />);
    const input = pickerInput(container);

    fireEvent.change(input, { target: { value: "#111111" } });
    vi.advanceTimersByTime(200);
    fireEvent.change(input, { target: { value: "#222222" } });
    vi.advanceTimersByTime(200);
    fireEvent.change(input, { target: { value: "#333333" } });

    expect(input.value).toBe("#333333");
    expect(mocks.setThemeColors).not.toHaveBeenCalled();

    vi.advanceTimersByTime(250);
    expect(mocks.setThemeColors).toHaveBeenCalledTimes(1);
    expect(savedColors(0)).toEqual({
      light: { backgroundDefault: "#333333" },
    });
  });

  it("flushes a pending pick when the section unmounts", () => {
    const { container, unmount } = render(<ThemeColorsSection />);
    fireEvent.change(pickerInput(container), {
      target: { value: "#abcdef" },
    });
    expect(mocks.setThemeColors).not.toHaveBeenCalled();

    unmount();
    expect(mocks.setThemeColors).toHaveBeenCalledTimes(1);
    expect(savedColors(0)).toEqual({
      light: { backgroundDefault: "#abcdef" },
    });
    vi.advanceTimersByTime(500);
    expect(mocks.setThemeColors).toHaveBeenCalledTimes(1);
  });
});
