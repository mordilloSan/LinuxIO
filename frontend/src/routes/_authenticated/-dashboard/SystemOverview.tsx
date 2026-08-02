import { useSuspenseQueries } from "@tanstack/react-query";
import { useState } from "react";

import { linuxio } from "@/api";
import DashboardCard from "@/components/cards/DashboardCard";

import DashboardStatRows from "./DashboardStatRows";
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

const SystemOverview = () => {
  const [{ data: hostInfo }, { data: uptime }, { data: serverTime }] =
    useSuspenseQueries({
      queries: [
        linuxio.system.get_host_info.queryOptions({ refetchInterval: 50000 }),
        linuxio.system.get_uptime.queryOptions({ refetchInterval: 30000 }),
        linuxio.system.get_server_time.queryOptions({
          refetchInterval: 60000,
        }),
      ],
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

  const stats = <DashboardStatRows rows={rows} />;

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
