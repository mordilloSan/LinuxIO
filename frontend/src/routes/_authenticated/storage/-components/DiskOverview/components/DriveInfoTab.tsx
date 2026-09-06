import type { CSSProperties, ReactNode } from "react";

import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";

import type { DriveInfo, SmartData } from "../types";

interface DriveInfoTabProps {
  drive: DriveInfo;
  rawDriveSize?: string;
  smartData?: SmartData;
}

interface DriveInfoRow {
  property: string;
  value: ReactNode;
  valueStyle?: CSSProperties;
}

const driveInfoColumns: AppVirtualTableColumnDef<DriveInfoRow>[] = [
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

export const DriveInfoTab = ({
  drive,
  rawDriveSize,
  smartData,
}: DriveInfoTabProps) => {
  const isNvme = drive.transport === "nvme";
  const rows: DriveInfoRow[] = [
    { property: "Model", value: drive.model || "N/A" },
    { property: "Serial Number", value: drive.serial || "N/A" },
    { property: "Vendor", value: drive.vendor || "N/A" },
    {
      property: "Firmware Version",
      value: smartData?.firmware_version || "N/A",
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
        value:
          smartData?.nvme_version?.string ??
          smartData?.nvme_version?.value?.toString() ??
          "N/A",
      },
      {
        property: "Number of Namespaces",
        value: smartData?.nvme_number_of_namespaces?.toString() ?? "N/A",
      },
    );
  }

  if (smartData?.device) {
    rows.push(
      { property: "Device Type", value: smartData.device.type },
      { property: "Protocol", value: smartData.device.protocol },
    );
  }

  rows.push({
    property: "SMART Health",
    value: smartData?.smart_status || "Unknown",
    valueStyle: {
      color:
        smartData?.smart_status === "PASSED"
          ? "var(--app-palette-success-main)"
          : smartData?.smart_status === "FAILED"
            ? "var(--app-palette-error-main)"
            : "inherit",
    },
  });

  return (
    <AppVirtualTable
      ariaLabel="Drive information"
      columns={driveInfoColumns}
      data={rows}
      density="compact"
      fillAvailable={false}
      getRowId={(row) => row.property}
      maxHeight={400}
      variant="embedded"
    />
  );
};
