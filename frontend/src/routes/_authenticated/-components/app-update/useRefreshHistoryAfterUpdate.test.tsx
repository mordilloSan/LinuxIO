import {
  QueryClient,
  QueryClientProvider,
  QueryObserver,
} from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { linuxio } from "@/api";

import { useRefreshHistoryAfterUpdate } from "./useRefreshHistoryAfterUpdate";

let unsubscribe: (() => void) | undefined;

afterEach(() => unsubscribe?.());

/** The hook beside a mounted history query, like the page's own. */
async function setup(updatePending: boolean) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  const queryFn = vi.fn(async () => ({ entries: [], more: false }));
  unsubscribe = new QueryObserver(queryClient, {
    queryKey: [...linuxio.tasks.history({}).queryKey, "pages"],
    queryFn,
  }).subscribe(() => {});
  await waitFor(() => expect(queryFn).toHaveBeenCalledTimes(1));
  queryFn.mockClear();

  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
  const hook = renderHook(
    ({ pending }) => useRefreshHistoryAfterUpdate(pending),
    { wrapper, initialProps: { pending: updatePending } },
  );
  return { hook, queryFn };
}

describe("useRefreshHistoryAfterUpdate", () => {
  it("refetches a mounted history query once when the wait ends", async () => {
    const { hook, queryFn } = await setup(true);
    expect(queryFn).not.toHaveBeenCalled();

    hook.rerender({ pending: false });
    await waitFor(() => expect(queryFn).toHaveBeenCalledTimes(1));

    hook.rerender({ pending: false });
    expect(queryFn).toHaveBeenCalledTimes(1);
  });

  it("refetches again only after the flag has been true again", async () => {
    const { hook, queryFn } = await setup(true);

    hook.rerender({ pending: false });
    await waitFor(() => expect(queryFn).toHaveBeenCalledTimes(1));
    hook.rerender({ pending: true });
    expect(queryFn).toHaveBeenCalledTimes(1);
    hook.rerender({ pending: false });
    await waitFor(() => expect(queryFn).toHaveBeenCalledTimes(2));
  });

  it("refetches nothing while the flag was never true", async () => {
    const { hook, queryFn } = await setup(false);

    hook.rerender({ pending: false });
    hook.rerender({ pending: false });

    expect(queryFn).not.toHaveBeenCalled();
  });

  it("refetches nothing while the wait goes on", async () => {
    const { hook, queryFn } = await setup(true);

    hook.rerender({ pending: true });

    expect(queryFn).not.toHaveBeenCalled();
  });
});
