import type {
  ForeignZramConfig,
  SwapDevice,
  SwapFile,
  SwapLeft,
  SwapModeResult,
  SwapStatus,
  ZramGeneratorDevice,
} from "@/api";

const GIB = 1024 ** 3;

export const zramDevice: SwapDevice = {
  kind: "zram",
  path: "/dev/zram0",
  priority: 100,
  size: 8 * GIB,
  used: 0,
};

export const swapFileDevice = (used = 0, fsType = "ext4"): SwapDevice => ({
  fs_type: fsType,
  kind: "file",
  path: "/swap.img",
  priority: -2,
  size: 4 * GIB,
  used,
});

export const swapFile = (overrides: Partial<SwapFile> = {}): SwapFile => ({
  active: true,
  disk_hint: "",
  fs_type: "ext4",
  managed: true,
  path: "/swap.img",
  unmanaged_reason: "",
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
  devices: [zramDevice],
  drain_blocked_reason: "nothing_parked",
  mem_available: 12 * GIB,
  mem_total: 16 * GIB,
  mode: "zram",
  page_cluster: 0,
  presets: [
    { mode: "zram", swappiness: 180 },
    { mode: "zswap", swappiness: 60 },
    { mode: "disk", swappiness: 60 },
  ],
  recommended_mode: "zram",
  recommended_swappiness: 180,
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
