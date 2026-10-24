import { Icon } from "@iconify/react";
import { useSuspenseQuery } from "@tanstack/react-query";
import { useState, type MouseEvent } from "react";

import { linuxio, type VirtualMachine } from "@/api";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppMenu, { AppMenuItem } from "@/components/ui/AppMenu";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import StatusDot from "@/components/ui/StatusDot";
import { useMediaQuery } from "@/hooks/useMediaQuery";
import DashboardCard from "@/routes/_authenticated/-dashboard/DashboardCard";
import { up } from "@/theme/breakpoints";

import DashboardStatRows from "./DashboardStatRows";
import {
  ACTION_PROGRESS_LABEL,
  useVMLifecycle,
} from "../vm/-components/useVMLifecycle";
import {
  formatDisk,
  formatMemory,
  normalizeState,
  stateChipColor,
  type VMAction,
  vmIPAddresses,
  vmOSIcon,
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
      icon: vmOSIcon(vm.os),
      running: vm.state === "running",
      paused: vm.state === "paused",
      state: vm.state,
      stateLabel: normalizeState(vm.state),
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
  const { pendingActions, runAction } = useVMLifecycle(vms);
  const isSmallUp = useMediaQuery(up("sm"));

  // Derived from the list so the menu disappears with the row it belongs to
  // when a poll drops that machine.
  const menuVM = vms.find((vm) => vm.name === menuName);
  const menuOpen = menuAnchor !== null && menuVM !== undefined;

  const closeMenu = () => {
    setMenuAnchor(null);
    setMenuName(null);
  };
  const act = (action: VMAction, vm: { name: string }) => {
    closeMenu();
    runAction(action, vm);
  };
  const openMenu = (event: MouseEvent<HTMLElement>, name: string) => {
    event.preventDefault();
    if (pendingActions.has(name)) return;
    setMenuAnchor(event.currentTarget);
    setMenuName(name);
  };
  if (vms.length === 0) {
    return <AppTypography variant="body2">No virtual machines.</AppTypography>;
  }

  return (
    <div
      aria-label="Virtual machines"
      role="list"
      style={{
        display: "grid",
        gridTemplateColumns: isSmallUp ? "repeat(4, 36px)" : "repeat(3, 36px)",
        columnGap: "var(--app-space-16)",
        rowGap: "var(--app-space-20)",
        justifyContent: "center",
        width: "fit-content",
        maxHeight: 110,
        overflowX: "hidden",
        overflowY: "auto",
        paddingRight: "var(--app-space-2)",
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
                  {progressLabel ?? vm.stateLabel}
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
                cursor: pendingAction ? "progress" : "context-menu",
                // Flex blockifies the inline icon so its baseline descent
                // doesn't overflow the cell and add a scrollbar.
                display: "flex",
                height: 36,
                position: "relative",
                width: 36,
              }}
            >
              <Icon
                color="var(--app-palette-text-primary)"
                height={36}
                icon={vm.icon}
                style={{ opacity: vm.running ? 1 : 0.45 }}
                width={36}
              />
              {progressLabel && (
                <div
                  style={{
                    alignItems: "center",
                    backgroundColor: "var(--app-overlay-dark)",
                    borderRadius: "inherit",
                    display: "flex",
                    inset: 0,
                    justifyContent: "center",
                    position: "absolute",
                    zIndex: 1,
                  }}
                >
                  <AppCircularProgress
                    aria-label={`${progressLabel} ${vm.name}`}
                    color="inherit"
                    size={20}
                  />
                </div>
              )}
              <StatusDot
                color={vm.color}
                size={8}
                style={{
                  border: "1.5px solid var(--app-palette-background-paper)",
                  bottom: 0,
                  position: "absolute",
                  right: 0,
                }}
              />
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
            onClick={() => menuVM && act("start", menuVM)}
            startAdornment={<Icon icon="mdi:play" width={18} />}
          >
            Start
          </AppMenuItem>
        )}
        {menuVM?.running && (
          <AppMenuItem
            onClick={() => menuVM && act("shutdown", menuVM)}
            startAdornment={<Icon icon="mdi:stop" width={18} />}
          >
            Shutdown
          </AppMenuItem>
        )}
        {menuVM?.running && (
          <AppMenuItem
            onClick={() => menuVM && act("reboot", menuVM)}
            startAdornment={<Icon icon="mdi:restart" width={18} />}
          >
            Reboot
          </AppMenuItem>
        )}
        {menuVM?.running && (
          <AppMenuItem
            onClick={() => menuVM && act("suspend", menuVM)}
            startAdornment={<Icon icon="mdi:pause" width={18} />}
          >
            Suspend
          </AppMenuItem>
        )}
        {menuVM?.paused && (
          <AppMenuItem
            onClick={() => menuVM && act("resume", menuVM)}
            startAdornment={<Icon icon="mdi:play-pause" width={18} />}
          >
            Resume
          </AppMenuItem>
        )}
        {(menuVM?.running || menuVM?.paused) && (
          <AppMenuItem
            onClick={() => menuVM && act("force_off", menuVM)}
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
