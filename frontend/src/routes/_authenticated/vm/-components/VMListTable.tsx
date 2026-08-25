import { useCallback, useMemo } from "react";
import type { CSSProperties } from "react";

import type { VirtualMachine } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppButton from "@/components/ui/AppButton";
import AppChip from "@/components/ui/AppChip";
import { useReorderableSurface } from "@/hooks/useReorderableSurface";
import { useReorderableTableDnd } from "@/hooks/useReorderableTableDnd";

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

const getVirtualMachineId = (vm: VirtualMachine) => vm.name;

export default function VMListTable({
  effectiveSelectedName,
  onDelete,
  onOpenConsole,
  onRunAction,
  onSelect,
  pendingActions,
  vms,
}: {
  effectiveSelectedName: string | null;
  onDelete: (vm: VirtualMachine) => void;
  onOpenConsole: (vm: VirtualMachine) => void;
  onRunAction: (action: VMAction, vm: VirtualMachine) => void;
  onSelect: (name: string) => void;
  pendingActions: ReadonlyMap<string, VMAction>;
  vms: VirtualMachine[];
}) {
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
        cell: ({ row }) => (
          <AppChip
            color={stateChipColor(row.original.state)}
            label={normalizeState(row.original.state)}
            size="small"
            variant="soft"
          />
        ),
        meta: { width: "minmax(110px, 0.8fr)" },
      },
      {
        id: "ipAddresses",
        header: "IP",
        cell: ({ row }) => {
          const addresses = vmIPAddresses(row.original);
          if (addresses.length === 0) {
            return <span style={mutedCellStyle}>No lease</span>;
          }
          return addresses.join(", ");
        },
        meta: { width: "minmax(150px, 1fr)" },
      },
      {
        accessorKey: "vcpus",
        header: "CPU",
        cell: ({ row }) => row.original.vcpus,
        meta: { align: "right", width: "80px" },
      },
      {
        accessorKey: "memoryMB",
        header: "Memory",
        cell: ({ row }) => formatMemory(row.original.memoryMB),
        meta: { align: "right", width: "120px" },
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
          return (
            <div
              aria-busy={rowBusy}
              onClick={(event) => event.stopPropagation()}
              style={rowActionsStyle}
            >
              <AppActionIconButton
                disabled={rowBusy || running}
                icon="mdi:play"
                label="Start"
                loading={pendingAction === "start"}
                onClick={() => onRunAction("start", vm)}
              />
              <AppActionIconButton
                disabled={rowBusy || !running}
                icon="mdi:stop"
                label="Shutdown"
                loading={pendingAction === "shutdown"}
                onClick={() => onRunAction("shutdown", vm)}
              />
              <AppActionIconButton
                disabled={rowBusy || !running}
                icon="mdi:restart"
                label="Reboot"
                loading={pendingAction === "reboot"}
                onClick={() => onRunAction("reboot", vm)}
              />
              <AppActionIconButton
                disabled={rowBusy || (!running && !paused)}
                icon="mdi:power"
                label="Force off"
                loading={pendingAction === "force_off"}
                onClick={() => onRunAction("force_off", vm)}
              />
              <AppActionIconButton
                disabled={rowBusy || !running}
                icon="mdi:pause"
                label="Suspend"
                loading={pendingAction === "suspend"}
                onClick={() => onRunAction("suspend", vm)}
              />
              <AppActionIconButton
                disabled={rowBusy || !paused}
                icon="mdi:play-pause"
                label="Resume"
                loading={pendingAction === "resume"}
                onClick={() => onRunAction("resume", vm)}
              />
              <AppActionIconButton
                disabled={rowBusy || !running || !vm.hasGraphics}
                icon="mdi:monitor"
                label="Console"
                onClick={() => onOpenConsole(vm)}
              />
              <AppActionIconButton
                disabled={rowBusy}
                icon="mdi:trash-can-outline"
                label="Delete"
                onClick={() => onDelete(vm)}
              />
            </div>
          );
        },
        meta: { align: "right", width: "minmax(320px, 1.4fr)" },
      },
    ],
    [onDelete, onOpenConsole, onRunAction, onSelect, pendingActions],
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
  );
}
