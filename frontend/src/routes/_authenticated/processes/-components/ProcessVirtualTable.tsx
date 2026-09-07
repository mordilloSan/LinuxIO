import type { SortingState } from "@tanstack/react-table";
import { useState } from "react";

import type { Process, Program } from "@/api";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import AppTypography from "@/components/ui/AppTypography";
import { formatFileSize } from "@/utils/formaters";

import type { ProcessView } from "./processData";

const formatPercent = (value: number): string => `${value.toFixed(1)}%`;

const processColumns: AppVirtualTableColumnDef<Process>[] = [
  { accessorKey: "pid", header: "PID", meta: { align: "right", width: 80 } },
  {
    accessorKey: "name",
    cell: ({ row }) => (
      <AppTypography noWrap>{row.original.name || "—"}</AppTypography>
    ),
    header: "Name",
    meta: { width: "minmax(150px, 1.4fr)" },
  },
  {
    accessorKey: "username",
    header: "User",
    meta: { width: "minmax(110px, 1fr)" },
  },
  {
    accessorKey: "cpu_percent",
    cell: ({ row }) => formatPercent(row.original.cpu_percent),
    header: "CPU",
    meta: { align: "right", width: 90 },
  },
  {
    accessorFn: (process) => process.memory_info.rss,
    cell: ({ row }) => formatFileSize(row.original.memory_info.rss),
    header: "Memory RSS",
    id: "memoryRss",
    meta: { align: "right", width: 125 },
  },
  {
    accessorFn: (process) => process.io_counters.disk_read_bytes_per_second,
    cell: ({ row }) =>
      `${formatFileSize(row.original.io_counters.disk_read_bytes_per_second)}/s`,
    header: "Read",
    id: "diskReadBytesPerSecond",
    meta: { align: "right", width: 105 },
  },
  {
    accessorFn: (process) => process.io_counters.disk_write_bytes_per_second,
    cell: ({ row }) =>
      `${formatFileSize(row.original.io_counters.disk_write_bytes_per_second)}/s`,
    header: "Write",
    id: "diskWriteBytesPerSecond",
    meta: { align: "right", width: 105 },
  },
  {
    accessorKey: "num_threads",
    header: "Threads",
    meta: { align: "right", width: 85 },
  },
  {
    accessorKey: "container_name",
    cell: ({ row }) =>
      row.original.container_name || row.original.container_id || "—",
    header: "Container",
    meta: { width: "minmax(120px, 1fr)" },
  },
];

const programColumns: AppVirtualTableColumnDef<Program>[] = [
  {
    accessorKey: "name",
    header: "Name",
    meta: { width: "minmax(180px, 1.5fr)" },
  },
  {
    accessorKey: "count",
    header: "Processes",
    meta: { align: "right", width: 100 },
  },
  {
    accessorKey: "cpu_percent",
    cell: ({ row }) => formatPercent(row.original.cpu_percent),
    header: "CPU",
    meta: { align: "right", width: 90 },
  },
  {
    accessorKey: "memory_percent",
    cell: ({ row }) => formatPercent(row.original.memory_percent),
    header: "Memory",
    meta: { align: "right", width: 100 },
  },
  {
    accessorKey: "memory_rss_bytes",
    cell: ({ row }) => formatFileSize(row.original.memory_rss_bytes),
    header: "Memory RSS",
    meta: { align: "right", width: 125 },
  },
  {
    accessorKey: "pids",
    cell: ({ row }) => row.original.pids?.join(", ") || "—",
    header: "PIDs",
    meta: { width: "minmax(140px, 1fr)" },
  },
];

const ProcessTable = ({ data }: { data: Process[] }) => {
  const [sorting, setSorting] = useState<SortingState>([]);
  return (
    <AppVirtualTable
      ariaLabel="Processes"
      columns={processColumns}
      data={data}
      emptyMessage="No processes found."
      enableSorting
      fillAvailable
      getRowId={(row) => String(row.pid)}
      onSortingChange={setSorting}
      sorting={sorting}
    />
  );
};

const ProgramTable = ({ data }: { data: Program[] }) => {
  const [sorting, setSorting] = useState<SortingState>([]);
  return (
    <AppVirtualTable
      ariaLabel="Programs"
      columns={programColumns}
      data={data}
      emptyMessage="No programs found."
      enableSorting
      fillAvailable
      getRowId={(row, index) => `${row.name}-${index}`}
      onSortingChange={setSorting}
      sorting={sorting}
    />
  );
};

type ProcessVirtualTableProps =
  | { data: Process[]; mode: Extract<ProcessView, "processes"> }
  | { data: Program[]; mode: Extract<ProcessView, "programs"> };

const ProcessVirtualTable = (props: ProcessVirtualTableProps) =>
  props.mode === "processes" ? (
    <ProcessTable data={props.data} />
  ) : (
    <ProgramTable data={props.data} />
  );

export default ProcessVirtualTable;
