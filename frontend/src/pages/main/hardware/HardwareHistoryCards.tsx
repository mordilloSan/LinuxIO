import { Icon } from "@iconify/react";
import React, { useMemo, useState } from "react";

import type { GpuDevice } from "@/api";
import { linuxio } from "@/api";
import CardIconHeader from "@/components/cards/CardIconHeader";
import FrostedCard from "@/components/cards/FrostedCard";
import HardwareCard from "@/components/cards/HardwareCard";
import HistoryAreaChart from "@/components/charts/HistoryAreaChart";
import AppSelect from "@/components/ui/AppSelect";
import AppTypography from "@/components/ui/AppTypography";
import { useCapability } from "@/hooks/useCapabilities";
import { useAppTheme } from "@/theme";
import { cardHeight } from "@/theme/constants";
import { formatFileSize, formatThroughput } from "@/utils/formaters";
import { formatGpuBytes, getGpuVendorLabel } from "@/utils/gpu";
import "@/pages/main/hardware/hardware-history.css";

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

export const MotherboardInfoCard: React.FC = () => {
  const theme = useAppTheme();
  const { data: motherboardInfo } =
    linuxio.system.get_motherboard_info.useQuery({
      staleTime: 300_000,
    });
  const { data: systemInfo } = linuxio.system.get_system_info.useQuery({
    staleTime: 300_000,
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

export const CPUDetailsCard: React.FC = () => {
  const theme = useAppTheme();
  const { data: cpuInfo } = linuxio.system.get_cpu_info.useQuery({
    staleTime: 300_000,
  });
  const { data: systemInfo } = linuxio.system.get_system_info.useQuery({
    staleTime: 300_000,
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

export const BIOSInfoCard: React.FC = () => {
  const theme = useAppTheme();
  const { data: motherboardInfo } =
    linuxio.system.get_motherboard_info.useQuery({
      staleTime: 300_000,
    });
  const { data: systemInfo } = linuxio.system.get_system_info.useQuery({
    staleTime: 300_000,
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

export const GPUInfoCard: React.FC = () => {
  const theme = useAppTheme();
  const [selectedGpuAddress, setSelectedGpuAddress] = useState("");
  const { data: gpus } = linuxio.system.get_gpu_info.useQuery({
    staleTime: 60_000,
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
  useMemo(() => {
    if (range.id === "7d" || range.id === "30d") {
      return (t: number) =>
        new Date(t).toLocaleDateString(undefined, {
          month: "short",
          day: "numeric",
        });
    }
    return (t: number) =>
      new Date(t).toLocaleTimeString(undefined, {
        hour: "2-digit",
        minute: "2-digit",
      });
  }, [range.id]);

const formatPercent = (value: number): string => `${value.toFixed(1)}%`;
const formatPercentTick = (value: number): string => `${Math.round(value)}%`;
const gbToBytes = (value: number): number => value * 1024 ** 3;
const percentOfTotal = (value: number, total: number): number =>
  total > 0 ? (value / total) * 100 : 0;

const RangeSelect: React.FC<{
  value: HardwareHistoryRangeId;
  onChange: (id: HardwareHistoryRangeId) => void;
}> = ({ value, onChange }) => {
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

const HistoryCardShell: React.FC<{
  title: string;
  avatarIcon: string;
  headerRight?: React.ReactNode;
  message?: string;
  children?: React.ReactNode;
}> = ({ title, avatarIcon, headerRight, message, children }) => {
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
  isPending: boolean,
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
  if (isPending) {
    return "Loading history…";
  }
  if (!points || points.length === 0) {
    return "Historical data not available.";
  }
  return null;
};

export const CPUHistoryCard: React.FC<HistoryCardProps> = ({
  rangeId,
  onRangeChange,
  hoverTime,
  onHoverTimeChange,
}) => {
  const theme = useAppTheme();
  const range = rangeById(rangeId);
  const formatTimestamp = useHistoryTimestampFormatter(range);
  const { isEnabled, reason } = useCapability("monitoringAvailable");
  const { data, isPending, error } =
    linuxio.monitoring.get_cpu_history.useQuery(
      { resolution: range.resolution, limit: HISTORY_REQUEST_LIMIT },
      {
        enabled: isEnabled,
        refetchInterval: range.refetchMs,
        placeholderData: (previous) => previous,
      },
    );

  const message = historyCardMessage(data, isPending, error, isEnabled, reason);
  const series = useMemo(
    () => [
      {
        label: "CPU",
        color: theme.palette.primary.main,
        points: (data ?? []).map((point) => ({
          t: point.captured_at_ms,
          v: point.usage_percent,
        })),
      },
    ],
    [data, theme.palette.primary.main],
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
        formatValue={formatPercent}
        hoverTime={hoverTime}
        onHoverTimeChange={onHoverTimeChange}
        windowMs={range.windowMs}
        series={series}
        yMax={100}
      />
    </HistoryCardShell>
  );
};

export const MemoryHistoryCard: React.FC<HistoryCardProps> = ({
  rangeId,
  onRangeChange,
  hoverTime,
  onHoverTimeChange,
}) => {
  const theme = useAppTheme();
  const range = rangeById(rangeId);
  const formatTimestamp = useHistoryTimestampFormatter(range);
  const { isEnabled, reason } = useCapability("monitoringAvailable");
  const { data, isPending, error } =
    linuxio.monitoring.get_memory_history.useQuery(
      { resolution: range.resolution, limit: HISTORY_REQUEST_LIMIT },
      {
        enabled: isEnabled,
        refetchInterval: range.refetchMs,
        placeholderData: (previous) => previous,
      },
    );
  const { data: liveMemory } = linuxio.system.get_memory_info.useQuery({
    refetchInterval: 2000,
  });

  const message = historyCardMessage(data, isPending, error, isEnabled, reason);
  const cacheColor = theme.palette.warning.main;
  const zfsColor = theme.palette.success.main;
  const series = useMemo(() => {
    const points = data ?? [];
    const memorySeries = [
      {
        label: "Used",
        color: theme.palette.primary.main,
        points: points.map((point) => ({
          t: point.captured_at_ms,
          v: point.used_percent,
          detail: `${point.used_gb.toFixed(1)} / ${point.total_gb.toFixed(1)} GB`,
        })),
      },
    ];

    if (points.some((point) => point.buffer_cache_gb > 0)) {
      memorySeries.push({
        label: "Cache",
        color: cacheColor,
        points: points.map((point) => ({
          t: point.captured_at_ms,
          v: percentOfTotal(point.buffer_cache_gb, point.total_gb),
          detail: `${point.buffer_cache_gb.toFixed(1)} GB`,
        })),
      });
    }

    if (points.some((point) => (point.zfs_arc_gb ?? 0) > 0)) {
      memorySeries.push({
        label: "ZFS ARC",
        color: zfsColor,
        points: points.map((point) => ({
          t: point.captured_at_ms,
          v: percentOfTotal(point.zfs_arc_gb ?? 0, point.total_gb),
          detail: `${(point.zfs_arc_gb ?? 0).toFixed(1)} GB`,
        })),
      });
    }

    return memorySeries;
  }, [cacheColor, data, theme.palette.primary.main, zfsColor]);
  const liveStats = useMemo(() => {
    const swapTotal = liveMemory?.system?.swapTotal ?? 0;
    const swapFree = liveMemory?.system?.swapFree ?? 0;
    const swapUsed = Math.max(swapTotal - swapFree, 0);

    return [
      {
        label: "Swap",
        value:
          liveMemory && swapTotal > 0
            ? `${formatFileSize(swapUsed, 1)} / ${formatFileSize(swapTotal, 1)}`
            : "0 Bytes",
      },
      {
        label: "Docker",
        value: liveMemory
          ? formatFileSize(liveMemory.docker?.used ?? 0, 1)
          : "—",
      },
    ];
  }, [liveMemory]);

  const latestMemory = data?.[data.length - 1];
  const latestStats = useMemo(
    () => [
      {
        label: "Used",
        value: latestMemory
          ? formatFileSize(gbToBytes(latestMemory.used_gb), 1)
          : "—",
      },
      {
        label: "Cache",
        value: latestMemory
          ? formatFileSize(gbToBytes(latestMemory.buffer_cache_gb), 1)
          : "—",
      },
      {
        label: "ZFS",
        value:
          latestMemory && (latestMemory.zfs_arc_gb ?? 0) > 0
            ? formatFileSize(gbToBytes(latestMemory.zfs_arc_gb ?? 0), 1)
            : "0 Bytes",
      },
    ],
    [latestMemory],
  );
  const memoryStats = [...latestStats, ...liveStats];

  return (
    <HistoryCardShell
      avatarIcon="la:memory"
      headerRight={<RangeSelect onChange={onRangeChange} value={rangeId} />}
      message={message ?? undefined}
      title="Memory"
    >
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          height: "100%",
          minHeight: 0,
        }}
      >
        <div style={{ flex: 1, minHeight: 0 }}>
          <HistoryAreaChart
            formatTick={formatPercentTick}
            formatTimestamp={formatTimestamp}
            formatValue={formatPercent}
            hoverTime={hoverTime}
            onHoverTimeChange={onHoverTimeChange}
            windowMs={range.windowMs}
            series={series}
            yMax={100}
          />
        </div>
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "repeat(5, minmax(0, 1fr))",
            gap: 4,
            paddingTop: 3,
          }}
        >
          {memoryStats.map((stat) => (
            <div key={stat.label} style={{ minWidth: 0 }}>
              <AppTypography
                color="text.secondary"
                style={{
                  display: "block",
                  fontSize: "0.55rem",
                  lineHeight: 1.1,
                  textTransform: "uppercase",
                }}
                variant="caption"
              >
                {stat.label}
              </AppTypography>
              <AppTypography
                noWrap
                style={{ display: "block", fontSize: "0.65rem" }}
                variant="caption"
              >
                {stat.value}
              </AppTypography>
            </div>
          ))}
        </div>
      </div>
    </HistoryCardShell>
  );
};

export const DiskIOHistoryCard: React.FC<HistoryCardProps> = ({
  rangeId,
  onRangeChange,
  hoverTime,
  onHoverTimeChange,
}) => {
  const theme = useAppTheme();
  const range = rangeById(rangeId);
  const formatTimestamp = useHistoryTimestampFormatter(range);
  const { isEnabled, reason } = useCapability("monitoringAvailable");
  const { data, isPending, error } =
    linuxio.monitoring.get_diskio_history.useQuery(
      { resolution: range.resolution, limit: HISTORY_REQUEST_LIMIT },
      {
        enabled: isEnabled,
        refetchInterval: range.refetchMs,
        placeholderData: (previous) => previous,
      },
    );

  const message = historyCardMessage(data, isPending, error, isEnabled, reason);
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

export const NetworkHistoryCard: React.FC<HistoryCardProps> = ({
  rangeId,
  onRangeChange,
  hoverTime,
  onHoverTimeChange,
}) => {
  const theme = useAppTheme();
  const range = rangeById(rangeId);
  const formatTimestamp = useHistoryTimestampFormatter(range);
  const { isEnabled, reason } = useCapability("monitoringAvailable");
  const { data, isPending, error } =
    linuxio.monitoring.get_network_history.useQuery(
      { resolution: range.resolution, limit: HISTORY_REQUEST_LIMIT },
      {
        enabled: isEnabled,
        refetchInterval: range.refetchMs,
        placeholderData: (previous) => previous,
      },
    );

  const message = historyCardMessage(data, isPending, error, isEnabled, reason);
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
