import { useSuspenseQuery } from "@tanstack/react-query";

import { linuxio } from "@/api";
import DashboardCard from "@/components/cards/DashboardCard";
import { GradientCircularGauge } from "@/components/gauge/CirularGauge";
import { useAppTheme } from "@/theme";
import { formatFileSize } from "@/utils/formaters";

import DashboardStatRows from "./DashboardStatRows";

const calculatePercentage = (used: number, total: number) =>
  ((used / total) * 100).toFixed(2);

const MemoryUsage = () => {
  const theme = useAppTheme();
  const { data: memoryData } = useSuspenseQuery(
    linuxio.system.get_memory_info.queryOptions({
      refetchInterval: 2000,
    }),
  );

  const ramUsagePercentage = memoryData?.system?.active
    ? parseFloat(
        calculatePercentage(memoryData.system.active, memoryData.system.total),
      )
    : 0;
  const swapUsed = Math.max(
    (memoryData?.system?.swapTotal ?? 0) - (memoryData?.system?.swapFree ?? 0),
    0,
  );

  const data = {
    title: "Memory Usage",
    stats2: (
      <GradientCircularGauge
        gradientColors={[
          theme.chart.tx,
          theme.palette.warning.main,
          theme.palette.error.main,
        ]}
        showPercentage={true}
        size={108}
        thickness={9.8}
        value={ramUsagePercentage}
      />
    ),
    stats: (
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
    ),
    avatarIcon: "la:memory",
  };

  return <DashboardCard {...data} />;
};

export default MemoryUsage;
