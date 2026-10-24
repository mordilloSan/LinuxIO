import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type {
  BtrfsFilesystem,
  BtrfsOfflinePool,
  FilesystemInfo,
  NFSMount,
} from "@/api";
import { renderWithTanStackRouter } from "@/test/render";
import { storageBlock } from "@/test/storageTopologyFixture";

import StoragePools from "./StoragePools";
import type { DriveInfo } from "./types";

vi.mock("@tanstack/react-virtual", async () =>
  (await import("@/test/reactVirtualMock")).reactVirtualMock(),
);

const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  mutations: {} as Record<string, ReturnType<typeof vi.fn> | undefined>,
  offline: vi.fn(),
  topology: vi.fn(),
}));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    call: (route: string) =>
      route === "tasks.list" ? Promise.resolve([]) : Promise.reject(),
    useCallMutation: (endpoint: { route: string }) => ({
      isPending: false,
      mutate: (mocks.mutations[endpoint.route] ??= vi.fn()),
    }),
    linuxio: {
      ...actual.linuxio,
      storage: {
        ...actual.linuxio.storage,
        btrfs_list: {
          queryKey: ["test-btrfs-list"],
          queryFn: () => mocks.list(),
        },
        btrfs_pools_offline: {
          queryKey: ["test-btrfs-pools-offline"],
          queryFn: () => mocks.offline(),
        },
        get_topology: {
          queryKey: ["test-get-topology"],
          queryFn: () => mocks.topology(),
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
    physicalBytes: 0,
    growable: false,
    errors: noErrors,
  })),
  scrub: {
    status: "none",
    errorCount: 0,
    uncorrectableErrors: 0,
    correctedErrors: 0,
    bytesScrubbed: 0,
    percent: 0,
  },
  degraded: false,
  lowMetadataSpace: false,
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

const volume = (
  mountpoint: string,
  device: string,
  fstype: string,
  freeGiB = 60,
): FilesystemInfo => ({
  mountpoint,
  device,
  fstype,
  total: 100 * GiB,
  used: (100 - freeGiB) * GiB,
  free: freeGiB * GiB,
  usedPercent: 100 - freeGiB,
});

const filesystems = [
  volume("/mnt/tank", "/dev/nvme0n1", "btrfs"),
  volume("/mnt/tank/media", "/dev/nvme0n1", "btrfs"),
  volume("/", "/dev/sda2", "btrfs"),
];

const privileged = { auth: { privileged: true } };
const capabilities = { btrfsAvailable: true };

type PoolsProps = Partial<React.ComponentProps<typeof StoragePools>>;

// Holds the selected pool the way the route's search params do.
const Pools = (props: PoolsProps) => {
  const [pool, setPool] = useState<string>();
  return (
    <StoragePools
      drives={drives}
      filesystems={filesystems}
      nfsMounts={[]}
      onInspectDrive={vi.fn()}
      onOpenVolume={vi.fn()}
      onSelectPool={setPool}
      selectedPool={pool}
      {...props}
    />
  );
};

const renderPools = (
  props: PoolsProps = {},
  options: Parameters<typeof renderWithTanStackRouter>[1] = {
    ...privileged,
    capabilities,
  },
) => renderWithTanStackRouter(<Pools {...props} />, options);

const cold: BtrfsOfflinePool = {
  uuid: "uuid-cold",
  label: "cold",
  numDevices: 2,
  devices: ["/dev/sdb"],
  totalBytes: 100 * GiB,
  mountpoint: "/mnt/cold",
};

beforeEach(() => {
  vi.clearAllMocks();
  mocks.list.mockResolvedValue([raid, root]);
  mocks.offline.mockResolvedValue([]);
  mocks.topology.mockResolvedValue({ devices: [], mounts: [] });
});

describe("StoragePools", () => {
  it("draws disks, pool and volumes for each btrfs filesystem", async () => {
    renderPools();

    expect(
      await screen.findByRole("button", { name: "tank" }),
    ).toHaveTextContent("RAID 1");
    expect(
      screen.getByRole("button", { name: "Storage pool 2" }),
    ).toHaveTextContent("Single");
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

  it("lists drives with no pool and no volume, with their model", async () => {
    const user = userEvent.setup();
    const onInspectDrive = vi.fn();
    renderPools({ onInspectDrive });

    expect(
      await screen.findByText("Disks without volumes"),
    ).toBeInTheDocument();
    // sda backs the "/" pool, so only sdb is outside every pool.
    expect(screen.getByText("Blank disk")).toBeInTheDocument();
    expect(screen.queryByText("Crucial MX500")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Drive details sdb" }));
    expect(onInspectDrive).toHaveBeenCalledWith("sdb");
  });

  it("hangs plain partitions off their disk and shares off their server", async () => {
    renderPools({
      filesystems: [
        ...filesystems,
        volume("/boot/efi", "/dev/sdb1", "vfat", 1),
        volume("/mnt/share", "nas.lan:/export/media", "nfs4", 900),
      ],
      nfsMounts: [
        {
          mountpoint: "/mnt/share",
          server: "nas.lan",
          exportPath: "/export/media",
        } as NFSMount,
      ],
    });

    expect(
      await screen.findByRole("button", { name: "Volume /boot/efi" }),
    ).toHaveTextContent("FAT");
    expect(
      screen.getByRole("button", { name: "Volume /boot/efi" }),
    ).toHaveTextContent("Free 1 GB");
    // Plain disks sit above the pools so the boot partition reads first.
    expect(
      screen
        .getByRole("button", { name: "Volume /boot/efi" })
        .compareDocumentPosition(
          screen.getByRole("button", { name: "Storage pool 2" }),
        ) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    // sdb now carries a volume, so nothing is left without one.
    expect(screen.queryByText("Disks without volumes")).toBeNull();
    expect(screen.getByText("nas.lan:/export/media")).toBeInTheDocument();
    expect(screen.getByText("Network share")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Volume /mnt/share" }),
    ).toHaveTextContent("NFS");
  });

  it("draws an md RAID array between its drives and its volume", async () => {
    mocks.list.mockResolvedValue([]);
    mocks.topology.mockResolvedValue({
      devices: [
        storageBlock("/dev/sda", "disk", 50),
        storageBlock("/dev/sda2", "part", 64, ["/dev/sda"]),
        storageBlock("/dev/sdb", "disk", 50),
        storageBlock("/dev/sdb2", "part", 64, ["/dev/sdb"]),
        storageBlock("/dev/md127", "raid1", 64, ["/dev/sda2", "/dev/sdb2"]),
      ],
      mounts: [],
    });
    renderPools({ filesystems: [volume("/", "/dev/md127", "ext4")] });

    const array = (await screen.findByText("/dev/md127")).closest(
      ".storage-pools__card",
    );
    expect(array).toHaveTextContent("RAID 1");
    expect(array).toHaveTextContent("64 GB");
    expect(screen.queryByText("No physical disk found")).toBeNull();
    expect(screen.getByText("Crucial MX500")).toBeInTheDocument();
    expect(screen.getByText("Blank disk")).toBeInTheDocument();
    // Only the NVMe drives, which hold nothing here, are left over.
    expect(
      within(
        screen.getByText("Disks without volumes")
          .nextElementSibling as HTMLElement,
      )
        .getAllByRole("button")
        .map((button) => button.getAttribute("aria-label")),
    ).toEqual(["Drive details nvme0n1", "Drive details nvme1n1"]);
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

    await screen.findByRole("button", { name: "tank" });
    expect(screen.queryByText(/^Details · /)).toBeNull();
    await user.click(screen.getByRole("button", { name: "Storage pool 2" }));
    expect(screen.getByText("Details · Storage pool 2")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Storage pool 2" }),
    ).toHaveAttribute("aria-pressed", "true");
    await user.click(screen.getByRole("button", { name: "Storage pool 2" }));
    expect(screen.queryByText(/^Details · /)).toBeNull();
  });

  it("shows the health and settings of the selected pool", async () => {
    const user = userEvent.setup();
    renderPools();

    await user.click(await screen.findByRole("button", { name: "tank" }));
    expect(screen.getByRole("heading", { name: "Healthy" })).toBeVisible();
    expect(screen.getByText("Never checked for errors")).toBeVisible();
    expect(screen.getByText("Settings")).toBeVisible();
  });

  it("opens the pool named by the URL and reports a new choice", async () => {
    const user = userEvent.setup();
    const onSelectPool = vi.fn();
    renderPools({ onSelectPool, selectedPool: "uuid-raid" });

    expect(await screen.findByText("Details · tank")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Storage pool 2" }));
    expect(onSelectPool).toHaveBeenCalledWith("uuid-root");
  });

  it("still lists the drives without btrfs or without privilege", async () => {
    const { unmount } = renderPools(
      {},
      { ...privileged, capabilities: { btrfsAvailable: false } },
    );
    // Without pools the btrfs volumes hang off their drives instead.
    expect(
      await screen.findByRole("button", { name: "Volume /mnt/tank" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "tank" })).toBeNull();
    expect(
      screen.getByRole("button", { name: "Drive details nvme0n1" }),
    ).toBeInTheDocument();
    unmount();

    renderPools({}, { capabilities });
    expect(
      await screen.findByRole("button", { name: "Drive details sda" }),
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
    expect(screen.queryByText("Disks without volumes")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Drive details nvme0n1" }),
    ).not.toBeInTheDocument();

    resolve([raid, root]);
    expect(
      await screen.findByRole("button", { name: "tank" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Disks without volumes")).toBeInTheDocument();
  });

  it("lists an unmounted pool with its missing disks and mounts it degraded", async () => {
    const user = userEvent.setup();
    mocks.offline.mockResolvedValue([cold]);
    renderPools();

    expect(await screen.findByText("cold")).toBeInTheDocument();
    expect(screen.getByText("Degraded")).toBeInTheDocument();
    expect(screen.getByText("1 missing disk")).toBeInTheDocument();
    expect(screen.getByText("fstab: /mnt/cold")).toBeInTheDocument();
    // sdb belongs to the cold pool now, so it is no longer idle.
    expect(screen.queryByText("Disks without volumes")).toBeNull();
    await user.click(screen.getByRole("button", { name: "Mount degraded" }));
    expect(mocks.mutations["storage.btrfs_pool_mount"]).toHaveBeenCalledWith({
      uuid: "uuid-cold",
      degraded: true,
    });
  });

  it("destroys the selected pool after confirmation", async () => {
    const user = userEvent.setup();
    renderPools();

    await user.click(await screen.findByRole("button", { name: "tank" }));
    await user.click(screen.getByRole("button", { name: "Destroy pool" }));
    const dialog = screen.getByRole("dialog", { name: "Destroy pool" });
    expect(dialog).toHaveTextContent("/dev/nvme0n1, /dev/nvme1n1");
    await user.click(within(dialog).getByRole("button", { name: "Destroy" }));
    expect(mocks.mutations["storage.btrfs_pool_destroy"]).toHaveBeenCalledWith(
      { uuid: "uuid-raid" },
      expect.anything(),
    );
  });

  it("renames the selected pool", async () => {
    const user = userEvent.setup();
    renderPools();

    await user.click(await screen.findByRole("button", { name: "tank" }));
    await user.click(screen.getByRole("button", { name: "Rename pool" }));
    const name = screen.getByLabelText("Name");
    expect(name).toHaveValue("tank");
    await user.clear(name);
    await user.type(name, "media");
    await user.click(screen.getByRole("button", { name: "Rename" }));
    expect(mocks.mutations["storage.btrfs_set_label"]).toHaveBeenCalledWith(
      { uuid: "uuid-raid", label: "media" },
      expect.anything(),
    );
  });

  it("opens the create pool dialog for privileged users with btrfs", async () => {
    const user = userEvent.setup();
    renderPools();

    await user.click(
      await screen.findByRole("button", { name: "Create pool" }),
    );
    expect(
      screen.getByRole("dialog", { name: "Create pool" }),
    ).toBeInTheDocument();
  });

  it("says when there are no drives or volumes at all", async () => {
    renderPools({ drives: [], filesystems: [] }, { capabilities });
    expect(
      await screen.findByText("No drives or volumes found."),
    ).toBeInTheDocument();
  });
});
