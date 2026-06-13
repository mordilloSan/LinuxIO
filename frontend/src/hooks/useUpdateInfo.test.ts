import { beforeEach, describe, expect, it, vi } from "vitest";

import { useUpdateInfo } from "@/hooks/useUpdateInfo";
import { act, renderHook } from "@/test/render";

describe("useUpdateInfo", () => {
  beforeEach(() => {
    vi.spyOn(console, "error").mockImplementation(() => {});
  });

  it("loads update info from session storage", () => {
    sessionStorage.setItem(
      "update_info",
      JSON.stringify({
        available: true,
        current_version: "1.0.0",
        latest_version: "1.1.0",
      }),
    );

    const { result } = renderHook(() => useUpdateInfo());

    expect(result.current.updateInfo).toMatchObject({
      available: true,
      current_version: "1.0.0",
      latest_version: "1.1.0",
    });
  });

  it("returns null for malformed stored update info", () => {
    sessionStorage.setItem("update_info", "{");

    const { result } = renderHook(() => useUpdateInfo());

    expect(result.current.updateInfo).toBeNull();
    expect(console.error).toHaveBeenCalled();
  });

  it("dismisses update info from state and storage", () => {
    sessionStorage.setItem(
      "update_info",
      JSON.stringify({ available: true, current_version: "1.0.0" }),
    );
    const { result } = renderHook(() => useUpdateInfo());

    act(() => result.current.dismissUpdate());

    expect(result.current.updateInfo).toBeNull();
    expect(sessionStorage.getItem("update_info")).toBeNull();
  });
});
