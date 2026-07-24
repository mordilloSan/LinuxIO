import { Icon } from "@iconify/react";
import {
  useQuery,
  useSuspenseQueries,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { useMemo, useState, type ReactNode } from "react";

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
      linuxio.system.get_motherboard_info.queryOptions({
        staleTime: 300_000,
      }),
      linuxio.system.get_system_info.queryOptions({
        staleTime: 300_000,
      }),
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
      linuxio.system.get_cpu_info.queryOptions({
        staleTime: 300_000,
      }),
      linuxio.system.get_system_info.queryOptions({
        staleTime: 300_000,
      }),
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
      linuxio.system.get_motherboard_info.queryOptions({
        staleTime: 300_000,
      }),
      linuxio.system.get_system_info.queryOptions({
        staleTime: 300_000,
      }),
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
  const { data: gpus } = useSuspenseQuery(
    linuxio.system.get_gpu_info.queryOptions({
      staleTime: 60_000,
      refetchInterval: 15_000,
    }),
  );

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

export const HARDWARE_HISTORY_RANGES = [
  {
    id: "1h",
    label: "1 Hour",
    resolution: "1m",
    refetchMs: 15_000,
    windowMs: 3_600_000,
  },
  {
    id: "12h",
    label: "12 Hours",
    resolution: "10m",
    refetchMs: 60_000,
    windowMs: 43_200_000,
  },
  {
    id: "24h",
    label: "24 Hours",
    resolution: "20m",
    refetchMs: 60_000,
    windowMs: 86_400_000,
  },
  {
    id: "7d",
    label: "7 Days",
    resolution: "120m",
    refetchMs: 300_000,
    windowMs: 604_800_000,
  },
  {
    id: "30d",
    label: "30 Days",
    resolution: "480m",
    refetchMs: 300_000,
    windowMs: 2_592_000_000,
  },
] as const;

export type HardwareHistoryRangeId =
  (typeof HARDWARE_HISTORY_RANGES)[number]["id"];

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
  message,
  children,
}: {
  title: string;
  avatarIcon: string;
  headerRight?: ReactNode;
  message?: string;
  children?: ReactNode;
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
      {message ? (
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
      )}
    </FrostedCard>
  );
};

interface HistoryCardProps {
  /** Shared time range so the four cards stay synchronized. */
  rangeId: HardwareHistoryRangeId;
  onRangeChange: (id: HardwareHistoryRangeId) => void;
  /** Shared crosshair timestamp so the four cards stay synchronized. */
  hoverTime: number | null;
  onHoverTimeChange: (t: number | null) => void;
}

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

export const CPUHistoryCard = ({
  rangeId,
  onRangeChange,
  hoverTime,
  onHoverTimeChange,
}: HistoryCardProps) => {
  const theme = useAppTheme();
  const range = rangeById(rangeId);
  const formatTimestamp = useHistoryTimestampFormatter(range);
  const { isEnabled, reason } = useCapability("monitoringAvailable");
  const { data, isLoading, error } = useQuery(
    linuxio.monitoring.get_cpu_history.queryOptions(
      { resolution: range.resolution, limit: HISTORY_REQUEST_LIMIT },
      {
        enabled: isEnabled,
        refetchInterval: range.refetchMs,
        placeholderData: (previous) => previous,
      },
    ),
  );

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
    <HistoryCardShell
      avatarIcon="ph:cpu"
      headerRight={<RangeSelect onChange={onRangeChange} value={rangeId} />}
      message={message ?? undefined}
      title="Processor"
    >
      <HistoryAreaChart
        formatTick={formatPercentTick}
        formatTimestamp={formatTimestamp}
        formatValue={formatCoreValue}
        hoverTime={hoverTime}
        onHoverTimeChange={onHoverTimeChange}
        stacked={coreCount > 1}
        windowMs={range.windowMs}
        series={series}
        yMax={100}
      />
    </HistoryCardShell>
  );
};

export const MemoryHistoryCard = ({
  rangeId,
  onRangeChange,
  hoverTime,
  onHoverTimeChange,
}: HistoryCardProps) => {
  const theme = useAppTheme();
  const range = rangeById(rangeId);
  const formatTimestamp = useHistoryTimestampFormatter(range);
  const { isEnabled, reason } = useCapability("monitoringAvailable");
  const { data, isLoading, error } = useQuery(
    linuxio.monitoring.get_memory_history.queryOptions(
      { resolution: range.resolution, limit: HISTORY_REQUEST_LIMIT },
      {
        enabled: isEnabled,
        refetchInterval: range.refetchMs,
        placeholderData: (previous) => previous,
      },
    ),
  );
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
    <HistoryCardShell
      avatarIcon="la:memory"
      headerRight={<RangeSelect onChange={onRangeChange} value={rangeId} />}
      message={message ?? undefined}
      title="Memory"
    >
      <HistoryAreaChart
        formatTick={formatPercentTick}
        formatTimestamp={formatTimestamp}
        formatValue={formatPercent}
        hoverTime={hoverTime}
        onHoverTimeChange={onHoverTimeChange}
        stacked
        windowMs={range.windowMs}
        series={series}
        yMax={100}
      />
    </HistoryCardShell>
  );
};

export const DiskIOHistoryCard = ({
  rangeId,
  onRangeChange,
  hoverTime,
  onHoverTimeChange,
}: HistoryCardProps) => {
  const theme = useAppTheme();
  const range = rangeById(rangeId);
  const formatTimestamp = useHistoryTimestampFormatter(range);
  const { isEnabled, reason } = useCapability("monitoringAvailable");
  const { data, isLoading, error } = useQuery(
    linuxio.monitoring.get_diskio_history.queryOptions(
      { resolution: range.resolution, limit: HISTORY_REQUEST_LIMIT },
      {
        enabled: isEnabled,
        refetchInterval: range.refetchMs,
        placeholderData: (previous) => previous,
      },
    ),
  );

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
    <HistoryCardShell
      avatarIcon="mdi:harddisk"
      headerRight={<RangeSelect onChange={onRangeChange} value={rangeId} />}
      message={message ?? undefined}
      title="Disk I/O"
    >
      <HistoryAreaChart
        formatTimestamp={formatTimestamp}
        formatValue={formatThroughput}
        hoverTime={hoverTime}
        onHoverTimeChange={onHoverTimeChange}
        windowMs={range.windowMs}
        series={series}
      />
    </HistoryCardShell>
  );
};

export const NetworkHistoryCard = ({
  rangeId,
  onRangeChange,
  hoverTime,
  onHoverTimeChange,
}: HistoryCardProps) => {
  const theme = useAppTheme();
  const range = rangeById(rangeId);
  const formatTimestamp = useHistoryTimestampFormatter(range);
  const { isEnabled, reason } = useCapability("monitoringAvailable");
  const { data, isLoading, error } = useQuery(
    linuxio.monitoring.get_network_history.queryOptions(
      { resolution: range.resolution, limit: HISTORY_REQUEST_LIMIT },
      {
        enabled: isEnabled,
        refetchInterval: range.refetchMs,
        placeholderData: (previous) => previous,
      },
    ),
  );

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
    <HistoryCardShell
      avatarIcon="mdi:ethernet"
      headerRight={<RangeSelect onChange={onRangeChange} value={rangeId} />}
      message={message ?? undefined}
      title="Network"
    >
      <HistoryAreaChart
        formatTimestamp={formatTimestamp}
        formatValue={formatThroughput}
        hoverTime={hoverTime}
        onHoverTimeChange={onHoverTimeChange}
        windowMs={range.windowMs}
        series={series}
      />
    </HistoryCardShell>
  );
};
