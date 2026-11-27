import type {
  ForeignZramConfig,
  SwapDevice,
  SwapFileCreate,
  SwapFileRow,
  SwapLeft,
  SwapModeResult,
  SwapStatus,
  ZramGeneratorDevice,
} from "@/api";

const GIB = 1024 ** 3;
const MIB = 1024 ** 2;

export const zramDevice: SwapDevice = {
  kind: "zram",
  path: "/dev/zram0",
  priority: 100,
  size: 8 * GIB,
  used: 0,
};

export const swapFileDevice = (
  used = 0,
  fsType = "ext4",
  path = "/swap.img",
): SwapDevice => ({
  fs_type: fsType,
  kind: "file",
  path,
  priority: -2,
  size: 4 * GIB,
  used,
});

/** A swap partition, which LinuxIO lists and never turns off. */
export const swapPartitionDevice = (used = 0): SwapDevice => ({
  kind: "partition",
  path: "/dev/vdb1",
  priority: -3,
  size: 2 * GIB,
  used,
});

/** An active swap file of 4 GiB that /etc/fstab names and LinuxIO acts on. */
export const swapFileRow = (
  overrides: Partial<SwapFileRow> = {},
): SwapFileRow => ({
  active: true,
  free_space: 40 * GIB,
  fs_type: "ext4",
  in_fstab: true,
  locked_reason: "",
  max_size: 30 * GIB,
  path: "/swap.img",
  resize_blocked_reason: "",
  size: 4 * GIB,
  used: 0,
  ...overrides,
});

/** LinuxIO can create its swap file: 40 GiB free, 2 GiB kept free. */
export const swapFileCreate = (
  overrides: Partial<SwapFileCreate> = {},
): SwapFileCreate => ({
  blocked_reason: "",
  default_size: 8 * GIB,
  free_space: 40 * GIB,
  fs_type: "ext4",
  max_size: 38 * GIB,
  min_size: 256 * MIB,
  path: "/var/lib/linuxio/swap/swapfile",
  reserve: 2 * GIB,
  ...overrides,
});

/** A zram configuration file that is not LinuxIO's, and where its copy goes. */
export const foreignZramConfig = (
  path = "/etc/default/zramswap",
): ForeignZramConfig => ({ copy_path: `${path}.before-linuxio`, path });

/** A device another zram-generator file defines. */
export const generatorDevice = (
  overrides: Partial<ZramGeneratorDevice> = {},
): ZramGeneratorDevice => ({
  device: "zram1",
  max_zram_size: "",
  path: "/etc/systemd/zram-generator.conf.d/50-big.conf",
  zram_fraction: "",
  zram_size: "2048",
  ...overrides,
});

/** A mode change's result: the status after it and what it left as it was. */
export const swapModeResult = (
  status: SwapStatus,
  left: SwapLeft[] = [],
): SwapModeResult => ({ ...status, left });

/** A healthy zram-only host with 16 GiB of RAM; each test overrides what it needs. */
export const swapStatus = (
  overrides: Partial<SwapStatus> = {},
): SwapStatus => ({
  can_drain: false,
  create: swapFileCreate(),
  devices: [zramDevice],
  drain_blocked_reason: "nothing_parked",
  mem_available: 12 * GIB,
  mem_total: 16 * GIB,
  mode: "zram",
  mode_saved_for_boot: true,
  page_cluster: 0,
  presets: [
    { mode: "zram", swappiness: 180 },
    { mode: "zswap", swappiness: 60 },
    { mode: "disk", swappiness: 60 },
  ],
  recommended_mode: "zram",
  recommended_swappiness: 180,
  swap_files: [],
  swap_total: 8 * GIB,
  swap_used: 0,
  swappiness: 180,
  tuned_active: false,
  tuned_profile: "",
  watermark_boost_factor: 0,
  watermark_scale_factor: 125,
  zram_available: true,
  zram_provider: "zram-tools",
  zram_devices: [],
  zram_generator_devices: [],
  zram_planned_size: 8 * GIB,
  zram_unavailable_reason: "",
  zswap: {
    available: true,
    compressor: "zstd",
    enabled: false,
    max_pool_percent: 20,
  },
  ...overrides,
});
