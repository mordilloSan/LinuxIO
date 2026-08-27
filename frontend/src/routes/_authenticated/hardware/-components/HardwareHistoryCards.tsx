import {
  useQuery,
  useSuspenseQueries,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { useMemo, useState } from "react";

import type { GpuDevice } from "@/api";
import { linuxio } from "@/api";
import HardwareCard from "@/components/cards/HardwareCard";
import {
  historyCardMessage,
  historyRequest,
  HistoryCardBody,
  HistoryCardShell,
  rangeById,
  RangeSelect,
  SynchronizedHistoryAreaChart,
  useHistoryTimestampFormatter,
  type HistoryCardProps,
  type HistoryLiveProps,
} from "@/components/charts/HistoryCard";
import AppSelect from "@/components/ui/AppSelect";
import { useCapability } from "@/hooks/useCapabilities";
import { formatThroughput } from "@/utils/formaters";
import { formatGpuBytes, getGpuVendorLabel } from "@/utils/gpu";

import {
  hardwareGpuQueryOptions,
  hardwareStableQueryOptions,
} from "./hardwareQueryOptions";

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
  const [{ data: motherboardInfo }, { data: systemInfo }] = useSuspenseQueries({
    queries: [
      { ...linuxio.system.get_motherboard_info, ...hardwareStableQueryOptions },
      { ...linuxio.system.get_system_info, ...hardwareStableQueryOptions },
    ],
  });

  return (
    <HardwareCard
      accentColor="var(--app-palette-primary-main)"
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
  const [{ data: cpuInfo }, { data: systemInfo }] = useSuspenseQueries({
    queries: [
      { ...linuxio.system.get_cpu_info, ...hardwareStableQueryOptions },
      { ...linuxio.system.get_system_info, ...hardwareStableQueryOptions },
    ],
  });

  return (
    <HardwareCard
      accentColor="var(--app-palette-primary-main)"
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
  const [{ data: motherboardInfo }, { data: systemInfo }] = useSuspenseQueries({
    queries: [
      { ...linuxio.system.get_motherboard_info, ...hardwareStableQueryOptions },
      { ...linuxio.system.get_system_info, ...hardwareStableQueryOptions },
    ],
  });

  return (
    <HardwareCard
      accentColor="var(--app-palette-warning-main)"
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
      accentColor="var(--app-palette-primary-main)"
      actions={
        gpuCount > 1 ? (
          <AppSelect
            disableUnderline
            onChange={(event) => setSelectedGpuAddress(event.target.value)}
            size="small"
            style={{
              ["--app-select-input-font-size" as string]: "0.72rem",
              width: 190,
              color: "var(--app-palette-text-secondary)",
              lineHeight: 1.43,
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

const formatPercent = (value: number): string => `${value.toFixed(1)}%`;
const formatPercentTick = (value: number): string => `${Math.round(value)}%`;
const percentOfTotal = (value: number, total: number): number =>
  total > 0 ? (value / total) * 100 : 0;

/** Netdata-style hue ramp: purple for the bottom band up to red at the top. */
const stackBandColor = (index: number, count: number): string => {
  const ratio = count <= 1 ? 0 : index / (count - 1);
  return `hsl(${Math.round(280 - 280 * ratio)}, 70%, 55%)`;
};

const CPUHistoryLive = ({ rangeId }: HistoryLiveProps) => {
  const range = rangeById(rangeId);
  const request = historyRequest(range);
  const formatTimestamp = useHistoryTimestampFormatter(range);
  const { isEnabled, reason } = useCapability("monitoringAvailable");
  const { data, isLoading, error } = useQuery({
    ...linuxio.monitoring.get_cpu_history({
      ...request,
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
        color: "var(--app-palette-primary-main)",
        points: points.map((point) => ({
          t: point.captured_at_ms,
          v: point.usage_percent,
        })),
      },
    ];
  }, [coreCount, data]);

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
  const range = rangeById(rangeId);
  const request = historyRequest(range);
  const formatTimestamp = useHistoryTimestampFormatter(range);
  const { isEnabled, reason } = useCapability("monitoringAvailable");
  const { data, isLoading, error } = useQuery({
    ...linuxio.monitoring.get_memory_history({
      ...request,
    }),
    enabled: isEnabled,
    refetchInterval: range.refetchMs,
    placeholderData: (previous) => previous,
  });
  const message = historyCardMessage(data, isLoading, error, isEnabled, reason);
  const zfsColor = "var(--app-palette-success-main)";
  const dockerColor = "var(--app-chart-rx)";
  const buffersColor = "var(--app-chart-tx)";
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
      layer("Apps", "var(--app-palette-primary-main)", (point) =>
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
  }, [buffersColor, data, dockerColor, zfsColor]);

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
  const range = rangeById(rangeId);
  const request = historyRequest(range);
  const formatTimestamp = useHistoryTimestampFormatter(range);
  const { isEnabled, reason } = useCapability("monitoringAvailable");
  const { data, isLoading, error } = useQuery({
    ...linuxio.monitoring.get_diskio_history({
      ...request,
    }),
    enabled: isEnabled,
    refetchInterval: range.refetchMs,
    placeholderData: (previous) => previous,
  });

  const message = historyCardMessage(data, isLoading, error, isEnabled, reason);
  const readColor = "var(--app-chart-rx)";
  const writeColor = "var(--app-chart-tx)";
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
  const range = rangeById(rangeId);
  const request = historyRequest(range);
  const formatTimestamp = useHistoryTimestampFormatter(range);
  const { isEnabled, reason } = useCapability("monitoringAvailable");
  const { data, isLoading, error } = useQuery({
    ...linuxio.monitoring.get_network_history({
      ...request,
    }),
    enabled: isEnabled,
    refetchInterval: range.refetchMs,
    placeholderData: (previous) => previous,
  });

  const message = historyCardMessage(data, isLoading, error, isEnabled, reason);
  const rxColor = "var(--app-chart-rx)";
  const txColor = "var(--app-chart-tx)";
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
