import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { StorageBlockDevice, StorageTopologyInventory } from "@/api";
import { render } from "@/test/render";

import BtrfsCreatePoolDialog from "./BtrfsCreatePoolDialog";

const mocks = vi.hoisted(() => ({ topology: vi.fn() }));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    linuxio: {
      ...actual.linuxio,
      storage: {
        ...actual.linuxio.storage,
        get_topology: {
          queryKey: ["test-get-topology"],
          queryFn: () => mocks.topology(),
        },
      },
    },
  };
});

const GiB = 1024 ** 3;

const device = (
  path: string,
  extra: Partial<StorageBlockDevice> = {},
): StorageBlockDevice => ({
  path,
  name: path.slice(5),
  kernelName: path.slice(5),
  type: "disk",
  sizeBytes: 100 * GiB,
  fsType: "",
  mountpoints: [],
  parents: [],
  model: "",
  serial: "",
  transport: "sata",
  readOnly: false,
  freeBytes: 0,
  ...extra,
});

const topology: StorageTopologyInventory = {
  devices: [
    device("/dev/sdb", { model: "WD Red" }),
    device("/dev/sdc"),
    device("/dev/sdd", { fsType: "ext4" }),
    device("/dev/sde", { mountpoints: ["/data"] }),
    device("/dev/sdf"),
  ],
  mounts: [],
};

beforeEach(() => {
  vi.clearAllMocks();
  mocks.topology.mockResolvedValue(topology);
});

describe("BtrfsCreatePoolDialog", () => {
  it("offers free disks, limits profiles to the disk count and submits the pool", async () => {
    const user = userEvent.setup();
    const onConfirm = vi.fn();
    render(
      <BtrfsCreatePoolDialog
        onClose={vi.fn()}
        onConfirm={onConfirm}
        pooledDevices={["/dev/sdf"]}
      />,
    );

    expect(
      await screen.findByRole("checkbox", {
        name: /\/dev\/sdb · 100 GB · WD Red/,
      }),
    ).toBeInTheDocument();
    // A signature needs force; a mounted disk and a pool member are never offered.
    for (const absent of ["/dev/sdd", "/dev/sde", "/dev/sdf"]) {
      expect(
        screen.queryByRole("checkbox", { name: new RegExp(absent) }),
      ).not.toBeInTheDocument();
    }
    const create = screen.getByRole("button", { name: "Create pool" });
    expect(create).toBeDisabled();

    await user.type(screen.getByLabelText("Name"), "tank");
    expect(screen.getByLabelText("Mount point")).toHaveValue("/mnt/tank");
    await user.click(screen.getByRole("checkbox", { name: /\/dev\/sdb/ }));
    const dataProfile = screen.getByRole("combobox", { name: "Data profile" });
    expect(dataProfile).toHaveTextContent("single");
    await user.click(dataProfile);
    expect(screen.getByRole("option", { name: "dup" })).toBeInTheDocument();
    expect(
      screen.queryByRole("option", { name: "raid1" }),
    ).not.toBeInTheDocument();
    await user.click(dataProfile);

    await user.click(screen.getByRole("checkbox", { name: /\/dev\/sdc/ }));
    expect(
      screen.getByText("Everything on /dev/sdb, /dev/sdc is erased."),
    ).toBeInTheDocument();
    expect(dataProfile).toHaveTextContent("raid1");
    await user.click(dataProfile);
    await user.click(await screen.findByRole("option", { name: "raid0" }));
    await user.click(create);

    expect(onConfirm).toHaveBeenCalledWith({
      label: "tank",
      mountpoint: "/mnt/tank",
      dataProfile: "raid0",
      metadataProfile: "raid1",
      devices: ["/dev/sdb", "/dev/sdc"],
      freeSpace: [],
      force: false,
      partition: false,
    });
  });

  it("builds a pool from the unpartitioned space of disks in use", async () => {
    const user = userEvent.setup();
    const onConfirm = vi.fn();
    // Like a host whose disks hold the root RAID in their first 65 GiB.
    mocks.topology.mockResolvedValue({
      devices: [0, 1].flatMap((n) => [
        device(`/dev/nvme${n}n1`, {
          model: "Patriot M.2 P300",
          freeBytes: 888 * GiB,
        }),
        device(`/dev/nvme${n}n1p2`, {
          type: "part",
          sizeBytes: 64 * GiB,
          fsType: "linux_raid_member",
          parents: [`/dev/nvme${n}n1`],
        }),
      ]),
      mounts: [],
    });
    render(
      <BtrfsCreatePoolDialog
        onClose={vi.fn()}
        onConfirm={onConfirm}
        pooledDevices={[]}
      />,
    );

    await user.click(
      await screen.findByRole("checkbox", {
        name: "Free space on /dev/nvme0n1 · 888 GB · Patriot M.2 P300",
      }),
    );
    await user.click(
      screen.getByRole("checkbox", { name: /Free space on \/dev\/nvme1n1/ }),
    );
    // The disks themselves and their RAID partitions are never offered.
    expect(
      screen.queryByRole("checkbox", { name: /^\/dev\/nvme/ }),
    ).not.toBeInTheDocument();
    // Nothing is erased, and there is no whole disk to partition.
    expect(screen.queryByText(/is erased/)).not.toBeInTheDocument();
    expect(
      screen.queryByText(/Partition the whole disks/),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("combobox", { name: "Data profile" }),
    ).toHaveTextContent("raid1");
    await user.type(screen.getByLabelText("Name"), "tank");
    await user.click(screen.getByRole("button", { name: "Create pool" }));

    expect(onConfirm).toHaveBeenCalledWith(
      expect.objectContaining({
        devices: [],
        freeSpace: ["/dev/nvme0n1", "/dev/nvme1n1"],
        dataProfile: "raid1",
      }),
    );
  });

  it("keeps metadata at least as redundant as data, raid1c3 from three disks", async () => {
    const user = userEvent.setup();
    mocks.topology.mockResolvedValue({
      devices: ["/dev/sdb", "/dev/sdc", "/dev/sdd", "/dev/sde"].map((path) =>
        device(path),
      ),
      mounts: [],
    });
    render(
      <BtrfsCreatePoolDialog
        onClose={vi.fn()}
        onConfirm={vi.fn()}
        pooledDevices={[]}
      />,
    );
    for (const path of ["/dev/sdb", "/dev/sdc", "/dev/sdd", "/dev/sde"]) {
      await user.click(
        await screen.findByRole("checkbox", { name: new RegExp(path) }),
      );
    }

    const metadata = screen.getByRole("combobox", { name: "Metadata profile" });
    expect(metadata).toHaveTextContent("raid1c3");
    await user.click(metadata);
    for (const weaker of ["single", "dup", "raid0"]) {
      expect(
        screen.queryByRole("option", { name: weaker }),
      ).not.toBeInTheDocument();
    }
    await user.click(screen.getByRole("option", { name: "raid1" }));
    expect(metadata).toHaveTextContent(/raid1(?!c)/);

    // raid1 metadata cannot follow raid1c4 data, so it moves up with it.
    await user.click(screen.getByRole("combobox", { name: "Data profile" }));
    await user.click(await screen.findByRole("option", { name: "raid1c4" }));
    expect(metadata).toHaveTextContent("raid1c4");
  });

  it("offers disks with a foreign signature only under force", async () => {
    const user = userEvent.setup();
    render(
      <BtrfsCreatePoolDialog
        onClose={vi.fn()}
        onConfirm={vi.fn()}
        pooledDevices={[]}
      />,
    );

    await screen.findByRole("checkbox", { name: /\/dev\/sdb/ });
    expect(
      screen.queryByRole("checkbox", { name: /\/dev\/sdd/ }),
    ).not.toBeInTheDocument();
    await user.click(screen.getByRole("checkbox", { name: /Force/ }));
    expect(
      screen.getByRole("checkbox", { name: /\/dev\/sdd · 100 GB · ext4/ }),
    ).toBeInTheDocument();
  });

  it("shows the step a running create is on and holds the buttons", async () => {
    render(
      <BtrfsCreatePoolDialog
        isPending
        onClose={vi.fn()}
        onConfirm={vi.fn()}
        pooledDevices={[]}
        progress="Formatting: SSDs are trimmed first, which can take a few minutes"
      />,
    );
    expect(await screen.findByRole("status")).toHaveTextContent(
      "Formatting: SSDs are trimmed first",
    );
    expect(screen.getByRole("button", { name: "Creating..." })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
  });
});
