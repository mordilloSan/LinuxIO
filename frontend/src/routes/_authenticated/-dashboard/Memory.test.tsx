import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import { linuxio } from "@/api";
import { topologyLive } from "@/test/dockerTopologyFixture";
import { createTestQueryClient, render, screen } from "@/test/render";

import MemoryUsage from "./Memory";

vi.mock("@/routes/_authenticated/-dashboard/DashboardCard", () => ({
  default: ({ stats }: { stats: ReactNode }) => <section>{stats}</section>,
}));

describe("MemoryUsage", () => {
  it("shows the memory Docker and VMs hold from the live sample", () => {
    const queryClient = createTestQueryClient();
    // Seeded data must not trigger a network refetch on mount.
    queryClient.setDefaultOptions({ queries: { staleTime: Infinity } });
    const live = topologyLive();
    queryClient.setQueryData(linuxio.monitoring.get_live.queryKey, {
      ...live,
      memory: {
        ...live.memory,
        docker_used_bytes: 512 * 1024 * 1024,
        vm_used_bytes: 1.5 * 1024 * 1024 * 1024,
      },
    });

    render(<MemoryUsage />, { queryClient });

    expect(screen.getByText("512 MB")).toBeInTheDocument();
    expect(screen.getByText("VMs")).toBeInTheDocument();
    expect(screen.getByText("1.5 GB")).toBeInTheDocument();
  });
});
