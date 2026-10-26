import { act, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { BtrfsFilesystem, StorageBlockDevice } from "@/api";
import { LinuxIOError } from "@/api/errors";
import { renderWithTanStackRouter as render } from "@/test/render";
import { storageBlock } from "@/test/storageTopologyFixture";
import type { TaskStreamActionMockConfig } from "@/test/taskStreamAction";

import PartitionsTab from "./PartitionsTab";

vi.mock("@tanstack/react-virtual", async () =>
  (await import("@/test/reactVirtualMock")).reactVirtualMock(),
);

const mocks = vi.hoisted(() => ({
  mutations: {} as Record<string, ReturnType<typeof vi.fn> | undefined>,
  btrfs: vi.fn(),
  submit: vi.fn(),
  topology: vi.fn(),
}));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  const { createTaskStreamActionMock } =
    await import("@/test/taskStreamAction");
  const task = (name: string) => ({
    useTaskStreamAction: (config?: TaskStreamActionMockConfig) => ({
      isPending: false,
      ...createTaskStreamActionMock(
        {
          openStream: () => ({}),
          runStream: () => Promise.resolve(undefined),
          submit: (request) => mocks.submit(name, request),
        },
        // The real hook toasts a string `error`; the double only takes a function.
        config && {
          ...config,
          error: typeof config.error === "function" ? config.error : undefined,
        },
      ),
    }),
  });
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
          queryFn: () => mocks.btrfs(),
        },
        btrfs_pools_offline: {
          queryKey: ["test-btrfs-offline"],
          queryFn: () => [],
        },
        get_topology: {
          queryKey: ["test-get-topology"],
          queryFn: () => mocks.topology(),
        },
        partition_create: task("partition_create"),
        partition_format: task("partition_format"),
      },
    },
  };
});

const MiB = 1024 ** 2;
const options = {
  auth: { privileged: true },
  capabilities: {
    btrfsAvailable: true,
    e2fsprogsAvailable: true,
    xfsprogsAvailable: false,
  },
};
const withXfs = {
  ...options,
  capabilities: { ...options.capabilities, xfsprogsAvailable: true },
};

const sda = (extra: Partial<StorageBlockDevice> = {}): StorageBlockDevice => ({
  ...storageBlock("/dev/sda", "disk", 10),
  ptType: "gpt",
  // The first GiB and the last two are taken; 8 GiB is free after sda2.
  freeRegions: [{ startBytes: 2 * 1024 * MiB, sizeBytes: 8 * 1024 * MiB }],
  ...extra,
});
const part = (
  n: number,
  startMiB: number,
  extra: Partial<StorageBlockDevice> = {},
): StorageBlockDevice => ({
  ...storageBlock(`/dev/sda${n}`, "part", 1, ["/dev/sda"]),
  ptType: "gpt",
  partNumber: n,
  startBytes: startMiB * MiB,
  ...extra,
});

const topology = (devices: StorageBlockDevice[]) => ({ devices, mounts: [] });
const busySda1 = part(1, 1, {
  fsType: "ext4",
  mountpoints: ["/srv"],
  busy: "Mounted at /srv",
});
const freeSda2 = part(2, 1024, { fsType: "ext4" });

beforeEach(() => {
  vi.clearAllMocks();
  mocks.mutations = {};
  mocks.submit.mockResolvedValue({ id: "task-1" });
  mocks.btrfs.mockResolvedValue([]);
  mocks.topology.mockResolvedValue(topology([sda(), busySda1, freeSda2]));
});

const user = () => userEvent.setup();

const openCreate = async (u: ReturnType<typeof userEvent.setup>) => {
  await u.click(
    await screen.findByRole("button", { name: "Create partition" }),
  );
  return screen.findByRole("dialog", { name: "Create partition" });
};

const pick = async (
  u: ReturnType<typeof userEvent.setup>,
  dialog: HTMLElement,
  select: string,
  option: string,
) => {
  await u.click(within(dialog).getByRole("combobox", { name: select }));
  await u.click(await screen.findByRole("option", { name: option }));
};

describe("PartitionsTab", () => {
  it("disables a busy partition's Format and Delete and says why", async () => {
    const u = user();
    render(<PartitionsTab driveName="sda" />, options);

    const format = await screen.findAllByRole("button", { name: "Format" });
    const remove = screen.getAllByRole("button", { name: "Delete" });
    // sda1 (busy) is listed first, sda2 is free.
    expect(format[0]).toBeDisabled();
    expect(remove[0]).toBeDisabled();
    expect(format[1]).toBeEnabled();
    expect(remove[1]).toBeEnabled();

    await u.hover(format[0].parentElement!);
    expect(await screen.findByRole("tooltip")).toHaveTextContent(
      "Mounted at /srv",
    );
    await u.unhover(format[0].parentElement!);
    await u.hover(remove[0].parentElement!);
    expect(await screen.findByRole("tooltip")).toHaveTextContent(
      "Mounted at /srv",
    );
  });

  it("explains an fstab entry and a pool member the same way", async () => {
    const u = user();
    mocks.topology.mockResolvedValue(
      topology([sda(), part(1, 1, { fsType: "ext4", inFstab: true })]),
    );
    render(<PartitionsTab driveName="sda" />, options);

    const format = await screen.findByRole("button", { name: "Format" });
    expect(format).toBeDisabled();
    await u.hover(format.parentElement!);
    expect(await screen.findByRole("tooltip")).toHaveTextContent(
      "In /etc/fstab",
    );
  });

  it("opens Create with the whole free region as the size", async () => {
    const u = user();
    render(<PartitionsTab driveName="sda" />, options);

    const dialog = await openCreate(u);
    expect(within(dialog).getByLabelText("Size")).toHaveValue("8");
    expect(
      within(dialog).getByRole("combobox", { name: "Unit" }),
    ).toHaveTextContent("GiB");
  });

  it("clamps a size over the region to the maximum", async () => {
    const u = user();
    render(<PartitionsTab driveName="sda" />, options);

    const dialog = await openCreate(u);
    const size = within(dialog).getByLabelText("Size");
    await u.clear(size);
    await u.type(size, "20");
    expect(size).toHaveValue("8");
  });

  it("sends the size rounded down to a whole MiB", async () => {
    const u = user();
    render(<PartitionsTab driveName="sda" />, options);

    const dialog = await openCreate(u);
    await pick(u, dialog, "Unit", "MiB");
    const size = within(dialog).getByLabelText("Size");
    await u.clear(size);
    await u.type(size, "1500.9");
    await u.click(within(dialog).getByRole("button", { name: "Create" }));

    expect(mocks.submit).toHaveBeenCalledWith("partition_create", {
      device: "/dev/sda",
      startBytes: 2 * 1024 * MiB,
      sizeBytes: 1500 * MiB,
    });
  });

  it("adds the format, and the mount only after choosing a format", async () => {
    const u = user();
    render(<PartitionsTab driveName="sda" />, options);

    const dialog = await openCreate(u);
    expect(
      within(dialog).queryByLabelText("Mount after formatting"),
    ).not.toBeInTheDocument();
    await pick(u, dialog, "Filesystem", "ext4");
    await u.type(within(dialog).getByLabelText("Label"), "data");
    await u.click(within(dialog).getByLabelText("Mount after formatting"));
    expect(within(dialog).getByLabelText("Mount point")).toHaveValue(
      "/mnt/data",
    );
    await u.click(within(dialog).getByLabelText("Mount at boot"));
    await u.click(within(dialog).getByRole("button", { name: "Create" }));

    expect(mocks.submit).toHaveBeenCalledWith("partition_create", {
      device: "/dev/sda",
      startBytes: 2 * 1024 * MiB,
      sizeBytes: 8 * 1024 * MiB,
      format: { fsType: "ext4", label: "data" },
      mount: { mountpoint: "/mnt/data", persist: true },
    });
  });

  it("refuses an ext4 label over 16 bytes", async () => {
    const u = user();
    render(<PartitionsTab driveName="sda" />, options);

    const dialog = await openCreate(u);
    await pick(u, dialog, "Filesystem", "ext4");
    await u.type(within(dialog).getByLabelText("Label"), "x".repeat(17));
    expect(
      within(dialog).getByText("ext4 labels take at most 16 bytes"),
    ).toBeInTheDocument();
    expect(
      within(dialog).getByRole("button", { name: "Create" }),
    ).toBeDisabled();
  });

  it("lists a filesystem whose tools are missing as unavailable", async () => {
    const u = user();
    render(<PartitionsTab driveName="sda" />, options);

    const dialog = await openCreate(u);
    await u.click(within(dialog).getByRole("combobox", { name: "Filesystem" }));
    expect(
      await screen.findByRole("option", { name: "xfs (unavailable)" }),
    ).toHaveClass("app-select__option--disabled");
    expect(
      screen.getByRole("link", { name: "Install them in Settings" }),
    ).toHaveAttribute("href", "/settings?tab=capabilities");
  });

  it("offers only the filesystems the chosen size is big enough for", async () => {
    const u = user();
    render(<PartitionsTab driveName="sda" />, withXfs);

    const dialog = await openCreate(u);
    await pick(u, dialog, "Unit", "MiB");
    const size = within(dialog).getByLabelText("Size");
    await u.clear(size);
    await u.type(size, "512");
    await pick(u, dialog, "Filesystem", "xfs");
    const create = within(dialog).getByRole("button", { name: "Create" });
    expect(create).toBeEnabled();

    await u.clear(size);
    await u.type(size, "200");
    const filesystem = within(dialog).getByRole("combobox", {
      name: "Filesystem",
    });
    expect(filesystem).toHaveTextContent("xfs (needs 300 MiB)");
    expect(create).toBeDisabled();
    await u.click(filesystem);
    expect(
      await screen.findByRole("option", { name: "xfs (needs 300 MiB)" }),
    ).toHaveClass("app-select__option--disabled");
  });

  it("offers only the filesystems a partition is big enough for when formatting", async () => {
    const u = user();
    mocks.topology.mockResolvedValue(
      topology([sda(), part(1, 1, { sizeBytes: 200 * MiB })]),
    );
    render(<PartitionsTab driveName="sda" />, withXfs);

    await u.click(await screen.findByRole("button", { name: "Format" }));
    const dialog = await screen.findByRole("dialog");
    await u.click(within(dialog).getByRole("combobox", { name: "Filesystem" }));
    expect(
      await screen.findByRole("option", { name: "xfs (needs 300 MiB)" }),
    ).toHaveClass("app-select__option--disabled");
    expect(screen.getByRole("option", { name: "btrfs" })).not.toHaveClass(
      "app-select__option--disabled",
    );
  });

  it("requires Overwrite to format a partition with a filesystem", async () => {
    const u = user();
    render(<PartitionsTab driveName="sda" />, options);

    await u.click(
      (await screen.findAllByRole("button", { name: "Format" }))[1],
    );
    const dialog = await screen.findByRole("dialog", {
      name: "Format /dev/sda2",
    });
    const submit = within(dialog).getByRole("button", { name: "Format" });
    expect(submit).toBeDisabled();
    await u.click(within(dialog).getByLabelText("Overwrite existing data"));
    expect(submit).toBeEnabled();
    await u.click(submit);

    expect(mocks.submit).toHaveBeenCalledWith("partition_format", {
      device: "/dev/sda2",
      fsType: "ext4",
      label: "",
      overwrite: true,
    });
  });

  it("offers Overwrite without requiring it when no filesystem is known", async () => {
    const u = user();
    mocks.topology.mockResolvedValue(topology([sda(), part(1, 1)]));
    render(<PartitionsTab driveName="sda" />, options);

    await u.click(await screen.findByRole("button", { name: "Format" }));
    const dialog = await screen.findByRole("dialog");
    expect(
      within(dialog).getByLabelText("Overwrite existing data"),
    ).not.toBeChecked();
    expect(
      within(dialog).getByRole("button", { name: "Format" }),
    ).toBeEnabled();
  });

  it("shows the bridge's refusal to format data, which says to tick Overwrite", async () => {
    const u = user();
    mocks.topology.mockResolvedValue(topology([sda(), part(1, 1)]));
    mocks.submit.mockRejectedValue(
      new LinuxIOError("/dev/sda1 holds data; overwrite to replace it"),
    );
    render(<PartitionsTab driveName="sda" />, options);

    await u.click(await screen.findByRole("button", { name: "Format" }));
    const dialog = await screen.findByRole("dialog");
    await u.click(within(dialog).getByRole("button", { name: "Format" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      "/dev/sda1 holds data; overwrite to replace it",
    );

    await u.click(within(dialog).getByLabelText("Overwrite existing data"));
    mocks.submit.mockResolvedValue({ id: "task-2" });
    await u.click(within(dialog).getByRole("button", { name: "Format" }));
    expect(mocks.submit).toHaveBeenLastCalledWith(
      "partition_format",
      expect.objectContaining({ overwrite: true }),
    );
  });

  it("keeps a failure that came with no dialog open out of the next dialog", async () => {
    const u = user();
    mocks.topology.mockResolvedValue(topology([sda(), part(1, 1)]));
    let fail: (error: Error) => void = () => undefined;
    mocks.submit.mockReturnValue(
      new Promise((_, reject) => {
        fail = reject;
      }),
    );
    render(<PartitionsTab driveName="sda" />, options);

    await u.click(await screen.findByRole("button", { name: "Format" }));
    const dialog = await screen.findByRole("dialog");
    await u.click(within(dialog).getByRole("button", { name: "Format" }));
    // Dismissed while the task runs, which then fails.
    await u.keyboard("{Escape}");
    await vi.waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
    await act(async () => fail(new LinuxIOError("mkfs.ext4 failed")));

    const create = await openCreate(u);
    expect(within(create).queryByRole("alert")).not.toBeInTheDocument();
  });

  it("offers no Mount on a pool member, even one listed in /etc/fstab", async () => {
    mocks.topology.mockResolvedValue(
      topology([sda(), part(1, 1, { fsType: "btrfs", inFstab: true })]),
    );
    mocks.btrfs.mockResolvedValue([
      {
        label: "tank",
        uuid: "u1",
        devices: [{ path: "/dev/sda1" }],
      } as BtrfsFilesystem,
    ]);
    render(<PartitionsTab driveName="sda" />, options);

    // The row is listed with its fstab reason before the pool is known.
    expect(
      await screen.findByRole("button", { name: "Format" }),
    ).toBeDisabled();
    await vi.waitFor(() =>
      expect(
        screen.queryByRole("button", { name: "Mount" }),
      ).not.toBeInTheDocument(),
    );
    expect(screen.getByText("In /etc/fstab")).toBeInTheDocument();
  });

  it("names the partition, its size and filesystem when deleting", async () => {
    const u = user();
    render(<PartitionsTab driveName="sda" />, options);

    await u.click(
      (await screen.findAllByRole("button", { name: "Delete" }))[1],
    );
    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent(/\/dev\/sda2 \(1 GB, ext4\)/);
    await u.click(within(dialog).getByRole("button", { name: "Delete" }));
    expect(mocks.mutations["storage.partition_delete"]).toHaveBeenCalledWith(
      { device: "/dev/sda2" },
      expect.anything(),
    );
  });

  it("lists every partition that initializing destroys", async () => {
    const u = user();
    mocks.topology.mockResolvedValue(
      topology([sda(), part(1, 1, { fsType: "ext4" }), part(2, 1024)]),
    );
    render(<PartitionsTab driveName="sda" />, options);

    await u.click(
      await screen.findByRole("button", { name: "Initialize disk" }),
    );
    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent(/\/dev\/sda1 \(1 GB, ext4\)/);
    expect(dialog).toHaveTextContent(/\/dev\/sda2 \(1 GB\)/);
    await u.click(within(dialog).getByRole("button", { name: "Initialize" }));
    expect(mocks.mutations["storage.disk_initialize"]).toHaveBeenCalledWith(
      { device: "/dev/sda" },
      expect.anything(),
    );
  });

  it("blocks Initialize for a disk that is itself in /etc/fstab or an offline pool", async () => {
    const u = user();
    mocks.topology.mockResolvedValue(topology([sda({ inFstab: true })]));
    const { unmount } = render(<PartitionsTab driveName="sda" />, options);
    const button = await screen.findByRole("button", {
      name: "Initialize disk",
    });
    expect(button).toBeDisabled();
    await u.hover(button.parentElement!);
    expect(await screen.findByRole("tooltip")).toHaveTextContent(
      "In /etc/fstab",
    );
    unmount();
  });

  it("names a disk's own filesystem when confirming Initialize", async () => {
    const u = user();
    mocks.topology.mockResolvedValue(topology([sda({ fsType: "ext4" })]));
    render(<PartitionsTab driveName="sda" />, options);

    await u.click(
      await screen.findByRole("button", { name: "Initialize disk" }),
    );
    expect(await screen.findByRole("dialog")).toHaveTextContent(
      "/dev/sda (ext4)",
    );
  });

  it("blocks Initialize while a partition is in use", async () => {
    render(<PartitionsTab driveName="sda" />, options);
    expect(
      await screen.findByRole("button", { name: "Initialize disk" }),
    ).toBeDisabled();
  });

  it("offers an MBR disk Initialize but no Create or Delete", async () => {
    mocks.topology.mockResolvedValue(
      topology([
        sda({ ptType: "dos", freeRegions: undefined }),
        part(1, 1, { ptType: "dos", fsType: "ext4" }),
      ]),
    );
    render(<PartitionsTab driveName="sda" />, options);

    expect(await screen.findByText("Partition table: MBR")).toBeInTheDocument();
    expect(screen.getByText("sda1")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Create partition" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Delete" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Initialize disk" }),
    ).toBeEnabled();
  });

  it("offers nothing on a drive with no media", async () => {
    mocks.topology.mockResolvedValue(
      topology([sda({ sizeBytes: 0, freeRegions: undefined })]),
    );
    render(<PartitionsTab driveName="sda" />, options);

    expect(
      await screen.findByText("No media in this drive."),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Initialize disk" }),
    ).not.toBeInTheDocument();
  });

  it("offers Mount only on an unmounted partition with a mountable filesystem", async () => {
    mocks.topology.mockResolvedValue(
      topology([sda(), busySda1, freeSda2, part(3, 3000, { fsType: "swap" })]),
    );
    render(<PartitionsTab driveName="sda" />, options);

    expect(
      await screen.findAllByRole("button", { name: "Mount" }),
    ).toHaveLength(1);
  });
});
