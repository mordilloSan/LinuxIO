import type { CSSProperties, ReactNode } from "react";

import AppVirtualDataTable from "@/components/tables/AppVirtualDataTable";
import type { AppVirtualDataTableColumnDef } from "@/components/tables/AppVirtualDataTable";
import AppTypography from "@/components/ui/AppTypography";

import type { SmartAttribute, SmartData } from "../types";
import { formatDataUnits, formatPowerOnTime, getSmartNumber } from "../utils";

interface SmartAttributesTabProps {
  ataAttrs?: SmartAttribute[];
  isNvme: boolean;
  smartData?: SmartData;
  smartError?: string;
  nvmeHealthRaw?: Record<string, unknown>;
}

interface SmartSummaryRow {
  attribute: string;
  id: string;
  value: ReactNode;
  valueStyle?: CSSProperties;
}

const smartSummaryColumns: AppVirtualDataTableColumnDef<SmartSummaryRow>[] = [
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

const ataAttributeColumns: AppVirtualDataTableColumnDef<SmartAttribute>[] = [
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
            (getSmartNumber(row.original.raw?.value) ?? 0) > 0
              ? "var(--app-palette-warning-main)"
              : "inherit",
        }}
      >
        {row.original.raw?.string ||
          getSmartNumber(row.original.raw?.value)?.toLocaleString()}
      </span>
    ),
    meta: { align: "right" },
  },
];

export const SmartAttributesTab = ({
  isNvme,
  smartData,
  smartError,
  nvmeHealthRaw,
  ataAttrs,
}: SmartAttributesTabProps) => {
  if (isNvme && (nvmeHealthRaw || smartData)) {
    const rows: SmartSummaryRow[] = [];
    const addNumberRow = (
      id: string,
      attribute: string,
      input: unknown,
      format: (value: number) => ReactNode,
      valueStyle?: CSSProperties,
    ) => {
      if (rows.some((row) => row.id === id)) return;
      const value = getSmartNumber(input);
      if (value === null) return;
      rows.push({
        attribute,
        id,
        value: format(value),
        valueStyle,
      });
    };

    const temperature = getSmartNumber(
      nvmeHealthRaw?.temperature ?? smartData?.temperature?.current,
    );
    const percentageUsed = getSmartNumber(nvmeHealthRaw?.percentage_used);
    const mediaErrors = getSmartNumber(nvmeHealthRaw?.media_errors);

    addNumberRow(
      "critical_warning",
      "Critical Warning",
      nvmeHealthRaw?.critical_warning,
      (value) => `0x${value.toString(16).padStart(2, "0").toUpperCase()}`,
    );
    addNumberRow(
      "temperature",
      "Temperature",
      nvmeHealthRaw?.temperature ?? smartData?.temperature?.current,
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
      nvmeHealthRaw?.available_spare,
      (value) => `${value}%`,
    );
    addNumberRow(
      "available_spare_threshold",
      "Available Spare Threshold",
      nvmeHealthRaw?.available_spare_threshold,
      (value) => `${value}%`,
    );
    addNumberRow(
      "percentage_used",
      "Percentage Used",
      nvmeHealthRaw?.percentage_used,
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
      nvmeHealthRaw?.data_units_read,
      (value) => formatDataUnits(value),
    );
    addNumberRow(
      "data_units_written",
      "Data Units Written",
      nvmeHealthRaw?.data_units_written,
      (value) => formatDataUnits(value),
    );
    addNumberRow(
      "host_reads",
      "Host Read Commands",
      nvmeHealthRaw?.host_reads,
      (value) => value.toLocaleString(),
    );
    addNumberRow(
      "host_writes",
      "Host Write Commands",
      nvmeHealthRaw?.host_writes,
      (value) => value.toLocaleString(),
    );
    addNumberRow(
      "controller_busy_time",
      "Controller Busy Time",
      nvmeHealthRaw?.controller_busy_time,
      (value) => value.toLocaleString(),
    );
    addNumberRow(
      "power_cycles",
      "Power Cycles",
      nvmeHealthRaw?.power_cycles ?? smartData?.power_cycle_count,
      (value) => value.toLocaleString(),
    );
    addNumberRow(
      "power_on_hours",
      "Power On Hours",
      nvmeHealthRaw?.power_on_hours ?? smartData?.power_on_time?.hours,
      (value) => formatPowerOnTime(value),
    );
    addNumberRow(
      "unsafe_shutdowns",
      "Unsafe Shutdowns",
      nvmeHealthRaw?.unsafe_shutdowns,
      (value) => value.toLocaleString(),
    );
    addNumberRow(
      "media_errors",
      "Media and Data Integrity Errors",
      nvmeHealthRaw?.media_errors,
      (value) => value.toLocaleString(),
      {
        color:
          (mediaErrors ?? 0) > 0 ? "var(--app-palette-error-main)" : "inherit",
      },
    );
    addNumberRow(
      "num_err_log_entries",
      "Error Information Log Entries",
      nvmeHealthRaw?.num_err_log_entries,
      (value) => value.toLocaleString(),
    );

    if (rows.length > 0) {
      return (
        <AppVirtualDataTable
          ariaLabel="NVMe SMART attributes"
          columns={smartSummaryColumns}
          data={rows}
          density="compact"
          emptyMessage="No SMART attributes available for this drive."
          fillAvailable={false}
          getRowId={(row) => row.id}
          maxHeight={400}
          variant="embedded"
        />
      );
    }
  }

  if (ataAttrs && ataAttrs.length > 0) {
    return (
      <AppVirtualDataTable
        ariaLabel="ATA SMART attributes"
        columns={ataAttributeColumns}
        data={ataAttrs}
        density="compact"
        emptyMessage="No SMART attributes available for this drive."
        fillAvailable={false}
        getRowId={(attr) => String(attr.id)}
        maxHeight={400}
        variant="embedded"
      />
    );
  }

  if (smartError) {
    return (
      <AppTypography color="error">
        SMART data unavailable: {smartError}
      </AppTypography>
    );
  }

  return (
    <AppTypography color="text.secondary">
      No SMART attributes available for this drive.
    </AppTypography>
  );
};
