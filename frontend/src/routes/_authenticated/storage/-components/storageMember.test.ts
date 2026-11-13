import { describe, expect, it } from "vitest";

import type { StorageBlockDevice } from "@/api";

import { describeMember, poolDeviceChoices } from "./storageMember";

const disk = (
  path: string,
  extra: Partial<StorageBlockDevice> = {},
): StorageBlockDevice => ({
  path,
  name: path.slice(5),
  kernelName: path.slice(5),
  type: "disk",
  sizeBytes: 1,
  fsType: "",
  mountpoints: [],
  parents: [],
  model: "",
  serial: "",
  transport: "",
  readOnly: false,
  freeBytes: 0,
  ...extra,
});

describe("describeMember", () => {
  it("words a membership as the bridge does", () => {
    expect(describeMember({ kind: "btrfs", name: "tank", uuid: "u" })).toBe(
      "btrfs pool tank",
    );
    expect(describeMember({ kind: "btrfs", uuid: "u" })).toBe("btrfs pool u");
    expect(describeMember({ kind: "md", name: "host:0" })).toBe(
      "md array host:0",
    );
    expect(describeMember({ kind: "md" })).toBe("an md array");
    expect(describeMember({ kind: "other", name: "zfs_member" })).toBe(
      "another storage set (zfs_member)",
    );
    expect(describeMember({ kind: "lvm", name: "vg0", uuid: "pv" })).toBe(
      "LVM volume group vg0",
    );
    expect(describeMember({ kind: "lvm", uuid: "pv" })).toBe(
      "an LVM volume group (physical volume pv)",
    );
  });
});

describe("poolDeviceChoices", () => {
  const devices = [
    disk("/dev/sdb"),
    disk("/dev/sdc", { fsType: "ext4" }),
    disk("/dev/sdd", { mountpoints: ["/srv"], busy: "Mounted at /srv" }),
    disk("/dev/sde", { busy: "Active swap", fsType: "swap" }),
    disk("/dev/sdf"),
    disk("/dev/sdf1", { type: "part", parents: ["/dev/sdf"] }),
    disk("/dev/sdg", { inFstab: true }),
    disk("/dev/sdh", {
      fsType: "btrfs",
      member: { kind: "btrfs", name: "old" },
    }),
    disk("/dev/sdl", {
      fsType: "linux_raid_member",
      member: { kind: "md", name: "host:data" },
    }),
  ];
  const listed = (force: boolean) =>
    Object.fromEntries(
      poolDeviceChoices(devices, ["/dev/sdb"], force).map(
        ({ device, reason }) => [device.path, reason],
      ),
    );

  it("lists blocked devices with their reason and old filesystems only under force", () => {
    const members = {
      "/dev/sde": "Active swap",
      "/dev/sdf1": null,
      "/dev/sdg": "In /etc/fstab",
      "/dev/sdh": "Member of btrfs pool old",
      "/dev/sdl": "Member of md array host:data",
    };
    expect(listed(false)).toEqual(members);
    // Force adds the disk with an old filesystem and nothing else.
    expect(listed(true)).toEqual({ ...members, "/dev/sdc": null });
  });
});
