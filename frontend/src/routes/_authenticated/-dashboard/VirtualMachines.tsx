import { useSuspenseQuery } from "@tanstack/react-query";

import { linuxio, type VirtualMachine } from "@/api";
import DashboardCard from "@/components/cards/DashboardCard";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import StatusDot from "@/components/ui/StatusDot";

import DashboardStatRows from "./DashboardStatRows";
import {
  formatDisk,
  formatMemory,
  normalizeState,
  stateChipColor,
} from "../vm/-components/vmShared";

const VMS_REFETCH_MS = 5000;

const STATE_COLOR: Record<ReturnType<typeof stateChipColor>, string> = {
  success: "var(--app-palette-success-main)",
  warning: "var(--app-palette-warning-main)",
  error: "var(--app-palette-error-main)",
  default: "var(--app-palette-text-disabled)",
};

const selectTotals = (vms: VirtualMachine[]) => {
  const running = vms.filter((vm) => vm.state === "running");
  return {
    running: running.length,
    total: vms.length,
    vcpus: running.reduce((sum, vm) => sum + vm.vcpus, 0),
    memoryMB: running.reduce((sum, vm) => sum + vm.memoryMB, 0),
    diskGB: vms.reduce((sum, vm) => sum + vm.diskGB, 0),
  };
};

const selectSummaries = (vms: VirtualMachine[]) =>
  [...vms]
    .sort(
      (a, b) => Number(b.state === "running") - Number(a.state === "running"),
    )
    .map((vm) => ({
      name: vm.name,
      state: normalizeState(vm.state),
      color: STATE_COLOR[stateChipColor(vm.state)],
    }));

const VMStats = () => {
  const { data: totals } = useSuspenseQuery({
    ...linuxio.virt.list,
    refetchInterval: VMS_REFETCH_MS,
    select: selectTotals,
  });

  return (
    <DashboardStatRows
      rows={[
        { label: "Machines", value: `${totals.running}/${totals.total}` },
        { label: "vCPUs", value: totals.vcpus },
        { label: "Memory", value: formatMemory(totals.memoryMB) },
        { label: "Disk", value: formatDisk(totals.diskGB) },
      ]}
    />
  );
};

const VMList = () => {
  const { data: vms } = useSuspenseQuery({
    ...linuxio.virt.list,
    refetchInterval: VMS_REFETCH_MS,
    select: selectSummaries,
  });

  if (vms.length === 0) {
    return <AppTypography variant="body2">No virtual machines.</AppTypography>;
  }

  return (
    <div
      aria-label="Virtual machines"
      role="list"
      style={{
        display: "flex",
        flexDirection: "column",
        gap: "var(--app-space-4)",
        maxHeight: 110,
        overflowX: "hidden",
        overflowY: "auto",
        paddingRight: "var(--app-space-2)",
        width: "100%",
      }}
    >
      {vms.map((vm) => (
        <AppTooltip arrow key={vm.name} placement="top" title={vm.state}>
          <div
            role="listitem"
            style={{
              alignItems: "center",
              display: "flex",
              gap: "var(--app-space-8)",
              minWidth: 0,
            }}
          >
            <StatusDot color={vm.color} size={8} />
            <AppTypography noWrap variant="body2">
              {vm.name}
            </AppTypography>
          </div>
        </AppTooltip>
      ))}
    </div>
  );
};

const VirtualMachinesCard = () => (
  <DashboardCard
    avatarIcon="mdi:monitor-multiple"
    stats={<VMStats />}
    stats2={<VMList />}
    title="Virtual Machines"
  />
);

export default VirtualMachinesCard;
