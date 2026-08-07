import { beforeEach, describe, expect, it, vi } from "vitest";

import { act, renderHook, waitFor } from "@/test/render";

import { useUpdateInfo } from "./useUpdateInfo";

const authState = vi.hoisted(() => ({
  isAuthenticated: false,
  privileged: false,
}));

vi.mock("@/hooks/useAuth", () => ({
  default: () => authState,
}));

const updateInfo = {
  available: true,
  current_version: "1.0.0",
  latest_version: "1.1.0",
};

describe("useUpdateInfo", () => {
  beforeEach(() => {
    sessionStorage.clear();
    authState.isAuthenticated = false;
    authState.privileged = false;
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("loads valid update info from session storage", () => {
    sessionStorage.setItem("update_info", JSON.stringify(updateInfo));
    sessionStorage.setItem("update_info_checked", "1");

    const { result } = renderHook(() => useUpdateInfo());

    expect(result.current.updateInfo).toEqual(updateInfo);
  });

  it("removes malformed stored update info and treats it as unfetched", () => {
    sessionStorage.setItem("update_info", "{");
    sessionStorage.setItem("update_info_checked", "1");

    const { result } = renderHook(() => useUpdateInfo());

    expect(result.current.updateInfo).toBeNull();
    expect(sessionStorage.getItem("update_info")).toBeNull();
    expect(sessionStorage.getItem("update_info_checked")).toBeNull();
  });

  it("fetches once for an authenticated privileged user and stores a 200 response", async () => {
    authState.isAuthenticated = true;
    authState.privileged = true;
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => updateInfo,
    });
    vi.stubGlobal("fetch", fetchMock);

    const { result, rerender } = renderHook(() => useUpdateInfo());

    await waitFor(() => expect(result.current.updateInfo).toEqual(updateInfo));
    rerender();

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/update-info",
      expect.objectContaining({
        cache: "no-store",
        credentials: "include",
        signal: expect.any(AbortSignal),
      }),
    );
    expect(sessionStorage.getItem("update_info")).toBe(
      JSON.stringify(updateInfo),
    );
    expect(sessionStorage.getItem("update_info_checked")).toBe("1");
  });

  it("marks a 204 response as checked without refetching on remount", async () => {
    authState.isAuthenticated = true;
    authState.privileged = true;
    const fetchMock = vi.fn().mockResolvedValue({ ok: false, status: 204 });
    vi.stubGlobal("fetch", fetchMock);

    const first = renderHook(() => useUpdateInfo());
    await waitFor(() =>
      expect(sessionStorage.getItem("update_info_checked")).toBe("1"),
    );
    first.unmount();

    const second = renderHook(() => useUpdateInfo());
    expect(second.result.current.updateInfo).toBeNull();
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("does not fetch for unprivileged or unauthenticated users", () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);

    renderHook(() => useUpdateInfo());
    expect(fetchMock).not.toHaveBeenCalled();

    authState.isAuthenticated = true;
    authState.privileged = false;
    renderHook(() => useUpdateInfo());
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("persists dismissal across remounts", async () => {
    authState.isAuthenticated = true;
    authState.privileged = true;
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => updateInfo,
    });
    vi.stubGlobal("fetch", fetchMock);

    const first = renderHook(() => useUpdateInfo());
    await waitFor(() =>
      expect(first.result.current.updateInfo).toEqual(updateInfo),
    );

    act(() => first.result.current.dismissUpdate());
    expect(sessionStorage.getItem("update_info")).toBeNull();
    expect(sessionStorage.getItem("update_info_checked")).toBe("1");
    first.unmount();

    const second = renderHook(() => useUpdateInfo());
    expect(second.result.current.updateInfo).toBeNull();
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("ignores a late response after unmount", async () => {
    authState.isAuthenticated = true;
    authState.privileged = true;
    let requestSignal: AbortSignal | null | undefined;
    let resolveFetch!: (response: unknown) => void;
    const fetchMock = vi
      .fn()
      .mockImplementation((_url: string, options: RequestInit) => {
        requestSignal = options.signal;
        return new Promise((resolve) => {
          resolveFetch = resolve;
        });
      });
    vi.stubGlobal("fetch", fetchMock);

    const { result, unmount } = renderHook(() => useUpdateInfo());
    unmount();
    expect(requestSignal?.aborted).toBe(true);

    await act(async () => {
      resolveFetch({
        ok: true,
        status: 200,
        json: async () => updateInfo,
      });
      await Promise.resolve();
    });

    expect(result.current.updateInfo).toBeNull();
    expect(sessionStorage.getItem("update_info")).toBeNull();
    expect(sessionStorage.getItem("update_info_checked")).toBeNull();
  });
});
