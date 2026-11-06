import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import { linuxio, type VirtualMachine } from "@/api";
import { topologyLive } from "@/test/dockerTopologyFixture";
import { createTestQueryClient, render, screen } from "@/test/render";

import MemoryUsage from "./Memory";

vi.mock("@/routes/_authenticated/-dashboard/DashboardCard", () => ({
  default: ({ stats }: { stats: ReactNode }) => <section>{stats}</section>,
}));

const vm = (name: string, metrics?: VirtualMachine["metrics"]) => ({
  name,
  state: metrics ? "running" : "shut off",
  vcpus: 2,
  memoryMB: 4096,
  diskGB: 20,
  autostart: false,
  hasGraphics: false,
  ownedDisks: [],
  metrics,
});

function renderCard(libvirtAvailable: boolean, privileged: boolean) {
  const queryClient = createTestQueryClient();
  // Seeded data must not trigger a network refetch on mount.
  queryClient.setDefaultOptions({ queries: { staleTime: Infinity } });
  queryClient.setQueryData(
    linuxio.monitoring.get_live.queryKey,
    topologyLive(),
  );
  queryClient.setQueryData(linuxio.virt.list.queryKey, [
    vm("alpha", { memoryUsedMB: 1024 }),
    vm("beta", { memoryUsedMB: 512 }),
    vm("gamma"),
  ]);
  return render(<MemoryUsage />, {
    auth: { privileged },
    capabilities: { libvirtAvailable },
    queryClient,
  });
}

describe("MemoryUsage", () => {
  it("sums the host memory held by running VMs", () => {
    renderCard(true, true);

    expect(screen.getByText("VMs")).toBeInTheDocument();
    expect(screen.getByText("1.5 GB")).toBeInTheDocument();
  });

  it("hides the VMs row when libvirt is unavailable or the session is not sudo", () => {
    renderCard(false, true);
    expect(screen.queryByText("VMs")).not.toBeInTheDocument();

    renderCard(true, false);
    expect(screen.queryByText("VMs")).not.toBeInTheDocument();
  });
});
