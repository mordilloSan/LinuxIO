import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { VMUnusedDisk } from "@/api";
import { render, screen, within } from "@/test/render";

import VMDisksPage from "./VMDisksPage";
import { VMDisksPruneButton, VMDisksPruneDialog } from "./VMDisksPrune";

const mocks = vi.hoisted(() => ({
  remove: vi.fn(),
  removeAsync: vi.fn(),
  list: vi.fn(),
  pools: vi.fn(),
  vms: vi.fn(),
  disk: {
    name: "linuxio-teste.qcow2",
    path: "/var/lib/libvirt/images/linuxio/cloud-images/linuxio-teste.qcow2",
    vmName: "teste",
    sizeBytes: 2 * 1024 ** 3,
    modifiedAt: "2026-08-24T22:19:19Z",
  },
}));

vi.mock("@tanstack/react-virtual", async () =>
  (await import("@/test/reactVirtualMock")).reactVirtualMock(),
);

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  const { mockMutationHooks } = await import("@/test/mutationHooks");
  const useCallMutation = () => ({
    mutate: mocks.remove,
    mutateAsync: mocks.removeAsync,
    isPending: false,
  });
  return {
    ...actual,
    useCallMutation,
    linuxio: mockMutationHooks(
      {
        ...actual.linuxio,
        virt: {
          ...actual.linuxio.virt,
          unused_disks: {
            queryKey: ["test-unused-disks"],
            queryFn: () => mocks.list(),
          },
          pools: { queryKey: ["test-pools"], queryFn: () => mocks.pools() },
          list: { queryKey: ["test-vms"], queryFn: () => mocks.vms() },
        },
        system: {
          ...actual.linuxio.system,
          get_host_info: {
            queryKey: ["test-host"],
            queryFn: () => Promise.resolve({ hostname: "TNAS" }),
          },
        },
      },
      useCallMutation,
    ),
  };
});

beforeEach(() => {
  vi.clearAllMocks();
  mocks.list.mockResolvedValue([mocks.disk]);
  mocks.removeAsync.mockResolvedValue(undefined);
  mocks.pools.mockResolvedValue([
    {
      active: true,
      allocationBytes: 300 * 1024 ** 3,
      availableBytes: 700 * 1024 ** 3,
      capacityBytes: 1000 * 1024 ** 3,
      name: "default",
      path: "/var/lib/libvirt/images",
      type: "dir",
    },
  ]);
  mocks.vms.mockResolvedValue([
    {
      disks: [
        {
          allocatedBytes: 17 * 1024 ** 3,
          device: "disk",
          path: "/var/lib/libvirt/images/linuxio/cloud-images/linuxio-ha.qcow2",
          sizeGB: 32,
        },
        { device: "cdrom", path: "/var/lib/libvirt/images/installer.iso" },
        { device: "disk", path: "/srv/elsewhere.qcow2", sizeGB: 8 },
      ],
      name: "ha",
    },
  ]);
});

describe("VM storage pools", () => {
  it("shows each pool's usage and the VM disks it holds", async () => {
    render(<VMDisksPage />);
    const card = await screen.findByRole("region", { name: "default" });
    expect(within(card).getByText("Normal")).toBeInTheDocument();
    expect(
      within(card).getByText("Used / Total: 300 GB / 1000 GB"),
    ).toBeInTheDocument();
    expect(
      within(card).getByRole("progressbar", { name: "default usage" }),
    ).toHaveAttribute("aria-valuenow", "30");
    expect(within(card).getByText("TNAS")).toBeInTheDocument();
    expect(within(card).getByText("700 GB")).toBeInTheDocument();
    // Only the VM disk under the pool path counts, not the ISO or outside disks.
    expect(within(card).getByText("32 GB")).toBeInTheDocument();
    const table = within(card).getByRole("table", {
      name: "default virtual machine usage",
    });
    expect(within(table).getByText("ha")).toBeInTheDocument();
    expect(within(table).getByText("linuxio-ha.qcow2")).toBeInTheDocument();
    expect(
      within(table).getByText("17 GB / 32 GB (53.1%)"),
    ).toBeInTheDocument();
    expect(within(table).queryByText("installer.iso")).not.toBeInTheDocument();
  });
});

describe("VM unused disks", () => {
  it("lists leftover disks with the space they free", async () => {
    render(<VMDisksPage />);
    const table = await screen.findByRole("table", {
      name: "Unused VM disks",
    });
    expect(within(table).getByText("linuxio-teste.qcow2")).toBeInTheDocument();
    expect(within(table).getByText("teste")).toBeInTheDocument();
    expect(screen.getByText(/frees 2 GB/)).toBeInTheDocument();
  });

  it("deletes the chosen disk only after confirmation", async () => {
    const user = userEvent.setup();
    render(<VMDisksPage />);
    await user.click(
      await screen.findByRole("button", {
        name: "Delete linuxio-teste.qcow2",
      }),
    );
    expect(mocks.remove).not.toHaveBeenCalled();
    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByText(mocks.disk.path)).toBeInTheDocument();
    await user.click(
      within(dialog).getByRole("button", { name: "Delete disk" }),
    );
    expect(mocks.remove).toHaveBeenCalledWith({ path: mocks.disk.path });
  });

  it("says when there is nothing to clean up", async () => {
    mocks.list.mockResolvedValue([]);
    render(<VMDisksPage />);
    expect(await screen.findByText("No unused disks.")).toBeInTheDocument();
  });
});

// VMPage renders the broom in the header and its dialog beside the page.
function Prune() {
  const [prune, setPrune] = useState<{ disks: VMUnusedDisk[]; open: boolean }>({
    disks: [],
    open: false,
  });
  return (
    <>
      <VMDisksPruneButton
        onClick={(disks) => setPrune({ disks, open: true })}
      />
      <VMDisksPruneDialog
        disks={prune.disks}
        onClose={() => setPrune((current) => ({ ...current, open: false }))}
        open={prune.open}
      />
    </>
  );
}

describe("VM unused disks prune", () => {
  const seed = {
    name: "linuxio-old.qcow2",
    path: "/var/lib/libvirt/images/linuxio-old.qcow2",
    vmName: "old",
    sizeBytes: 1024 ** 3,
    modifiedAt: "2026-08-20T10:00:00Z",
  };

  it("deletes every listed disk after one confirmation", async () => {
    mocks.list.mockResolvedValue([mocks.disk, seed]);
    const user = userEvent.setup();
    render(<Prune />);
    const broom = await screen.findByRole("button", {
      name: "Prune unused disks",
    });
    await vi.waitFor(() => expect(broom).toBeEnabled());
    await user.click(broom);
    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByText(mocks.disk.name)).toBeInTheDocument();
    expect(within(dialog).getByText(seed.name)).toBeInTheDocument();
    expect(within(dialog).getByText(/This frees 3 GB/)).toBeInTheDocument();
    expect(mocks.removeAsync).not.toHaveBeenCalled();
    await user.click(within(dialog).getByRole("button", { name: "Delete" }));
    await vi.waitFor(() => expect(mocks.removeAsync).toHaveBeenCalledTimes(2));
    expect(mocks.removeAsync).toHaveBeenCalledWith({ path: mocks.disk.path });
    expect(mocks.removeAsync).toHaveBeenCalledWith({ path: seed.path });
  });

  it("keeps going past a disk that fails", async () => {
    mocks.list.mockResolvedValue([mocks.disk, seed]);
    mocks.removeAsync.mockRejectedValueOnce(new Error("disk is used by a VM"));
    const user = userEvent.setup();
    render(<Prune />);
    const broom = await screen.findByRole("button", {
      name: "Prune unused disks",
    });
    await vi.waitFor(() => expect(broom).toBeEnabled());
    await user.click(broom);
    await user.click(
      within(screen.getByRole("dialog")).getByRole("button", {
        name: "Delete",
      }),
    );
    await vi.waitFor(() =>
      expect(mocks.removeAsync).toHaveBeenLastCalledWith({ path: seed.path }),
    );
  });

  it("is disabled when there is nothing to prune", async () => {
    mocks.list.mockResolvedValue([]);
    render(<Prune />);
    await vi.waitFor(() => expect(mocks.list).toHaveBeenCalled());
    expect(
      screen.getByRole("button", { name: "Prune unused disks" }),
    ).toBeDisabled();
  });
});
