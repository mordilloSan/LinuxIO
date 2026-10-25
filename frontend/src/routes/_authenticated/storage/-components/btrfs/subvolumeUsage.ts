import type {
  BtrfsFilesystem,
  BtrfsSnapshotRule,
  NFSExport,
  SambaShare,
} from "@/api";

import { TRANSIENT_TOP } from "./btrfsShared";

const trimSlashes = (path: string) => path.replace(/^\/+|\/+$/g, "");

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
    // A bind mount shows a folder inside a subvolume, not the subvolume.
    if (mount.subPath || mount.mountpoint.startsWith(TRANSIENT_TOP)) continue;
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
