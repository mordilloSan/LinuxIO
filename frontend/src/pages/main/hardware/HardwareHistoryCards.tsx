import React, { useMemo, useState } from "react";

import type { GpuDevice, MonitoringRange } from "@/api";
import { linuxio } from "@/api";
import HardwareCard from "@/components/cards/HardwareCard";
import MonitorCard from "@/components/cards/MonitorCard";
import {
  DiskIOMonitorGraph,
  MonitorGraph,
  NetworkMonitorGraph,
} from "@/components/charts/MonitorGraph";
import AppSelect from "@/components/ui/AppSelect";
import { useAppTheme } from "@/theme";
import { formatGpuBytes, getGpuVendorLabel } from "@/utils/gpu";

// Poll interval per range — matches the backend step size so we don't refetch
// faster than new data points arrive (pcp.go rangeDefinitions).
const RANGE_STEP_MS: Record<MonitoringRange, number> = {
  "1m": 5_000,
  "5m": 5_000,
  "15m": 15_000,
  "60m": 60_000,
  "6h": 300_000,
  "24h": 900_000,
  "7d": 3_600_000,
  "30d": 21_600_000,
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
      title="Motherboard"
      subtitle="Board & system details"
      avatarIcon="bi:motherboard"
      accentColor={theme.palette.primary.main}
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
      title="CPU"
      subtitle="Processor specifications"
      avatarIcon="ph:cpu"
      accentColor={theme.palette.primary.main}
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
      title="BIOS"
      subtitle="Firmware information"
      avatarIcon="mdi:chip"
      accentColor={theme.palette.warning.main}
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
      title="GPU"
      subtitle="Graphics card details"
      avatarIcon="bi:gpu-card"
      accentColor={theme.palette.primary.main}
      actions={
        gpuCount > 1 ? (
          <AppSelect
            size="small"
            variant="standard"
            disableUnderline
            value={selectedValue}
            onChange={(event) => setSelectedGpuAddress(event.target.value)}
            style={{
              ["--app-select-input-font-size" as string]: "0.72rem",
              width: 190,
              color: theme.palette.text.secondary,
              fontSize: "0.78rem",
              lineHeight: theme.typography.body2.lineHeight,
            }}
          >
            {(gpus ?? []).map((gpu, index) => (
              <option key={gpu.address} value={gpu.address}>
                {`GPU ${index + 1}: ${gpu.model || getGpuVendorLabel(gpu)}`}
              </option>
            ))}
          </AppSelect>
        ) : undefined
      }
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
    />
  );
};

// ─── History cards ────────────────────────────────────────────────────────────

export const CPUHistoryCard: React.FC<{
  range?: MonitoringRange;
  onRangeChange?: (v: MonitoringRange) => void;
  hoverRatio?: number | null;
  onHoverChange?: (ratio: number | null) => void;
}> = ({
  range: rangeProp,
  onRangeChange: onRangeChangeProp,
  hoverRatio,
  onHoverChange,
}) => {
  const theme = useAppTheme();
  const [rangeInternal, setRangeInternal] = useState<MonitoringRange>("1m");
  const range = rangeProp ?? rangeInternal;
  const setRange = onRangeChangeProp ?? setRangeInternal;
  const { data: series, isPending } =
    linuxio.monitoring.get_cpu_series.useQuery(range, {
      refetchInterval: RANGE_STEP_MS[range],
    });

  return (
    <MonitorCard
      title="Processor"
      avatarIcon="ph:cpu"
      accentColor={theme.palette.primary.main}
      range={range}
      onRangeChange={setRange}
      chart={
        <MonitorGraph
          color={theme.palette.primary.main}
          label="CPU"
          range={range}
          series={series}
          loading={isPending}
          emptyMessage="CPU history is not available yet."
          hoverRatio={hoverRatio}
          onHoverChange={onHoverChange}
        />
      }
    />
  );
};

export const MemoryHistoryCard: React.FC<{
  range?: MonitoringRange;
  onRangeChange?: (v: MonitoringRange) => void;
  hoverRatio?: number | null;
  onHoverChange?: (ratio: number | null) => void;
}> = ({
  range: rangeProp,
  onRangeChange: onRangeChangeProp,
  hoverRatio,
  onHoverChange,
}) => {
  const theme = useAppTheme();
  const [rangeInternal, setRangeInternal] = useState<MonitoringRange>("1m");
  const range = rangeProp ?? rangeInternal;
  const setRange = onRangeChangeProp ?? setRangeInternal;
  const { data: memoryData } = linuxio.system.get_memory_info.useQuery({
    refetchInterval: 5_000,
  });
  const { data: series, isPending } =
    linuxio.monitoring.get_memory_series.useQuery(range, {
      refetchInterval: RANGE_STEP_MS[range],
    });

  const dockerPercent =
    memoryData?.system?.total && memoryData.system.total > 0
      ? ((memoryData?.docker?.used ?? 0) / memoryData.system.total) * 100
      : 0;

  return (
    <MonitorCard
      title="Memory Usage"
      avatarIcon="la:memory"
      accentColor={theme.palette.warning.main}
      range={range}
      onRangeChange={setRange}
      chart={
        <MonitorGraph
          color={theme.palette.warning.main}
          label="Memory"
          range={range}
          series={series}
          loading={isPending}
          emptyMessage="Memory history is not available yet."
          stackedPercent={dockerPercent}
          stackedColor={theme.palette.info.main}
          stackedLabel="Docker"
          stackedTooltipLabel="System"
          hoverRatio={hoverRatio}
          onHoverChange={onHoverChange}
        />
      }
    />
  );
};

export const NetworkHistoryCard: React.FC<{
  range?: MonitoringRange;
  onRangeChange?: (v: MonitoringRange) => void;
  hoverRatio?: number | null;
  onHoverChange?: (ratio: number | null) => void;
}> = ({
  range: rangeProp,
  onRangeChange: onRangeChangeProp,
  hoverRatio,
  onHoverChange,
}) => {
  const theme = useAppTheme();
  const [rangeInternal, setRangeInternal] = useState<MonitoringRange>("1m");
  const [selectedInterfaceInternal, setSelectedInterfaceInternal] =
    useState("");
  const range = rangeProp ?? rangeInternal;
  const setRange = onRangeChangeProp ?? setRangeInternal;
  const { data: interfaces, isPending: interfacesPending } =
    linuxio.system.get_network_info.useQuery({
      refetchInterval: 30_000,
      staleTime: 30_000,
    });
  const filteredInterfaces = useMemo(
    () =>
      (interfaces ?? []).filter(
        (iface) =>
          iface.name &&
          iface.name !== "lo" &&
          !iface.name.startsWith("veth") &&
          !iface.name.startsWith("docker") &&
          !iface.name.startsWith("br"),
      ),
    [interfaces],
  );
  const interfaceOptions = useMemo(
    () =>
      filteredInterfaces.map((iface) => ({
        value: iface.name,
        label: iface.name,
      })),
    [filteredInterfaces],
  );
  const defaultInterface = useMemo(() => {
    const primary =
      filteredInterfaces.find((iface) => (iface.ipv4?.length ?? 0) > 0) ??
      filteredInterfaces[0];
    return primary?.name ?? "";
  }, [filteredInterfaces]);

  const selectedInterface = useMemo(
    () =>
      filteredInterfaces.find(
        (iface) => iface.name === selectedInterfaceInternal,
      )?.name ?? defaultInterface,
    [defaultInterface, filteredInterfaces, selectedInterfaceInternal],
  );

  const { data: series, isPending } =
    linuxio.monitoring.get_network_series.useQuery({
      args: [range, selectedInterface],
      enabled: !!selectedInterface,
      refetchInterval: RANGE_STEP_MS[range],
    });

  return (
    <MonitorCard
      title="Network"
      avatarIcon="mdi:ethernet"
      accentColor={theme.palette.primary.main}
      range={range}
      onRangeChange={setRange}
      controls={
        <AppSelect
          size="small"
          variant="standard"
          disableUnderline
          value={selectedInterface}
          onChange={(event) => setSelectedInterfaceInternal(event.target.value)}
          style={{
            ["--app-select-input-font-size" as string]: "0.68rem",
            marginLeft: 0,
            maxWidth: 140,
            color: theme.palette.text.secondary,
            fontSize: "0.75rem",
            lineHeight: theme.typography.body2.lineHeight,
          }}
        >
          {interfaceOptions.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </AppSelect>
      }
      chart={
        <NetworkMonitorGraph
          range={range}
          series={series}
          loading={interfacesPending || isPending}
          emptyMessage={
            selectedInterface
              ? "Historical network data is not available yet."
              : "No eligible network interface found for historical monitoring."
          }
          hoverRatio={hoverRatio}
          onHoverChange={onHoverChange}
        />
      }
    />
  );
};

export const DiskIOHistoryCard: React.FC<{
  range?: MonitoringRange;
  onRangeChange?: (v: MonitoringRange) => void;
  hoverRatio?: number | null;
  onHoverChange?: (ratio: number | null) => void;
}> = ({
  range: rangeProp,
  onRangeChange: onRangeChangeProp,
  hoverRatio,
  onHoverChange,
}) => {
  const theme = useAppTheme();
  const [rangeInternal, setRangeInternal] = useState<MonitoringRange>("1m");
  const range = rangeProp ?? rangeInternal;
  const setRange = onRangeChangeProp ?? setRangeInternal;

  const { data: series, isPending } =
    linuxio.monitoring.get_disk_io_series.useQuery({
      args: [range, ""],
      refetchInterval: RANGE_STEP_MS[range],
    });

  return (
    <MonitorCard
      title="I/O"
      avatarIcon="mdi:harddisk"
      accentColor={theme.palette.primary.main}
      range={range}
      onRangeChange={setRange}
      chart={
        <DiskIOMonitorGraph
          range={range}
          series={series}
          loading={isPending}
          emptyMessage="Historical disk I/O data is not available yet."
          hoverRatio={hoverRatio}
          onHoverChange={onHoverChange}
        />
      }
    />
  );
};
