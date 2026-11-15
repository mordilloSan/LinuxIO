import { describe, expect, it } from "vitest";

import type { BtrfsFilesystem, Mount } from "@/api";

import {
  consumers,
  poolFolder,
  subvolumeFolderProblem,
  subvolumeHostPath,
  subvolumeNameProblem,
  subvolumeUses,
} from "./subvolumeUsage";

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
  // As the backend reports it: the folder the mount shows, which for a
  // subvolume mount is the subvolume itself.
  subPath: subvol.replace(/^\/+/, ""),
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

describe("poolFolder", () => {
  it("prefers the top-level mount, then the shallowest subvolume mount", () => {
    expect(poolFolder(fs(mount("/home", "@home"), mount("/", "@")))).toBe(
      "/home",
    );
    expect(
      poolFolder(fs(mount("/mnt/media", "media/x"), mount("/mnt/pool", ""))),
    ).toBe("/mnt/pool");
    expect(
      poolFolder(fs(mount("/mnt/deep", "a/b"), mount("/mnt/media", "media"))),
    ).toBe("/mnt/media");
  });

  it("takes a writable mount before a read-only one", () => {
    expect(
      poolFolder(
        fs(
          mount("/mnt/pool-ro", "", { options: ["ro", "noatime"] }),
          mount("/mnt/data", "data", { options: ["rw"] }),
        ),
      ),
    ).toBe("/mnt/data");
    expect(poolFolder(fs(mount("/mnt/pool-ro", "", { options: ["ro"] })))).toBe(
      "/mnt/pool-ro",
    );
  });

  it("skips LinuxIO's own top-level mounts and bind mounts", () => {
    expect(
      poolFolder(
        fs(
          mount("/run/linuxio/btrfs/uuid-1", ""),
          mount("/srv/bind", "data", { subPath: "data/photos" }),
        ),
      ),
    ).toBeUndefined();
  });
});

describe("subvolumeFolderProblem", () => {
  it("refuses a space or a comma below the pool's mount, not in the mountpoint", () => {
    const pool = fs(mount("/mnt/my pool", ""), mount("/mnt/data", "data"));
    expect(subvolumeFolderProblem(pool, "/mnt/my pool")).toBeUndefined();
    expect(subvolumeFolderProblem(pool, "/mnt/my pool/media")).toBeUndefined();
    expect(subvolumeFolderProblem(pool, "/mnt/data/My Documents")).toMatch(
      /space or a comma/,
    );
    expect(subvolumeFolderProblem(pool, "/mnt/data/a,b/c")).toMatch(
      /space or a comma/,
    );
    // Not on the pool as far as the page knows: the server decides.
    expect(subvolumeFolderProblem(pool, "/srv/other dir")).toBeUndefined();
  });
});

describe("subvolumeNameProblem", () => {
  it("accepts a plain name and names what is wrong with the others", () => {
    expect(subvolumeNameProblem("photos-2026")).toBeUndefined();
    expect(subvolumeNameProblem("a/b")).toBe("A name, not a path");
    expect(subvolumeNameProblem("..")).toBe("Not a usable name");
    expect(subvolumeNameProblem("my photos")).toBe("No spaces or commas");
    expect(subvolumeNameProblem("a,b")).toBe("No spaces or commas");
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
