import { beforeEach, describe, expect, it, vi } from "vitest";

import type { TableCardViewMode } from "@/api";

const configMocks = vi.hoisted(() => ({
  setViewModes: vi.fn(),
  viewModes: {} as Record<string, TableCardViewMode>,
  viewModeDefault: "card" as TableCardViewMode,
}));

vi.mock("@/hooks/useConfig", () => ({
  useConfigValue: vi.fn(() => [
    configMocks.viewModes,
    configMocks.setViewModes,
  ]),
  useViewModeDefault: () => configMocks.viewModeDefault,
}));

const { useViewMode } = await import("@/hooks/useViewMode");
const { act, renderHook } = await import("@/test/render");

describe("useViewMode", () => {
  beforeEach(() => {
    configMocks.viewModes = {};
    configMocks.viewModeDefault = "card";
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

  it("stores selected modes", () => {
    const { result } = renderHook(() => useViewMode("services"));

    act(() => result.current[1]("table"));
    const updater = configMocks.setViewModes.mock.calls[0][0];

    expect(updater({})).toEqual({ services: "table" });
  });

  it("prunes an explicit selection of the backend default", () => {
    const { result } = renderHook(() => useViewMode("services"));

    act(() => result.current[1]("card"));
    const updater = configMocks.setViewModes.mock.calls[0][0];

    expect(updater({ services: "table", docker: "table" })).toEqual({
      docker: "table",
    });
    expect(updater({ services: "card" })).toEqual({});
  });

  it("uses a changed backend default to re-default inherited surfaces", () => {
    configMocks.viewModes = { services: "card" };
    configMocks.viewModeDefault = "table";
    const { result } = renderHook(() => useViewMode("services"));

    act(() => result.current[1]("table"));
    const updater = configMocks.setViewModes.mock.calls[0][0];

    expect(updater({ services: "card" })).toEqual({});
  });

  it("supports functional updates", () => {
    const { result } = renderHook(() => useViewMode("services"));

    act(() =>
      result.current[1]((prev) => (prev === "card" ? "table" : "card")),
    );
    const updater = configMocks.setViewModes.mock.calls[0][0];

    expect(updater({})).toEqual({ services: "table" });
  });
});
