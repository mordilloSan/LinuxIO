import { Box } from "@mui/material";
import React from "react";

import type { DriveInfo } from "../types";
import { getSmartNumber, getSmartString } from "../utils";
import { InfoRow } from "./InfoRow";

interface DriveInfoTabProps {
  drive: DriveInfo;
  rawDriveSize?: string;
  smartData?: Record<string, unknown>;
  deviceInfo?: Record<string, unknown>;
  smartHealth?: { passed?: boolean };
}

export const DriveInfoTab: React.FC<DriveInfoTabProps> = ({
  drive,
  rawDriveSize,
  smartData,
  deviceInfo,
  smartHealth,
}) => {
  const isNvme = drive.transport === "nvme";

  return (
    <Box sx={{ maxWidth: 600 }}>
      <InfoRow label="Model" value={drive.model || "N/A"} />
      <InfoRow label="Serial Number" value={drive.serial || "N/A"} />
      <InfoRow label="Vendor" value={drive.vendor || "N/A"} />
      <InfoRow
        label="Firmware Version"
        value={getSmartString(smartData?.firmware_version) || "N/A"}
      />
      <InfoRow label="Capacity" value={rawDriveSize || "N/A"} />
      <InfoRow
        label="Transport"
        value={drive.transport?.toUpperCase() || "N/A"}
      />
      <InfoRow label="Read Only" value={drive.ro ? "Yes" : "No"} />
      {isNvme && (
        <>
          <InfoRow
            label="NVMe Version"
            value={getSmartString(smartData?.nvme_version) || "N/A"}
          />
          <InfoRow
            label="Number of Namespaces"
            value={
              getSmartNumber(
                smartData?.nvme_number_of_namespaces,
              )?.toString() || "N/A"
            }
          />
        </>
      )}
      {deviceInfo && (
        <>
          <InfoRow
            label="Device Type"
            value={getSmartString(deviceInfo.type) || "N/A"}
          />
          <InfoRow
            label="Protocol"
            value={getSmartString(deviceInfo.protocol) || "N/A"}
          />
        </>
      )}
      <InfoRow
        label="SMART Health"
        value={
          smartHealth?.passed === true
            ? "Passed"
            : smartHealth?.passed === false
              ? "Failed"
              : "Unknown"
        }
        valueColor={
          smartHealth?.passed === true
            ? "success.main"
            : smartHealth?.passed === false
              ? "error.main"
              : undefined
        }
      />
    </Box>
  );
};
