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

  it("defaults to card view when no config value exists", () => {
    const { result } = renderHook(() => useViewMode("services"));

    expect(result.current[0]).toBe("card");
  });

  it("returns configured mode for the key", () => {
    configMocks.viewModes = { services: "table" };

    const { result } = renderHook(() => useViewMode("services"));

    expect(result.current[0]).toBe("table");
  });

  it("stores non-default modes", () => {
    const { result } = renderHook(() => useViewMode("services"));

    act(() => result.current[1]("table"));
    const updater = configMocks.setViewModes.mock.calls[0][0];

    expect(updater(undefined)).toEqual({ services: "table" });
  });

  it("removes keys when resetting to the default", () => {
    const { result } = renderHook(() => useViewMode("services"));

    act(() => result.current[1]("card"));
    const updater = configMocks.setViewModes.mock.calls[0][0];

    expect(updater({ services: "table", docker: "table" })).toEqual({
      docker: "table",
    });
    expect(updater({ services: "table" })).toBeUndefined();
  });

  it("supports functional updates", () => {
    const { result } = renderHook(() => useViewMode("services"));

    act(() =>
      result.current[1]((prev) => (prev === "card" ? "table" : "card")),
    );
    const updater = configMocks.setViewModes.mock.calls[0][0];

    expect(updater(undefined)).toEqual({ services: "table" });
  });
});
