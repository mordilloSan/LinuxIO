import {
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
} from "@mui/material";
import React from "react";

import type { DriveInfo } from "../types";
import { getSmartNumber, getSmartString } from "../utils";

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
    <TableContainer className="custom-scrollbar" sx={{ maxHeight: 400 }}>
      <Table
        size="small"
        stickyHeader
        sx={{ "& .MuiTableCell-root": { borderColor: "divider" } }}
      >
        <TableHead>
          <TableRow>
            <TableCell sx={{ fontWeight: 600 }}>Property</TableCell>
            <TableCell sx={{ fontWeight: 600 }} align="right">
              Value
            </TableCell>
          </TableRow>
        </TableHead>
        <TableBody>
          <TableRow>
            <TableCell>Model</TableCell>
            <TableCell align="right">{drive.model || "N/A"}</TableCell>
          </TableRow>
          <TableRow>
            <TableCell>Serial Number</TableCell>
            <TableCell align="right">{drive.serial || "N/A"}</TableCell>
          </TableRow>
          <TableRow>
            <TableCell>Vendor</TableCell>
            <TableCell align="right">{drive.vendor || "N/A"}</TableCell>
          </TableRow>
          <TableRow>
            <TableCell>Firmware Version</TableCell>
            <TableCell align="right">
              {getSmartString(smartData?.firmware_version) || "N/A"}
            </TableCell>
          </TableRow>
          <TableRow>
            <TableCell>Capacity</TableCell>
            <TableCell align="right">{rawDriveSize || "N/A"}</TableCell>
          </TableRow>
          <TableRow>
            <TableCell>Transport</TableCell>
            <TableCell align="right">
              {drive.transport?.toUpperCase() || "N/A"}
            </TableCell>
          </TableRow>
          <TableRow>
            <TableCell>Read Only</TableCell>
            <TableCell align="right">{drive.ro ? "Yes" : "No"}</TableCell>
          </TableRow>
          {isNvme && (
            <>
              <TableRow>
                <TableCell>NVMe Version</TableCell>
                <TableCell align="right">
                  {getSmartString(smartData?.nvme_version) || "N/A"}
                </TableCell>
              </TableRow>
              <TableRow>
                <TableCell>Number of Namespaces</TableCell>
                <TableCell align="right">
                  {getSmartNumber(
                    smartData?.nvme_number_of_namespaces,
                  )?.toString() || "N/A"}
                </TableCell>
              </TableRow>
            </>
          )}
          {deviceInfo && (
            <>
              <TableRow>
                <TableCell>Device Type</TableCell>
                <TableCell align="right">
                  {getSmartString(deviceInfo.type) || "N/A"}
                </TableCell>
              </TableRow>
              <TableRow>
                <TableCell>Protocol</TableCell>
                <TableCell align="right">
                  {getSmartString(deviceInfo.protocol) || "N/A"}
                </TableCell>
              </TableRow>
            </>
          )}
          <TableRow>
            <TableCell>SMART Health</TableCell>
            <TableCell
              align="right"
              sx={{
                color:
                  smartHealth?.passed === true
                    ? "success.main"
                    : smartHealth?.passed === false
                      ? "error.main"
                      : "inherit",
              }}
            >
              {smartHealth?.passed === true
                ? "Passed"
                : smartHealth?.passed === false
                  ? "Failed"
                  : "Unknown"}
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>
    </TableContainer>
  );
};
