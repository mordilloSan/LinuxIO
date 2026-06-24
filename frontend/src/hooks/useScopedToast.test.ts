import { describe, expect, it, vi } from "vitest";

const toastMock = vi.hoisted(() => ({
  error: vi.fn(),
  info: vi.fn(),
  success: vi.fn(),
  warning: vi.fn(),
}));

vi.mock("sonner", () => ({
  toast: toastMock,
}));

const { useScopedToast } = await import("@/hooks/useScopedToast");
const { renderHook } = await import("@/test/render");

describe("useScopedToast", () => {
  it("adds scope metadata to toast calls", () => {
    const { result } = renderHook(() =>
      useScopedToast({ href: "/updates", label: "Updates" }),
    );

    result.current.success("Updated", {
      meta: { label: "Custom" },
    });

    expect(toastMock.success).toHaveBeenCalledWith("Updated", {
      meta: { href: "/updates", label: "Custom" },
    });
  });

  it("exposes all toast severities", () => {
    const { result } = renderHook(() => useScopedToast({ label: "System" }));

    result.current.error("E");
    result.current.info("I");
    result.current.warning("W");

    expect(toastMock.error).toHaveBeenCalledWith("E", {
      meta: { label: "System" },
    });
    expect(toastMock.info).toHaveBeenCalledWith("I", {
      meta: { label: "System" },
    });
    expect(toastMock.warning).toHaveBeenCalledWith("W", {
      meta: { label: "System" },
    });
  });
});
