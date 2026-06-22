import { beforeEach, describe, expect, it, vi } from "vitest";

import type { TableCardViewMode } from "@/api";

const configMocks = vi.hoisted(() => ({
  setViewModes: vi.fn(),
  viewModes: undefined as Record<string, TableCardViewMode> | undefined,
}));

vi.mock("@/hooks/useConfig", () => ({
  useConfigValue: vi.fn(() => [
    configMocks.viewModes,
    configMocks.setViewModes,
  ]),
}));

const { useViewMode } = await import("@/hooks/useViewMode");
const { act, renderHook } = await import("@/test/render");

describe("useViewMode", () => {
  beforeEach(() => {
    configMocks.viewModes = undefined;
    configMocks.setViewModes.mockReset();
  });

  it("uses fallback mode when no config value exists", () => {
    const { result } = renderHook(() => useViewMode("services"));

    expect(result.current[0]).toBe("table");
  });

  it("returns configured mode for the key", () => {
    configMocks.viewModes = { services: "card" };

    const { result } = renderHook(() => useViewMode("services"));

    expect(result.current[0]).toBe("card");
  });

  it("stores non-fallback modes", () => {
    const { result } = renderHook(() => useViewMode("services"));

    act(() => result.current[1]("card"));
    const updater = configMocks.setViewModes.mock.calls[0][0];

    expect(updater(undefined)).toEqual({ services: "card" });
  });

  it("removes keys when resetting to fallback", () => {
    const { result } = renderHook(() => useViewMode("services"));

    act(() => result.current[1]("table"));
    const updater = configMocks.setViewModes.mock.calls[0][0];

    expect(updater({ services: "card", docker: "card" })).toEqual({
      docker: "card",
    });
    expect(updater({ services: "card" })).toBeUndefined();
  });

  it("supports functional updates", () => {
    const { result } = renderHook(() => useViewMode("services"));

    act(() =>
      result.current[1]((prev) => (prev === "table" ? "card" : "table")),
    );
    const updater = configMocks.setViewModes.mock.calls[0][0];

    expect(updater(undefined)).toEqual({ services: "card" });
  });
});
