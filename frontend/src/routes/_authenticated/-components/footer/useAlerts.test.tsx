import { QueryClientProvider } from "@tanstack/react-query";
import { renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import { createTestQueryClient } from "@/test/render";

const mocks = vi.hoisted(() => ({ fetch: vi.fn(), privileged: false }));

vi.mock("@/hooks/useAuth", () => ({
  default: () => ({ privileged: mocks.privileged }),
}));
vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    linuxio: {
      alerts: {
        list: {
          queryKey: ["linuxio", "alerts.list"],
          queryFn: mocks.fetch,
        },
        mark_all_seen: { route: "alerts.mark_all_seen" },
        dismiss: { route: "alerts.dismiss" },
      },
    },
    useCallMutation: () => ({ mutate: vi.fn() }),
  };
});

import { useAlerts } from "./useAlerts";

const wrapper = ({ children }: { children: ReactNode }) => (
  <QueryClientProvider client={createTestQueryClient()}>
    {children}
  </QueryClientProvider>
);

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
});
