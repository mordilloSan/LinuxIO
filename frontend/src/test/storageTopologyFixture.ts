import type {
  ContainerInfo,
  FilesystemInfo,
  StorageBlockDevice,
  StorageTopologyInventory,
  VirtualMachine,
  VolumeGroup,
} from "@/api";

import { topologyContainers, topologyLive } from "./dockerTopologyFixture";

const GiB = 1024 ** 3;
export const storageBlock = (
  path: string,
  type: string,
  size: number,
  parents: string[] = [],
  mountpoints: string[] = [],
): StorageBlockDevice => ({
  path,
  name: path.split("/").at(-1)!,
  kernelName: path.split("/").at(-1)!,
  type,
  sizeBytes: size * GiB,
  parents,
  mountpoints,
  fsType: mountpoints.length ? "ext4" : "",
  model: "",
  serial: "",
  transport: "",
  readOnly: false,
});
export const storageInventory: StorageTopologyInventory = {
  devices: [
    {
      ...storageBlock("/dev/nvme0n1", "disk", 1024),
      model: "Samsung SSD 990 PRO",
      serial: "SAMPLE-NVME",
      transport: "nvme",
    },
    {
      ...storageBlock(
        "/dev/nvme0n1p1",
        "part",
        0.5,
        ["/dev/nvme0n1"],
        ["/boot/efi"],
      ),
      fsType: "vfat",
    },
    {
      ...storageBlock("/dev/nvme0n1p2", "part", 1023.5, ["/dev/nvme0n1"]),
      fsType: "LVM2_member",
    },
    {
      ...storageBlock(
        "/dev/mapper/system-root",
        "lvm",
        64,
        ["/dev/nvme0n1p2"],
        ["/"],
      ),
      kernelName: "dm-0",
    },
    {
      ...storageBlock(
        "/dev/mapper/system-apps",
        "lvm",
        800,
        ["/dev/nvme0n1p2"],
        ["/srv/appdata"],
      ),
      kernelName: "dm-1",
    },
    {
      ...storageBlock("/dev/sda", "disk", 4096),
      model: "WD Red Plus",
      transport: "sata",
      serial: "SAMPLE-HDD",
    },
    storageBlock("/dev/sda1", "part", 4096, ["/dev/sda"], ["/srv/media"]),
    {
      ...storageBlock("/dev/sdb", "disk", 2048),
      model: "Samsung Portable SSD",
      transport: "usb",
      serial: "SAMPLE-USB",
    },
  ],
  mounts: [
    {
      path: "/",
      device: "/dev/mapper/system-root",
      fsType: "ext4",
      readOnly: false,
    },
    {
      path: "/boot/efi",
      device: "/dev/nvme0n1p1",
      fsType: "vfat",
      readOnly: false,
    },
    {
      path: "/srv/appdata",
      device: "/dev/mapper/system-apps",
      fsType: "ext4",
      readOnly: false,
    },
    {
      path: "/srv/media",
      device: "/dev/sda1",
      fsType: "ext4",
      readOnly: false,
    },
    {
      path: "/mnt/backup",
      device: "nas.local:/backup",
      fsType: "nfs4",
      readOnly: true,
    },
    { path: "/run", device: "tmpfs", fsType: "tmpfs", readOnly: false },
  ],
};
export const storageGroups: VolumeGroup[] = [
  {
    name: "system",
    size: 1023.5 * GiB,
    free: 159.5 * GiB,
    pvCount: 1,
    lvCount: 2,
    pvNames: ["/dev/nvme0n1p2"],
    attributes: "wz--n-",
  },
];
const mount = (source: string, destination: string, readOnly = false) => ({
  Type: "bind",
  Source: source,
  Destination: destination,
  Mode: readOnly ? "ro" : "rw",
  Propagation: "rprivate",
  RW: !readOnly,
});
export const storageContainers: ContainerInfo[] = topologyContainers
  .filter((container) => container.Id !== "redis-id")
  .map((container) => ({
    ...container,
    Mounts:
      container.Id === "jellyfin-id"
        ? [
            mount("/srv/media", "/media", true),
            mount("/srv/appdata/jellyfin", "/config"),
          ]
        : container.Id === "backup-id"
          ? [mount("/mnt/backup", "/backup", true)]
          : [mount(`/srv/appdata/${container.Names[0].slice(1)}`, "/data")],
  }));
export const storageVMs: VirtualMachine[] = [
  {
    name: "home-assistant",
    state: "running",
    vcpus: 2,
    memoryMB: 4096,
    diskGB: 32,
    autostart: true,
    hasGraphics: true,
    ownedDisks: ["/srv/appdata/vms/home-assistant.qcow2"],
    disks: [
      {
        device: "disk",
        target: "vda",
        path: "/srv/appdata/vms/home-assistant.qcow2",
        owned: true,
      },
    ],
  },
];
export const storageFilesystems: FilesystemInfo[] = storageInventory.mounts
  .filter((item) => item.fsType !== "tmpfs")
  .map((item) => {
    const block = storageInventory.devices.find((device) =>
      device.mountpoints.includes(item.path),
    );
    const total = block?.sizeBytes ?? 2048 * GiB;
    const fraction = item.path === "/srv/media" ? 0.64 : 0.32;
    return {
      device: item.device,
      mountpoint: item.path,
      fstype: item.fsType,
      readOnly: item.readOnly,
      total,
      used: total * fraction,
      free: total * (1 - fraction),
      usedPercent: fraction * 100,
    };
  });
export const storageLive = (timestamp = Date.now()) => ({
  ...topologyLive(timestamp),
  filesystems: storageFilesystems,
  disks: {
    nvme0n1: {
      read_bytes_per_sec: 4.8 * 1024 ** 2,
      write_bytes_per_sec: 1.2 * 1024 ** 2,
      read_ops_per_sec: 80,
      write_ops_per_sec: 32,
    },
    sda: {
      read_bytes_per_sec: 12.5 * 1024 ** 2,
      write_bytes_per_sec: 84000,
      read_ops_per_sec: 120,
      write_ops_per_sec: 4,
    },
    sdb: {
      read_bytes_per_sec: 0,
      write_bytes_per_sec: 0,
      read_ops_per_sec: 0,
      write_ops_per_sec: 0,
    },
  },
  smart: {
    nvme0n1: { data: { smart_status: "PASSED", temperature_celsius: 38 } },
    sda: { data: { smart_status: "PASSED", temperature_celsius: 33 } },
  },
});
