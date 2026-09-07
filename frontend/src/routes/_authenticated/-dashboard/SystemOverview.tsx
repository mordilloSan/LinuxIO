import { useSuspenseQueries, useSuspenseQuery } from "@tanstack/react-query";
import { useState } from "react";

import { CACHE_TTL_MS, type HostInfo, linuxio } from "@/api";
import DashboardCard from "@/components/cards/DashboardCard";
import { DASHBOARD_REFETCH_FAST_MS } from "@/constants/liveCharts";
import { getDistroIcon } from "@/icons/distro";

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

const selectPlatform = (hostInfo: HostInfo): string =>
  hostInfo?.platform || "linux";

interface OverviewRow {
  label: string;
  onEdit?: () => void;
  value: string;
}

const OverviewStats = () => {
  const [{ data: hostInfo }, { data: live }, { data: serverTime }] =
    useSuspenseQueries({
      queries: [
        {
          ...linuxio.system.get_host_info,
          staleTime: CACHE_TTL_MS.ONE_DAY,
        },
        {
          ...linuxio.monitoring.get_live,
          refetchInterval: DASHBOARD_REFETCH_FAST_MS,
        },
        { ...linuxio.system.get_server_time, refetchInterval: 60000 },
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
      value: live ? formatUptime(live.uptime_seconds) : "---",
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
  const { data: platform } = useSuspenseQuery({
    ...linuxio.system.get_host_info,
    staleTime: CACHE_TTL_MS.ONE_DAY,
    select: selectPlatform,
  });

  return (
    <DashboardCard
      avatarIcon={getDistroIcon(platform)}
      stats={<OverviewStats />}
      title="System Overview"
    />
  );
};

export default SystemOverview;
