import React from "react";

import { linuxio } from "@/api";
import DashboardCard from "@/components/cards/DashboardCard";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";

function formatUptime(seconds: number): string {
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);

  if (days > 0) return `${days}d ${hours}h`;
  return `${hours}h ${minutes}m`;
}

const SystemOverview: React.FC = () => {
  const theme = useAppTheme();

  const { data: hostInfo } = linuxio.system.get_host_info.useQuery({
    refetchInterval: 50000,
  });

  const { data: uptime } = linuxio.system.get_uptime.useQuery({
    refetchInterval: 30000,
  });

  const { data: cpuInfo } = linuxio.system.get_cpu_info.useQuery({
    refetchInterval: 5000,
  });

  const peakTemp = cpuInfo?.temperature
    ? Math.max(...Object.values(cpuInfo.temperature))
    : null;

  const stats = (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        alignSelf: "flex-start",
        width: "fit-content",
      }}
    >
      {[
        {
          label: "Hostname",
          value: hostInfo?.hostname ?? "---",
          tag: hostInfo?.os ?? "",
        },
        {
          label: "Platform",
          value: hostInfo?.platform ?? "---",
          tag: hostInfo?.platformVersion ?? "",
        },
        {
          label: "Kernel",
          value: hostInfo?.kernelVersion ?? "---",
          tag: hostInfo?.kernelArch ?? "",
        },
        {
          label: "Uptime",
          value: uptime != null ? formatUptime(uptime) : "---",
          tag: peakTemp != null ? `Peak: ${peakTemp.toFixed(1)}\u00B0C` : "",
        },
      ].map(({ label, value, tag }, index, rows) => (
        <div
          key={label}
          style={{
            display: "flex",
            alignItems: "baseline",
            justifyContent: "flex-start",
            paddingTop: theme.spacing(0.5),
            paddingBottom: theme.spacing(0.5),
            borderBottom:
              index === rows.length - 1
                ? "none"
                : "1px solid var(--app-palette-divider)",
            gap: theme.spacing(1),
          }}
        >
          <AppTypography
            variant="caption"
            color="text.secondary"
            style={{
              textTransform: "uppercase",
              letterSpacing: "0.06em",
              fontSize: "0.62rem",
              flexShrink: 0,
            }}
          >
            {label}
          </AppTypography>
          <AppTypography variant="body2" fontWeight={500} noWrap>
            {value}
          </AppTypography>
          {tag && (
            <AppTypography
              variant="caption"
              color="text.secondary"
              style={{ marginLeft: "auto", flexShrink: 0 }}
            >
              {tag}
            </AppTypography>
          )}
        </div>
      ))}
    </div>
  );

  return (
    <DashboardCard
      title="System Overview"
      stats={stats}
      avatarIcon={`simple-icons:${hostInfo?.platform || "linux"}`}
    />
  );
};

export default SystemOverview;
