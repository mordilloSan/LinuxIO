import { Icon } from "@iconify/react";
import { useSuspenseQuery } from "@tanstack/react-query";
import { useState, type MouseEvent } from "react";

import { linuxio, type VirtualMachine, useCallMutation } from "@/api";
import DashboardCard from "@/components/cards/DashboardCard";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppMenu, { AppMenuItem } from "@/components/ui/AppMenu";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import StatusDot from "@/components/ui/StatusDot";

import DashboardStatRows from "./DashboardStatRows";
import {
  formatDisk,
  formatMemory,
  normalizeState,
  stateChipColor,
  type VMAction,
  VM_TOAST,
  vmIPAddresses,
} from "../vm/-components/vmShared";

const VMS_REFETCH_MS = 5000;

const STATE_COLOR: Record<ReturnType<typeof stateChipColor>, string> = {
  success: "var(--app-palette-success-main)",
  warning: "var(--app-palette-warning-main)",
  error: "var(--app-palette-error-main)",
  default: "var(--app-palette-text-disabled)",
};

const ACTION_PROGRESS_LABEL: Record<VMAction, string> = {
  start: "Starting",
  shutdown: "Shutting down",
  reboot: "Rebooting",
  force_off: "Powering off",
  suspend: "Suspending",
  resume: "Resuming",
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
      running: vm.state === "running",
      paused: vm.state === "paused",
      state: normalizeState(vm.state),
      color: STATE_COLOR[stateChipColor(vm.state)],
      detail: [
        `${vm.vcpus} vCPU`,
        formatMemory(vm.memoryMB),
        ...vmIPAddresses(vm),
      ].join(" · "),
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
  const [menuAnchor, setMenuAnchor] = useState<null | HTMLElement>(null);
  const [menuName, setMenuName] = useState<string | null>(null);
  const [pendingActions, setPendingActions] = useState<
    ReadonlyMap<string, VMAction>
  >(() => new Map());

  const actionConfig = (success: string, error: string) => ({
    success,
    error,
    toast: VM_TOAST,
  });
  const mutations: Record<
    VMAction,
    { mutateAsync: (request: { name: string }) => Promise<unknown> }
  > = {
    start: useCallMutation(
      linuxio.virt.start,
      actionConfig("VM started", "Failed to start VM"),
    ),
    shutdown: useCallMutation(
      linuxio.virt.shutdown,
      actionConfig("VM shutdown requested", "Failed to shutdown VM"),
    ),
    reboot: useCallMutation(
      linuxio.virt.reboot,
      actionConfig("VM reboot requested", "Failed to reboot VM"),
    ),
    force_off: useCallMutation(
      linuxio.virt.force_off,
      actionConfig("VM powered off", "Failed to force off VM"),
    ),
    suspend: useCallMutation(
      linuxio.virt.suspend,
      actionConfig("VM suspended", "Failed to suspend VM"),
    ),
    resume: useCallMutation(
      linuxio.virt.resume,
      actionConfig("VM resumed", "Failed to resume VM"),
    ),
  };

  // Derived from the list so the menu disappears with the row it belongs to
  // when a poll drops that machine.
  const menuVM = vms.find((vm) => vm.name === menuName);
  const menuOpen = menuAnchor !== null && menuVM !== undefined;

  const closeMenu = () => {
    setMenuAnchor(null);
    setMenuName(null);
  };
  const openMenu = (event: MouseEvent<HTMLElement>, name: string) => {
    event.preventDefault();
    if (pendingActions.has(name)) return;
    setMenuAnchor(event.currentTarget);
    setMenuName(name);
  };
  const runAction = (action: VMAction) => {
    if (!menuVM || pendingActions.has(menuVM.name)) return;
    const { name } = menuVM;
    setPendingActions((current) => new Map(current).set(name, action));
    closeMenu();
    void mutations[action]
      .mutateAsync({ name })
      .catch(() => undefined)
      .finally(() => {
        setPendingActions((current) => {
          if (current.get(name) !== action) return current;
          const next = new Map(current);
          next.delete(name);
          return next;
        });
      });
  };

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
      {vms.map((vm) => {
        const pendingAction = pendingActions.get(vm.name);
        const progressLabel = pendingAction
          ? ACTION_PROGRESS_LABEL[pendingAction]
          : undefined;
        return (
          <AppTooltip
            arrow
            key={vm.name}
            placement="top"
            title={
              <div style={{ textAlign: "center" }}>
                <AppTypography component="div" variant="body2">
                  {vm.detail}
                </AppTypography>
                <AppTypography
                  color={
                    progressLabel ? "var(--app-palette-info-main)" : vm.color
                  }
                  component="div"
                >
                  {progressLabel ?? vm.state}
                </AppTypography>
              </div>
            }
          >
            <div
              aria-busy={Boolean(pendingAction)}
              aria-label={
                progressLabel ? `${vm.name}: ${progressLabel}` : vm.name
              }
              onContextMenu={(event) => openMenu(event, vm.name)}
              role="listitem"
              style={{
                alignItems: "center",
                cursor: pendingAction ? "progress" : "context-menu",
                display: "flex",
                gap: "var(--app-space-8)",
                minWidth: 0,
              }}
            >
              {progressLabel ? (
                <AppCircularProgress
                  aria-label={`${progressLabel} ${vm.name}`}
                  size={8}
                />
              ) : (
                <StatusDot color={vm.color} size={8} />
              )}
              <AppTypography noWrap variant="body2">
                {vm.name}
              </AppTypography>
            </div>
          </AppTooltip>
        );
      })}
      <AppMenu
        anchorEl={menuAnchor}
        autoFocus={false}
        minWidth={140}
        onClose={closeMenu}
        open={menuOpen}
      >
        {!menuVM?.running && !menuVM?.paused && (
          <AppMenuItem
            onClick={() => runAction("start")}
            startAdornment={<Icon icon="mdi:play" width={18} />}
          >
            Start
          </AppMenuItem>
        )}
        {menuVM?.running && (
          <AppMenuItem
            onClick={() => runAction("shutdown")}
            startAdornment={<Icon icon="mdi:stop" width={18} />}
          >
            Shutdown
          </AppMenuItem>
        )}
        {menuVM?.running && (
          <AppMenuItem
            onClick={() => runAction("reboot")}
            startAdornment={<Icon icon="mdi:restart" width={18} />}
          >
            Reboot
          </AppMenuItem>
        )}
        {menuVM?.running && (
          <AppMenuItem
            onClick={() => runAction("suspend")}
            startAdornment={<Icon icon="mdi:pause" width={18} />}
          >
            Suspend
          </AppMenuItem>
        )}
        {menuVM?.paused && (
          <AppMenuItem
            onClick={() => runAction("resume")}
            startAdornment={<Icon icon="mdi:play-pause" width={18} />}
          >
            Resume
          </AppMenuItem>
        )}
        {(menuVM?.running || menuVM?.paused) && (
          <AppMenuItem
            onClick={() => runAction("force_off")}
            startAdornment={<Icon icon="mdi:power" width={18} />}
          >
            Force off
          </AppMenuItem>
        )}
      </AppMenu>
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
