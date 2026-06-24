import { useMemo } from "react";
import type { CSSProperties } from "react";

import {
  type VMAction,
  formatMemory,
  normalizeState,
  stateChipColor,
  vmIPAddresses,
} from "./vmShared";

import type { VirtualMachine } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import AppDataTable from "@/components/tables/AppDataTable";
import type { AppDataTableColumnDef } from "@/components/tables/AppDataTable";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppChip from "@/components/ui/AppChip";

const listPanelStyle: CSSProperties = {
  minWidth: 0,
  overflow: "hidden",
};

const nameButtonStyle: CSSProperties = {
  background: "transparent",
  border: 0,
  color: "var(--app-palette-primary-main)",
  cursor: "pointer",
  font: "inherit",
  fontWeight: 600,
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

export default function VMListTable({
  actionPending,
  effectiveSelectedName,
  isLoading,
  onDelete,
  onOpenConsole,
  onRunAction,
  onSelect,
  vms,
}: {
  actionPending: boolean;
  effectiveSelectedName: string | null;
  isLoading: boolean;
  onDelete: (vm: VirtualMachine) => void;
  onOpenConsole: (vm: VirtualMachine) => void;
  onRunAction: (action: VMAction, vm: VirtualMachine) => void;
  onSelect: (name: string) => void;
  vms: VirtualMachine[];
}) {
  const columns = useMemo<AppDataTableColumnDef<VirtualMachine>[]>(
    () => [
      {
        accessorKey: "name",
        header: "Name",
        cell: ({ row }) => {
          const vm = row.original;
          return (
            <button
              onClick={() => onSelect(vm.name)}
              style={nameButtonStyle}
              type="button"
            >
              {vm.name}
            </button>
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
        meta: { width: "80px" },
      },
      {
        accessorKey: "memoryMB",
        header: "Memory",
        cell: ({ row }) => formatMemory(row.original.memoryMB),
        meta: { width: "120px" },
      },
      {
        id: "actions",
        header: "Actions",
        cell: ({ row }) => {
          const vm = row.original;
          const running = vm.state === "running";
          const paused = vm.state === "paused";
          return (
            <div
              onClick={(event) => event.stopPropagation()}
              style={rowActionsStyle}
            >
              <AppActionIconButton
                disabled={actionPending || running}
                icon="mdi:play"
                label="Start"
                onClick={() => onRunAction("start", vm)}
              />
              <AppActionIconButton
                disabled={actionPending || !running}
                icon="mdi:stop"
                label="Shutdown"
                onClick={() => onRunAction("shutdown", vm)}
              />
              <AppActionIconButton
                disabled={actionPending || !running}
                icon="mdi:restart"
                label="Reboot"
                onClick={() => onRunAction("reboot", vm)}
              />
              <AppActionIconButton
                disabled={actionPending || (!running && !paused)}
                icon="mdi:power"
                label="Force off"
                onClick={() => onRunAction("force_off", vm)}
              />
              <AppActionIconButton
                disabled={actionPending || !running}
                icon="mdi:pause"
                label="Suspend"
                onClick={() => onRunAction("suspend", vm)}
              />
              <AppActionIconButton
                disabled={actionPending || !paused}
                icon="mdi:play-pause"
                label="Resume"
                onClick={() => onRunAction("resume", vm)}
              />
              <AppActionIconButton
                disabled={!running || !vm.hasGraphics}
                icon="mdi:monitor"
                label="Console"
                onClick={() => onOpenConsole(vm)}
              />
              <AppActionIconButton
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
    [actionPending, onDelete, onOpenConsole, onRunAction, onSelect],
  );

  return (
    <FrostedCard style={listPanelStyle}>
      <AppDataTable
        ariaLabel="Virtual machines"
        columns={columns}
        data={isLoading ? [] : vms}
        emptyMessage={isLoading ? "Loading VMs" : "No virtual machines."}
        enableSorting={false}
        getRowId={(vm) => vm.name}
        onRowClick={(row) => onSelect(row.original.name)}
        selectedRowId={effectiveSelectedName}
        variant="embedded"
      />
    </FrostedCard>
  );
}
