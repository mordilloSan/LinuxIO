import { useSuspenseQuery } from "@tanstack/react-query";

import { linuxio, type MemoryInfoResponse } from "@/api";
import DashboardCard from "@/components/cards/DashboardCard";
import { GradientCircularGauge } from "@/components/gauge/CirularGauge";
import { DASHBOARD_REFETCH_MEMORY_MS } from "@/constants/liveCharts";
import { formatFileSize } from "@/utils/formaters";

import DashboardStatRows from "./DashboardStatRows";

const calculatePercentage = (used: number, total: number) =>
  ((used / total) * 100).toFixed(2);

const selectRamUsagePercent = (memoryData: MemoryInfoResponse): number =>
  memoryData?.system?.active
    ? parseFloat(
        calculatePercentage(memoryData.system.active, memoryData.system.total),
      )
    : 0;

const MemoryStats = () => {
  const { data: memoryData } = useSuspenseQuery({
    ...linuxio.system.get_memory_info,
    refetchInterval: DASHBOARD_REFETCH_MEMORY_MS,
  });

  const swapUsed = Math.max(
    (memoryData?.system?.swapTotal ?? 0) - (memoryData?.system?.swapFree ?? 0),
    0,
  );

  return (
    <DashboardStatRows
      rows={[
        {
          label: "Usage",
          value: `${formatFileSize(memoryData?.system?.active ?? 0, 2)} / ${formatFileSize(memoryData?.system?.total ?? 0, 2)}`,
        },
        {
          label: "Swap",
          value: `${formatFileSize(swapUsed, 2)} / ${formatFileSize(memoryData?.system?.swapTotal ?? 0, 2)}`,
        },
        {
          label: "Docker",
          value: formatFileSize(memoryData?.docker?.used ?? 0, 2),
        },
        {
          label: "ZFS ARC",
          value: formatFileSize(memoryData?.zfs?.arc ?? 0, 2),
        },
      ]}
    />
  );
};

const MemoryGauge = () => {
  const { data: ramUsagePercentage } = useSuspenseQuery({
    ...linuxio.system.get_memory_info,
    refetchInterval: DASHBOARD_REFETCH_MEMORY_MS,
    select: selectRamUsagePercent,
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
