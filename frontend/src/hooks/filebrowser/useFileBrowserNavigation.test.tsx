import { useLocation } from "@tanstack/react-router";
import { describe, expect, it, vi } from "vitest";

import { useFileBrowserNavigation } from "@/hooks/filebrowser/useFileBrowserNavigation";
import {
  act,
  createTanStackRouterWrapper,
  renderHook,
  waitFor,
} from "@/test/render";

function routerWrapper(initialPath: string) {
  return createTanStackRouterWrapper({ initialEntries: [initialPath] }).Wrapper;
}

describe("useFileBrowserNavigation", () => {
  it("normalizes the root filebrowser URL to '/'", async () => {
    const { result } = renderHook(
      () => useFileBrowserNavigation({ onPathChange: vi.fn() }),
      { wrapper: routerWrapper("/filebrowser") },
    );

    await waitFor(() => expect(result.current?.normalizedPath).toBe("/"));
  });

  it("decodes URL-encoded segments into a normalized path", async () => {
    const { result } = renderHook(
      () => useFileBrowserNavigation({ onPathChange: vi.fn() }),
      { wrapper: routerWrapper("/filebrowser/srv/my%20files/sub%20dir") },
    );

    await waitFor(() =>
      expect(result.current?.normalizedPath).toBe("/srv/my files/sub dir"),
    );
  });

  it("does not call onPathChange on the initial render", async () => {
    const onPathChange = vi.fn();

    const { result } = renderHook(
      () => useFileBrowserNavigation({ onPathChange }),
      {
        wrapper: routerWrapper("/filebrowser/srv"),
      },
    );

    await waitFor(() => expect(result.current).not.toBeNull());
    expect(onPathChange).not.toHaveBeenCalled();
  });

  it("navigates to the root route and reports the path change", async () => {
    const onPathChange = vi.fn();
    const { result } = renderHook(
      () => useFileBrowserNavigation({ onPathChange }),
      { wrapper: routerWrapper("/filebrowser/srv/projects") },
    );

    await waitFor(() =>
      expect(result.current?.normalizedPath).toBe("/srv/projects"),
    );

    act(() => result.current?.handleOpenDirectory("/"));

    expect(result.current.normalizedPath).toBe("/");
    await waitFor(() => {
      expect(result.current?.normalizedPath).toBe("/");
      expect(onPathChange).toHaveBeenCalledTimes(1);
    });
  });

  it("encodes directory segments when navigating and round-trips the path", async () => {
    const onPathChange = vi.fn();
    const { result } = renderHook(
      () => ({
        navigation: useFileBrowserNavigation({ onPathChange }),
        location: useLocation(),
      }),
      { wrapper: routerWrapper("/filebrowser") },
    );

    await waitFor(() => expect(result.current).not.toBeNull());
    act(() =>
      result.current.navigation.handleOpenDirectory(
        "/srv/my files/hash#question?",
      ),
    );

    expect(result.current.navigation.normalizedPath).toBe(
      "/srv/my files/hash#question?",
    );
    expect(onPathChange).toHaveBeenCalledTimes(1);
  });

  it("reports a single path change per navigation", async () => {
    const onPathChange = vi.fn();
    const { result } = renderHook(
      () => useFileBrowserNavigation({ onPathChange }),
      { wrapper: routerWrapper("/filebrowser") },
    );

    await waitFor(() => expect(result.current?.normalizedPath).toBe("/"));

    act(() => result.current?.handleOpenDirectory("/srv"));
    act(() => result.current?.handleOpenDirectory("/srv/projects"));

    await waitFor(() => {
      expect(result.current?.normalizedPath).toBe("/srv/projects");
      expect(onPathChange).toHaveBeenCalledTimes(2);
    });
  });
  it.each([
    "/srv/percent%value",
    "/srv/hash#value",
    "/srv/question?value",
    "/srv/with spaces",
    "/srv/olá-東京",
    "/srv/[brackets]",
    "/srv/pipe|value",
    "/srv/caret^value",
  ])("round-trips special filebrowser segments: %s", async (path) => {
    const { router, Wrapper } = createTanStackRouterWrapper({
      initialEntries: ["/filebrowser"],
    });
    const { result } = renderHook(
      () => useFileBrowserNavigation({ onPathChange: vi.fn() }),
      { wrapper: Wrapper },
    );

    await waitFor(() => expect(result.current?.normalizedPath).toBe("/"));
    act(() => result.current?.handleOpenDirectory(path));

    await waitFor(() => expect(result.current?.normalizedPath).toBe(path));
    expect(router.state.location.hash).toBe("");
    expect(router.state.location.searchStr).toBe("");
  });
});
