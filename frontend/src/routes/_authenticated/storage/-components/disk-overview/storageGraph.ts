import type {
  BtrfsFilesystem,
  BtrfsOfflinePool,
  EFIBootEntry,
  FilesystemInfo,
  LiveDiskRates,
  MDArray,
  NFSMount,
  StorageBlockDevice,
} from "@/api";

import type { DriveInfo } from "./types";
import { findBackingDrive } from "./utils";

/** GPT type GUID of an EFI System Partition. */
export const ESP_TYPE = "c12a7328-f81f-11d2-ba4b-00a0c93ec93b";

export const DRIVE_HEIGHT = 120;
export const PARTITION_LINE = 16;
export const MIDDLE_HEIGHT = 140;
export const OFFLINE_HEIGHT = 188;
export const VOLUME_HEIGHT = 56;
export const MISSING_HEIGHT = 56;
export const ROW_GAP = 24;
export const EXIT_SPACING = 14;
/** Vertical room a drive → volume line reserves across the middle column. */
const PASS_HEIGHT = 24;

interface PoolDisk {
  key: string;
  missing: boolean;
  name: string;
  sizeBytes: number;
}

export interface PoolSummary {
  allocatedBytes: number;
  disks: PoolDisk[];
  freeBytes: number;
  name: string;
  /** Data profile as shown to people, e.g. "RAID 10"; empty when unknown. */
  profile: string;
  uuid: string;
  volumes: string[];
}

/** "raid10" -> "RAID 10", "raid1c3" -> "RAID 1C3", "dup" -> "DUP". */
export const profileLabel = (profile: string) => {
  const raid = /^raid(\d+)(c\d+)?$/.exec(profile);
  if (raid) return `RAID ${raid[1]}${raid[2]?.toUpperCase() ?? ""}`;
  if (profile === "dup") return "DUP";
  return profile.charAt(0).toUpperCase() + profile.slice(1);
};

/**
 * A btrfs filesystem as a pool: its disks, data profile, space and mounts.
 * `index` numbers unlabelled pools; mount points belong to the volumes.
 */
export const describePool = (
  fs: BtrfsFilesystem,
  index: number,
): PoolSummary => {
  // A mixed allocation reports its kind as "data+metadata".
  const data = fs.allocation.find((a) => a.kind.split("+").includes("data"));
  const mountpoints = [...new Set(fs.mountpoints.map((m) => m.mountpoint))];
  return {
    uuid: fs.uuid,
    name: fs.label || `Storage pool ${index + 1}`,
    profile: data ? profileLabel(data.profile) : "",
    allocatedBytes: Math.max(fs.totalBytes - fs.unallocatedBytes, 0),
    freeBytes: Math.max(fs.totalBytes - fs.usedBytes, 0),
    disks: fs.devices.map((device) => ({
      key: device.path || String(device.id),
      name: device.path ? device.path.replace(/^\/dev\//, "") : "Missing disk",
      sizeBytes: device.sizeBytes,
      missing: device.missing,
    })),
    volumes: mountpoints,
  };
};

/** "btrfs" -> "Btrfs", "vfat" -> "FAT", "nfs4" -> "NFS"; others as reported. */
export const fsTypeLabel = (fstype: string) => {
  const known: Record<string, string> = {
    btrfs: "Btrfs",
    xfs: "XFS",
    vfat: "FAT",
    exfat: "exFAT",
    ntfs: "NTFS",
    zfs: "ZFS",
  };
  if (fstype.startsWith("nfs")) return "NFS";
  if (fstype.startsWith("cifs") || fstype === "smb3") return "SMB";
  return known[fstype] ?? fstype;
};

/** lsblk's device type: "raid1" -> "RAID 1", "lvm" -> "LVM", "crypt" -> "Encrypted". */
export const deviceTypeLabel = (type: string) => {
  if (type.startsWith("raid")) return profileLabel(type);
  const known: Record<string, string> = { lvm: "LVM", crypt: "Encrypted" };
  return known[type] ?? type;
};

/** "nvme0n1p2" on nvme0n1 -> "p2", "sda1" on sda -> "1", a whole disk -> "". */
export const partitionLabel = (name: string, drive: DriveInfo) => {
  const short = name.replace(/^\/dev\//, "");
  return short.startsWith(drive.name) ? short.slice(drive.name.length) : "";
};

interface Placed {
  id: string;
  column: 0 | 1 | 2;
  top: number;
  height: number;
  /** Position among the column's nodes, which picks the edge lane into it. */
  lane: number;
}

export type GraphNode = Placed &
  (
    | {
        kind: "drive";
        drive: DriveInfo;
        /** Partitions nothing mounts or builds on. */
        unused: StorageBlockDevice[];
      }
    | { kind: "missing"; count: number }
    | { kind: "pool"; fs: BtrfsFilesystem; pool: PoolSummary }
    | { kind: "offlinePool"; pool: BtrfsOfflinePool; missing: number }
    | {
        kind: "device";
        array?: MDArray;
        /** Layers between the members and this device, e.g. "LVM on Encrypted". */
        caption: string;
        label: string;
        sizeBytes: number;
        type: string;
      }
    | { kind: "server"; label: string; network: boolean }
    | {
        kind: "boot";
        copies: StorageBlockDevice[];
        /** Firmware boot entries that point at one of the copies. */
        entries: number;
        firmware: boolean;
        mounted?: StorageBlockDevice;
      }
    | { kind: "volume"; volume: FilesystemInfo }
  );

export interface GraphEdge {
  id: string;
  from: string;
  to: string;
  label: string;
  /** Mounted or running: the edge carries a stream even at 0 B/s. */
  live: boolean;
  rates?: LiveDiskRates;
  /** Vertical offset of the exit from the source's centre, in px. */
  exit: number;
}

export interface StorageGraph {
  nodes: GraphNode[];
  edges: GraphEdge[];
  height: number;
}

interface GraphInput {
  arrays: MDArray[];
  bootEntries: EFIBootEntry[];
  btrfs: BtrfsFilesystem[];
  devices: StorageBlockDevice[];
  drives: DriveInfo[];
  filesystems: FilesystemInfo[];
  nfsMounts: NFSMount[];
  offline: BtrfsOfflinePool[];
}

/** The sum of the drives' rates, or nothing when none reports any. */
const sumRates = (drives: DriveInfo[]): LiveDiskRates | undefined => {
  const rated = drives.flatMap((drive) => drive.rates ?? []);
  if (rated.length === 0) return undefined;
  return rated.reduce((sum, r) => ({
    read_bytes_per_sec: sum.read_bytes_per_sec + r.read_bytes_per_sec,
    write_bytes_per_sec: sum.write_bytes_per_sec + r.write_bytes_per_sec,
    read_ops_per_sec: sum.read_ops_per_sec + r.read_ops_per_sec,
    write_ops_per_sec: sum.write_ops_per_sec + r.write_ops_per_sec,
  }));
};

const centre = (node: Placed) => node.top + node.height / 2;

/**
 * Lays `items` out in one column: each at its wanted top, pushed down until
 * it clears the one above by ROW_GAP. Items without a wish go last.
 */
const placeColumn = (items: { node: Placed; wanted?: number }[]) => {
  const ordered = items
    .map((item, index) => ({ ...item, index }))
    .sort(
      (a, b) =>
        (a.wanted ?? Infinity) - (b.wanted ?? Infinity) || a.index - b.index,
    );
  let bottom = -ROW_GAP;
  for (const { node, wanted } of ordered) {
    node.top = Math.max(wanted ?? bottom + ROW_GAP, bottom + ROW_GAP);
    bottom = node.top + node.height;
  }
  // Nodes that all wanted the same spot were pushed one way; lift the
  // column back so they sit around that spot, never above the top.
  const wished = ordered.filter((item) => item.wanted !== undefined);
  if (wished.length > 0) {
    const drift =
      wished.reduce((sum, { node, wanted }) => sum + node.top - wanted!, 0) /
      wished.length;
    const lift = Math.min(drift, ...ordered.map(({ node }) => node.top));
    for (const { node } of ordered) node.top -= lift;
  }
  return ordered.map(({ node }) => node);
};

/**
 * Every drive once, what the drives build in the middle, every mounted
 * volume on the right, and the edges between them with their liveness and
 * rates. Positions are in px from fixed card heights; the caller supplies
 * the horizontal layout.
 */
export const buildStorageGraph = ({
  arrays,
  bootEntries,
  btrfs,
  devices,
  drives,
  filesystems,
  nfsMounts,
  offline,
}: GraphInput): StorageGraph => {
  const nodes: GraphNode[] = [];
  const edges: GraphEdge[] = [];
  const byPath = new Map(devices.map((d) => [d.path, d]));
  const byId = new Map<string, GraphNode>();
  const claimed = new Set<string>();
  const used = new Set<string>();
  const place = { column: 0 as const, top: 0, height: 0, lane: 0 };

  const add = (node: GraphNode) => {
    nodes.push(node);
    byId.set(node.id, node);
    return node;
  };
  const link = (
    from: GraphNode,
    to: GraphNode,
    label: string,
    live: boolean,
    rates?: LiveDiskRates,
  ) => {
    const id = `${from.id}>${to.id}:${label}`;
    if (edges.some((edge) => edge.id === id)) return;
    edges.push({ id, from: from.id, to: to.id, label, live, rates, exit: 0 });
  };
  const driveNode = (path: string) => {
    const drive = findBackingDrive(path, drives);
    return drive ? byId.get(`drive:${drive.name}`) : undefined;
  };
  const driveOf = (node: GraphNode | undefined) =>
    node?.kind === "drive" ? node.drive : undefined;
  const volumeNode = (volume: FilesystemInfo) => {
    claimed.add(volume.mountpoint);
    return add({
      ...place,
      id: `volume:${volume.mountpoint}`,
      column: 2,
      height: VOLUME_HEIGHT,
      kind: "volume",
      volume,
    });
  };
  const missingNode = (id: string, count: number) =>
    add({
      ...place,
      id: `missing:${id}`,
      height: MISSING_HEIGHT,
      kind: "missing",
      count,
    });
  /** Partitions or whole disks under a virtual device, and the layers between. */
  const membersOf = (
    path: string,
    layers: string[] = [],
  ): { members: StorageBlockDevice[]; layers: string[] } => {
    const block = byPath.get(path);
    if (!block) return { members: [], layers };
    if (block.type === "part" || block.type === "disk")
      return { members: [block], layers };
    const found = block.parents.map((parent) =>
      membersOf(parent, [...layers, block.type]),
    );
    return {
      members: [...new Set(found.flatMap((f) => f.members))],
      layers: [...new Set(found.flatMap((f) => f.layers))],
    };
  };

  for (const drive of drives)
    add({
      ...place,
      id: `drive:${drive.name}`,
      height: DRIVE_HEIGHT,
      kind: "drive",
      drive,
      unused: [],
    });

  // Two or more EFI System Partitions are one boot set: the firmware can
  // start from any of them, Linux mounts at most one.
  const esps = devices.filter(
    (d) => d.type === "part" && d.partType === ESP_TYPE,
  );
  if (esps.length >= 2) {
    const mounted = esps.find((esp) => esp.mountpoints.length > 0);
    const boot = add({
      ...place,
      id: "boot",
      column: 1,
      height: MIDDLE_HEIGHT,
      kind: "boot",
      copies: esps,
      entries: bootEntries.filter((entry) =>
        esps.some(
          (esp) => esp.partUUID?.toLowerCase() === entry.partUUID.toLowerCase(),
        ),
      ).length,
      firmware: bootEntries.length > 0,
      mounted,
    });
    for (const esp of esps) {
      used.add(esp.path);
      const source = driveNode(esp.path);
      const drive = driveOf(source);
      if (!source || !drive) continue;
      const live = esp.mountpoints.length > 0;
      link(source, boot, partitionLabel(esp.name, drive), live, drive.rates);
    }
    if (mounted) {
      const rates = driveOf(driveNode(mounted.path))?.rates;
      for (const volume of filesystems.filter((v) => v.device === mounted.path))
        link(boot, volumeNode(volume), "", true, rates);
    }
  }

  btrfs.forEach((fs, index) => {
    const pool = describePool(fs, index);
    const live = fs.mountpoints.length > 0;
    const node = add({
      ...place,
      id: `pool:${fs.uuid}`,
      column: 1,
      height: MIDDLE_HEIGHT,
      kind: "pool",
      fs,
      pool,
    });
    const members: DriveInfo[] = [];
    for (const device of fs.devices) {
      if (!device.path) continue;
      used.add(device.path);
      const source = driveNode(device.path);
      const drive = driveOf(source);
      if (!source || !drive) continue;
      members.push(drive);
      link(source, node, partitionLabel(device.path, drive), live, drive.rates);
    }
    const missing = fs.devices.filter((device) => device.missing).length;
    if (missing > 0) link(missingNode(fs.uuid, missing), node, "", false);
    for (const volume of filesystems.filter(
      (v) => pool.volumes.includes(v.mountpoint) && !claimed.has(v.mountpoint),
    ))
      link(node, volumeNode(volume), "", live, sumRates(members));
  });

  for (const pool of offline) {
    const missing = pool.numDevices - pool.devices.length;
    const node = add({
      ...place,
      id: `pool:${pool.uuid}`,
      column: 1,
      height: OFFLINE_HEIGHT,
      kind: "offlinePool",
      pool,
      missing,
    });
    for (const path of pool.devices) {
      used.add(path);
      const source = driveNode(path);
      const drive = driveOf(source);
      if (source && drive)
        link(source, node, partitionLabel(path, drive), false);
    }
    if (missing > 0) link(missingNode(pool.uuid, missing), node, "", false);
  }

  for (const volume of filesystems) {
    if (claimed.has(volume.mountpoint)) continue;
    // Without an inventory entry (not loaded, or a device udev has not
    // probed yet) a volume whose name matches a drive is a plain partition.
    const block = byPath.get(volume.device);
    const plain = !block || block.type === "part" || block.type === "disk";
    const source = plain ? driveNode(volume.device) : undefined;
    const drive = driveOf(source);
    if (source && drive) {
      used.add(volume.device);
      link(
        source,
        volumeNode(volume),
        partitionLabel(volume.device, drive),
        true,
        drive.rates,
      );
      continue;
    }
    if (block && !plain) {
      const { members, layers } = membersOf(block.path);
      const id = `device:${block.path}`;
      const node =
        byId.get(id) ??
        add({
          ...place,
          id,
          column: 1,
          height: MIDDLE_HEIGHT,
          kind: "device",
          array: arrays.find((array) => array.path === block.path),
          caption:
            layers.length > 1 ? layers.map(deviceTypeLabel).join(" on ") : "",
          label: block.path,
          sizeBytes: block.sizeBytes,
          type: block.type,
        });
      const memberDrives: DriveInfo[] = [];
      for (const member of members) {
        used.add(member.path);
        const memberSource = driveNode(member.path);
        const memberDrive = driveOf(memberSource);
        if (!memberSource || !memberDrive) continue;
        memberDrives.push(memberDrive);
        link(
          memberSource,
          node,
          member.type === "part"
            ? partitionLabel(member.name, memberDrive)
            : "",
          true,
          memberDrive.rates,
        );
      }
      link(node, volumeNode(volume), "", true, sumRates(memberDrives));
      continue;
    }
    const nfs = nfsMounts.find((m) => m.mountpoint === volume.mountpoint);
    const network =
      nfs !== undefined ||
      volume.fstype.startsWith("nfs") ||
      volume.fstype.startsWith("cifs") ||
      volume.fstype === "smb3";
    const label = nfs ? `${nfs.server}:${nfs.exportPath}` : volume.device;
    const id = `server:${label}`;
    const node =
      byId.get(id) ??
      add({
        ...place,
        id,
        column: 1,
        height: MIDDLE_HEIGHT,
        kind: "server",
        label,
        network,
      });
    link(node, volumeNode(volume), "", true);
  }

  // A partition is in use when mounted, a parent of another device, or
  // claimed above (pool member, boot copy); the rest are listed on the drive.
  const parents = new Set(devices.flatMap((d) => d.parents));
  for (const block of devices) {
    if (
      block.type !== "part" ||
      block.mountpoints.length > 0 ||
      parents.has(block.path) ||
      used.has(block.path)
    )
      continue;
    const node = driveNode(block.path);
    if (node?.kind === "drive") node.unused.push(block);
  }

  // Column 0 in list order; the middle column over its sources; volumes
  // beside theirs. A drive → volume line reserves a band in the middle.
  let top = 0;
  for (const node of nodes.filter((n) => n.column === 0)) {
    node.top = top;
    if (node.kind === "drive")
      node.height = DRIVE_HEIGHT + PARTITION_LINE * node.unused.length;
    top = node.top + node.height + ROW_GAP;
  }
  const sourcesOf = (node: GraphNode) =>
    edges
      .filter((edge) => edge.to === node.id)
      .map((edge) => byId.get(edge.from)!);
  const passes = new Map<string, Placed>();
  const middle: { node: Placed; wanted?: number }[] = nodes
    .filter((n) => n.column === 1)
    .map((node) => {
      const sources = sourcesOf(node);
      return {
        node,
        wanted:
          sources.length > 0
            ? sources.reduce((sum, s) => sum + centre(s), 0) / sources.length -
              node.height / 2
            : undefined,
      };
    });
  for (const edge of edges) {
    const from = byId.get(edge.from)!;
    const to = byId.get(edge.to)!;
    if (from.column !== 0 || to.column !== 2) continue;
    const pass: Placed = {
      id: `pass:${edge.id}`,
      column: 1,
      top: 0,
      height: PASS_HEIGHT,
      lane: 0,
    };
    passes.set(edge.id, pass);
    middle.push({ node: pass, wanted: centre(from) - PASS_HEIGHT / 2 });
  }
  placeColumn(middle)
    .filter((placed) => byId.has(placed.id))
    .forEach((placed, lane) => {
      placed.lane = lane;
    });
  placeColumn(
    nodes
      .filter((n) => n.column === 2)
      .map((node) => {
        const edge = edges.find((e) => e.to === node.id);
        const source = edge && (passes.get(edge.id) ?? byId.get(edge.from));
        return {
          node,
          wanted: source ? centre(source) - node.height / 2 : undefined,
        };
      }),
  );

  for (const node of nodes) {
    const out = edges
      .filter((edge) => edge.from === node.id)
      .sort((a, b) => centre(byId.get(a.to)!) - centre(byId.get(b.to)!));
    out.forEach((edge, index) => {
      edge.exit = (index - (out.length - 1) / 2) * EXIT_SPACING;
    });
  }

  return {
    nodes,
    edges,
    height: Math.max(0, ...nodes.map((n) => n.top + n.height)),
  };
};
