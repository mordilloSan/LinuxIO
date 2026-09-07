import type {
  ContainerInfo,
  FilesystemInfo,
  StorageBlockDevice,
  StorageMount,
  StorageTopologyInventory,
  VirtualMachine,
  VolumeGroup,
} from "@/api";
import { getContainerName } from "@/utils/dockerContainer";

export interface StorageSelection {
  node?: string;
}
export interface StorageUse {
  source: string;
  destination: string;
  readOnly?: boolean;
  mountId?: string;
}
export interface StorageNode {
  id: string;
  kind: "drive" | "volume" | "mount" | "container" | "vm";
  label: string;
  description: string;
  block?: StorageBlockDevice;
  filesystem?: FilesystemInfo;
  mountpoint?: string;
  mount?: StorageMount;
  group?: VolumeGroup;
  container?: ContainerInfo;
  vm?: VirtualMachine;
  uses?: StorageUse[];
  backing: StorageBlockDevice[];
  column: number;
  top: number;
}
export interface StorageEdge {
  from: string;
  to: string;
  path: string;
}

// Match complete path segments; /data must never claim /database. This is a
// reported-path relationship, not symlink resolution or per-file I/O attribution.
export function findStorageMount(
  source: string,
  mounts: string[],
): string | undefined {
  if (!source.startsWith("/")) return;
  const parts: string[] = [];
  for (const part of source.split("/")) {
    if (part === "..") parts.pop();
    else if (part && part !== ".") parts.push(part);
  }
  const path = `/${parts.join("/")}`;
  return mounts
    .filter(
      (mount) =>
        mount === "/" || path === mount || path.startsWith(`${mount}/`),
    )
    .sort((a, b) => b.length - a.length || a.localeCompare(b))[0];
}

export function buildStorageTopology(
  snapshot: StorageTopologyInventory,
  filesystems: FilesystemInfo[],
  groups: VolumeGroup[],
  containers: ContainerInfo[],
  vms: VirtualMachine[],
) {
  const { devices } = snapshot;
  const byPath = new Map(devices.map((device) => [device.path, device]));
  const backing = (device?: StorageBlockDevice): StorageBlockDevice[] => {
    const found = new Map<string, StorageBlockDevice>();
    const visit = (item: StorageBlockDevice) => {
      if (found.has(item.path)) return;
      found.set(item.path, item);
      for (const parent of item.parents) {
        const next = byPath.get(parent);
        if (next) visit(next);
      }
    };
    if (device) visit(device);
    return [...found.values()].reverse();
  };
  const nodes: StorageNode[] = [];
  const links = new Map<string, { from: string; to: string }>();
  const link = (from: string, to: string) =>
    links.set(JSON.stringify([from, to]), { from, to });
  const roots = devices.filter((device) => device.parents.length === 0);
  for (const block of roots) {
    nodes.push({
      id: `drive:${block.path}`,
      kind: "drive",
      label: block.kernelName,
      description: block.model || block.type,
      block,
      backing: [block],
      column: 0,
      top: 0,
    });
  }

  const mounts = new Map(snapshot.mounts.map((mount) => [mount.path, mount]));
  const addStorage = (
    id: string,
    blocks: StorageBlockDevice[],
    mount?: StorageMount,
  ) => {
    const block = blocks[0];
    const mountpoint = mount?.path;
    const filesystem = filesystems.find(
      (fs) => fs.mountpoint === mountpoint && fs.device === mount?.device,
    );
    const chain = [
      ...new Map(
        blocks.flatMap(backing).map((item) => [item.path, item]),
      ).values(),
    ];
    const group = chain.some((item) => item.type === "lvm")
      ? groups.find((item) =>
          item.pvNames?.some((path) =>
            chain.some(
              (member) =>
                member.path === path || `/dev/${member.kernelName}` === path,
            ),
          ),
        )
      : undefined;
    nodes.push({
      id,
      kind: mountpoint ? "mount" : "volume",
      label: mountpoint ?? block.name,
      description: [
        mount?.fsType || block?.fsType || block?.type || "Filesystem",
        group?.name,
      ]
        .filter(Boolean)
        .join(" · "),
      block,
      filesystem,
      mountpoint,
      mount,
      group,
      backing: chain,
      column: 1,
      top: 0,
    });
    for (const root of roots) {
      if (chain.some((item) => item.path === root.path))
        link(`drive:${root.path}`, id);
    }
  };
  const addMount = (mount: StorageMount) => {
    const id = `mount:${mount.path}`;
    if (nodes.some((node) => node.id === id)) return;
    addStorage(
      id,
      devices.filter(
        (device) =>
          device.mountpoints.includes(mount.path) ||
          device.path === mount.device ||
          `/dev/${device.kernelName}` === mount.device,
      ),
      mount,
    );
  };
  // Kernel and memory mounts still act as path boundaries; display them only
  // when an application uses them, so /data/cache on tmpfs cannot look disk-backed.
  const hiddenFilesystems = new Set([
    "proc",
    "sysfs",
    "devtmpfs",
    "devpts",
    "tmpfs",
    "cgroup",
    "cgroup2",
    "pstore",
    "securityfs",
    "debugfs",
    "tracefs",
    "configfs",
    "overlay",
    "squashfs",
    "ramfs",
    "bpf",
    "nsfs",
    "autofs",
    "fusectl",
    "mqueue",
    "hugetlbfs",
    "binfmt_misc",
  ]);
  for (const mount of mounts.values())
    if (!hiddenFilesystems.has(mount.fsType)) addMount(mount);
  const parents = new Set(devices.flatMap((device) => device.parents));
  for (const block of devices) {
    if (
      !parents.has(block.path) &&
      !nodes.some(
        (node) =>
          node.column === 1 &&
          node.backing.some((item) => item.path === block.path),
      ) &&
      block.parents.length > 0
    )
      addStorage(`volume:${block.path}`, [block]);
  }
  const mountPaths = [...mounts.keys()];
  const addApplication = (node: StorageNode) => {
    if (!node.uses?.length) return;
    for (const use of node.uses) {
      if (node.kind === "vm" && use.source.startsWith("/dev/")) {
        const block = devices.find(
          (device) =>
            device.path === use.source ||
            `/dev/${device.kernelName}` === use.source,
        );
        if (block) {
          const id = `volume:${block.path}`;
          if (!nodes.some((item) => item.id === id)) addStorage(id, [block]);
          use.mountId = id;
          link(id, node.id);
        }
        continue;
      }
      const mount = findStorageMount(use.source, mountPaths);
      if (mount !== undefined) {
        addMount(mounts.get(mount)!);
        use.mountId = `mount:${mount}`;
        link(use.mountId, node.id);
      }
    }
    nodes.push(node);
  };
  for (const container of containers) {
    addApplication({
      id: `container:${container.Id}`,
      kind: "container",
      label: getContainerName(container),
      description: container.State,
      container,
      backing: [],
      column: 2,
      top: 0,
      uses: (container.Mounts ?? [])
        .filter(
          (mount) =>
            ["bind", "volume"].includes(mount.Type) &&
            mount.Source.startsWith("/"),
        )
        .map((mount) => ({
          source: mount.Source,
          destination: mount.Destination,
          readOnly: !mount.RW,
        })),
    });
  }
  for (const vm of vms) {
    addApplication({
      id: `vm:${vm.name}`,
      kind: "vm",
      label: vm.name,
      description: vm.state,
      vm,
      backing: [],
      column: 2,
      top: 0,
      uses: (vm.disks ?? [])
        .filter((disk) => disk.path.startsWith("/"))
        .map((disk) => ({ source: disk.path, destination: disk.target })),
    });
  }
  // ponytail: fixed three-column layout; add routing if dense hosts need it.
  nodes.sort(
    (a, b) =>
      a.column - b.column ||
      a.label.localeCompare(b.label) ||
      a.id.localeCompare(b.id),
  );
  const columns = [0, 1, 2].map((column) =>
    nodes.filter((node) => node.column === column),
  );
  const height = Math.max(
    520,
    Math.max(...columns.map((column) => column.length)) * 144 + 88,
  );
  for (const column of columns) {
    for (const [index, node] of column.entries())
      node.top = 76 + (index * (height - 220)) / Math.max(1, column.length - 1);
  }
  const inventory = new Map(nodes.map((node) => [node.id, node]));
  const edges: StorageEdge[] = [...links.values()].map(({ from, to }) => {
    const start = inventory.get(from)!;
    const end = inventory.get(to)!;
    const x1 = 270 + start.column * 350;
    const x2 = 30 + end.column * 350;
    return {
      from,
      to,
      path: `M ${x1} ${start.top + 60} C ${x1 + 55} ${start.top + 60}, ${x2 - 55} ${end.top + 60}, ${x2} ${end.top + 60}`,
    };
  });
  return { nodes, columns, edges, height, inventory };
}

export function relatedStorageNodes(
  id: string | undefined,
  edges: StorageEdge[],
): Set<string> {
  const related = new Set<string>();
  if (!id) return related;
  related.add(id);
  for (const [from, to] of [
    ["from", "to"],
    ["to", "from"],
  ] as const) {
    const seen = new Set<string>([id]);
    const pending = [id];
    for (const current of pending) {
      for (const edge of edges) {
        if (edge[from] === current && !seen.has(edge[to])) {
          seen.add(edge[to]);
          related.add(edge[to]);
          pending.push(edge[to]);
        }
      }
    }
  }
  return related;
}
