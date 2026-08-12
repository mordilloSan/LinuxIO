import { Icon } from "@iconify/react";
import {
  useQuery,
  useSuspenseQueries,
  useSuspenseQuery,
} from "@tanstack/react-query";
import {
  createContext,
  useContext,
  useMemo,
  useState,
  useSyncExternalStore,
  type ComponentProps,
  type ReactNode,
} from "react";

import type { GpuDevice } from "@/api";
import { linuxio } from "@/api";
import CardIconHeader from "@/components/cards/CardIconHeader";
import FrostedCard from "@/components/cards/FrostedCard";
import HardwareCard from "@/components/cards/HardwareCard";
import HistoryAreaChart from "@/components/charts/HistoryAreaChart";
import {
  formatChartClock,
  formatChartDay,
} from "@/components/charts/timeFormat";
import AppSelect from "@/components/ui/AppSelect";
import AppTypography from "@/components/ui/AppTypography";
import { useCapability } from "@/hooks/useCapabilities";
import { useAppTheme } from "@/theme";
import { cardHeight } from "@/theme/constants";
import { formatThroughput } from "@/utils/formaters";
import { formatGpuBytes, getGpuVendorLabel } from "@/utils/gpu";
import "./hardware-history.css";

import {
  HARDWARE_HISTORY_RANGES,
  type HardwareHistoryRangeId,
} from "./hardwareHistoryRanges";
import {
  hardwareGpuQueryOptions,
  hardwareStableQueryOptions,
} from "./hardwareQueryOptions";

type HistoryHoverListener = () => void;

/** Scoped external store for the four history charts' synchronized crosshair. */
export class HistoryHoverStore {
  private hoverTime: number | null = null;
  private readonly listeners = new Set<HistoryHoverListener>();

  getSnapshot = (): number | null => this.hoverTime;

  setHoverTime = (hoverTime: number | null): void => {
    if (this.hoverTime === hoverTime) return;
    this.hoverTime = hoverTime;
    for (const listener of this.listeners) listener();
  };

  subscribe = (listener: HistoryHoverListener): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };
}

const HistoryHoverContext = createContext<HistoryHoverStore | null>(null);

export const HistoryHoverProvider = ({ children }: { children: ReactNode }) => {
  const [store] = useState(() => new HistoryHoverStore());
  return (
    <HistoryHoverContext.Provider value={store}>
      {children}
    </HistoryHoverContext.Provider>
  );
};

const useHistoryHover = (): [number | null, (time: number | null) => void] => {
  const store = useContext(HistoryHoverContext);
  if (!store) {
    throw new Error(
      "History charts must be rendered inside HistoryHoverProvider",
    );
  }
  const hoverTime = useSyncExternalStore(
    store.subscribe,
    store.getSnapshot,
    store.getSnapshot,
  );
  return [hoverTime, store.setHoverTime];
};

type SynchronizedHistoryAreaChartProps = Omit<
  ComponentProps<typeof HistoryAreaChart>,
  "hoverTime" | "onHoverTimeChange"
>;

/** Only this chart leaf subscribes to crosshair movement. */
const SynchronizedHistoryAreaChart = (
  props: SynchronizedHistoryAreaChartProps,
) => {
  const [hoverTime, onHoverTimeChange] = useHistoryHover();
  return (
    <HistoryAreaChart
      {...props}
      hoverTime={hoverTime}
      onHoverTimeChange={onHoverTimeChange}
    />
  );
};

// ─── GPU helpers ──────────────────────────────────────────────────────────────

const getPrimaryGpu = (gpus: GpuDevice[] | undefined): GpuDevice | undefined =>
  gpus?.find((gpu) => gpu.boot_vga) ?? gpus?.[0];

const getGpuVramSummary = (gpu: GpuDevice | undefined): string => {
  if (!gpu) {
    return "—";
  }
  if (
    typeof gpu.memory_used_bytes === "number" &&
    typeof gpu.memory_total_bytes === "number"
  ) {
    return `${formatGpuBytes(gpu.memory_used_bytes)}/${formatGpuBytes(gpu.memory_total_bytes)}`;
  }
  return formatGpuBytes(gpu.memory_total_bytes);
};

const getGpuDriverSummary = (gpu: GpuDevice | undefined): string => {
  if (!gpu) {
    return "—";
  }

  return (
    gpu.driver_version || gpu.driver_module || gpu.driver || gpu.drm_card || "—"
  );
};

// ─── Info cards ───────────────────────────────────────────────────────────────

export const MotherboardInfoCard = () => {
  const theme = useAppTheme();
  const [{ data: motherboardInfo }, { data: systemInfo }] = useSuspenseQueries({
    queries: [
      { ...linuxio.system.get_motherboard_info, ...hardwareStableQueryOptions },
      { ...linuxio.system.get_system_info, ...hardwareStableQueryOptions },
    ],
  });

  return (
    <HardwareCard
      accentColor={theme.palette.primary.main}
      avatarIcon="bi:motherboard"
      rows={[
        {
          label: "Board",
          value:
            motherboardInfo?.baseboard?.model || systemInfo?.productName || "—",
          noWrap: false,
        },
        {
          label: "Vendor",
          value:
            motherboardInfo?.baseboard?.manufacturer ||
            systemInfo?.productVendor ||
            "—",
          noWrap: false,
        },
        {
          label: "Type",
          value: systemInfo?.chassisType || "—",
        },
        {
          label: "Version",
          value: systemInfo?.productVersion || "—",
          noWrap: false,
        },
      ]}
      subtitle="Board & system details"
      title="Motherboard"
    />
  );
};

export const CPUDetailsCard = () => {
  const theme = useAppTheme();
  const [{ data: cpuInfo }, { data: systemInfo }] = useSuspenseQueries({
    queries: [
      { ...linuxio.system.get_cpu_info, ...hardwareStableQueryOptions },
      { ...linuxio.system.get_system_info, ...hardwareStableQueryOptions },
    ],
  });

  return (
    <HardwareCard
      accentColor={theme.palette.primary.main}
      avatarIcon="ph:cpu"
      rows={[
        {
          label: "CPU",
          value: systemInfo?.cpuSummary || cpuInfo?.modelName || "—",
          noWrap: false,
        },
        {
          label: "Vendor",
          value: cpuInfo?.vendorId || "—",
        },
        {
          label: "Cores",
          value: cpuInfo ? `${cpuInfo.cores} Threads` : "—",
        },
        {
          label: "Speed",
          value:
            typeof cpuInfo?.mhz === "number" && Number.isFinite(cpuInfo.mhz)
              ? `${Math.round(cpuInfo.mhz)} MHz`
              : "—",
        },
      ]}
      subtitle="Processor specifications"
      title="CPU"
    />
  );
};

export const BIOSInfoCard = () => {
  const theme = useAppTheme();
  const [{ data: motherboardInfo }, { data: systemInfo }] = useSuspenseQueries({
    queries: [
      { ...linuxio.system.get_motherboard_info, ...hardwareStableQueryOptions },
      { ...linuxio.system.get_system_info, ...hardwareStableQueryOptions },
    ],
  });

  return (
    <HardwareCard
      accentColor={theme.palette.warning.main}
      avatarIcon="mdi:chip"
      rows={[
        {
          label: "Vendor",
          value: motherboardInfo?.bios?.vendor || systemInfo?.biosVendor || "—",
          noWrap: false,
        },
        {
          label: "Version",
          value:
            motherboardInfo?.bios?.version || systemInfo?.biosVersion || "—",
          noWrap: false,
        },
        {
          label: "Date",
          value: systemInfo?.biosDate || "—",
        },
        {
          label: "Board",
          value:
            motherboardInfo?.baseboard?.model || systemInfo?.productName || "—",
          noWrap: false,
        },
      ]}
      subtitle="Firmware information"
      title="BIOS"
    />
  );
};

export const GPUInfoCard = () => {
  const theme = useAppTheme();
  const [selectedGpuAddress, setSelectedGpuAddress] = useState("");
  const { data: gpus } = useSuspenseQuery({
    ...linuxio.system.get_gpu_info,
    ...hardwareGpuQueryOptions,
    refetchInterval: 15_000,
  });

  const primaryGpu = useMemo(
    () =>
      gpus?.find((gpu) => gpu.address === selectedGpuAddress) ??
      getPrimaryGpu(gpus),
    [gpus, selectedGpuAddress],
  );
  const gpuCount = gpus?.length ?? 0;
  const selectedValue = primaryGpu?.address ?? "";

  return (
    <HardwareCard
      accentColor={theme.palette.primary.main}
      actions={
        gpuCount > 1 ? (
          <AppSelect
            disableUnderline
            onChange={(event) => setSelectedGpuAddress(event.target.value)}
            size="small"
            style={{
              ["--app-select-input-font-size" as string]: "0.72rem",
              width: 190,
              color: theme.palette.text.secondary,
              fontSize: "0.78rem",
              lineHeight: theme.typography.body2.lineHeight,
            }}
            value={selectedValue}
            variant="standard"
          >
            {(gpus ?? []).map((gpu, index) => (
              <option key={gpu.address} value={gpu.address}>
                {`GPU ${index + 1}: ${gpu.model || getGpuVendorLabel(gpu)}`}
              </option>
            ))}
          </AppSelect>
        ) : undefined
      }
      avatarIcon="bi:gpu-card"
      rows={
        primaryGpu
          ? [
              {
                label: "GPU",
                value: primaryGpu.model || "—",
                noWrap: false,
              },
              {
                label: "Vendor",
                value: getGpuVendorLabel(primaryGpu),
                noWrap: false,
              },
              {
                label: "Driver",
                value: getGpuDriverSummary(primaryGpu),
                noWrap: false,
              },
              {
                label: "VRAM",
                value: getGpuVramSummary(primaryGpu),
              },
            ]
          : [
              { label: "Status", value: "No GPU detected" },
              { label: "Vendor", value: "—" },
              { label: "Driver", value: "—" },
              { label: "VRAM", value: "—" },
            ]
      }
      subtitle="Graphics card details"
      title="GPU"
    />
  );
};

// ─── History cards ───────────────────────────────────────────────────────────

type HardwareHistoryRange = (typeof HARDWARE_HISTORY_RANGES)[number];

// Each resolution's retention window is the range: 1m→1h, 10m→12h, 20m→24h,
// 120m→7d, 480m→30d. Asking for up to `limit` points without `from` therefore
// returns exactly the retained window with a cache-stable request.
const HISTORY_REQUEST_LIMIT = 400;

const rangeById = (id: HardwareHistoryRangeId): HardwareHistoryRange =>
  HARDWARE_HISTORY_RANGES.find((range) => range.id === id) ??
  HARDWARE_HISTORY_RANGES[0];

const useHistoryTimestampFormatter = (range: HardwareHistoryRange) =>
  useMemo(
    () =>
      range.id === "7d" || range.id === "30d"
        ? formatChartDay
        : formatChartClock,
    [range.id],
  );

const formatPercent = (value: number): string => `${value.toFixed(1)}%`;
const formatPercentTick = (value: number): string => `${Math.round(value)}%`;
const percentOfTotal = (value: number, total: number): number =>
  total > 0 ? (value / total) * 100 : 0;

/** Netdata-style hue ramp: purple for the bottom band up to red at the top. */
const stackBandColor = (index: number, count: number): string => {
  const ratio = count <= 1 ? 0 : index / (count - 1);
  return `hsl(${Math.round(280 - 280 * ratio)}, 70%, 55%)`;
};

const RangeSelect = ({
  value,
  onChange,
}: {
  value: HardwareHistoryRangeId;
  onChange: (id: HardwareHistoryRangeId) => void;
}) => {
  const theme = useAppTheme();

  return (
    <AppSelect
      className="history-range-select"
      disableUnderline
      onChange={(event) =>
        onChange(event.target.value as HardwareHistoryRangeId)
      }
      renderValue={(selected) => selected}
      size="small"
      style={{
        ["--app-select-input-font-size" as string]: "0.72rem",
        width: 46,
        opacity: 0.65,
        color: theme.palette.text.secondary,
        fontSize: "0.78rem",
        lineHeight: theme.typography.body2.lineHeight,
      }}
      value={value}
      variant="standard"
    >
      {HARDWARE_HISTORY_RANGES.map((range) => (
        <option key={range.id} value={range.id}>
          {range.label}
        </option>
      ))}
    </AppSelect>
  );
};

const HistoryCardShell = ({
  title,
  avatarIcon,
  headerRight,
  children,
}: {
  title: string;
  avatarIcon: string;
  headerRight?: ReactNode;
  children: ReactNode;
}) => {
  const theme = useAppTheme();

  return (
    <FrostedCard
      style={{
        minHeight: cardHeight,
        boxSizing: "border-box",
        height: "100%",
        display: "flex",
        flexDirection: "column",
        padding: 6,
      }}
    >
      <CardIconHeader
        icon={
          <Icon
            color={theme.palette.primary.main}
            height={28}
            icon={avatarIcon}
            width={28}
          />
        }
        right={headerRight}
        style={{ marginBottom: 8 }}
        title={title}
      />
      {children}
    </FrostedCard>
  );
};

const HistoryCardBody = ({
  children,
  message,
}: {
  children: ReactNode;
  message: string | null;
}) => {
  const theme = useAppTheme();

  return message ? (
    <div
      style={{
        flex: 1,
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        color: theme.palette.text.secondary,
        padding: 16,
      }}
    >
      <AppTypography align="center" variant="body2">
        {message}
      </AppTypography>
    </div>
  ) : (
    <div style={{ flex: 1, minHeight: 0, padding: "0 4px 2px" }}>
      {children}
    </div>
  );
};

interface HistoryCardProps {
  /** Shared time range so the four cards stay synchronized. */
  rangeId: HardwareHistoryRangeId;
  onRangeChange: (id: HardwareHistoryRangeId) => void;
}

type HistoryLiveProps = Pick<HistoryCardProps, "rangeId">;

const historyCardMessage = (
  points: readonly unknown[] | undefined,
  isLoading: boolean,
  error: { message: string } | null,
  monitoringEnabled: boolean,
  monitoringReason: string | undefined,
): string | null => {
  if (!monitoringEnabled) {
    return monitoringReason ?? "Monitoring agent is unavailable.";
  }
  if (error) {
    return `Historical data not available. ${error.message}`;
  }
  if (isLoading) {
    return "Loading history…";
  }
  if (!points || points.length === 0) {
    return "Historical data not available.";
  }
  return null;
};

const CPUHistoryLive = ({ rangeId }: HistoryLiveProps) => {
  const theme = useAppTheme();
  const range = rangeById(rangeId);
  const formatTimestamp = useHistoryTimestampFormatter(range);
  const { isEnabled, reason } = useCapability("monitoringAvailable");
  const { data, isLoading, error } = useQuery({
    ...linuxio.monitoring.get_cpu_history({
      resolution: range.resolution,
      limit: HISTORY_REQUEST_LIMIT,
    }),
    enabled: isEnabled,
    refetchInterval: range.refetchMs,
    placeholderData: (previous) => previous,
  });

  const message = historyCardMessage(data, isLoading, error, isEnabled, reason);

  // Most recent sample that reports per-core data decides the core count;
  // older agents (or old rows) fall back to the single average series.
  const coreCount = useMemo(() => {
    const points = data ?? [];
    for (let i = points.length - 1; i >= 0; i--) {
      const cores = points[i].cores_percent;
      if (cores && cores.length > 0) return cores.length;
    }
    return 0;
  }, [data]);

  const series = useMemo(() => {
    const points = data ?? [];
    if (coreCount > 1) {
      // Each band is the core's share of total capacity (core% / cores), so
      // the top of the stack traces the machine-wide usage percentage.
      return Array.from({ length: coreCount }, (_, core) => ({
        label: `Core ${core}`,
        color: stackBandColor(core, coreCount),
        points: points.map((point) => ({
          t: point.captured_at_ms,
          v: (point.cores_percent?.[core] ?? 0) / coreCount,
        })),
      }));
    }
    return [
      {
        label: "CPU",
        color: theme.palette.primary.main,
        points: points.map((point) => ({
          t: point.captured_at_ms,
          v: point.usage_percent,
        })),
      },
    ];
  }, [coreCount, data, theme.palette.primary.main]);

  const formatCoreValue = useMemo(
    () =>
      coreCount > 1
        ? (value: number) => formatPercent(value * coreCount)
        : formatPercent,
    [coreCount],
  );

  return (
    <HistoryCardBody message={message}>
      <SynchronizedHistoryAreaChart
        formatTick={formatPercentTick}
        formatTimestamp={formatTimestamp}
        formatValue={formatCoreValue}
        stacked={coreCount > 1}
        windowMs={range.windowMs}
        series={series}
        yMax={100}
      />
    </HistoryCardBody>
  );
};

const MemoryHistoryLive = ({ rangeId }: HistoryLiveProps) => {
  const theme = useAppTheme();
  const range = rangeById(rangeId);
  const formatTimestamp = useHistoryTimestampFormatter(range);
  const { isEnabled, reason } = useCapability("monitoringAvailable");
  const { data, isLoading, error } = useQuery({
    ...linuxio.monitoring.get_memory_history({
      resolution: range.resolution,
      limit: HISTORY_REQUEST_LIMIT,
    }),
    enabled: isEnabled,
    refetchInterval: range.refetchMs,
    placeholderData: (previous) => previous,
  });
  const message = historyCardMessage(data, isLoading, error, isEnabled, reason);
  const zfsColor = theme.palette.success.main;
  const dockerColor = theme.chart.rx;
  const buffersColor = theme.chart.tx;
  const series = useMemo(() => {
    const points = data ?? [];
    const hasDocker = points.some((point) => (point.docker_used_gb ?? 0) > 0);
    const hasZfs = points.some((point) => (point.zfs_arc_gb ?? 0) > 0);
    const hasBuffers = points.some((point) => (point.buffers_gb ?? 0) > 0);

    const layer = (
      label: string,
      color: string,
      valueGB: (point: (typeof points)[number]) => number,
    ) => ({
      label,
      color,
      points: points.map((point) => {
        const gb = Math.max(valueGB(point), 0);
        return {
          t: point.captured_at_ms,
          v: percentOfTotal(gb, point.total_gb),
          detail: `${gb.toFixed(1)} GB`,
        };
      }),
    });

    // Bottom-up bands; the agent already excludes cache and ZFS ARC from
    // "used", and Docker containers are carved out of it here.
    const memorySeries = [
      layer("Apps", theme.palette.primary.main, (point) =>
        hasDocker ? point.used_gb - (point.docker_used_gb ?? 0) : point.used_gb,
      ),
    ];
    if (hasDocker) {
      memorySeries.push(
        layer("Docker", dockerColor, (point) => point.docker_used_gb ?? 0),
      );
    }
    if (hasZfs) {
      memorySeries.push(
        layer("ZFS ARC", zfsColor, (point) => point.zfs_arc_gb ?? 0),
      );
    }
    if (hasBuffers) {
      memorySeries.push(
        layer("Buffers", buffersColor, (point) => point.buffers_gb ?? 0),
      );
    }
    return memorySeries;
  }, [buffersColor, data, dockerColor, theme.palette.primary.main, zfsColor]);

  return (
    <HistoryCardBody message={message}>
      <SynchronizedHistoryAreaChart
        formatTick={formatPercentTick}
        formatTimestamp={formatTimestamp}
        formatValue={formatPercent}
        stacked
        windowMs={range.windowMs}
        series={series}
        yMax={100}
      />
    </HistoryCardBody>
  );
};

const DiskIOLive = ({ rangeId }: HistoryLiveProps) => {
  const theme = useAppTheme();
  const range = rangeById(rangeId);
  const formatTimestamp = useHistoryTimestampFormatter(range);
  const { isEnabled, reason } = useCapability("monitoringAvailable");
  const { data, isLoading, error } = useQuery({
    ...linuxio.monitoring.get_diskio_history({
      resolution: range.resolution,
      limit: HISTORY_REQUEST_LIMIT,
    }),
    enabled: isEnabled,
    refetchInterval: range.refetchMs,
    placeholderData: (previous) => previous,
  });

  const message = historyCardMessage(data, isLoading, error, isEnabled, reason);
  const readColor = theme.chart.rx;
  const writeColor = theme.chart.tx;
  const series = useMemo(
    () => [
      {
        label: "Read",
        color: readColor,
        points: (data ?? []).map((point) => ({
          t: point.captured_at_ms,
          v: point.read_bytes_per_sec,
        })),
      },
      {
        label: "Write",
        color: writeColor,
        points: (data ?? []).map((point) => ({
          t: point.captured_at_ms,
          v: point.write_bytes_per_sec,
        })),
      },
    ],
    [data, readColor, writeColor],
  );

  return (
    <HistoryCardBody message={message}>
      <SynchronizedHistoryAreaChart
        formatTimestamp={formatTimestamp}
        formatValue={formatThroughput}
        windowMs={range.windowMs}
        series={series}
      />
    </HistoryCardBody>
  );
};

const NetworkHistoryLive = ({ rangeId }: HistoryLiveProps) => {
  const theme = useAppTheme();
  const range = rangeById(rangeId);
  const formatTimestamp = useHistoryTimestampFormatter(range);
  const { isEnabled, reason } = useCapability("monitoringAvailable");
  const { data, isLoading, error } = useQuery({
    ...linuxio.monitoring.get_network_history({
      resolution: range.resolution,
      limit: HISTORY_REQUEST_LIMIT,
    }),
    enabled: isEnabled,
    refetchInterval: range.refetchMs,
    placeholderData: (previous) => previous,
  });

  const message = historyCardMessage(data, isLoading, error, isEnabled, reason);
  const rxColor = theme.chart.rx;
  const txColor = theme.chart.tx;
  const series = useMemo(
    () => [
      {
        label: "Rx",
        color: rxColor,
        points: (data ?? []).map((point) => ({
          t: point.captured_at_ms,
          v: point.recv_bytes_per_sec,
        })),
      },
      {
        label: "Tx",
        color: txColor,
        points: (data ?? []).map((point) => ({
          t: point.captured_at_ms,
          v: point.sent_bytes_per_sec,
        })),
      },
    ],
    [data, rxColor, txColor],
  );

  return (
    <HistoryCardBody message={message}>
      <SynchronizedHistoryAreaChart
        formatTimestamp={formatTimestamp}
        formatValue={formatThroughput}
        windowMs={range.windowMs}
        series={series}
      />
    </HistoryCardBody>
  );
};

export const CPUHistoryCard = ({
  rangeId,
  onRangeChange,
}: HistoryCardProps) => (
  <HistoryCardShell
    avatarIcon="ph:cpu"
    headerRight={<RangeSelect onChange={onRangeChange} value={rangeId} />}
    title="Processor"
  >
    <CPUHistoryLive rangeId={rangeId} />
  </HistoryCardShell>
);

export const MemoryHistoryCard = ({
  rangeId,
  onRangeChange,
}: HistoryCardProps) => (
  <HistoryCardShell
    avatarIcon="la:memory"
    headerRight={<RangeSelect onChange={onRangeChange} value={rangeId} />}
    title="Memory"
  >
    <MemoryHistoryLive rangeId={rangeId} />
  </HistoryCardShell>
);

export const DiskIOHistoryCard = ({
  rangeId,
  onRangeChange,
}: HistoryCardProps) => (
  <HistoryCardShell
    avatarIcon="mdi:harddisk"
    headerRight={<RangeSelect onChange={onRangeChange} value={rangeId} />}
    title="Disk I/O"
  >
    <DiskIOLive rangeId={rangeId} />
  </HistoryCardShell>
);

export const NetworkHistoryCard = ({
  rangeId,
  onRangeChange,
}: HistoryCardProps) => (
  <HistoryCardShell
    avatarIcon="mdi:ethernet"
    headerRight={<RangeSelect onChange={onRangeChange} value={rangeId} />}
    title="Network"
  >
    <NetworkHistoryLive rangeId={rangeId} />
  </HistoryCardShell>
);
