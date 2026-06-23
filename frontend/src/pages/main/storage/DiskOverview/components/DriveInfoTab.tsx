import React from "react";

import type { DriveInfo } from "../types";
import { getSmartNumber, getSmartString } from "../utils";

import AppDataTable from "@/components/tables/AppDataTable";
import type { AppDataTableColumnDef } from "@/components/tables/AppDataTable";

interface DriveInfoTabProps {
  deviceInfo?: Record<string, unknown>;
  drive: DriveInfo;
  rawDriveSize?: string;
  smartData?: Record<string, unknown>;
  smartHealth?: { passed?: boolean };
}

interface DriveInfoRow {
  property: string;
  value: React.ReactNode;
  valueStyle?: React.CSSProperties;
}

const driveInfoColumns: AppDataTableColumnDef<DriveInfoRow>[] = [
  {
    accessorKey: "property",
    header: "Property",
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

export const DriveInfoTab: React.FC<DriveInfoTabProps> = ({
  drive,
  rawDriveSize,
  smartData,
  deviceInfo,
  smartHealth,
}) => {
  const isNvme = drive.transport === "nvme";
  const rows: DriveInfoRow[] = [
    { property: "Model", value: drive.model || "N/A" },
    { property: "Serial Number", value: drive.serial || "N/A" },
    { property: "Vendor", value: drive.vendor || "N/A" },
    {
      property: "Firmware Version",
      value: getSmartString(smartData?.firmware_version) || "N/A",
    },
    { property: "Capacity", value: rawDriveSize || "N/A" },
    {
      property: "Transport",
      value: drive.transport?.toUpperCase() || "N/A",
    },
    { property: "Read Only", value: drive.ro ? "Yes" : "No" },
  ];

  if (isNvme) {
    rows.push(
      {
        property: "NVMe Version",
        value: getSmartString(smartData?.nvme_version) || "N/A",
      },
      {
        property: "Number of Namespaces",
        value:
          getSmartNumber(smartData?.nvme_number_of_namespaces)?.toString() ||
          "N/A",
      },
    );
  }

  if (deviceInfo) {
    rows.push(
      {
        property: "Device Type",
        value: getSmartString(deviceInfo.type) || "N/A",
      },
      {
        property: "Protocol",
        value: getSmartString(deviceInfo.protocol) || "N/A",
      },
    );
  }

  rows.push({
    property: "SMART Health",
    value:
      smartHealth?.passed === true
        ? "Passed"
        : smartHealth?.passed === false
          ? "Failed"
          : "Unknown",
    valueStyle: {
      color:
        smartHealth?.passed === true
          ? "var(--app-palette-success-main)"
          : smartHealth?.passed === false
            ? "var(--app-palette-error-main)"
            : "inherit",
    },
  });

  return (
    <AppDataTable
      ariaLabel="Drive information"
      columns={driveInfoColumns}
      data={rows}
      density="compact"
      getRowId={(row) => row.property}
      maxHeight={400}
      variant="embedded"
    />
  );
};
