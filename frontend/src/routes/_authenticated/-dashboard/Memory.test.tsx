import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import { linuxio } from "@/api";
import { topologyLive } from "@/test/dockerTopologyFixture";
import { swapStatus } from "@/test/memoryStatusFixture";
import {
  createTestQueryClient,
  render,
  renderWithTanStackRouter,
  screen,
} from "@/test/render";

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

  const seedSwap = () => {
    const queryClient = createTestQueryClient();
    queryClient.setDefaultOptions({ queries: { staleTime: Infinity } });
    const live = topologyLive();
    queryClient.setQueryData(linuxio.monitoring.get_live.queryKey, {
      ...live,
      memory: {
        ...live.memory,
        swap_free_bytes: 1.5 * 1024 * 1024 * 1024,
        swap_total_bytes: 2 * 1024 * 1024 * 1024,
      },
    });
    return queryClient;
  };

  it("opens the memory overflow dialog from the swap row for a session with sudo", async () => {
    const queryClient = seedSwap();
    queryClient.setQueryData(
      linuxio.memory.get_swap_status.queryKey,
      swapStatus(),
    );
    const { user } = renderWithTanStackRouter(<MemoryUsage />, {
      auth: { privileged: true },
      queryClient,
    });

    const edit = await screen.findByRole("button", { name: "Edit Swap" });
    expect(edit).toHaveTextContent("512 MB / 2 GB");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();

    await user.click(edit);

    // The dialog fades in, so it is in the page before it is visible.
    expect(
      await screen.findByRole("dialog", { name: "Memory overflow" }),
    ).toBeInTheDocument();
    expect(
      await screen.findByRole("radiogroup", { name: "When memory runs out" }),
    ).toBeInTheDocument();
  });

  it("shows the swap row as plain text without sudo", () => {
    render(<MemoryUsage />, {
      auth: { privileged: false },
      queryClient: seedSwap(),
    });

    expect(screen.getByText("512 MB / 2 GB")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Edit Swap" }),
    ).not.toBeInTheDocument();
  });
});
