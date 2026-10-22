import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { BtrfsFilesystem } from "@/api";
import { render, renderWithTanStackRouter, screen } from "@/test/render";

import BtrfsPage from "./BtrfsPage";

const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  rules: vi.fn(),
  snapshots: vi.fn(),
  subvolumes: vi.fn(),
}));

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
          queryFn: () => mocks.subvolumes(),
        }),
        btrfs_snapshot_rules: {
          queryKey: ["test-btrfs-snapshot-rules"],
          queryFn: () => mocks.rules(),
        },
        btrfs_snapshots: (request: { uuid: string }) => ({
          queryKey: ["test-btrfs-snapshots", request.uuid],
          queryFn: () => mocks.snapshots(),
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
  lowMetadataSpace: false,
});

const data = filesystem("uuid-data", "data", "/mnt/data");
const backup = filesystem("uuid-backup", "", "/mnt/backup");

beforeEach(() => {
  vi.clearAllMocks();
  mocks.list.mockResolvedValue([data, backup]);
  mocks.rules.mockResolvedValue({ rules: [], timerEnabled: false });
  mocks.snapshots.mockResolvedValue([]);
  mocks.subvolumes.mockResolvedValue([]);
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
    expect(
      await screen.findByRole("heading", { name: "Snapshots" }),
    ).toBeInTheDocument();
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
    expect(
      screen.getByRole("heading", { name: "Snapshots" }),
    ).toBeInTheDocument();
    expect(screen.queryByText("Subvolumes")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Advanced" }));
    expect(await screen.findByText("Subvolumes")).toBeInTheDocument();
    expect(screen.getByText("Replication")).toBeInTheDocument();
  });

  it("leaves disks, health and settings to the pool on the Disks tab", async () => {
    const user = userEvent.setup();
    renderWithTanStackRouter(
      <BtrfsPage selectedUuid="uuid-data" onSelect={vi.fn()} />,
      { capabilities },
    );
    await screen.findByText("Automatic snapshots");
    await user.click(screen.getByRole("button", { name: "Advanced" }));
    expect(screen.queryByText("Disks")).not.toBeInTheDocument();
    expect(screen.queryByText("Add device")).not.toBeInTheDocument();
    expect(screen.queryByText("Settings")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Check for errors" }),
    ).not.toBeInTheDocument();
  });

  it("sums up snapshots and protected folders in tiles", async () => {
    mocks.list.mockResolvedValue([data]);
    mocks.snapshots.mockResolvedValue([
      { path: "a", source: "home", createdAt: "2026-10-20T00:00:00Z" },
      { path: "b", source: "home", createdAt: "2026-10-19T00:00:00Z" },
    ]);
    mocks.subvolumes.mockResolvedValue([
      { id: 1, path: "home", isSnapshot: false },
      { id: 2, path: "srv", isSnapshot: false },
    ]);
    mocks.rules.mockResolvedValue({
      rules: [{ uuid: "uuid-data", path: "home", enabled: true }],
      timerEnabled: true,
    });
    renderWithTanStackRouter(<BtrfsPage onSelect={vi.fn()} />, {
      capabilities,
    });
    expect(await screen.findByText("of 2 folders")).toBeInTheDocument();
    expect(screen.getByText(/^Last .*ago$/)).toBeInTheDocument();
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
