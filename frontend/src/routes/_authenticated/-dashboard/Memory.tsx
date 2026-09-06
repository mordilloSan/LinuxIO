import { useSuspenseQuery } from "@tanstack/react-query";

import { linuxio } from "@/api";
import DashboardCard from "@/components/cards/DashboardCard";
import { GradientCircularGauge } from "@/components/gauge/CirularGauge";
import { DASHBOARD_REFETCH_FAST_MS } from "@/constants/liveCharts";
import { formatFileSize } from "@/utils/formaters";

import DashboardStatRows from "./DashboardStatRows";

const selectRamUsagePercent = (memory: {
  total_bytes: number;
  used_bytes: number;
}): number =>
  memory.total_bytes > 0 ? (memory.used_bytes / memory.total_bytes) * 100 : 0;

const MemoryStats = () => {
  const { data: memory } = useSuspenseQuery({
    ...linuxio.monitoring.get_live,
    refetchInterval: DASHBOARD_REFETCH_FAST_MS,
    select: (live) => live.memory,
  });

  const swapUsed = Math.max(
    (memory.swap_total_bytes ?? 0) - (memory.swap_free_bytes ?? 0),
    0,
  );

  return (
    <DashboardStatRows
      rows={[
        {
          label: "Usage",
          value: `${formatFileSize(memory.used_bytes ?? 0, 2)} / ${formatFileSize(memory.total_bytes ?? 0, 2)}`,
        },
        {
          label: "Swap",
          value: `${formatFileSize(swapUsed, 2)} / ${formatFileSize(memory.swap_total_bytes ?? 0, 2)}`,
        },
        {
          label: "Docker",
          value: formatFileSize(memory.docker_used_bytes ?? 0, 2),
        },
        {
          label: "ZFS ARC",
          value: formatFileSize(memory.zfs_arc_bytes ?? 0, 2),
        },
      ]}
    />
  );
};

const MemoryGauge = () => {
  const { data: ramUsagePercentage } = useSuspenseQuery({
    ...linuxio.monitoring.get_live,
    refetchInterval: DASHBOARD_REFETCH_FAST_MS,
    select: (live) => selectRamUsagePercent(live.memory),
  });

  return (
    <GradientCircularGauge
      showPercentage={true}
      size={108}
      thickness={9.8}
      value={ramUsagePercentage}
    />
  );
};

const MemoryUsage = () => (
  <DashboardCard
    avatarIcon="la:memory"
    stats={<MemoryStats />}
    stats2={<MemoryGauge />}
    title="Memory Usage"
  />
);

export default MemoryUsage;
