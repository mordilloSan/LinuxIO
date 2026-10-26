import { describe, expect, it } from "vitest";

import type {
  BtrfsFilesystem,
  BtrfsOfflinePool,
  StorageBlockDevice,
} from "@/api";
import { storageBlock } from "@/test/storageTopologyFixture";

import { diskLayout, initializeBlocker } from "./partitionLayout";

const MiB = 1024 ** 2;
const disk = {
  ...storageBlock("/dev/sda", "disk", 10),
  ptType: "gpt",
  freeRegions: [
    { startBytes: 1 * MiB, sizeBytes: 100 * MiB },
    { startBytes: 500 * MiB, sizeBytes: 200 * MiB },
  ],
};
const part = (
  n: number,
  startMiB: number,
  extra: Partial<ReturnType<typeof storageBlock>> = {},
) => ({
  ...storageBlock(`/dev/sda${n}`, "part", 1, ["/dev/sda"]),
  partNumber: n,
  startBytes: startMiB * MiB,
  ...extra,
});
const pool = (label: string, uuid: string, devices: string[]) =>
  ({
    label,
    uuid,
    devices: devices.map((path) => ({ path })),
  }) as never as BtrfsFilesystem;

describe("diskLayout", () => {
  it("interleaves partitions and free regions by start", () => {
    const rows = diskLayout(
      disk,
      [
        disk,
        part(2, 300),
        part(1, 101),
        storageBlock("/dev/sdb1", "part", 1, ["/dev/sdb"]),
      ],
      [],
      [],
    );
    expect(
      rows.map((row) =>
        row.kind === "free" ? `free@${row.startBytes / MiB}` : row.device.path,
      ),
    ).toEqual(["free@1", "/dev/sda1", "/dev/sda2", "free@500"]);
  });

  it("gives a reason by priority: busy, fstab, then pool membership", () => {
    const devices = [
      disk,
      part(1, 101, { busy: "Mounted at /srv", inFstab: true }),
      part(2, 300, { inFstab: true }),
      part(3, 400),
    ];
    const rows = diskLayout(
      disk,
      devices,
      [pool("tank", "u1", ["/dev/sda2", "/dev/sda3"])],
      [],
    );
    const reasons = rows.flatMap((row) =>
      row.kind === "partition" ? [row.reason] : [],
    );
    expect(reasons).toEqual([
      "Mounted at /srv",
      "In /etc/fstab",
      "Member of btrfs pool tank",
    ]);
  });

  it("flags a member of an offline pool, naming it by uuid when unlabelled", () => {
    const offline = {
      label: "",
      uuid: "abc-123",
      devices: ["/dev/sda1"],
    } as BtrfsOfflinePool;
    const rows = diskLayout(disk, [disk, part(1, 101)], [], [offline]);
    expect(rows.find((row) => row.kind === "partition")).toMatchObject({
      reason: "Member of btrfs pool abc-123",
    });
  });
});

describe("initializeBlocker", () => {
  const blocker = (
    target: StorageBlockDevice,
    devices: StorageBlockDevice[],
    btrfs: BtrfsFilesystem[] = [],
    offline: BtrfsOfflinePool[] = [],
  ) =>
    initializeBlocker(
      target,
      diskLayout(target, devices, btrfs, offline),
      btrfs,
      offline,
    );

  it("returns the first partition reason, the read-only state, or null", () => {
    expect(
      blocker(disk, [
        disk,
        part(1, 101),
        part(2, 300, { busy: "Active swap" }),
      ]),
    ).toBe("Active swap");
    expect(blocker({ ...disk, readOnly: true }, [])).toBe("Read-only disk");
    expect(blocker(disk, [disk, part(1, 101)])).toBeNull();
  });

  it("blocks a disk that is itself in /etc/fstab", () => {
    const data = { ...disk, inFstab: true };
    expect(blocker(data, [data])).toBe("In /etc/fstab");
  });

  it("blocks a whole-disk member of an online or offline pool", () => {
    expect(blocker(disk, [disk], [pool("tank", "u1", ["/dev/sda"])])).toBe(
      "Member of btrfs pool tank",
    );
    const cold = {
      label: "",
      uuid: "abc-123",
      devices: ["/dev/sda"],
    } as BtrfsOfflinePool;
    expect(blocker(disk, [disk], [], [cold])).toBe(
      "Member of btrfs pool abc-123",
    );
  });
});
