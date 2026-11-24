import { QueryClient, QueryObserver } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";

import { linuxio } from "@/api";

import { markListingStale } from "./useListingInvalidation";

const key = linuxio.filebrowser.list_directory({ path: "/srv" }).queryKey;

describe("markListingStale", () => {
  const cleanups: (() => void)[] = [];
  afterEach(() => {
    for (const cleanup of cleanups.splice(0)) cleanup();
  });

  /**
   * A shown listing that has data. Its reads after the first wait until they
   * are released, so one can be running when a frame comes; TanStack never
   * cancels a first read, so only a refetch shows a read being restarted.
   */
  function shownListing() {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: 60_000 } },
    });
    queryClient.setQueryData(key, { path: "/srv" });
    const signals: AbortSignal[] = [];
    const releases: (() => void)[] = [];
    const queryFn = vi.fn(({ signal }: { signal: AbortSignal }) => {
      signals.push(signal);
      return new Promise((resolve) => {
        releases.push(() => resolve({ path: "/srv" }));
      });
    });
    const observer = new QueryObserver(queryClient, { queryFn, queryKey: key });
    cleanups.push(observer.subscribe(() => {}));
    const release = () => {
      for (const done of releases.splice(0)) done();
    };
    return { queryClient, queryFn, release, signals };
  }

  const settle = () => new Promise((resolve) => setTimeout(resolve, 20));

  it("does not restart a listing that is being read, and reads it once more after", async () => {
    const { queryClient, queryFn, release, signals } = shownListing();
    expect(queryFn).not.toHaveBeenCalled();
    void queryClient.refetchQueries({ queryKey: key });
    expect(queryFn).toHaveBeenCalledTimes(1);

    markListingStale(queryClient, "/srv", true);
    markListingStale(queryClient, "/srv", true);
    expect(queryFn).toHaveBeenCalledTimes(1);
    expect(signals[0].aborted).toBe(false);

    release();
    await vi.waitFor(() => expect(queryFn).toHaveBeenCalledTimes(2));
    release();
    await settle();
    expect(queryFn).toHaveBeenCalledTimes(2);
  });

  it("reads a listing that is not being read once", async () => {
    const { queryClient, queryFn, release } = shownListing();
    markListingStale(queryClient, "/srv", true);
    expect(queryFn).toHaveBeenCalledTimes(1);
    release();
    await settle();
    expect(queryFn).toHaveBeenCalledTimes(1);
  });
});
