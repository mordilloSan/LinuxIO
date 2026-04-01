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
        },
        {
          label: "Platform",
          value: hostInfo
            ? `${hostInfo.platform} ${hostInfo.platformVersion}`.trim()
            : "---",
        },
        {
          label: "Kernel",
          value: hostInfo?.kernelVersion ?? "---",
        },
        {
          label: "Uptime",
          value: uptime != null ? formatUptime(uptime) : "---",
        },
      ].map(({ label, value }, index, rows) => (
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
