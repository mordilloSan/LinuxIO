import { describe, expect, it } from "vitest";

import type { BtrfsFilesystem, Mount } from "@/api";

import { consumers, subvolumeHostPath, subvolumeUses } from "./subvolumeUsage";

const mount = (
  mountpoint: string,
  subvol: string,
  extra: Partial<Mount> = {},
): Mount => ({
  mountpoint,
  subvol,
  device: "/dev/sdb",
  uuid: "uuid-1",
  subvolId: subvol === "" ? 5 : 300,
  options: [],
  ...extra,
});

const fs = (...mountpoints: Mount[]) =>
  ({ uuid: "uuid-1", mountpoints }) as unknown as BtrfsFilesystem;

describe("subvolumeHostPath", () => {
  it("finds a subvolume under the top level or the nearest mounted parent", () => {
    // A root filesystem on @ with @home mounted separately, the way
    // installers lay it out.
    const root = fs(mount("/", "@"), mount("/home", "@home"));
    expect(subvolumeHostPath(root, "@")).toBe("/");
    expect(subvolumeHostPath(root, "@/var/lib/docker")).toBe("/var/lib/docker");
    expect(subvolumeHostPath(root, "@home")).toBe("/home");
    expect(subvolumeHostPath(root, "@snapshots")).toBeUndefined();

    // A pool with its top level mounted, plus a subvolume mounted elsewhere.
    const pool = fs(mount("/mnt/pool", ""), mount("/home/me/docker", "docker"));
    expect(subvolumeHostPath(pool, "data/photos")).toBe(
      "/mnt/pool/data/photos",
    );
    expect(subvolumeHostPath(pool, "docker")).toBe("/home/me/docker");
    expect(subvolumeHostPath(pool, "docker/immich")).toBe(
      "/home/me/docker/immich",
    );
  });

  it("ignores LinuxIO's own top-level mounts and bind mounts", () => {
    const pool = fs(
      mount("/run/linuxio/btrfs/uuid-1", ""),
      mount("/srv/bind", "data", { subPath: "data/photos" }),
    );
    expect(subvolumeHostPath(pool, "data")).toBeUndefined();
  });

  it("accepts a top level reported as /", () => {
    expect(subvolumeHostPath(fs(mount("/mnt/pool", "/")), "data")).toBe(
      "/mnt/pool/data",
    );
  });
});

describe("subvolumeUses", () => {
  const hostPaths = new Map([
    ["@", "/"],
    ["data", "/mnt/pool/data"],
    ["docker", "/mnt/pool/docker"],
    ["docker/immich", "/mnt/pool/docker/immich"],
  ]);

  it("credits each share and Docker folder to the deepest subvolume holding it", () => {
    const uses = subvolumeUses(
      hostPaths,
      consumers(
        [
          { name: "media", properties: { path: "/mnt/pool/data/media/" } },
          { name: "printers", properties: {} },
        ],
        [{ path: "/mnt/pool/data", active: true, clients: [] }],
        ["/mnt/pool/docker/immich", "/home/me/docker"],
      ),
    );
    expect(uses.get("data")).toEqual(["NFS", "SMB"]);
    expect(uses.get("docker/immich")).toEqual(["Docker"]);
    expect(uses.get("docker")).toBeUndefined();
    // Outside every pool subvolume, the root subvolume holds it.
    expect(uses.get("@")).toEqual(["Docker"]);
  });

  it("does not match a sibling that only shares a prefix", () => {
    const uses = subvolumeUses(
      new Map([["data", "/mnt/pool/data"]]),
      consumers(
        [],
        [{ path: "/mnt/pool/data2", active: true, clients: [] }],
        [],
      ),
    );
    expect(uses.size).toBe(0);
  });
});
