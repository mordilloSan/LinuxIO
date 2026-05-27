import { Icon } from "@iconify/react";
import React, { useState } from "react";

import { linuxio } from "@/api";
import DashboardCard from "@/components/cards/DashboardCard";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";

import SetDateTimeDialog from "./SetDateTimeDialog";
import SetHostnameDialog from "./SetHostnameDialog";

function formatUptime(seconds: number): string {
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);

  if (days > 0) return `${days}d ${hours}h`;
  return `${hours}h ${minutes}m`;
}

function formatServerTime(iso: string): string {
  const d = new Date(iso);
  return d.toLocaleString(undefined, {
    weekday: "short",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

interface OverviewRow {
  label: string;
  onEdit?: () => void;
  value: string;
}

const SystemOverview: React.FC = () => {
  const theme = useAppTheme();

  const { data: hostInfo } = linuxio.system.get_host_info.useQuery({
    refetchInterval: 50000,
  });
  const { data: uptime } = linuxio.system.get_uptime.useQuery({
    refetchInterval: 30000,
  });
  const { data: serverTime } = linuxio.system.get_server_time.useQuery({
    refetchInterval: 60000,
  });

  const [hostnameDialogOpen, setHostnameDialogOpen] = useState(false);
  const [dateTimeDialogOpen, setDateTimeDialogOpen] = useState(false);

  const rows: OverviewRow[] = [
    {
      label: "Date / Time",
      value: serverTime ? formatServerTime(serverTime) : "---",
      onEdit: () => setDateTimeDialogOpen(true),
    },
    {
      label: "Hostname",
      value: hostInfo?.hostname ?? "---",
      onEdit: () => setHostnameDialogOpen(true),
    },
    {
      label: "Platform",
      value: hostInfo
        ? `${hostInfo.platform} ${hostInfo.platformVersion}`.trim()
        : "---",
    },
    {
      label: "Uptime",
      value: uptime != null ? formatUptime(uptime) : "---",
    },
  ];

  const stats = (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        alignSelf: "flex-start",
        width: "fit-content",
      }}
    >
      {rows.map(({ label, value, onEdit }, index, items) => (
        <div
          key={label}
          onClick={onEdit}
          style={{
            display: "flex",
            alignItems: "baseline",
            justifyContent: "flex-start",
            paddingTop: theme.spacing(0.5),
            paddingBottom: theme.spacing(0.5),
            borderBottom:
              index === items.length - 1
                ? "none"
                : "1px solid var(--app-palette-divider)",
            gap: theme.spacing(1),
            cursor: onEdit ? "pointer" : undefined,
          }}
        >
          <AppTypography
            color="text.secondary"
            style={{
              textTransform: "uppercase",
              letterSpacing: "0.06em",
              fontSize: "0.62rem",
              flexShrink: 0,
            }}
            variant="caption"
          >
            {label}
          </AppTypography>
          <AppTypography fontWeight={500} noWrap variant="body2">
            {value}
          </AppTypography>
          {onEdit && (
            <Icon
              height={13}
              icon="mdi:pencil-outline"
              style={{
                color: theme.palette.text.secondary,
                flexShrink: 0,
                alignSelf: "center",
                opacity: 0.7,
              }}
              width={13}
            />
          )}
        </div>
      ))}
    </div>
  );

  return (
    <>
      <DashboardCard
        avatarIcon={`simple-icons:${hostInfo?.platform || "linux"}`}
        stats={stats}
        title="System Overview"
      />
      <SetHostnameDialog
        current={hostInfo?.hostname ?? ""}
        onClose={() => setHostnameDialogOpen(false)}
        open={hostnameDialogOpen}
      />
      <SetDateTimeDialog
        onClose={() => setDateTimeDialogOpen(false)}
        open={dateTimeDialogOpen}
      />
    </>
  );
};

export default SystemOverview;
