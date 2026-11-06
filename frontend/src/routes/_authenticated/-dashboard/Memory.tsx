import { useQuery, useSuspenseQuery } from "@tanstack/react-query";

import { linuxio, type VirtualMachine } from "@/api";
import { GradientCircularGauge } from "@/components/gauge/CircularGauge";
import { DASHBOARD_REFETCH_FAST_MS } from "@/constants/liveCharts";
import { useAccessContext } from "@/hooks/useCapabilities";
import DashboardCard from "@/routes/_authenticated/-dashboard/DashboardCard";
import { formatFileSize } from "@/utils/formatters";

import DashboardStatRows from "./DashboardStatRows";

const VMS_REFETCH_MS = 5000;

const selectRamUsagePercent = (memory: {
  total_bytes: number;
  used_bytes: number;
}): number =>
  memory.total_bytes > 0 ? (memory.used_bytes / memory.total_bytes) * 100 : 0;

// Host RAM the running VMs actually hold (libvirt's balloon RSS), so the row
// reads like the Docker one rather than the allocation the VM card shows.
const selectVMUsedBytes = (vms: VirtualMachine[]): number =>
  vms.reduce((sum, vm) => sum + (vm.metrics?.memoryUsedMB ?? 0), 0) *
  1024 *
  1024;

const MemoryStats = () => {
  const { data: memory } = useSuspenseQuery({
    ...linuxio.monitoring.get_live,
    refetchInterval: DASHBOARD_REFETCH_FAST_MS,
    select: (live) => live.memory,
  });
  const { libvirtAvailable, privileged } = useAccessContext();
  // virt.list is a privileged call, so the row follows the VM card's gate.
  const vmsVisible = Boolean(libvirtAvailable && privileged);
  const { data: vmUsedBytes } = useQuery({
    ...linuxio.virt.list,
    enabled: vmsVisible,
    refetchInterval: VMS_REFETCH_MS,
    select: selectVMUsedBytes,
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
        ...(vmsVisible
          ? [{ label: "VMs", value: formatFileSize(vmUsedBytes, 2, "-") }]
          : []),
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
