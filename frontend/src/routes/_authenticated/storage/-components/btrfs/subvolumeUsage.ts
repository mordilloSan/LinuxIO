import type {
  BtrfsFilesystem,
  BtrfsSnapshotRule,
  Mount,
  NFSExport,
  SambaShare,
} from "@/api";

import { TRANSIENT_TOP } from "./btrfsShared";

const trimSlashes = (path: string) => path.replace(/^\/+|\/+$/g, "");

/**
 * A mount that lasts and shows a whole subvolume. LinuxIO's own top-level
 * mounts go at reboot, and a bind mount shows a folder inside a subvolume:
 * its subPath, the folder the mount shows, is not the subvolume's path.
 */
const showsSubvolume = (mount: Mount) =>
  !mount.mountpoint.startsWith(TRANSIENT_TOP) &&
  trimSlashes(mount.subPath ?? "") === trimSlashes(mount.subvol);

/**
 * The host folder a new subvolume is offered in: the pool's top-level mount,
 * or else the mount of its shallowest mounted subvolume, a writable mount
 * before a read-only one. Undefined when no lasting mount shows the pool.
 */
export const poolFolder = (fs: BtrfsFilesystem): string | undefined => {
  let best: { mountpoint: string; rank: number } | undefined;
  for (const mount of fs.mountpoints) {
    if (!showsSubvolume(mount)) continue;
    const root = trimSlashes(mount.subvol);
    const depth = root === "" ? 0 : root.split("/").length;
    // Nothing can be created through a read-only mount.
    const rank = depth + (mount.options.includes("ro") ? 1000 : 0);
    if (!best || rank < best.rank) {
      best = { mountpoint: mount.mountpoint, rank };
    }
  }
  return best?.mountpoint;
};

/**
 * Why a subvolume cannot be made in this host folder, or undefined when
 * nothing here says so: its path from the top level, the pool's part of the
 * folder, may not hold a comma or a space either. Only the part below the
 * pool's mount counts, so a mountpoint with a space is fine. The server has
 * the last word (ValidateSubvolumePath on the real path).
 */
export const subvolumeFolderProblem = (
  fs: BtrfsFilesystem,
  folder: string,
): string | undefined => {
  let best: Mount | undefined;
  for (const mount of fs.mountpoints) {
    const at = mount.mountpoint;
    const under = at === "/" || folder === at || folder.startsWith(`${at}/`);
    if (under && (!best || at.length > best.mountpoint.length)) best = mount;
  }
  if (!best) return undefined;
  const inPool = `${best.subPath ?? ""}/${folder.slice(best.mountpoint.length)}`;
  return /[\s,]/.test(inPool)
    ? "A folder with a space or a comma in its name cannot hold a subvolume"
    : undefined;
};

/**
 * Why a subvolume may not have this name, or undefined when it may: a comma
 * or a space would break the subvol= mount option and fstab, and the server
 * refuses them too (ValidateSubvolumePath).
 */
export const subvolumeNameProblem = (name: string): string | undefined => {
  if (name.includes("/")) return "A name, not a path";
  if (name === "." || name === "..") return "Not a usable name";
  if (/[\s,]/.test(name)) return "No spaces or commas";
  return undefined;
};

/**
 * Where the files of the subvolume at path appear on the host: under the
 * mount of that subvolume or of the nearest one above it. Undefined when no
 * lasting mount reaches it.
 */
export const subvolumeHostPath = (
  fs: BtrfsFilesystem,
  path: string,
): string | undefined => {
  let best: { mountpoint: string; root: string } | undefined;
  for (const mount of fs.mountpoints) {
    if (!showsSubvolume(mount)) continue;
    const root = trimSlashes(mount.subvol);
    if (root !== "" && path !== root && !path.startsWith(`${root}/`)) continue;
    if (!best || root.length > best.root.length) {
      best = { mountpoint: mount.mountpoint, root };
    }
  }
  if (!best) return undefined;
  const rest = best.root === "" ? path : path.slice(best.root.length + 1);
  if (!rest) return best.mountpoint;
  return best.mountpoint === "/" ? `/${rest}` : `${best.mountpoint}/${rest}`;
};

export type SubvolumeUse = "SMB" | "NFS" | "Docker";

interface Consumer {
  use: SubvolumeUse;
  path: string;
}

/** The host paths that SMB and NFS shares and Docker folders use. */
export const consumers = (
  samba: SambaShare[],
  nfs: NFSExport[],
  dockerFolders: string[],
): Consumer[] => [
  ...samba.flatMap((share) =>
    share.properties.path
      ? [{ use: "SMB" as const, path: share.properties.path }]
      : [],
  ),
  ...nfs.map((share) => ({ use: "NFS" as const, path: share.path })),
  ...dockerFolders.map((path) => ({ use: "Docker" as const, path })),
];

/**
 * What each subvolume, by its path in the pool, is used for: every consumer
 * counts for the deepest subvolume holding its path, since a nested
 * subvolume is snapshotted on its own.
 */
export const subvolumeUses = (
  hostPaths: ReadonlyMap<string, string>,
  uses: Consumer[],
): Map<string, SubvolumeUse[]> => {
  const result = new Map<string, SubvolumeUse[]>();
  for (const { use, path } of uses) {
    const target = path.length > 1 ? path.replace(/\/+$/, "") : path;
    let owner: string | undefined;
    let ownerPath = "";
    for (const [subvolume, hostPath] of hostPaths) {
      const holds =
        target === hostPath ||
        target.startsWith(hostPath === "/" ? "/" : `${hostPath}/`);
      if (holds && hostPath.length >= ownerPath.length) {
        owner = subvolume;
        ownerPath = hostPath;
      }
    }
    if (owner === undefined) continue;
    const list = result.get(owner) ?? [];
    if (!list.includes(use)) result.set(owner, [...list, use].sort());
  }
  return result;
};

/** The enabled automatic snapshot rule of a subvolume, if any. */
export const snapshotRuleFor = (
  rules: BtrfsSnapshotRule[],
  uuid: string,
  path: string,
) =>
  rules.find(
    (rule) => rule.uuid === uuid && rule.path === path && rule.enabled,
  );
