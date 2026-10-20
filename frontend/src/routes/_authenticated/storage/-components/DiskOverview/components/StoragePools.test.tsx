import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { BtrfsFilesystem } from "@/api";
import { render } from "@/test/render";

import type { DriveInfo } from "../types";
import StoragePools from "./StoragePools";

vi.mock("@tanstack/react-virtual", async () =>
  (await import("@/test/reactVirtualMock")).reactVirtualMock(),
);

const mocks = vi.hoisted(() => ({ list: vi.fn() }));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    call: (route: string) =>
      route === "tasks.list" ? Promise.resolve([]) : Promise.reject(),
    linuxio: {
      ...actual.linuxio,
      storage: {
        ...actual.linuxio.storage,
        btrfs_list: {
          queryKey: ["test-btrfs-list"],
          queryFn: () => mocks.list(),
        },
      },
    },
  };
});

const GiB = 1024 ** 3;
const noErrors = { write: 0, read: 0, flush: 0, corruption: 0, generation: 0 };

const filesystem = (
  uuid: string,
  label: string,
  mountpoints: string[],
  disks: string[],
  profile: string,
): BtrfsFilesystem => ({
  uuid,
  label,
  mountpoints: mountpoints.map((mountpoint) => ({
    mountpoint,
    device: "/dev/x",
    uuid,
    subvol: "/",
    subvolId: 5,
    options: [],
  })),
  totalBytes: 100 * GiB,
  unallocatedBytes: 40 * GiB,
  usedBytes: 30 * GiB,
  allocation: [
    { kind: "data", profile, totalBytes: GiB, usedBytes: GiB },
    { kind: "metadata", profile: "dup", totalBytes: GiB, usedBytes: GiB },
  ],
  devices: disks.map((path, index) => ({
    id: index + 1,
    path,
    sizeBytes: 50 * GiB,
    usedBytes: 10 * GiB,
    missing: false,
    errors: noErrors,
  })),
  scrub: { status: "none", errorCount: 0, bytesScrubbed: 0, percent: 0 },
  degraded: false,
});

const raid = filesystem(
  "uuid-raid",
  "tank",
  ["/mnt/tank", "/mnt/tank/media"],
  ["/dev/nvme0n1", "/dev/nvme1n1"],
  "raid1",
);
const root = filesystem("uuid-root", "", ["/"], ["/dev/sda2"], "single");

const drive = (
  name: string,
  model: string,
  temperature?: number,
): DriveInfo => ({
  name,
  model,
  sizeBytes: 50 * GiB,
  transport: "nvme",
  smart:
    temperature === undefined
      ? undefined
      : { temperature_celsius: temperature },
});

const drives = [
  drive("nvme0n1", "Samsung 980", 38),
  drive("nvme1n1", "WD Black", 72),
  drive("sda", "Crucial MX500", 41),
  drive("sdb", "Blank disk"),
];

const privileged = { auth: { privileged: true } };
const capabilities = { btrfsAvailable: true };

const renderPools = (
  props: Partial<React.ComponentProps<typeof StoragePools>> = {},
  options: Parameters<typeof render>[1] = { ...privileged, capabilities },
) =>
  render(
    <StoragePools
      drives={drives}
      onInspectDrive={vi.fn()}
      onOpenVolume={vi.fn()}
      {...props}
    />,
    options,
  );

beforeEach(() => {
  vi.clearAllMocks();
  mocks.list.mockResolvedValue([raid, root]);
});

describe("StoragePools", () => {
  it("draws disks, pool and volumes for each btrfs filesystem", async () => {
    renderPools();

    expect(
      await screen.findByRole("button", { name: "Pool tank" }),
    ).toHaveTextContent("RAID 1");
    expect(screen.getByRole("button", { name: "Pool /" })).toHaveTextContent(
      "Single",
    );
    expect(screen.getByText("nvme0n1")).toBeInTheDocument();
    expect(screen.getByText("nvme1n1")).toBeInTheDocument();
    expect(screen.getAllByText("Allocated 60 GB")).toHaveLength(2);
    expect(
      screen.getByRole("button", { name: "Volume /mnt/tank/media" }),
    ).toHaveTextContent("Btrfs");
  });

  it("shows each disk's temperature and links to its drive details", async () => {
    const user = userEvent.setup();
    const onInspectDrive = vi.fn();
    renderPools({ onInspectDrive });

    expect(await screen.findByText("38°C")).toBeInTheDocument();
    expect(screen.getByText("72°C")).toBeInTheDocument();
    await user.click(
      screen.getByRole("button", { name: "Drive details nvme1n1" }),
    );
    expect(onInspectDrive).toHaveBeenCalledWith("nvme1n1");
  });

  it("lists drives outside every pool, with their model", async () => {
    const user = userEvent.setup();
    const onInspectDrive = vi.fn();
    renderPools({ onInspectDrive });

    expect(await screen.findByText("Other disks")).toBeInTheDocument();
    // sda backs the "/" pool, so only sdb is outside every pool.
    expect(screen.getByText("Blank disk")).toBeInTheDocument();
    expect(screen.queryByText("Crucial MX500")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Drive details sdb" }));
    expect(onInspectDrive).toHaveBeenCalledWith("sdb");
  });

  it("opens a volume", async () => {
    const user = userEvent.setup();
    const onOpenVolume = vi.fn();
    renderPools({ onOpenVolume });

    await user.click(
      await screen.findByRole("button", { name: "Volume /mnt/tank" }),
    );
    expect(onOpenVolume).toHaveBeenCalledWith("/mnt/tank");
  });

  it("shows the details of the selected pool", async () => {
    const user = userEvent.setup();
    renderPools();

    expect(await screen.findByText("Pool details · tank")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Pool /" }));
    expect(screen.getByText("Pool details · /")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Pool /" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
  });

  it("still lists the drives without btrfs or without privilege", () => {
    const { unmount } = renderPools(
      {},
      { ...privileged, capabilities: { btrfsAvailable: false } },
    );
    expect(screen.queryByRole("button", { name: /^Pool / })).toBeNull();
    expect(screen.queryByText("Other disks")).toBeNull();
    expect(
      screen.getByRole("button", { name: "Drive details nvme0n1" }),
    ).toBeInTheDocument();
    unmount();

    renderPools({}, { capabilities });
    expect(
      screen.getByRole("button", { name: "Drive details sda" }),
    ).toBeInTheDocument();
    expect(mocks.list).not.toHaveBeenCalled();
  });

  it("waits for the pools before listing drives, so none jump", async () => {
    let resolve: (value: BtrfsFilesystem[]) => void = () => {};
    mocks.list.mockReturnValue(
      new Promise<BtrfsFilesystem[]>((done) => {
        resolve = done;
      }),
    );
    renderPools();
    expect(screen.queryByText("Other disks")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Drive details nvme0n1" }),
    ).not.toBeInTheDocument();

    resolve([raid, root]);
    expect(
      await screen.findByRole("button", { name: "Pool tank" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Other disks")).toBeInTheDocument();
  });

  it("says when there are no drives at all", () => {
    renderPools({ drives: [] }, { capabilities });
    expect(screen.getByText("No drives found.")).toBeInTheDocument();
  });
});
