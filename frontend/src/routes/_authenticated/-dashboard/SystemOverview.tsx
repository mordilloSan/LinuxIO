import { useSuspenseQueries, useSuspenseQuery } from "@tanstack/react-query";
import { useState } from "react";

import { type HostInfo, linuxio } from "@/api";
import DashboardCard from "@/components/cards/DashboardCard";

import DashboardStatRows from "./DashboardStatRows";
import SetDateTimeDialog from "./SetDateTimeDialog";
import SetHostnameDialog from "./SetHostnameDialog";

const HOST_INFO_REFETCH_MS = 50000;

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

const selectPlatform = (hostInfo: HostInfo): string =>
  hostInfo?.platform || "linux";

interface OverviewRow {
  label: string;
  onEdit?: () => void;
  value: string;
}

const OverviewStats = () => {
  const [{ data: hostInfo }, { data: uptime }, { data: serverTime }] =
    useSuspenseQueries({
      queries: [
        linuxio.system.get_host_info.queryOptions({
          refetchInterval: HOST_INFO_REFETCH_MS,
        }),
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

  return (
    <>
      <DashboardStatRows rows={rows} />
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

const SystemOverview = () => {
  const { data: platform } = useSuspenseQuery(
    linuxio.system.get_host_info.queryOptions({
      refetchInterval: HOST_INFO_REFETCH_MS,
      select: selectPlatform,
    }),
  );

  return (
    <DashboardCard
      avatarIcon={`simple-icons:${platform}`}
      stats={<OverviewStats />}
      title="System Overview"
    />
  );
};

export default SystemOverview;
