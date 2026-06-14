import type { ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import { useFileBrowserNavigation } from "@/hooks/filebrowser/useFileBrowserNavigation";
import { act, renderHook } from "@/test/render";

function routerWrapper(initialPath: string) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <MemoryRouter initialEntries={[initialPath]}>{children}</MemoryRouter>
    );
  };
}

describe("useFileBrowserNavigation", () => {
  it("normalizes the root filebrowser URL to '/'", () => {
    const { result } = renderHook(
      () => useFileBrowserNavigation({ onPathChange: vi.fn() }),
      { wrapper: routerWrapper("/filebrowser") },
    );

    expect(result.current.normalizedPath).toBe("/");
  });

  it("decodes URL-encoded segments into a normalized path", () => {
    const { result } = renderHook(
      () => useFileBrowserNavigation({ onPathChange: vi.fn() }),
      { wrapper: routerWrapper("/filebrowser/srv/my%20files/sub%20dir") },
    );

    expect(result.current.normalizedPath).toBe("/srv/my files/sub dir");
  });

  it("does not call onPathChange on the initial render", () => {
    const onPathChange = vi.fn();

    renderHook(() => useFileBrowserNavigation({ onPathChange }), {
      wrapper: routerWrapper("/filebrowser/srv"),
    });

    expect(onPathChange).not.toHaveBeenCalled();
  });

  it("navigates to the root route and reports the path change", () => {
    const onPathChange = vi.fn();
    const { result } = renderHook(
      () => useFileBrowserNavigation({ onPathChange }),
      { wrapper: routerWrapper("/filebrowser/srv/projects") },
    );

    expect(result.current.normalizedPath).toBe("/srv/projects");

    act(() => result.current.handleOpenDirectory("/"));

    expect(result.current.normalizedPath).toBe("/");
    expect(onPathChange).toHaveBeenCalledTimes(1);
  });

  it("encodes directory segments when navigating and round-trips the path", () => {
    const onPathChange = vi.fn();
    const { result } = renderHook(
      () => useFileBrowserNavigation({ onPathChange }),
      { wrapper: routerWrapper("/filebrowser") },
    );

    act(() => result.current.handleOpenDirectory("/srv/my files/sub dir"));

    expect(result.current.normalizedPath).toBe("/srv/my files/sub dir");
    expect(onPathChange).toHaveBeenCalledTimes(1);
  });

  it("reports a single path change per navigation", () => {
    const onPathChange = vi.fn();
    const { result } = renderHook(
      () => useFileBrowserNavigation({ onPathChange }),
      { wrapper: routerWrapper("/filebrowser") },
    );

    act(() => result.current.handleOpenDirectory("/srv"));
    act(() => result.current.handleOpenDirectory("/srv/projects"));

    expect(result.current.normalizedPath).toBe("/srv/projects");
    expect(onPathChange).toHaveBeenCalledTimes(2);
  });
});
