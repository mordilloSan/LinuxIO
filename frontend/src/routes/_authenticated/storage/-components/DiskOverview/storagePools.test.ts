import { describe, expect, it } from "vitest";

import type { BtrfsFilesystem, FilesystemInfo, NFSMount } from "@/api";

import {
  buildStorageLayout,
  describePool,
  fsTypeLabel,
  profileLabel,
} from "./storagePools";
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
      errors: noErrors,
    },
    {
      id: 2,
      path: "",
      sizeBytes: 0,
      usedBytes: 0,
      missing: true,
      errors: noErrors,
    },
  ],
  scrub: { status: "none", errorCount: 0, bytesScrubbed: 0, percent: 0 },
  degraded: false,
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

describe("buildStorageLayout", () => {
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
  const drive = (name: string): DriveInfo => ({
    name,
    model: name,
    sizeBytes: 500 * GiB,
    transport: "nvme",
  });
  const drives = [drive("nvme0n1"), drive("sda"), drive("sdb")];
  const nfs = {
    mountpoint: "/mnt/share",
    server: "nas.lan",
    exportPath: "/export/media",
  } as NFSMount;

  it("places every mounted filesystem exactly once", () => {
    const layout = buildStorageLayout(
      [filesystem({ devices: [filesystem({}).devices[0]] })],
      [
        volume("/", "/dev/nvme0n1p2", "btrfs"),
        volume("/boot/efi", "/dev/nvme0n1p1", "vfat"),
        volume("/srv", "/dev/sda1", "ext4"),
        volume("/mnt/share", "nas.lan:/export/media", "nfs4"),
        volume("/secure", "/dev/mapper/luks-1", "ext4"),
      ],
      drives,
      [nfs],
    );

    expect(layout.poolStacks[0].volumes.map((v) => v.mountpoint)).toEqual([
      "/",
    ]);
    expect(
      layout.driveStacks.map((s) => [
        s.drive.name,
        s.volumes.map((v) => v.mountpoint),
      ]),
    ).toEqual([
      ["nvme0n1", ["/boot/efi"]],
      ["sda", ["/srv"]],
    ]);
    expect(
      layout.sourceStacks.map((s) => [s.label, s.network, s.volumes.length]),
    ).toEqual([
      ["nas.lan:/export/media", true, 1],
      ["/dev/mapper/luks-1", false, 1],
    ]);
    // nvme0n1 backs the pool, sda has a volume: only sdb is idle.
    expect(layout.idleDrives.map((d) => d.name)).toEqual(["sdb"]);
  });

  it("falls back to drive stacks when no btrfs pools are known", () => {
    const layout = buildStorageLayout(
      [],
      [volume("/", "/dev/nvme0n1p2", "btrfs")],
      drives,
      [],
    );
    expect(layout.poolStacks).toEqual([]);
    expect(layout.driveStacks[0].drive.name).toBe("nvme0n1");
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
