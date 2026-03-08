import MemoryIcon from "@mui/icons-material/Memory";
import { Chip, Typography } from "@mui/material";
import { useTheme } from "@mui/material/styles";
import React from "react";

import { linuxio } from "@/api";
import type { GpuDevice } from "@/api/linuxio-types";
import DashboardCard from "@/components/cards/DashboardCard";
import MetricBar from "@/components/gauge/MetricBar";
import {
  formatGpuBytes,
  formatGpuClock,
  formatGpuDisplays,
  formatGpuPercent,
  formatGpuTemperature,
  formatGpuWatts,
  getGpuType,
  hasGpuValue,
} from "@/utils/gpu";

const getBarColor = (
  value: number,
  palette: { success: string; warning: string; error: string },
) => {
  if (value < 50) return palette.success;
  if (value < 80) return palette.warning;
  return palette.error;
};

const gpuRows = (gpu: GpuDevice) =>
  [
    {
      label: "Type",
      value: getGpuType(gpu),
    },
    {
      label: "Usage",
      value: hasGpuValue(gpu.utilization_percent)
        ? formatGpuPercent(gpu.utilization_percent)
        : undefined,
    },
    {
      label: "Temperature",
      value: hasGpuValue(gpu.temperature_c)
        ? formatGpuTemperature(gpu.temperature_c)
        : undefined,
    },
    gpu.memory_total_bytes
      ? {
          label: "Memory",
          value: `${formatGpuBytes(gpu.memory_used_bytes ?? 0)} / ${formatGpuBytes(gpu.memory_total_bytes)}`,
        }
      : null,
    hasGpuValue(gpu.current_freq_mhz)
      ? {
          label: "Clock",
          value: `${formatGpuClock(gpu.current_freq_mhz)} / ${formatGpuClock(gpu.max_freq_mhz)}`,
        }
      : null,
    {
      label: "Displays",
      value:
        gpu.display_names?.length || typeof gpu.connected_displays === "number"
          ? formatGpuDisplays(gpu)
          : undefined,
    },
    {
      label: "Driver",
      value: gpu.driver_version
        ? `${gpu.driver} (${gpu.driver_version})`
        : gpu.driver || "—",
    },
    {
      label: "Address",
      value: gpu.address || "—",
    },
    {
      label: "Runtime",
      value: gpu.runtime_status || undefined,
    },
    {
      label: "Power",
      value: hasGpuValue(gpu.power_draw_watts)
        ? hasGpuValue(gpu.power_limit_watts)
          ? `${formatGpuWatts(gpu.power_draw_watts)} / ${formatGpuWatts(gpu.power_limit_watts)}`
          : formatGpuWatts(gpu.power_draw_watts)
        : "—",
    },
    {
      label: "PCIe",
      value: gpu.link_speed
        ? gpu.link_width
          ? `${gpu.link_speed} x${gpu.link_width}`
          : gpu.link_speed
        : undefined,
    },
    {
      label: "Vendor ID",
      value: gpu.vendor_id || undefined,
    },
    {
      label: "Device ID",
      value: gpu.device_id || undefined,
    },
    {
      label: "Revision",
      value: gpu.revision || undefined,
    },
    {
      label: "Subsystem",
      value: gpu.subsystem || undefined,
    },
    {
      label: "Class",
      value: gpu.class_name || undefined,
    },
    {
      label: "Interface",
      value: gpu.programming_interface || undefined,
    },
  ].filter(
    (row): row is { label: string; value: string } =>
      typeof row?.value === "string" && row.value.trim() !== "",
  );

const GpuInfo: React.FC = () => {
  const theme = useTheme();
  const {
    data: gpus,
    isPending: isLoading,
    isError,
  } = linuxio.system.get_gpu_info.useQuery({
    refetchInterval: 10_000,
  });

  let content: React.ReactNode = null;

  if (isLoading) {
    content = <Typography variant="body2">Loading…</Typography>;
  } else if (isError || !gpus || gpus.length === 0) {
    content = (
      <Typography variant="body2">No GPU information available.</Typography>
    );
  } else {
    content = (
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          width: "100%",
          gap: theme.spacing(1.5),
        }}
      >
        {gpus.map((gpu, idx) => (
          <div
            key={`${gpu.address}-${idx}`}
            style={{
              paddingBottom: idx === gpus.length - 1 ? 0 : 12,
              borderBottom:
                idx === gpus.length - 1
                  ? "none"
                  : "1px solid var(--mui-palette-divider)",
            }}
          >
            <div
              style={{
                display: "grid",
                gridTemplateColumns: "repeat(auto-fit, minmax(260px, 1fr))",
                gap: theme.spacing(2),
                marginBottom: 12,
              }}
            >
              <div style={{ minWidth: 0 }}>
                <Typography variant="subtitle2" fontWeight={700} noWrap>
                  {gpu.model || `GPU ${idx + 1}`}
                </Typography>
                <Typography variant="caption" color="text.secondary" noWrap>
                  {gpu.vendor} • {getGpuType(gpu)}
                </Typography>
              </div>
              <div
                style={{
                  display: "flex",
                  alignItems: "flex-start",
                  gap: 6,
                  flexWrap: "wrap",
                  justifyContent: "flex-end",
                }}
              >
                {hasGpuValue(gpu.runtime_status) && (
                  <Chip
                    size="small"
                    label={gpu.runtime_status}
                    color={gpu.runtime_status === "active" ? "success" : "info"}
                    variant="outlined"
                  />
                )}
                {typeof gpu.connected_displays === "number" && (
                  <Chip
                    size="small"
                    label={`${gpu.connected_displays} display${gpu.connected_displays === 1 ? "" : "s"}`}
                    variant="outlined"
                  />
                )}
              </div>
            </div>

            <div
              style={{
                display: "grid",
                gridTemplateColumns: "repeat(auto-fit, minmax(220px, 1fr))",
                gap: 10,
              }}
            >
              {hasGpuValue(gpu.utilization_percent) && (
                <MetricBar
                  label="GPU Load"
                  percent={gpu.utilization_percent}
                  color={getBarColor(gpu.utilization_percent, {
                    success: theme.palette.success.main,
                    warning: theme.palette.warning.main,
                    error: theme.palette.error.main,
                  })}
                  tooltip={`Current GPU usage: ${formatGpuPercent(gpu.utilization_percent)}`}
                  rightLabel={formatGpuPercent(gpu.utilization_percent)}
                />
              )}
              {gpu.memory_total_bytes ? (
                <MetricBar
                  label="VRAM"
                  percent={Math.min(
                    ((gpu.memory_used_bytes ?? 0) / gpu.memory_total_bytes) *
                      100,
                    100,
                  )}
                  color={theme.palette.primary.main}
                  tooltip={`${formatGpuBytes(gpu.memory_used_bytes ?? 0)} / ${formatGpuBytes(gpu.memory_total_bytes)}`}
                  rightLabel={formatGpuBytes(gpu.memory_used_bytes ?? 0)}
                />
              ) : null}
              {hasGpuValue(gpu.temperature_c) && (
                <MetricBar
                  label="Temperature"
                  percent={Math.min((gpu.temperature_c / 105) * 100, 100)}
                  color={getBarColor(gpu.temperature_c, {
                    success: theme.palette.success.main,
                    warning: theme.palette.warning.main,
                    error: theme.palette.error.main,
                  })}
                  tooltip={`Current GPU temperature: ${formatGpuTemperature(gpu.temperature_c)}`}
                  rightLabel={formatGpuTemperature(gpu.temperature_c)}
                />
              )}
            </div>

            <div
              style={{
                display: "grid",
                gridTemplateColumns: "repeat(auto-fit, minmax(180px, 1fr))",
                gap: 12,
                marginTop: 14,
              }}
            >
              {gpuRows(gpu).map(({ label, value }) => (
                <div key={`${gpu.address}-${label}`} style={{ minWidth: 0 }}>
                  <Typography
                    variant="caption"
                    color="text.secondary"
                    sx={{
                      textTransform: "uppercase",
                      letterSpacing: "0.06em",
                      fontSize: "0.62rem",
                    }}
                  >
                    {label}
                  </Typography>
                  <Typography
                    variant="body2"
                    fontWeight={500}
                    sx={{
                      lineHeight: 1.3,
                      wordBreak: "break-word",
                      color: theme.palette.text.primary,
                    }}
                  >
                    {value}
                  </Typography>
                </div>
              ))}
            </div>
          </div>
        ))}
      </div>
    );
  }

  return (
    <DashboardCard
      title="GPU"
      stats={content}
      icon={MemoryIcon}
      iconProps={{ sx: { color: "text.secondary" } }}
      avatarIcon="bi:gpu-card"
    />
  );
};

export default GpuInfo;
