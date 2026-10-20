import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { BtrfsFilesystem } from "@/api";
import { render, renderWithTanStackRouter, screen } from "@/test/render";

import BtrfsPage from "./BtrfsPage";

const mocks = vi.hoisted(() => ({ list: vi.fn() }));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    linuxio: {
      ...actual.linuxio,
      storage: {
        ...actual.linuxio.storage,
        btrfs_list: {
          queryKey: ["test-btrfs-list"],
          queryFn: () => mocks.list(),
        },
        btrfs_subvolumes: (request: { uuid: string }) => ({
          queryKey: ["test-btrfs-subvolumes", request.uuid],
          queryFn: () => Promise.resolve([]),
        }),
        btrfs_snapshot_rules: {
          queryKey: ["test-btrfs-snapshot-rules"],
          queryFn: () => Promise.resolve({ rules: [], timerEnabled: false }),
        },
        btrfs_settings: (request: { uuid: string }) => ({
          queryKey: ["test-btrfs-settings", request.uuid],
          queryFn: () =>
            Promise.resolve({
              uuid: request.uuid,
              quotasEnabled: false,
              compression: {
                algorithm: "none",
                force: false,
                fstabPersisted: false,
              },
            }),
        }),
        get_btrfs_maintenance: {
          queryKey: ["test-btrfs-maintenance"],
          queryFn: () => Promise.resolve({ tasks: [] }),
        },
        btrfs_snapshots: (request: { uuid: string }) => ({
          queryKey: ["test-btrfs-snapshots", request.uuid],
          queryFn: () => Promise.resolve([]),
        }),
      },
    },
  };
});

const filesystem = (
  uuid: string,
  label: string,
  mountpoint: string,
): BtrfsFilesystem => ({
  uuid,
  label,
  mountpoints: [
    {
      mountpoint,
      device: "/dev/sda1",
      uuid,
      subvol: "/",
      subvolId: 5,
      options: ["rw"],
    },
  ],
  totalBytes: 0,
  unallocatedBytes: 0,
  usedBytes: 0,
  allocation: [],
  devices: [],
  scrub: {} as BtrfsFilesystem["scrub"],
  degraded: false,
});

const data = filesystem("uuid-data", "data", "/mnt/data");
const backup = filesystem("uuid-backup", "", "/mnt/backup");

beforeEach(() => {
  vi.clearAllMocks();
  mocks.list.mockResolvedValue([data, backup]);
});

const capabilities = { btrfsAvailable: true };

describe("BtrfsPage", () => {
  it("shows the selected filesystem and reports a new choice", async () => {
    const user = userEvent.setup();
    const onSelect = vi.fn();
    renderWithTanStackRouter(
      <BtrfsPage selectedUuid="uuid-data" onSelect={onSelect} />,
      {
        capabilities,
      },
    );
    const select = await screen.findByRole("combobox", {
      name: "Filesystem",
    });
    expect(select).toHaveTextContent("/mnt/data · data");

    await user.click(select);
    await user.click(
      screen.getByRole("option", { name: "/mnt/backup · uuid-backup" }),
    );
    expect(onSelect).toHaveBeenCalledWith("uuid-backup");
  });

  it("falls back to the first filesystem for an unknown uuid", async () => {
    renderWithTanStackRouter(
      <BtrfsPage selectedUuid="gone" onSelect={vi.fn()} />,
      {
        capabilities,
      },
    );
    expect(
      await screen.findByRole("combobox", { name: "Filesystem" }),
    ).toHaveTextContent("/mnt/data · data");
  });

  it("hides the picker when only one filesystem is mounted", async () => {
    mocks.list.mockResolvedValue([data]);
    renderWithTanStackRouter(<BtrfsPage onSelect={vi.fn()} />, {
      capabilities,
    });
    expect(await screen.findByText("Snapshots")).toBeInTheDocument();
    expect(
      screen.queryByRole("combobox", { name: "Filesystem" }),
    ).not.toBeInTheDocument();
  });

  it("keeps the advanced cards collapsed until asked", async () => {
    const user = userEvent.setup();
    renderWithTanStackRouter(
      <BtrfsPage selectedUuid="uuid-data" onSelect={vi.fn()} />,
      { capabilities },
    );
    expect(await screen.findByText("Automatic snapshots")).toBeInTheDocument();
    expect(screen.getByText("Snapshots")).toBeInTheDocument();
    expect(screen.queryByText("Settings")).not.toBeInTheDocument();
    expect(screen.queryByText("Subvolumes")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Advanced" }));
    expect(await screen.findByText("Settings")).toBeInTheDocument();
    expect(screen.getByText("Subvolumes")).toBeInTheDocument();
    expect(screen.getByText("Replication")).toBeInTheDocument();
  });

  it("leaves disk and pool management to the Disks tab", async () => {
    const user = userEvent.setup();
    renderWithTanStackRouter(
      <BtrfsPage selectedUuid="uuid-data" onSelect={vi.fn()} />,
      { capabilities },
    );
    await screen.findByText("Automatic snapshots");
    await user.click(screen.getByRole("button", { name: "Advanced" }));
    expect(screen.queryByText("Disks")).not.toBeInTheDocument();
    expect(screen.queryByText("Add device")).not.toBeInTheDocument();
  });

  it("says when no btrfs filesystem is mounted", async () => {
    mocks.list.mockResolvedValue([]);
    render(<BtrfsPage onSelect={vi.fn()} />, { capabilities });
    expect(
      await screen.findByText("No btrfs filesystems are mounted."),
    ).toBeInTheDocument();
  });

  it("explains when btrfs-progs is missing", () => {
    render(<BtrfsPage onSelect={vi.fn()} />, {
      capabilities: { btrfsAvailable: false },
    });
    expect(screen.getByText("btrfs-progs is not installed.")).toBeVisible();
    expect(mocks.list).not.toHaveBeenCalled();
  });
});
