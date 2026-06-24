import { Icon } from "@iconify/react";
import React, { useMemo, useState } from "react";

import type { GpuDevice } from "@/api";
import { linuxio } from "@/api";
import CardIconHeader from "@/components/cards/CardIconHeader";
import FrostedCard from "@/components/cards/FrostedCard";
import HardwareCard from "@/components/cards/HardwareCard";
import AppSelect from "@/components/ui/AppSelect";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";
import { cardHeight } from "@/theme/constants";
import { formatGpuBytes, getGpuVendorLabel } from "@/utils/gpu";

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

// ─── History card placeholders ───────────────────────────────────────────────

const HistoryPlaceholder: React.FC<{
  title: string;
  avatarIcon: string;
}> = ({ title, avatarIcon }) => {
  const theme = useAppTheme();

  return (
    <FrostedCard
      style={{
        minHeight: cardHeight,
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
        style={{ marginBottom: 8 }}
        title={title}
      />
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
          Historical data not available.
        </AppTypography>
      </div>
    </FrostedCard>
  );
};

export const CPUHistoryCard: React.FC = () => (
  <HistoryPlaceholder avatarIcon="ph:cpu" title="Processor" />
);

export const MemoryHistoryCard: React.FC = () => (
  <HistoryPlaceholder avatarIcon="la:memory" title="Memory" />
);

export const NetworkHistoryCard: React.FC = () => (
  <HistoryPlaceholder avatarIcon="mdi:ethernet" title="Network" />
);

export const DiskIOHistoryCard: React.FC = () => (
  <HistoryPlaceholder avatarIcon="mdi:harddisk" title="Disk I/O" />
);
