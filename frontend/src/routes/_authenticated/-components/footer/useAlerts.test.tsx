import { QueryClientProvider } from "@tanstack/react-query";
import { renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import { createTestQueryClient } from "@/test/render";

const mocks = vi.hoisted(() => ({
  fetch: vi.fn(),
  mutate: vi.fn(),
  privileged: false,
}));

vi.mock("@/hooks/useAuth", () => ({
  default: () => ({ privileged: mocks.privileged }),
}));
vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  const { mockMutationHooks } = await import("@/test/mutationHooks");
  const useCallMutation = (endpoint: { route: string }, config: unknown) => ({
    mutate: (request: unknown) => mocks.mutate(endpoint.route, request, config),
  });
  return {
    ...actual,
    linuxio: mockMutationHooks(
      {
        alerts: {
          list: {
            queryKey: ["linuxio", "alerts.list"],
            queryFn: mocks.fetch,
          },
          mark_all_seen: { route: "alerts.mark_all_seen" },
          dismiss: { route: "alerts.dismiss" },
        },
      },
      useCallMutation,
    ),
    useCallMutation,
  };
});

import { useAlerts, useMarkAlertsSeen } from "./useAlerts";

const wrapper = ({ children }: { children: ReactNode }) => (
  <QueryClientProvider client={createTestQueryClient()}>
    {children}
  </QueryClientProvider>
);

type MutationConfig = { success: (result: unknown) => void };

describe("useAlerts", () => {
  it("does not query alerts for unprivileged sessions", () => {
    mocks.privileged = false;
    const { result } = renderHook(() => useAlerts(), { wrapper });
    expect(mocks.fetch).not.toHaveBeenCalled();
    expect(result.current.enabled).toBe(false);
    expect(result.current.alerts).toEqual([]);
  });

  it("queries alerts for privileged sessions", async () => {
    mocks.privileged = true;
    mocks.fetch.mockResolvedValue({ alerts: [], unseen: 3 });
    const { result } = renderHook(() => useAlerts(), { wrapper });
    await vi.waitFor(() => expect(result.current.unseen).toBe(3));
  });

  it("marks every alert seen and keeps the answer the bridge gives", () => {
    mocks.mutate.mockClear();
    const queryClient = createTestQueryClient();
    const { result } = renderHook(() => useMarkAlertsSeen(), {
      wrapper: ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={queryClient}>
          {children}
        </QueryClientProvider>
      ),
    });

    result.current();

    expect(mocks.mutate).toHaveBeenCalledTimes(1);
    const [route, request, config] = mocks.mutate.mock.calls[0];
    expect([route, request]).toEqual(["alerts.mark_all_seen", undefined]);
    const answer = { alerts: [], unseen: 0 };
    (config as MutationConfig).success(answer);
    expect(queryClient.getQueryData(["linuxio", "alerts.list"])).toBe(answer);
  });

  it("marks them seen the same way from the bell's hook", () => {
    mocks.mutate.mockClear();
    mocks.privileged = true;
    mocks.fetch.mockResolvedValue({ alerts: [], unseen: 0 });
    const { result } = renderHook(() => useAlerts(), { wrapper });

    result.current.markAllSeen();

    expect(
      mocks.mutate.mock.calls.map(([route, request]) => [route, request]),
    ).toEqual([["alerts.mark_all_seen", undefined]]);
  });
});
