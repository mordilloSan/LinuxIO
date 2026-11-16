import { useSuspenseQuery } from "@tanstack/react-query";
import { lazy, Suspense, useState } from "react";

import { linuxio } from "@/api";
import { GradientCircularGauge } from "@/components/gauge/CircularGauge";
import { DASHBOARD_REFETCH_FAST_MS } from "@/constants/liveCharts";
import useAuth from "@/hooks/useAuth";
import DashboardCard from "@/routes/_authenticated/-dashboard/DashboardCard";
import { formatFileSize } from "@/utils/formatters";

import DashboardStatRows from "./DashboardStatRows";

const MemoryOverflowDialog = lazy(() => import("./MemoryOverflowDialog"));

const selectRamUsagePercent = (memory: {
  total_bytes: number;
  used_bytes: number;
}): number =>
  memory.total_bytes > 0 ? (memory.used_bytes / memory.total_bytes) * 100 : 0;

const MemoryStats = () => {
  // Changing what the host does when memory runs out is admin-only, so only
  // an admin session gets the pencil.
  const { privileged } = useAuth();
  const [overflowOpen, setOverflowOpen] = useState(false);
  // The dialog loads on first use and then stays mounted, so that it can
  // animate closed.
  const [overflowLoaded, setOverflowLoaded] = useState(false);
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
    <>
      <DashboardStatRows
        rows={[
          {
            label: "Usage",
            value: `${formatFileSize(memory.used_bytes ?? 0, 2)} / ${formatFileSize(memory.total_bytes ?? 0, 2)}`,
          },
          {
            label: "Swap",
            value: `${formatFileSize(swapUsed, 2)} / ${formatFileSize(memory.swap_total_bytes ?? 0, 2)}`,
            onEdit: privileged
              ? () => {
                  setOverflowLoaded(true);
                  setOverflowOpen(true);
                }
              : undefined,
          },
          {
            label: "Docker",
            value: formatFileSize(memory.docker_used_bytes ?? 0, 2),
          },
          {
            label: "VMs",
            value: formatFileSize(memory.vm_used_bytes ?? 0, 2),
          },
        ]}
      />
      {overflowLoaded ? (
        <Suspense fallback={null}>
          <MemoryOverflowDialog
            onClose={() => setOverflowOpen(false)}
            open={overflowOpen}
          />
        </Suspense>
      ) : null}
    </>
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
