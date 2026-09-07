import { useSuspenseQueries } from "@tanstack/react-query";

import { CACHE_TTL_MS, linuxio, type MonitoringLive } from "@/api";
import DashboardCard from "@/components/cards/DashboardCard";
import MetricBar from "@/components/gauge/MetricBar";
import Chip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";
import { DASHBOARD_REFETCH_FAST_MS } from "@/constants/liveCharts";
import {
  formatGpuPercent,
  getGpuType,
  getGpuVendorLabel,
  hasGpuValue,
} from "@/utils/gpu";

const GpuStats = () => {
  const [{ data: gpus }, { data: liveGpus }] = useSuspenseQueries({
    queries: [
      { ...linuxio.system.get_gpu_info, staleTime: CACHE_TTL_MS.ONE_DAY },
      {
        ...linuxio.monitoring.get_live,
        refetchInterval: DASHBOARD_REFETCH_FAST_MS,
        select: (live: MonitoringLive) => live.gpus ?? {},
      },
    ],
  });

  if (!gpus || gpus.length === 0) {
    return (
      <AppTypography variant="body2">
        No GPU information available.
      </AppTypography>
    );
  }

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        width: "100%",
        gap: "var(--app-space-6)",
      }}
    >
      {gpus.map((gpu, idx) => {
        const live = liveGpus?.[gpu.address];
        return (
          <div
            key={`${gpu.address}-${idx}`}
            style={{
              paddingBottom: idx === gpus.length - 1 ? 0 : 12,
              borderBottom:
                idx === gpus.length - 1
                  ? "none"
                  : "1px solid var(--app-palette-divider)",
            }}
          >
            <div
              style={{
                display: "flex",
                alignItems: "flex-start",
                justifyContent: "space-between",
                gap: "var(--app-space-4)",
                marginBottom: 12,
              }}
            >
              <div style={{ minWidth: 0 }}>
                <AppTypography fontWeight={700} noWrap variant="subtitle2">
                  {gpu.model || `GPU ${idx + 1}`}
                </AppTypography>
                <AppTypography color="text.secondary" noWrap variant="caption">
                  {getGpuVendorLabel(gpu)} • {getGpuType(gpu)}
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
                {hasGpuValue(live?.runtime_status) && (
                  <Chip
                    color={
                      live.runtime_status === "active" ? "success" : "info"
                    }
                    label={live.runtime_status}
                    size="small"
                    variant="soft"
                  />
                )}
              </div>
            </div>

            {hasGpuValue(live?.utilization_percent) && (
              <MetricBar
                color="var(--app-palette-primary-main)"
                label="GPU Load"
                percent={live.utilization_percent}
                rightLabel={formatGpuPercent(live.utilization_percent)}
                tooltip={`Current GPU usage: ${formatGpuPercent(live.utilization_percent)}`}
              />
            )}
          </div>
        );
      })}
    </div>
  );
};

const GpuInfo = () => (
  <DashboardCard avatarIcon="bi:gpu-card" stats={<GpuStats />} title="GPU" />
);

export default GpuInfo;
