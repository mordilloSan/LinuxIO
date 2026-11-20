import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import { linuxio } from "@/api";
import { topologyLive } from "@/test/dockerTopologyFixture";
import { swapStatus } from "@/test/memoryStatusFixture";
import {
  createTestQueryClient,
  renderWithTanStackRouter,
  screen,
  waitFor,
} from "@/test/render";

import MemoryUsage from "./Memory";

vi.mock("@/routes/_authenticated/-dashboard/DashboardCard", () => ({
  default: ({ stats }: { stats: ReactNode }) => <section>{stats}</section>,
}));

describe("MemoryUsage", () => {
  it("shows the memory Docker and VMs hold from the live sample", async () => {
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

    // The card reads the dashboard's search, so it renders in a router as
    // it does on the dashboard.
    renderWithTanStackRouter(<MemoryUsage />, { queryClient });

    expect(await screen.findByText("512 MB")).toBeInTheDocument();
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

  it("keeps the open dialog in the URL and takes it out on close", async () => {
    const queryClient = seedSwap();
    queryClient.setQueryData(
      linuxio.memory.get_swap_status.queryKey,
      swapStatus(),
    );
    const { router, user } = renderWithTanStackRouter(<MemoryUsage />, {
      auth: { privileged: true },
      queryClient,
    });

    await user.click(await screen.findByRole("button", { name: "Edit Swap" }));

    await screen.findByRole("dialog", { name: "Memory overflow" });
    await waitFor(() =>
      expect(router.state.location.search).toEqual({
        dialog: "memory-overflow",
      }),
    );

    await user.click(await screen.findByRole("button", { name: "Close" }));

    await waitFor(() => expect(router.state.location.search).toEqual({}));
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
  });

  it("opens the dialog from the URL, as after a reload or from a bookmark", async () => {
    const queryClient = seedSwap();
    queryClient.setQueryData(
      linuxio.memory.get_swap_status.queryKey,
      swapStatus(),
    );
    renderWithTanStackRouter(<MemoryUsage />, {
      auth: { privileged: true },
      queryClient,
      tanstackRouter: { initialEntries: ["/?dialog=memory-overflow"] },
    });

    expect(
      await screen.findByRole("dialog", { name: "Memory overflow" }),
    ).toBeInTheDocument();
  });

  it("does not open the dialog from the URL without sudo", async () => {
    renderWithTanStackRouter(<MemoryUsage />, {
      auth: { privileged: false },
      queryClient: seedSwap(),
      tanstackRouter: { initialEntries: ["/?dialog=memory-overflow"] },
    });

    expect(await screen.findByText("512 MB / 2 GB")).toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("shows the swap row as plain text without sudo", async () => {
    renderWithTanStackRouter(<MemoryUsage />, {
      auth: { privileged: false },
      queryClient: seedSwap(),
    });

    expect(await screen.findByText("512 MB / 2 GB")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Edit Swap" }),
    ).not.toBeInTheDocument();
  });
});
