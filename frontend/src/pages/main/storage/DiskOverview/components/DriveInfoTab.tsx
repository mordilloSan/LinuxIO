import React from "react";

import type { DriveInfo } from "../types";
import { getSmartNumber, getSmartString } from "../utils";

import {
  AppTable,
  AppTableBody,
  AppTableCell,
  AppTableContainer,
  AppTableHead,
  AppTableRow,
} from "@/components/ui/AppTable";

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
    <AppTableContainer className="custom-scrollbar" style={{ maxHeight: 400 }}>
      <AppTable className="app-table--sticky">
        <AppTableHead>
          <AppTableRow>
            <AppTableCell style={{ fontWeight: 600 }}>Property</AppTableCell>
            <AppTableCell style={{ fontWeight: 600 }} align="right">
              Value
            </AppTableCell>
          </AppTableRow>
        </AppTableHead>
        <AppTableBody>
          <AppTableRow>
            <AppTableCell>Model</AppTableCell>
            <AppTableCell align="right">{drive.model || "N/A"}</AppTableCell>
          </AppTableRow>
          <AppTableRow>
            <AppTableCell>Serial Number</AppTableCell>
            <AppTableCell align="right">{drive.serial || "N/A"}</AppTableCell>
          </AppTableRow>
          <AppTableRow>
            <AppTableCell>Vendor</AppTableCell>
            <AppTableCell align="right">{drive.vendor || "N/A"}</AppTableCell>
          </AppTableRow>
          <AppTableRow>
            <AppTableCell>Firmware Version</AppTableCell>
            <AppTableCell align="right">
              {getSmartString(smartData?.firmware_version) || "N/A"}
            </AppTableCell>
          </AppTableRow>
          <AppTableRow>
            <AppTableCell>Capacity</AppTableCell>
            <AppTableCell align="right">{rawDriveSize || "N/A"}</AppTableCell>
          </AppTableRow>
          <AppTableRow>
            <AppTableCell>Transport</AppTableCell>
            <AppTableCell align="right">
              {drive.transport?.toUpperCase() || "N/A"}
            </AppTableCell>
          </AppTableRow>
          <AppTableRow>
            <AppTableCell>Read Only</AppTableCell>
            <AppTableCell align="right">{drive.ro ? "Yes" : "No"}</AppTableCell>
          </AppTableRow>
          {isNvme && (
            <>
              <AppTableRow>
                <AppTableCell>NVMe Version</AppTableCell>
                <AppTableCell align="right">
                  {getSmartString(smartData?.nvme_version) || "N/A"}
                </AppTableCell>
              </AppTableRow>
              <AppTableRow>
                <AppTableCell>Number of Namespaces</AppTableCell>
                <AppTableCell align="right">
                  {getSmartNumber(
                    smartData?.nvme_number_of_namespaces,
                  )?.toString() || "N/A"}
                </AppTableCell>
              </AppTableRow>
            </>
          )}
          {deviceInfo && (
            <>
              <AppTableRow>
                <AppTableCell>Device Type</AppTableCell>
                <AppTableCell align="right">
                  {getSmartString(deviceInfo.type) || "N/A"}
                </AppTableCell>
              </AppTableRow>
              <AppTableRow>
                <AppTableCell>Protocol</AppTableCell>
                <AppTableCell align="right">
                  {getSmartString(deviceInfo.protocol) || "N/A"}
                </AppTableCell>
              </AppTableRow>
            </>
          )}
          <AppTableRow>
            <AppTableCell>SMART Health</AppTableCell>
            <AppTableCell
              align="right"
              style={{
                color:
                  smartHealth?.passed === true
                    ? "var(--mui-palette-success-main)"
                    : smartHealth?.passed === false
                      ? "var(--mui-palette-error-main)"
                      : "inherit",
              }}
            >
              {smartHealth?.passed === true
                ? "Passed"
                : smartHealth?.passed === false
                  ? "Failed"
                  : "Unknown"}
            </AppTableCell>
          </AppTableRow>
        </AppTableBody>
      </AppTable>
    </AppTableContainer>
  );
};
