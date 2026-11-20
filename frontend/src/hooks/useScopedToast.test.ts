import { afterEach, describe, expect, it, vi } from "vitest";

const toastMock = vi.hoisted(() => ({
  error: vi.fn(),
  info: vi.fn(),
  success: vi.fn(),
  warning: vi.fn(),
}));

vi.mock("sonner", () => ({
  toast: toastMock,
}));

const { registerToastLinkRouter } = await import("@/api/toast-meta");
const { useScopedToast } = await import("@/hooks/useScopedToast");
const { renderHook } = await import("@/test/render");

let releaseLinkRouter = () => {};

/** Puts the user on pathname as far as toast links can tell. */
function onPage(pathname: string) {
  const link = { navigate: vi.fn(), pathname: () => pathname };
  releaseLinkRouter = registerToastLinkRouter(link);
  return link;
}

describe("useScopedToast", () => {
  afterEach(() => releaseLinkRouter());

  it("offers the scope's page when a toast fires on another page", () => {
    const link = onPage("/");
    const { result } = renderHook(() =>
      useScopedToast({ label: "Updates", to: "/updates" }),
    );

    result.current.success("Updated", {
      description: "3 packages",
      meta: { label: "Custom" },
    });

    expect(toastMock.success).toHaveBeenCalledWith("Updated", {
      action: { label: "Custom", onClick: expect.any(Function) },
      description: "3 packages",
      duration: 10_000,
    });
    const { action } = toastMock.success.mock.calls[0][1] as {
      action: { onClick: (event: unknown) => void };
    };
    action.onClick({});
    expect(link.navigate).toHaveBeenCalledWith(
      expect.objectContaining({ to: "/updates" }),
    );
  });

  it("adds nothing on the scope's own page", () => {
    onPage("/updates/history");
    const { result } = renderHook(() =>
      useScopedToast({ label: "Updates", to: "/updates" }),
    );

    result.current.success("Updated", { meta: { label: "Custom" } });

    expect(toastMock.success).toHaveBeenCalledWith("Updated", {});
  });

  it("keeps an action the caller passes", () => {
    onPage("/");
    const { result } = renderHook(() =>
      useScopedToast({
        label: "Open files",
        params: { _splat: "" },
        to: "/filebrowser/$",
      }),
    );
    const action = { label: "Open the real folder", onClick: vi.fn() };

    result.current.error("Refused", { action });

    expect(toastMock.error).toHaveBeenCalledWith("Refused", { action });
  });

  it("keeps one toast scope across renders of an inline meta", () => {
    const { rerender, result } = renderHook(() =>
      useScopedToast({ label: "Updates", to: "/updates" }),
    );
    const first = result.current;

    rerender();

    expect(result.current).toBe(first);
  });

  it("exposes all toast severities", () => {
    onPage("/");
    const { result } = renderHook(() => useScopedToast({ label: "System" }));

    result.current.error("E");
    result.current.info("I");
    result.current.warning("W");

    expect(toastMock.error).toHaveBeenCalledWith("E", {});
    expect(toastMock.info).toHaveBeenCalledWith("I", {});
    expect(toastMock.warning).toHaveBeenCalledWith("W", {});
  });
});
