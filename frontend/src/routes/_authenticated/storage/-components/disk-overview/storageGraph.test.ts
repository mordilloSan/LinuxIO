import { describe, expect, it } from "vitest";

import type {
  BtrfsFilesystem,
  BtrfsOfflinePool,
  FilesystemInfo,
  MDArray,
  NFSMount,
  StorageBlockDevice,
} from "@/api";
import { storageBlock } from "@/test/storageTopologyFixture";

import {
  buildStorageGraph,
  describePool,
  deviceTypeLabel,
  DRIVE_HEIGHT,
  ESP_TYPE,
  fsTypeLabel,
  partitionLabel,
  profileLabel,
  ROW_GAP,
  type GraphNode,
} from "./storageGraph";
import type { DriveInfo } from "./types";

const GiB = 1024 ** 3;
const noErrors = { write: 0, read: 0, flush: 0, corruption: 0, generation: 0 };

const mount = (mountpoint: string) => ({
  mountpoint,
  device: "/dev/nvme0n1",
  uuid: "u",
  subvol: "/",
  subvolId: 5,
  options: [],
});

const filesystem = (overrides: Partial<BtrfsFilesystem>): BtrfsFilesystem => ({
  uuid: "7f84fdfd-4bef-4624-ba23-7bb4730ac4a9",
  label: "",
  mountpoints: [mount("/"), mount("/")],
  totalBytes: 100 * GiB,
  unallocatedBytes: 40 * GiB,
  usedBytes: 30 * GiB,
  allocation: [
    { kind: "metadata", profile: "dup", totalBytes: GiB, usedBytes: GiB },
    { kind: "data", profile: "raid10", totalBytes: GiB, usedBytes: GiB },
  ],
  devices: [
    {
      id: 1,
      path: "/dev/nvme0n1p2",
      sizeBytes: 50 * GiB,
      usedBytes: GiB,
      missing: false,
      physicalBytes: 0,
      growable: false,
      errors: noErrors,
    },
    {
      id: 2,
      path: "",
      sizeBytes: 0,
      usedBytes: 0,
      missing: true,
      physicalBytes: 0,
      growable: false,
      errors: noErrors,
    },
  ],
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
  ...overrides,
});

describe("profileLabel", () => {
  it("writes profiles the way people say them", () => {
    expect(profileLabel("raid10")).toBe("RAID 10");
    expect(profileLabel("raid1c3")).toBe("RAID 1C3");
    expect(profileLabel("dup")).toBe("DUP");
    expect(profileLabel("single")).toBe("Single");
  });
});

describe("describePool", () => {
  it("summarises disks, data profile, space and volumes", () => {
    const pool = describePool(filesystem({}), 0);
    expect(pool.profile).toBe("RAID 10");
    expect(pool.allocatedBytes).toBe(60 * GiB);
    expect(pool.freeBytes).toBe(70 * GiB);
    expect(pool.volumes).toEqual(["/"]);
    expect(pool.name).toBe("Storage pool 1");
    expect(pool.disks.map((d) => d.name)).toEqual([
      "nvme0n1p2",
      "Missing disk",
    ]);
    expect(pool.disks[1].missing).toBe(true);
  });

  it("prefers the label and reads the profile of a mixed allocation", () => {
    const pool = describePool(
      filesystem({
        label: "data",
        allocation: [
          {
            kind: "data+metadata",
            profile: "single",
            totalBytes: GiB,
            usedBytes: GiB,
          },
        ],
      }),
      0,
    );
    expect(pool.name).toBe("data");
    expect(pool.profile).toBe("Single");
  });

  it("numbers unlabelled pools and shows no profile when nothing is known", () => {
    const pool = describePool(
      filesystem({ mountpoints: [], allocation: [], devices: [] }),
      2,
    );
    expect(pool.name).toBe("Storage pool 3");
    expect(pool.profile).toBe("");
    expect(pool.volumes).toEqual([]);
  });
});

describe("partitionLabel", () => {
  it("keeps the suffix after the drive name", () => {
    const nvme = {
      name: "nvme0n1",
      model: "",
      sizeBytes: 0,
      transport: "nvme",
    };
    const sda = { name: "sda", model: "", sizeBytes: 0, transport: "sata" };
    expect(partitionLabel("/dev/nvme0n1p2", nvme)).toBe("p2");
    expect(partitionLabel("sda1", sda)).toBe("1");
    expect(partitionLabel("/dev/sda", sda)).toBe("");
    expect(partitionLabel("/dev/mapper/x", sda)).toBe("");
  });
});

describe("buildStorageGraph", () => {
  const volume = (
    mountpoint: string,
    device: string,
    fstype: string,
  ): FilesystemInfo => ({
    mountpoint,
    device,
    fstype,
    total: 100 * GiB,
    used: 40 * GiB,
    free: 60 * GiB,
    usedPercent: 40,
  });
  const rates = (read: number, write: number) => ({
    read_bytes_per_sec: read,
    write_bytes_per_sec: write,
    read_ops_per_sec: 1,
    write_ops_per_sec: 1,
  });
  const drive = (name: string, read = 0, write = 0): DriveInfo => ({
    name,
    model: name,
    sizeBytes: 500 * GiB,
    transport: "nvme",
    rates: rates(read, write),
  });
  const esp = (
    path: string,
    parent: string,
    mountpoints: string[] = [],
  ): StorageBlockDevice => ({
    ...storageBlock(path, "part", 1, [parent], mountpoints),
    fsType: "vfat",
    partType: ESP_TYPE,
    partUUID: `uuid-${path.slice(5)}`,
  });
  const nfs = {
    mountpoint: "/mnt/share",
    server: "nas.lan",
    exportPath: "/export/media",
  } as NFSMount;
  const empty = {
    arrays: [] as MDArray[],
    bootEntries: [],
    btrfs: [] as BtrfsFilesystem[],
    devices: [] as StorageBlockDevice[],
    filesystems: [] as FilesystemInfo[],
    nfsMounts: [] as NFSMount[],
    offline: [] as BtrfsOfflinePool[],
  };
  const byKind = (nodes: GraphNode[], kind: GraphNode["kind"]) =>
    nodes.filter((node) => node.kind === kind);
  const edgesInto = (graph: ReturnType<typeof buildStorageGraph>, id: string) =>
    graph.edges.filter((edge) => edge.to === id);
  const overlapFree = (nodes: GraphNode[]) => {
    const sorted = [...nodes].sort((a, b) => a.top - b.top);
    return sorted.every(
      (node, index) =>
        index === 0 ||
        node.top >= sorted[index - 1].top + sorted[index - 1].height + ROW_GAP,
    );
  };

  // The reference host: four NVMe drives, each with an ESP (p1), an md RAID 1
  // member (p2) and a btrfs RAID 10 member (p3); nvme2n1p1 is the mounted ESP.
  const names = ["nvme0n1", "nvme1n1", "nvme2n1", "nvme3n1"];
  const host = () => {
    const devices = names.flatMap((name) => [
      storageBlock(`/dev/${name}`, "disk", 954),
      esp(
        `/dev/${name}p1`,
        `/dev/${name}`,
        name === "nvme2n1" ? ["/boot/efi"] : [],
      ),
      {
        ...storageBlock(`/dev/${name}p2`, "part", 64, [`/dev/${name}`]),
        fsType: "linux_raid_member",
      },
      {
        ...storageBlock(
          `/dev/${name}p3`,
          "part",
          888,
          [`/dev/${name}`],
          name === "nvme0n1" ? ["/mnt/Volume1"] : [],
        ),
        fsType: "btrfs",
      },
    ]);
    devices.push(
      storageBlock(
        "/dev/md127",
        "raid1",
        64,
        names.map((name) => `/dev/${name}p2`),
        ["/"],
      ),
    );
    const pool = filesystem({
      label: "linuxio",
      mountpoints: [{ ...mount("/mnt/Volume1"), device: "/dev/nvme0n1p3" }],
      devices: names.map((name, index) => ({
        ...filesystem({}).devices[0],
        id: index + 1,
        path: `/dev/${name}p3`,
      })),
    });
    const md127 = {
      name: "md127",
      path: "/dev/md127",
      level: "raid1",
      degraded: 0,
    } as MDArray;
    return buildStorageGraph({
      ...empty,
      arrays: [md127],
      bootEntries: names.map((name, index) => ({
        id: `000${index}`,
        label: "Ubuntu",
        partUUID: `UUID-${name}p1`,
        active: true,
        current: index === 0,
        order: index,
      })),
      btrfs: [pool],
      devices,
      drives: names.map((name, index) => drive(name, (index + 1) * 1000, 100)),
      filesystems: [
        volume("/", "/dev/md127", "ext4"),
        volume("/boot/efi", "/dev/nvme2n1p1", "vfat"),
        volume("/mnt/Volume1", "/dev/nvme0n1p3", "btrfs"),
      ],
    });
  };

  it("draws the reference host as four drives, three middle nodes and three volumes", () => {
    const graph = host();
    expect(byKind(graph.nodes, "drive").map((node) => node.id)).toEqual(
      names.map((name) => `drive:${name}`),
    );
    expect(
      graph.nodes.filter((node) => node.column === 1).map((n) => n.kind),
    ).toEqual(["boot", "pool", "device"]);
    expect(
      byKind(graph.nodes, "volume")
        .map((node) => node.id)
        .sort(),
    ).toEqual(["volume:/", "volume:/boot/efi", "volume:/mnt/Volume1"]);
    // Twelve edges leave the drives, one per partition, labelled by suffix.
    const fromDrives = graph.edges.filter((edge) =>
      edge.from.startsWith("drive:"),
    );
    expect(fromDrives).toHaveLength(12);
    expect(new Set(fromDrives.map((edge) => edge.label))).toEqual(
      new Set(["p1", "p2", "p3"]),
    );
    expect(
      fromDrives.filter((e) => e.to === "boot").map((e) => e.live),
    ).toEqual([false, false, true, false]);
    expect(
      fromDrives
        .filter((e) => e.to === "device:/dev/md127")
        .every((e) => e.live),
    ).toBe(true);
    // No drive is listed as idle and nothing is left over as unused.
    expect(
      byKind(graph.nodes, "drive").every(
        (n) => n.kind === "drive" && n.unused.length === 0,
      ),
    ).toBe(true);
  });

  it("builds the EFI boot node from the firmware entries and the mounted copy", () => {
    const graph = host();
    const boot = graph.nodes.find((node) => node.kind === "boot");
    expect(boot?.kind === "boot" && boot.entries).toBe(4);
    expect(boot?.kind === "boot" && boot.firmware).toBe(true);
    expect(boot?.kind === "boot" && boot.mounted?.name).toBe("nvme2n1p1");
    const [toVolume] = graph.edges.filter((edge) => edge.from === "boot");
    expect(toVolume.to).toBe("volume:/boot/efi");
    expect(toVolume.live).toBe(true);
    expect(toVolume.rates?.read_bytes_per_sec).toBe(3000);
  });

  it("gives a pool's volume the sum of its members' rates", () => {
    const graph = host();
    const [edge] = graph.edges.filter((e) => e.to === "volume:/mnt/Volume1");
    expect(edge.from).toBe("pool:7f84fdfd-4bef-4624-ba23-7bb4730ac4a9");
    expect(edge.rates).toEqual({
      ...rates(10000, 400),
      read_ops_per_sec: 4,
      write_ops_per_sec: 4,
    });
    const md = graph.nodes.find((n) => n.id === "device:/dev/md127");
    expect(md?.kind === "device" && md.array?.name).toBe("md127");
  });

  it("stacks the middle nodes and volumes without overlap, over their sources", () => {
    const graph = host();
    const middle = graph.nodes.filter((node) => node.column === 1);
    expect(overlapFree(middle)).toBe(true);
    expect(overlapFree(byKind(graph.nodes, "volume"))).toBe(true);
    expect(middle.map((node) => node.lane)).toEqual([0, 1, 2]);
    // Four drives of 120px and three middle nodes of 140px: the middle
    // column centres on the drives rather than hanging off the top.
    const drives = byKind(graph.nodes, "drive");
    expect(drives.map((node) => node.top)).toEqual([0, 144, 288, 432]);
    expect(middle[1].top).toBeGreaterThan(0);
    expect(graph.height).toBe(432 + DRIVE_HEIGHT);
    // A drive's three exits spread 14px apart in the order of their targets.
    const exits = graph.edges
      .filter((edge) => edge.from === "drive:nvme0n1")
      .sort((a, b) => a.exit - b.exit);
    expect(exits.map((edge) => edge.exit)).toEqual([-14, 0, 14]);
    expect(exits.map((edge) => edge.to)).toEqual([
      "boot",
      "pool:7f84fdfd-4bef-4624-ba23-7bb4730ac4a9",
      "device:/dev/md127",
    ]);
  });

  it("draws a single ESP as a plain partition and ignores entries for unknown partitions", () => {
    const graph = buildStorageGraph({
      ...empty,
      bootEntries: [
        {
          id: "0000",
          label: "Ubuntu",
          partUUID: "uuid-sda1",
          active: true,
          current: true,
          order: 0,
        },
        {
          id: "0001",
          label: "Windows",
          partUUID: "uuid-other",
          active: true,
          current: false,
          order: 1,
        },
      ],
      devices: [
        storageBlock("/dev/sda", "disk", 100),
        esp("/dev/sda1", "/dev/sda", ["/boot/efi"]),
      ],
      drives: [drive("sda")],
      filesystems: [volume("/boot/efi", "/dev/sda1", "vfat")],
    });
    expect(byKind(graph.nodes, "boot")).toHaveLength(0);
    expect(graph.edges).toEqual([
      expect.objectContaining({
        from: "drive:sda",
        to: "volume:/boot/efi",
        label: "1",
        live: true,
      }),
    ]);
    const two = buildStorageGraph({
      ...empty,
      bootEntries: [
        {
          id: "0000",
          label: "Ubuntu",
          partUUID: "uuid-sda1",
          active: true,
          current: true,
          order: 0,
        },
        {
          id: "0001",
          label: "Windows",
          partUUID: "uuid-other",
          active: true,
          current: false,
          order: 1,
        },
      ],
      devices: [
        storageBlock("/dev/sda", "disk", 100),
        esp("/dev/sda1", "/dev/sda", ["/boot/efi"]),
        storageBlock("/dev/sdb", "disk", 100),
        esp("/dev/sdb1", "/dev/sdb"),
      ],
      drives: [drive("sda"), drive("sdb")],
      filesystems: [volume("/boot/efi", "/dev/sda1", "vfat")],
    });
    const boot = two.nodes.find((node) => node.kind === "boot");
    expect(boot?.kind === "boot" && boot.entries).toBe(1);
    expect(boot?.kind === "boot" && boot.copies.length).toBe(2);
  });

  it("collapses mapper chains to the mounted device and names the layers", () => {
    const graph = buildStorageGraph({
      ...empty,
      devices: [
        storageBlock("/dev/nvme0n1", "disk", 100),
        storageBlock("/dev/nvme0n1p2", "part", 99, ["/dev/nvme0n1"]),
        storageBlock("/dev/mapper/luks", "crypt", 99, ["/dev/nvme0n1p2"]),
        storageBlock(
          "/dev/mapper/vg-root",
          "lvm",
          50,
          ["/dev/mapper/luks"],
          ["/"],
        ),
        storageBlock(
          "/dev/mapper/vg-home",
          "lvm",
          40,
          ["/dev/mapper/luks"],
          ["/home"],
        ),
      ],
      drives: [drive("nvme0n1", 5, 7)],
      filesystems: [
        volume("/", "/dev/mapper/vg-root", "ext4"),
        volume("/home", "/dev/mapper/vg-home", "ext4"),
      ],
    });
    const devices = byKind(graph.nodes, "device");
    expect(
      devices.map((node) => node.kind === "device" && node.caption),
    ).toEqual(["LVM on Encrypted", "LVM on Encrypted"]);
    expect(edgesInto(graph, "device:/dev/mapper/vg-root")).toEqual([
      expect.objectContaining({
        from: "drive:nvme0n1",
        label: "p2",
        live: true,
        rates: rates(5, 7),
      }),
    ]);
    expect(edgesInto(graph, "volume:/home")[0].rates).toEqual(rates(5, 7));
  });

  it("links md members that are whole disks without a label", () => {
    const graph = buildStorageGraph({
      ...empty,
      devices: [
        storageBlock("/dev/sda", "disk", 100),
        storageBlock("/dev/sdb", "disk", 100),
        storageBlock(
          "/dev/md0",
          "raid1",
          100,
          ["/dev/sda", "/dev/sdb"],
          ["/srv"],
        ),
      ],
      drives: [drive("sda"), drive("sdb")],
      filesystems: [volume("/srv", "/dev/md0", "xfs")],
    });
    expect(
      edgesInto(graph, "device:/dev/md0").map((edge) => edge.label),
    ).toEqual(["", ""]);
  });

  it("puts network shares behind a server node", () => {
    const graph = buildStorageGraph({
      ...empty,
      drives: [drive("sda")],
      filesystems: [volume("/mnt/share", "nas.lan:/export/media", "nfs4")],
      nfsMounts: [nfs],
    });
    const server = graph.nodes.find((node) => node.kind === "server");
    expect(server?.kind === "server" && server.label).toBe(
      "nas.lan:/export/media",
    );
    expect(server?.kind === "server" && server.network).toBe(true);
    expect(graph.edges).toEqual([
      expect.objectContaining({
        from: server?.id,
        to: "volume:/mnt/share",
        live: true,
        rates: undefined,
      }),
    ]);
    // A drive with nothing on it still has its card, with no edges.
    expect(graph.edges.some((edge) => edge.from === "drive:sda")).toBe(false);
  });

  it("lists partitions nothing uses on their drive and leaves pool members off it", () => {
    const graph = buildStorageGraph({
      ...empty,
      btrfs: [
        filesystem({
          label: "tank",
          mountpoints: [],
          devices: [{ ...filesystem({}).devices[0], path: "/dev/sda3" }],
        }),
      ],
      devices: [
        storageBlock("/dev/sda", "disk", 100),
        storageBlock("/dev/sda1", "part", 10, ["/dev/sda"], ["/srv"]),
        storageBlock("/dev/sda2", "part", 16, ["/dev/sda"]),
        {
          ...storageBlock("/dev/sda3", "part", 40, ["/dev/sda"]),
          fsType: "btrfs",
        },
        storageBlock("/dev/sda4", "part", 30, ["/dev/sda"]),
        storageBlock("/dev/md0", "raid1", 30, ["/dev/sda4"]),
      ],
      drives: [drive("sda")],
      filesystems: [volume("/srv", "/dev/sda1", "ext4")],
    });
    const sda = graph.nodes.find((node) => node.id === "drive:sda");
    expect(sda?.kind === "drive" && sda.unused.map((p) => p.name)).toEqual([
      "sda2",
    ]);
    expect(sda?.height).toBe(DRIVE_HEIGHT + 16);
    // The unmounted pool is drawn, not live, and its volume column is empty.
    expect(
      edgesInto(graph, "pool:7f84fdfd-4bef-4624-ba23-7bb4730ac4a9")[0].live,
    ).toBe(false);
  });

  it("draws an offline pool with its missing disks", () => {
    const graph = buildStorageGraph({
      ...empty,
      devices: [storageBlock("/dev/sdb", "disk", 100)],
      drives: [drive("sdb")],
      offline: [
        {
          uuid: "cold",
          label: "cold",
          numDevices: 3,
          devices: ["/dev/sdb"],
          totalBytes: 0,
          mountpoint: "/mnt/cold",
        },
      ],
    });
    expect(byKind(graph.nodes, "offlinePool")).toHaveLength(1);
    const missing = graph.nodes.find((node) => node.kind === "missing");
    expect(missing?.kind === "missing" && missing.count).toBe(2);
    expect(missing?.column).toBe(0);
    expect(graph.edges.map((edge) => [edge.from, edge.live])).toEqual([
      ["drive:sdb", false],
      ["missing:cold", false],
    ]);
  });

  it("keeps a plain partition's volume beside its drive and pushes the middle column around the line", () => {
    const graph = buildStorageGraph({
      ...empty,
      devices: [
        storageBlock("/dev/sda", "disk", 100),
        storageBlock("/dev/sda1", "part", 100, ["/dev/sda"], ["/srv"]),
        storageBlock("/dev/sdb", "disk", 100),
        storageBlock("/dev/sdc", "disk", 100),
        storageBlock(
          "/dev/md0",
          "raid1",
          100,
          ["/dev/sdb", "/dev/sdc"],
          ["/data"],
        ),
      ],
      drives: [drive("sda"), drive("sdb"), drive("sdc")],
      filesystems: [
        volume("/srv", "/dev/sda1", "ext4"),
        volume("/data", "/dev/md0", "xfs"),
      ],
    });
    const srv = graph.nodes.find((node) => node.id === "volume:/srv")!;
    const sda = graph.nodes.find((node) => node.id === "drive:sda")!;
    expect(srv.top + srv.height / 2).toBe(sda.top + sda.height / 2);
    const md = graph.nodes.find((node) => node.id === "device:/dev/md0")!;
    // The line to /srv crosses the middle column at the drive's centre; md0
    // sits below it with the usual gap.
    expect(md.top).toBeGreaterThanOrEqual(
      sda.top + sda.height / 2 + 12 + ROW_GAP,
    );
    expect(overlapFree(byKind(graph.nodes, "volume"))).toBe(true);
  });
});

describe("deviceTypeLabel", () => {
  it("names RAID levels and mapper types the way people say them", () => {
    expect(deviceTypeLabel("raid1")).toBe("RAID 1");
    expect(deviceTypeLabel("raid10")).toBe("RAID 10");
    expect(deviceTypeLabel("lvm")).toBe("LVM");
    expect(deviceTypeLabel("crypt")).toBe("Encrypted");
  });
});

describe("fsTypeLabel", () => {
  it("names common filesystems the way people say them", () => {
    expect(fsTypeLabel("btrfs")).toBe("Btrfs");
    expect(fsTypeLabel("vfat")).toBe("FAT");
    expect(fsTypeLabel("nfs4")).toBe("NFS");
    expect(fsTypeLabel("ext4")).toBe("ext4");
  });
});
