import { useTheme } from "@mui/material/styles";
import React from "react";

import { linuxio } from "@/api";
import DashboardCard from "@/components/cards/DashboardCard";
import MetricBar from "@/components/gauge/MetricBar";
import Chip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";
import { formatGpuPercent, getGpuType, hasGpuValue } from "@/utils/gpu";

const GpuInfo: React.FC = () => {
  const theme = useTheme();
  const {
    data: gpus,
    isPending: isLoading,
    isError,
  } = linuxio.system.get_gpu_info.useQuery({
    refetchInterval: 2_000,
  });

  const content: React.ReactNode = isLoading ? (
    <AppTypography variant="body2">Loading...</AppTypography>
  ) : isError || !gpus || gpus.length === 0 ? (
    <AppTypography variant="body2">No GPU information available.</AppTypography>
  ) : (
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
              display: "flex",
              alignItems: "flex-start",
              justifyContent: "space-between",
              gap: theme.spacing(1),
              marginBottom: 12,
            }}
          >
            <div style={{ minWidth: 0 }}>
              <AppTypography variant="subtitle2" fontWeight={700} noWrap>
                {gpu.model || `GPU ${idx + 1}`}
              </AppTypography>
              <AppTypography variant="caption" color="text.secondary" noWrap>
                {gpu.vendor} • {getGpuType(gpu)}
              </AppTypography>
            </div>
            <div
              style={{
                display: "flex",
                flexDirection: "column",
                alignItems: "flex-end",
                gap: 4,
              }}
            >
              {hasGpuValue(gpu.runtime_status) && (
                <Chip
                  size="small"
                  label={gpu.runtime_status}
                  color={gpu.runtime_status === "active" ? "success" : "info"}
                  variant="soft"
                />
              )}
            </div>
          </div>

          {hasGpuValue(gpu.utilization_percent) && (
            <MetricBar
              label="GPU Load"
              percent={gpu.utilization_percent}
              color={theme.palette.primary.main}
              tooltip={`Current GPU usage: ${formatGpuPercent(gpu.utilization_percent)}`}
              rightLabel={formatGpuPercent(gpu.utilization_percent)}
            />
          )}
        </div>
      ))}
    </div>
  );

  return (
    <DashboardCard
      title="GPU"
      stats={content}
      icon="mdi:memory"
      avatarIcon="bi:gpu-card"
    />
  );
};

export default GpuInfo;
