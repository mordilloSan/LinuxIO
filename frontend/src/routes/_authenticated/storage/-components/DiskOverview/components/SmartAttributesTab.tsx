import type { CSSProperties, ReactNode } from "react";

import type {
  NVMeSmartHealthInformationLog,
  SmartAttribute,
  SmartData,
} from "@/api";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import AppTypography from "@/components/ui/AppTypography";

import { formatDataUnits, formatPowerOnTime } from "../utils";

interface SmartAttributesTabProps {
  isNvme: boolean;
  smartData?: SmartData;
  smartError?: string;
}

interface SmartSummaryRow {
  attribute: string;
  id: string;
  value: ReactNode;
  valueStyle?: CSSProperties;
}

const smartSummaryColumns: AppVirtualTableColumnDef<SmartSummaryRow>[] = [
  { accessorKey: "attribute", header: "Attribute" },
  {
    accessorKey: "value",
    header: "Value",
    cell: ({ row }) => (
      <span style={row.original.valueStyle}>{row.original.value}</span>
    ),
    meta: { align: "right" },
  },
];

const smartAttributeColumns: AppVirtualTableColumnDef<SmartAttribute>[] = [
  { accessorKey: "id", header: "#" },
  { accessorKey: "name", header: "Attribute" },
  { accessorKey: "value", header: "Value", meta: { align: "right" } },
  { accessorKey: "worst", header: "Worst", meta: { align: "right" } },
  { accessorKey: "threshold", header: "Threshold", meta: { align: "right" } },
  {
    id: "raw",
    header: "Raw",
    cell: ({ row }) => (
      <span
        style={{
          color:
            [5, 196, 197, 198].includes(row.original.id ?? 0) &&
            row.original.raw_value > 0
              ? "var(--app-palette-warning-main)"
              : "inherit",
        }}
      >
        {row.original.raw_string || row.original.raw_value.toLocaleString()}
      </span>
    ),
    meta: { align: "right" },
  },
];

const healthValueStyle = (value: number, limit: number): CSSProperties => ({
  color:
    value > limit
      ? "var(--app-palette-error-main)"
      : value > limit - 20
        ? "var(--app-palette-warning-main)"
        : "inherit",
});

const addNumberRow = (
  rows: SmartSummaryRow[],
  id: string,
  attribute: string,
  value: number | undefined,
  format: (value: number) => ReactNode = (number) => number.toLocaleString(),
  valueStyle?: CSSProperties,
) => {
  if (value === undefined) return;
  rows.push({ id, attribute, value: format(value), valueStyle });
};

const nvmeRows = (health: NVMeSmartHealthInformationLog): SmartSummaryRow[] => {
  const rows: SmartSummaryRow[] = [];
  const temperatureSensors = health.temperature_sensors ?? [];
  addNumberRow(
    rows,
    "critical_warning",
    "Critical Warning",
    health.critical_warning,
    (value) => `0x${value.toString(16).padStart(2, "0").toUpperCase()}`,
  );
  addNumberRow(
    rows,
    "temperature",
    "Temperature",
    health.temperature,
    (value) => `${value} Celsius`,
    healthValueStyle(health.temperature, 70),
  );
  addNumberRow(
    rows,
    "available_spare",
    "Available Spare",
    health.available_spare,
    (value) => `${value}%`,
  );
  addNumberRow(
    rows,
    "available_spare_threshold",
    "Available Spare Threshold",
    health.available_spare_threshold,
    (value) => `${value}%`,
  );
  addNumberRow(
    rows,
    "percentage_used",
    "Percentage Used",
    health.percentage_used,
    (value) => `${value}%`,
    healthValueStyle(health.percentage_used, 90),
  );
  addNumberRow(
    rows,
    "data_units_read",
    "Data Units Read",
    health.data_units_read,
    formatDataUnits,
  );
  addNumberRow(
    rows,
    "data_units_written",
    "Data Units Written",
    health.data_units_written,
    formatDataUnits,
  );
  addNumberRow(rows, "host_reads", "Host Read Commands", health.host_reads);
  addNumberRow(rows, "host_writes", "Host Write Commands", health.host_writes);
  addNumberRow(
    rows,
    "controller_busy_time",
    "Controller Busy Time",
    health.controller_busy_time,
  );
  addNumberRow(rows, "power_cycles", "Power Cycles", health.power_cycles);
  addNumberRow(
    rows,
    "power_on_hours",
    "Power On Hours",
    health.power_on_hours,
    formatPowerOnTime,
  );
  addNumberRow(
    rows,
    "unsafe_shutdowns",
    "Unsafe Shutdowns",
    health.unsafe_shutdowns,
  );
  addNumberRow(
    rows,
    "media_errors",
    "Media and Data Integrity Errors",
    health.media_errors,
    undefined,
    health.media_errors > 0
      ? { color: "var(--app-palette-error-main)" }
      : undefined,
  );
  addNumberRow(
    rows,
    "num_err_log_entries",
    "Error Information Log Entries",
    health.num_err_log_entries,
  );
  addNumberRow(
    rows,
    "warning_temp_time",
    "Warning Temperature Time",
    health.warning_temp_time,
  );
  addNumberRow(
    rows,
    "critical_comp_time",
    "Critical Temperature Time",
    health.critical_comp_time,
  );
  addNumberRow(
    rows,
    "temperature_sensors",
    "Temperature Sensors",
    temperatureSensors.length,
    () => `${temperatureSensors.join(", ")} Celsius`,
  );
  return rows;
};

export const SmartAttributesTab = ({
  isNvme,
  smartData,
  smartError,
}: SmartAttributesTabProps) => {
  if (!smartData) {
    return (
      <AppTypography color={smartError ? "error" : "text.secondary"}>
        {smartError
          ? `SMART data unavailable: ${smartError}`
          : "No SMART attributes available for this drive."}
      </AppTypography>
    );
  }

  const health = smartData.nvme_smart_health_information_log;
  if (isNvme && health) {
    return (
      <AppVirtualTable
        ariaLabel="NVMe SMART attributes"
        columns={smartSummaryColumns}
        data={nvmeRows(health)}
        density="compact"
        emptyMessage="No SMART attributes available for this drive."
        fillAvailable={false}
        getRowId={(row) => row.id}
        maxHeight={400}
        variant="embedded"
      />
    );
  }

  if (smartData.attributes && smartData.attributes.length > 0) {
    return (
      <AppVirtualTable
        ariaLabel="ATA SMART attributes"
        columns={smartAttributeColumns}
        data={smartData.attributes}
        density="compact"
        emptyMessage="No SMART attributes available for this drive."
        fillAvailable={false}
        getRowId={(attribute) => String(attribute.id ?? attribute.name)}
        maxHeight={400}
        variant="embedded"
      />
    );
  }

  const summary: SmartSummaryRow[] = [];
  addNumberRow(
    summary,
    "temperature",
    "Temperature",
    smartData.temperature_celsius,
    (value) => `${value} Celsius`,
  );
  if (smartData.smart_status) {
    summary.push({
      id: "health",
      attribute: "SMART Health",
      value: smartData.smart_status,
    });
  }
  return summary.length > 0 ? (
    <AppVirtualTable
      ariaLabel="SMART summary"
      columns={smartSummaryColumns}
      data={summary}
      density="compact"
      emptyMessage="No SMART attributes available for this drive."
      fillAvailable={false}
      getRowId={(row) => row.id}
      maxHeight={180}
      variant="embedded"
    />
  ) : (
    <AppTypography color="text.secondary">
      No SMART attributes available for this drive.
    </AppTypography>
  );
};
