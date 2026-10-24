import { useCallback, useMemo } from "react";
import type { CSSProperties } from "react";

import type { VirtualMachine } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import type { ReorderableSurface } from "@/components/reorder/useReorderableSurface";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import { useReorderableTableDnd } from "@/components/tables/useReorderableTableDnd";
import AppButton from "@/components/ui/AppButton";
import AppChip from "@/components/ui/AppChip";
import AppLinearProgress from "@/components/ui/AppLinearProgress";

import { ACTION_PROGRESS_LABEL } from "./useVMLifecycle";
import VMActions, { type VMActionHandlers } from "./VMActions";
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

const usageCellStyle: CSSProperties = {
  display: "flex",
  flexDirection: "column",
  gap: 4,
  minWidth: 0,
};

const getVirtualMachineId = (vm: VirtualMachine) => vm.name;

/** A value with a usage bar under it; no bar until live metrics exist. */
export function UsageCell({
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

export default function VMListTable({
  handlers,
  onSelect,
  pendingActions,
  surface,
}: {
  handlers: VMActionHandlers;
  onSelect: (name: string) => void;
  pendingActions: ReadonlyMap<string, VMAction>;
  surface: ReorderableSurface<VirtualMachine>;
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
        cell: ({ row }) => (
          // Keeps button and menu clicks from also selecting the row.
          <div onClick={(event) => event.stopPropagation()}>
            <VMActions
              handlers={handlers}
              pendingAction={pendingActions.get(row.original.name)}
              vm={row.original}
            />
          </div>
        ),
        meta: { align: "right", width: "minmax(210px, 0.9fr)" },
      },
    ],
    [handlers, onSelect, pendingActions],
  );
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
        variant="embedded"
      />
    </FrostedCard>
  );
}
