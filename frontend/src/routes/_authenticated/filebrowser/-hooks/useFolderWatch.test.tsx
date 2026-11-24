import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { linuxio, openChannel, type Stream } from "@/api";

import { useFolderWatch } from "./useFolderWatch";

const mux = vi.hoisted(() => ({ isOpen: true }));

vi.mock("@/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api")>()),
  openChannel: vi.fn(),
  useStreamMux: () => ({ isOpen: mux.isOpen }),
}));

const fakeStream = (id: number): Stream => ({
  abort: vi.fn(),
  close: vi.fn(),
  id,
  onClose: null,
  onData: null,
  onProgress: null,
  onResult: null,
  resize: vi.fn(),
  status: "open",
  type: "request",
  write: vi.fn(),
});

const listingKey = (path: string) =>
  linuxio.filebrowser.list_directory({ path }).queryKey;

/** A watch frame as the bridge sends it: one JSON line in a data frame. */
const frame = (stream: Stream, type: string) =>
  stream.onData?.(new TextEncoder().encode(`${JSON.stringify({ type })}\n`));

describe("useFolderWatch", () => {
  let queryClient: QueryClient;
  let streams: Stream[];
  let visibility: DocumentVisibilityState;

  beforeEach(() => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    mux.isOpen = true;
    visibility = "visible";
    vi.spyOn(document, "visibilityState", "get").mockImplementation(
      () => visibility,
    );
    streams = [];
    vi.mocked(openChannel).mockReset();
    vi.mocked(openChannel).mockImplementation(() => {
      const stream = fakeStream(streams.length + 1);
      streams.push(stream);
      return stream;
    });
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    for (const path of ["/srv", "/srv/other", "/home"]) {
      queryClient.setQueryData(listingKey(path), { path });
    }
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
  const watch = (path: string | null) =>
    renderHook(({ path: shown }) => useFolderWatch(shown), {
      initialProps: { path },
      wrapper,
    });
  const stale = (path: string) =>
    queryClient.getQueryState(listingKey(path))?.isInvalidated ?? false;
  const advance = (ms: number) =>
    act(() => {
      vi.advanceTimersByTime(ms);
    });

  it("marks only the shown folder's listing stale on a change", () => {
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    watch("/srv");
    expect(openChannel).toHaveBeenCalledWith("filebrowser.watch", {
      path: "/srv",
    });
    expect(stale("/srv")).toBe(false);

    act(() => {
      frame(streams[0], "changed");
    });

    expect(stale("/srv")).toBe(true);
    expect(stale("/srv/other")).toBe(false);
    expect(stale("/home")).toBe(false);
    expect(invalidate).toHaveBeenCalledTimes(1);
    expect(invalidate).toHaveBeenCalledWith(
      expect.objectContaining({ refetchType: "active" }),
      { cancelRefetch: false },
    );
  });

  it("reads watch frames as data, never as progress, which counts as the user's activity", () => {
    watch("/srv");
    expect(streams[0].onData).not.toBeNull();
    expect(streams[0].onProgress).toBeNull();
  });

  it("marks the listing stale without a fetch while the tab is hidden, and fetches when it shows", () => {
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    watch("/srv");
    visibility = "hidden";

    act(() => {
      frame(streams[0], "changed");
    });

    expect(stale("/srv")).toBe(true);
    expect(invalidate).toHaveBeenLastCalledWith(
      expect.objectContaining({ refetchType: "none" }),
      { cancelRefetch: false },
    );

    visibility = "visible";
    act(() => {
      document.dispatchEvent(new Event("visibilitychange"));
    });
    expect(invalidate).toHaveBeenCalledTimes(2);
    expect(invalidate).toHaveBeenLastCalledWith(
      expect.objectContaining({ refetchType: "active" }),
      { cancelRefetch: false },
    );
  });

  /** The bridge refuses the stream's watch with code, as it does. */
  const refuse = (stream: Stream, code: number) =>
    act(() => {
      stream.onResult?.({ code, error: "refused", status: "error" });
      stream.onClose?.();
    });
  const gone = (stream: Stream) =>
    act(() => {
      frame(stream, "gone");
      stream.onResult?.({ status: "ok" });
      stream.onClose?.();
    });

  it("reads the listing once when the folder is gone and the watch is refused after", () => {
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    watch("/srv");
    act(() => {
      frame(streams[0], "watching");
    });
    expect(invalidate).not.toHaveBeenCalled();

    gone(streams[0]);
    expect(stale("/srv")).toBe(true);

    // After "gone", a 404 is asked again at each pause, 1 s, 2 s, 5 s and
    // 30 s, and the one after the last pause ends it.
    for (const [index, pause] of [1000, 2000, 5000, 30_000].entries()) {
      advance(pause);
      expect(openChannel).toHaveBeenCalledTimes(index + 2);
      refuse(streams[index + 1], 404);
    }
    advance(120_000);
    expect(openChannel).toHaveBeenCalledTimes(5);
    expect(invalidate).toHaveBeenCalledTimes(1);
  });

  it("watches a folder made again after it was gone, and reads it then", () => {
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    watch("/srv");
    gone(streams[0]);
    advance(1000);
    refuse(streams[1], 404);
    advance(2000);
    refuse(streams[2], 404);
    advance(5000);
    expect(invalidate).toHaveBeenCalledTimes(1);

    // rm -rf dist, and dist made again: the watch is on.
    act(() => {
      frame(streams[3], "watching");
    });
    expect(invalidate).toHaveBeenCalledTimes(2);
    act(() => {
      frame(streams[3], "changed");
    });
    expect(invalidate).toHaveBeenCalledTimes(3);
  });

  it("stops at once on a 404 that no gone came before", () => {
    watch("/srv");
    refuse(streams[0], 404);
    advance(120_000);
    expect(openChannel).toHaveBeenCalledTimes(1);
  });

  it("reopens a Channel that ended after a growing pause and catches up", () => {
    watch("/srv");
    act(() => {
      streams[0].onClose?.();
    });
    expect(openChannel).toHaveBeenCalledTimes(1);

    advance(999);
    expect(openChannel).toHaveBeenCalledTimes(1);
    advance(1);
    expect(openChannel).toHaveBeenCalledTimes(2);
    // Caught up only once the bridge says the watch is on.
    expect(stale("/srv")).toBe(false);
    act(() => {
      frame(streams[1], "watching");
    });
    expect(stale("/srv")).toBe(true);

    // It ends again at once: the pauses grow, 2 s, 5 s, then 30 s.
    for (const [index, pause] of [2000, 5000, 30_000, 30_000].entries()) {
      act(() => {
        streams[index + 1].onClose?.();
      });
      advance(pause - 1);
      expect(openChannel).toHaveBeenCalledTimes(index + 2);
      advance(1);
      expect(openChannel).toHaveBeenCalledTimes(index + 3);
    }
  });

  it.each([400, 403, 404, 409])(
    "does not retry a watch the bridge refused with %i",
    (code) => {
      watch("/srv");
      act(() => {
        streams[0].onResult?.({ code, error: "refused", status: "error" });
        streams[0].onClose?.();
      });

      advance(120_000);
      expect(openChannel).toHaveBeenCalledTimes(1);
      expect(stale("/srv")).toBe(false);
    },
  );

  it("retries a watch that failed for a reason that can pass", () => {
    watch("/srv");
    act(() => {
      streams[0].onResult?.({
        code: 500,
        error: "create inotify instance: too many open files",
        status: "error",
      });
      streams[0].onClose?.();
    });

    advance(1000);
    expect(openChannel).toHaveBeenCalledTimes(2);
  });

  it("closes the old Channel and opens one for the new folder", () => {
    const { rerender, unmount } = watch("/srv");
    rerender({ path: "/home" });

    expect(streams[0].close).toHaveBeenCalledTimes(1);
    expect(openChannel).toHaveBeenLastCalledWith("filebrowser.watch", {
      path: "/home",
    });
    // A folder newly shown was just listed: nothing to catch up on.
    expect(stale("/home")).toBe(false);

    unmount();
    expect(streams[1].close).toHaveBeenCalledTimes(1);
    advance(120_000);
    expect(openChannel).toHaveBeenCalledTimes(2);
  });

  it("watches nothing without a folder", () => {
    watch(null);
    advance(120_000);
    expect(openChannel).not.toHaveBeenCalled();
  });

  it("reopens when the websocket comes back and catches up", () => {
    const { rerender } = watch("/srv");
    mux.isOpen = false;
    act(() => {
      streams[0].onClose?.();
    });
    rerender({ path: "/srv" });

    advance(120_000);
    expect(openChannel).toHaveBeenCalledTimes(1);

    mux.isOpen = true;
    rerender({ path: "/srv" });
    expect(openChannel).toHaveBeenCalledTimes(2);
    expect(stale("/srv")).toBe(false);
    act(() => {
      frame(streams[1], "watching");
    });
    expect(stale("/srv")).toBe(true);
  });

  it("does not read the listing when a first watch goes on", () => {
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    watch("/srv");
    act(() => {
      frame(streams[0], "watching");
    });
    expect(invalidate).not.toHaveBeenCalled();
  });
});
