import React from "react";

import type { SmartAttribute } from "../types";
import { formatDataUnits, formatPowerOnTime, getSmartNumber } from "../utils";

import AppDataTable from "@/components/tables/AppDataTable";
import type { AppDataTableColumnDef } from "@/components/tables/AppDataTable";
import AppTypography from "@/components/ui/AppTypography";

interface SmartAttributesTabProps {
  ataAttrs?: SmartAttribute[];
  isNvme: boolean;
  nvmeHealthRaw?: Record<string, unknown>;
}

interface SmartSummaryRow {
  attribute: string;
  id: string;
  value: React.ReactNode;
  valueStyle?: React.CSSProperties;
}

const smartSummaryColumns: AppDataTableColumnDef<SmartSummaryRow>[] = [
  {
    accessorKey: "attribute",
    header: "Attribute",
  },
  {
    accessorKey: "value",
    header: "Value",
    cell: ({ row }) => (
      <span style={row.original.valueStyle}>{row.original.value}</span>
    ),
    meta: { align: "right" },
  },
];

const ataAttributeColumns: AppDataTableColumnDef<SmartAttribute>[] = [
  {
    accessorKey: "id",
    header: "#",
  },
  {
    accessorKey: "name",
    header: "Attribute",
  },
  {
    accessorKey: "value",
    header: "Value",
    meta: { align: "right" },
  },
  {
    accessorKey: "worst",
    header: "Worst",
    meta: { align: "right" },
  },
  {
    accessorKey: "thresh",
    header: "Thresh",
    meta: { align: "right" },
  },
  {
    id: "raw",
    header: "Raw",
    cell: ({ row }) => (
      <span
        style={{
          color:
            [5, 196, 197, 198].includes(row.original.id) &&
            row.original.raw?.value &&
            row.original.raw.value > 0
              ? "var(--app-palette-warning-main)"
              : "inherit",
        }}
      >
        {row.original.raw?.string || row.original.raw?.value?.toLocaleString()}
      </span>
    ),
    meta: { align: "right" },
  },
];

export const SmartAttributesTab: React.FC<SmartAttributesTabProps> = ({
  isNvme,
  nvmeHealthRaw,
  ataAttrs,
}) => {
  if (isNvme && nvmeHealthRaw) {
    const rows: SmartSummaryRow[] = [];
    const addNumberRow = (
      id: string,
      attribute: string,
      input: unknown,
      format: (value: number) => React.ReactNode,
      valueStyle?: React.CSSProperties,
    ) => {
      const value = getSmartNumber(input);
      if (value === null) return;
      rows.push({
        attribute,
        id,
        value: format(value),
        valueStyle,
      });
    };

    const temperature = getSmartNumber(nvmeHealthRaw.temperature);
    const percentageUsed = getSmartNumber(nvmeHealthRaw.percentage_used);
    const mediaErrors = getSmartNumber(nvmeHealthRaw.media_errors);

    addNumberRow(
      "critical_warning",
      "Critical Warning",
      nvmeHealthRaw.critical_warning,
      (value) => `0x${value.toString(16).padStart(2, "0").toUpperCase()}`,
    );
    addNumberRow(
      "temperature",
      "Temperature",
      temperature,
      (value) => `${value} Celsius`,
      {
        color:
          (temperature ?? 0) > 70
            ? "var(--app-palette-error-main)"
            : (temperature ?? 0) > 50
              ? "var(--app-palette-warning-main)"
              : "inherit",
      },
    );
    addNumberRow(
      "available_spare",
      "Available Spare",
      nvmeHealthRaw.available_spare,
      (value) => `${value}%`,
    );
    addNumberRow(
      "available_spare_threshold",
      "Available Spare Threshold",
      nvmeHealthRaw.available_spare_threshold,
      (value) => `${value}%`,
    );
    addNumberRow(
      "percentage_used",
      "Percentage Used",
      percentageUsed,
      (value) => `${value}%`,
      {
        color:
          (percentageUsed ?? 0) > 90
            ? "var(--app-palette-error-main)"
            : (percentageUsed ?? 0) > 70
              ? "var(--app-palette-warning-main)"
              : "inherit",
      },
    );
    addNumberRow(
      "data_units_read",
      "Data Units Read",
      nvmeHealthRaw.data_units_read,
      (value) => formatDataUnits(value),
    );
    addNumberRow(
      "data_units_written",
      "Data Units Written",
      nvmeHealthRaw.data_units_written,
      (value) => formatDataUnits(value),
    );
    addNumberRow(
      "host_reads",
      "Host Read Commands",
      nvmeHealthRaw.host_reads,
      (value) => value.toLocaleString(),
    );
    addNumberRow(
      "host_writes",
      "Host Write Commands",
      nvmeHealthRaw.host_writes,
      (value) => value.toLocaleString(),
    );
    addNumberRow(
      "controller_busy_time",
      "Controller Busy Time",
      nvmeHealthRaw.controller_busy_time,
      (value) => value.toLocaleString(),
    );
    addNumberRow(
      "power_cycles",
      "Power Cycles",
      nvmeHealthRaw.power_cycles,
      (value) => value.toLocaleString(),
    );
    addNumberRow(
      "power_on_hours",
      "Power On Hours",
      nvmeHealthRaw.power_on_hours,
      (value) => formatPowerOnTime(value),
    );
    addNumberRow(
      "unsafe_shutdowns",
      "Unsafe Shutdowns",
      nvmeHealthRaw.unsafe_shutdowns,
      (value) => value.toLocaleString(),
    );
    addNumberRow(
      "media_errors",
      "Media and Data Integrity Errors",
      mediaErrors,
      (value) => value.toLocaleString(),
      {
        color:
          (mediaErrors ?? 0) > 0 ? "var(--app-palette-error-main)" : "inherit",
      },
    );
    addNumberRow(
      "num_err_log_entries",
      "Error Information Log Entries",
      nvmeHealthRaw.num_err_log_entries,
      (value) => value.toLocaleString(),
    );

    return (
      <AppDataTable
        ariaLabel="NVMe SMART attributes"
        columns={smartSummaryColumns}
        data={rows}
        density="compact"
        emptyMessage="No SMART attributes available for this drive."
        getRowId={(row) => row.id}
        maxHeight={400}
        variant="embedded"
      />
    );
  }

  if (ataAttrs && ataAttrs.length > 0) {
    return (
      <AppDataTable
        ariaLabel="ATA SMART attributes"
        columns={ataAttributeColumns}
        data={ataAttrs}
        density="compact"
        emptyMessage="No SMART attributes available for this drive."
        getRowId={(attr) => String(attr.id)}
        maxHeight={400}
        variant="embedded"
      />
    );
  }

  return (
    <AppTypography color="text.secondary">
      No SMART attributes available for this drive.
    </AppTypography>
  );
};
