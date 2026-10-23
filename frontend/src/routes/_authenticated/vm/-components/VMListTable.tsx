import { Icon } from "@iconify/react";
import { useCallback, useMemo, useState } from "react";
import type { CSSProperties } from "react";

import type { VirtualMachine } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import { useReorderableSurface } from "@/components/reorder/useReorderableSurface";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import { useReorderableTableDnd } from "@/components/tables/useReorderableTableDnd";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppButton from "@/components/ui/AppButton";
import AppChip from "@/components/ui/AppChip";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppMenu, { AppMenuItem } from "@/components/ui/AppMenu";

import { ACTION_PROGRESS_LABEL } from "./useVMLifecycle";
import {
  type VMAction,
  formatMemory,
  normalizeState,
  stateChipColor,
  vmIPAddresses,
} from "./vmShared";

const listPanelStyle: CSSProperties = {
  minWidth: 0,
  overflow: "hidden",
};

const nameButtonStyle: CSSProperties = {
  background: "transparent",
  border: 0,
  borderRadius: 0,
  color: "var(--app-palette-primary-main)",
  cursor: "pointer",
  font: "inherit",
  fontWeight: 600,
  minWidth: 0,
  padding: 0,
};

const mutedCellStyle: CSSProperties = {
  color: "var(--app-palette-text-secondary)",
};

const rowActionsStyle: CSSProperties = {
  alignItems: "center",
  display: "flex",
  flexWrap: "wrap",
  gap: 6,
};

const usageCellStyle: CSSProperties = {
  display: "flex",
  flexDirection: "column",
  gap: 4,
  minWidth: 0,
};

const getVirtualMachineId = (vm: VirtualMachine) => vm.name;

/** A value with a usage bar under it; no bar until live metrics exist. */
function UsageCell({
  label,
  percent,
  value,
}: {
  label: string;
  percent?: number;
  value: string;
}) {
  return (
    <div style={usageCellStyle}>
      <span style={percent === undefined ? mutedCellStyle : undefined}>
        {value}
      </span>
      {percent !== undefined && (
        <AppLinearProgress
          aria-label={label}
          value={Math.min(percent, 100)}
          variant="determinate"
        />
      )}
    </div>
  );
}

const POWER_ACTION: Record<
  string,
  { action: VMAction; icon: string; label: string }
> = {
  paused: { action: "resume", icon: "mdi:play-pause", label: "Resume" },
  running: { action: "shutdown", icon: "mdi:stop", label: "Shutdown" },
};
const START_ACTION = { action: "start", icon: "mdi:play", label: "Start" };

export default function VMListTable({
  effectiveSelectedName,
  onDelete,
  onEdit,
  onOpenConsole,
  onRunAction,
  onSelect,
  pendingActions,
  vms,
}: {
  effectiveSelectedName: string | null;
  onDelete: (vm: VirtualMachine) => void;
  onEdit: (vm: VirtualMachine) => void;
  onOpenConsole: (vm: VirtualMachine) => void;
  onRunAction: (action: VMAction, vm: VirtualMachine) => void;
  onSelect: (name: string) => void;
  pendingActions: ReadonlyMap<string, VMAction>;
  vms: VirtualMachine[];
}) {
  const [menu, setMenu] = useState<{
    anchor: HTMLElement;
    name: string;
  } | null>(null);
  // Derived from the list so the menu closes with the row it belongs to when
  // a poll drops that machine.
  const menuVM = vms.find((vm) => vm.name === menu?.name);
  const closeMenu = () => setMenu(null);
  const runMenuAction = (action: VMAction, vm: VirtualMachine) => {
    closeMenu();
    onRunAction(action, vm);
  };

  const columns = useMemo<AppVirtualTableColumnDef<VirtualMachine>[]>(
    () => [
      {
        accessorKey: "name",
        header: "Name",
        cell: ({ row }) => {
          const vm = row.original;
          return (
            <AppButton
              color="primary"
              onClick={() => onSelect(vm.name)}
              size="small"
              style={nameButtonStyle}
            >
              {vm.name}
            </AppButton>
          );
        },
        meta: { width: "minmax(150px, 1.1fr)" },
      },
      {
        accessorKey: "state",
        header: "State",
        cell: ({ row }) => {
          const pendingAction = pendingActions.get(row.original.name);
          return pendingAction ? (
            <AppChip
              color="info"
              label={ACTION_PROGRESS_LABEL[pendingAction]}
              size="small"
              variant="soft"
            />
          ) : (
            <AppChip
              color={stateChipColor(row.original.state)}
              label={normalizeState(row.original.state)}
              size="small"
              variant="soft"
            />
          );
        },
        meta: { width: "minmax(110px, 0.8fr)" },
      },
      {
        accessorKey: "vcpus",
        header: "CPU",
        cell: ({ row }) => {
          const { metrics, vcpus } = row.original;
          const percent = metrics?.cpuPercent;
          return (
            <UsageCell
              label="CPU usage"
              percent={percent}
              value={
                percent === undefined
                  ? `${vcpus} vCPU`
                  : `${percent.toFixed(1)}%`
              }
            />
          );
        },
        meta: { width: "minmax(120px, 0.9fr)" },
      },
      {
        accessorKey: "memoryMB",
        header: "Memory",
        cell: ({ row }) => {
          const { memoryMB, metrics } = row.original;
          const total = formatMemory(memoryMB);
          if (!metrics) {
            return <UsageCell label="Memory usage" value={total} />;
          }
          return (
            <UsageCell
              label="Memory usage"
              percent={memoryMB ? (metrics.memoryUsedMB / memoryMB) * 100 : 0}
              value={`${formatMemory(metrics.memoryUsedMB)} / ${total}`}
            />
          );
        },
        meta: { width: "minmax(150px, 1fr)" },
      },
      {
        id: "ipAddresses",
        header: "IP",
        cell: ({ row }) => {
          const addresses = vmIPAddresses(row.original);
          if (addresses.length === 0) {
            return <span style={mutedCellStyle}>Unavailable</span>;
          }
          return addresses.join(", ");
        },
        meta: { width: "minmax(150px, 1fr)" },
      },
      {
        id: "actions",
        header: "Actions",
        cell: ({ row }) => {
          const vm = row.original;
          const running = vm.state === "running";
          const paused = vm.state === "paused";
          const pendingAction = pendingActions.get(vm.name);
          const rowBusy = Boolean(pendingAction);
          const power = POWER_ACTION[vm.state] ?? START_ACTION;
          return (
            <div
              aria-busy={rowBusy}
              onClick={(event) => event.stopPropagation()}
              style={rowActionsStyle}
            >
              <AppActionIconButton
                disabled={rowBusy}
                icon={power.icon}
                label={power.label}
                loading={pendingAction === power.action}
                onClick={() => onRunAction(power.action, vm)}
              />
              <AppActionIconButton
                disabled={!running || !vm.hasGraphics}
                icon="mdi:monitor"
                label="Console"
                onClick={() => onOpenConsole(vm)}
              />
              <AppActionIconButton
                ariaLabel="Edit"
                disabled={rowBusy || vm.state !== "shutoff"}
                icon="mdi:pencil"
                label={
                  vm.state === "shutoff" ? "Edit" : "Shut off the VM to edit it"
                }
                onClick={() => onEdit(vm)}
              />
              <AppActionIconButton
                disabled={rowBusy}
                icon="mdi:trash-can-outline"
                label="Delete"
                onClick={() => onDelete(vm)}
              />
              <AppActionIconButton
                disabled={rowBusy || (!running && !paused)}
                icon="mdi:dots-horizontal"
                label="More actions"
                onClick={(event) =>
                  setMenu({ anchor: event.currentTarget, name: vm.name })
                }
              />
            </div>
          );
        },
        meta: { align: "right", width: "minmax(210px, 0.9fr)" },
      },
    ],
    [onDelete, onEdit, onOpenConsole, onRunAction, onSelect, pendingActions],
  );
  const surface = useReorderableSurface({
    getId: getVirtualMachineId,
    items: vms,
    surface: "vm.list",
  });
  const tableDnd = useReorderableTableDnd<VirtualMachine, VirtualMachine>({
    handleAriaLabel: "Reorder virtual machine",
    surface,
  });

  const handleRowClick = useCallback(
    ({ original: vm }: { original: VirtualMachine }) => onSelect(vm.name),
    [onSelect],
  );

  return (
    <>
      <FrostedCard style={listPanelStyle}>
        <AppVirtualTable
          ariaLabel="Virtual machines"
          columns={columns}
          data={surface.items}
          dnd={tableDnd}
          emptyMessage="No virtual machines."
          enableSorting={false}
          fillAvailable={false}
          getRowId={getVirtualMachineId}
          maxHeight={400}
          onRowClick={handleRowClick}
          selectedRowId={effectiveSelectedName}
          variant="embedded"
        />
      </FrostedCard>
      <AppMenu
        anchorEl={menu?.anchor ?? null}
        ariaLabel="More actions"
        minWidth={160}
        onClose={closeMenu}
        open={menu !== null && menuVM !== undefined}
      >
        {menuVM?.state === "running" && (
          <AppMenuItem
            onClick={() => runMenuAction("reboot", menuVM)}
            startAdornment={<Icon icon="mdi:restart" width={18} />}
          >
            Reboot
          </AppMenuItem>
        )}
        {menuVM?.state === "running" && (
          <AppMenuItem
            onClick={() => runMenuAction("suspend", menuVM)}
            startAdornment={<Icon icon="mdi:pause" width={18} />}
          >
            Suspend
          </AppMenuItem>
        )}
        {menuVM && (
          <AppMenuItem
            danger
            onClick={() => runMenuAction("force_off", menuVM)}
            startAdornment={<Icon icon="mdi:power" width={18} />}
          >
            Force off
          </AppMenuItem>
        )}
      </AppMenu>
    </>
  );
}
