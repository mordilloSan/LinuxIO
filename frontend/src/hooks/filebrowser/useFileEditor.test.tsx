import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { createContext, useContext, type ReactNode } from "react";
import { describe, expect, it } from "vitest";

import { useFileEditor } from "@/hooks/filebrowser/useFileEditor";
import { act, renderHook, waitFor } from "@/test/render";

const HookChildren = createContext<ReactNode>(null);

// The hook resolves `to: "."` against the matched route, so it runs inside a
// real filebrowser splat route rather than the shared root-only test router.
function setup(initialPath = "/filebrowser/srv") {
  const rootRoute = createRootRoute();
  const fileBrowserRoute = createRoute({
    component: function FileBrowserRoute() {
      return useContext(HookChildren);
    },
    getParentRoute: () => rootRoute,
    path: "filebrowser/$",
  });
  const router = createRouter({
    history: createMemoryHistory({ initialEntries: [initialPath] }),
    routeTree: rootRoute.addChildren([fileBrowserRoute]),
  });
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <HookChildren.Provider value={children}>
        <RouterProvider router={router} />
      </HookChildren.Provider>
    );
  }
  const { result } = renderHook(() => useFileEditor(), { wrapper: Wrapper });
  const current = () => {
    if (!result.current) throw new Error("editor not mounted");
    return result.current;
  };
  return { current, router };
}

describe("useFileEditor", () => {
  it("opens the file as a history entry that Back leaves", async () => {
    const { current, router } = setup();
    await waitFor(() => expect(current().showQuickSave).toBe(false));

    act(() => current().actions.openFile("/srv/note.md"));
    await waitFor(() => expect(current().editingPath).toBe("/srv/note.md"));
    expect(router.state.location.pathname).toBe("/filebrowser/srv");
    expect(router.state.location.search).toEqual({ edit: "/srv/note.md" });
    expect(current().showQuickSave).toBe(true);

    act(() => router.history.back());
    await waitFor(() => expect(current().editingPath).toBeNull());
    expect(router.state.location.state.__TSR_index).toBe(0);
  });

  it("closes by stepping back to the listing", async () => {
    const { current, router } = setup();
    await waitFor(() => expect(current().editingPath).toBeNull());

    act(() => current().actions.openFile("/srv/note.md"));
    await waitFor(() => expect(current().editingPath).toBe("/srv/note.md"));
    act(() => current().actions.close());

    await waitFor(() => expect(current().editingPath).toBeNull());
    expect(router.state.location.state.__TSR_index).toBe(0);
  });

  it("reopens from a deep link and closes it in place", async () => {
    const { current, router } = setup(
      `/filebrowser/srv?edit=${encodeURIComponent("/srv/note.md")}`,
    );
    await waitFor(() => expect(current().editingPath).toBe("/srv/note.md"));

    act(() => current().actions.close());

    await waitFor(() => expect(current().editingPath).toBeNull());
    expect(router.state.location.pathname).toBe("/filebrowser/srv");
    expect(router.state.location.state.__TSR_index).toBe(0);
  });

  // Memory history only runs blockers on push/replace, so this exercises the
  // deep-link close; the Back path is covered by the browser spec.
  it("holds unsaved changes on the close prompt until confirmed", async () => {
    const { current, router } = setup(
      `/filebrowser/srv?edit=${encodeURIComponent("/srv/note.md")}`,
    );
    await waitFor(() => expect(current().editingPath).toBe("/srv/note.md"));
    act(() => current().actions.setDirty(true));

    act(() => current().actions.close());
    await waitFor(() => expect(current().closeEditorDialog).toBe(true));
    expect(current().editingPath).toBe("/srv/note.md");

    act(() => current().actions.dismissClosePrompt());
    await waitFor(() => expect(current().closeEditorDialog).toBe(false));
    expect(current().editingPath).toBe("/srv/note.md");
    expect(current().isEditorDirty).toBe(true);

    // Pending until the prompt is answered, so it is not awaited here.
    act(() => {
      void router.navigate({
        params: { _splat: "tmp" },
        to: "/filebrowser/$",
      } as never);
    });
    await waitFor(() => expect(current().closeEditorDialog).toBe(true));
    expect(router.state.location.pathname).toBe("/filebrowser/srv");

    act(() => current().actions.confirmClose());
    await waitFor(() => expect(current().editingPath).toBeNull());
    expect(router.state.location.pathname).toBe("/filebrowser/tmp");
    expect(current().isEditorDirty).toBe(false);
    expect(current().closeEditorDialog).toBe(false);
  });
});
