import {
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Typography,
} from "@mui/material";
import React from "react";

import type { SmartAttribute } from "../types";
import { formatDataUnits, formatPowerOnTime, getSmartNumber } from "../utils";

interface SmartAttributesTabProps {
  isNvme: boolean;
  nvmeHealthRaw?: Record<string, unknown>;
  ataAttrs?: SmartAttribute[];
}

export const SmartAttributesTab: React.FC<SmartAttributesTabProps> = ({
  isNvme,
  nvmeHealthRaw,
  ataAttrs,
}) => {
  if (isNvme && nvmeHealthRaw) {
    return (
      <TableContainer className="custom-scrollbar" sx={{ maxHeight: 400 }}>
        <Table
          size="small"
          stickyHeader
          sx={{ "& .MuiTableCell-root": { borderColor: "divider" } }}
        >
          <TableHead>
            <TableRow>
              <TableCell sx={{ fontWeight: 600 }}>Attribute</TableCell>
              <TableCell sx={{ fontWeight: 600 }} align="right">
                Value
              </TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {getSmartNumber(nvmeHealthRaw.critical_warning) !== null && (
              <TableRow>
                <TableCell>Critical Warning</TableCell>
                <TableCell align="right">
                  0x
                  {(getSmartNumber(nvmeHealthRaw.critical_warning) ?? 0)
                    .toString(16)
                    .padStart(2, "0")
                    .toUpperCase()}
                </TableCell>
              </TableRow>
            )}
            {getSmartNumber(nvmeHealthRaw.temperature) !== null && (
              <TableRow>
                <TableCell>Temperature</TableCell>
                <TableCell
                  align="right"
                  sx={{
                    color:
                      (getSmartNumber(nvmeHealthRaw.temperature) ?? 0) > 70
                        ? "error.main"
                        : (getSmartNumber(nvmeHealthRaw.temperature) ?? 0) > 50
                          ? "warning.main"
                          : "inherit",
                  }}
                >
                  {getSmartNumber(nvmeHealthRaw.temperature)} Celsius
                </TableCell>
              </TableRow>
            )}
            {getSmartNumber(nvmeHealthRaw.available_spare) !== null && (
              <TableRow>
                <TableCell>Available Spare</TableCell>
                <TableCell align="right">
                  {getSmartNumber(nvmeHealthRaw.available_spare)}%
                </TableCell>
              </TableRow>
            )}
            {getSmartNumber(nvmeHealthRaw.available_spare_threshold) !==
              null && (
              <TableRow>
                <TableCell>Available Spare Threshold</TableCell>
                <TableCell align="right">
                  {getSmartNumber(nvmeHealthRaw.available_spare_threshold)}%
                </TableCell>
              </TableRow>
            )}
            {getSmartNumber(nvmeHealthRaw.percentage_used) !== null && (
              <TableRow>
                <TableCell>Percentage Used</TableCell>
                <TableCell
                  align="right"
                  sx={{
                    color:
                      (getSmartNumber(nvmeHealthRaw.percentage_used) ?? 0) > 90
                        ? "error.main"
                        : (getSmartNumber(nvmeHealthRaw.percentage_used) ?? 0) >
                            70
                          ? "warning.main"
                          : "inherit",
                  }}
                >
                  {getSmartNumber(nvmeHealthRaw.percentage_used)}%
                </TableCell>
              </TableRow>
            )}
            {getSmartNumber(nvmeHealthRaw.data_units_read) !== null && (
              <TableRow>
                <TableCell>Data Units Read</TableCell>
                <TableCell align="right">
                  {formatDataUnits(
                    getSmartNumber(nvmeHealthRaw.data_units_read) ?? undefined,
                  )}
                </TableCell>
              </TableRow>
            )}
            {getSmartNumber(nvmeHealthRaw.data_units_written) !== null && (
              <TableRow>
                <TableCell>Data Units Written</TableCell>
                <TableCell align="right">
                  {formatDataUnits(
                    getSmartNumber(nvmeHealthRaw.data_units_written) ??
                      undefined,
                  )}
                </TableCell>
              </TableRow>
            )}
            {getSmartNumber(nvmeHealthRaw.host_reads) !== null && (
              <TableRow>
                <TableCell>Host Read Commands</TableCell>
                <TableCell align="right">
                  {getSmartNumber(nvmeHealthRaw.host_reads)?.toLocaleString()}
                </TableCell>
              </TableRow>
            )}
            {getSmartNumber(nvmeHealthRaw.host_writes) !== null && (
              <TableRow>
                <TableCell>Host Write Commands</TableCell>
                <TableCell align="right">
                  {getSmartNumber(nvmeHealthRaw.host_writes)?.toLocaleString()}
                </TableCell>
              </TableRow>
            )}
            {getSmartNumber(nvmeHealthRaw.controller_busy_time) !== null && (
              <TableRow>
                <TableCell>Controller Busy Time</TableCell>
                <TableCell align="right">
                  {getSmartNumber(
                    nvmeHealthRaw.controller_busy_time,
                  )?.toLocaleString()}
                </TableCell>
              </TableRow>
            )}
            {getSmartNumber(nvmeHealthRaw.power_cycles) !== null && (
              <TableRow>
                <TableCell>Power Cycles</TableCell>
                <TableCell align="right">
                  {getSmartNumber(nvmeHealthRaw.power_cycles)?.toLocaleString()}
                </TableCell>
              </TableRow>
            )}
            {getSmartNumber(nvmeHealthRaw.power_on_hours) !== null && (
              <TableRow>
                <TableCell>Power On Hours</TableCell>
                <TableCell align="right">
                  {formatPowerOnTime(
                    getSmartNumber(nvmeHealthRaw.power_on_hours) ?? undefined,
                  )}
                </TableCell>
              </TableRow>
            )}
            {getSmartNumber(nvmeHealthRaw.unsafe_shutdowns) !== null && (
              <TableRow>
                <TableCell>Unsafe Shutdowns</TableCell>
                <TableCell align="right">
                  {getSmartNumber(
                    nvmeHealthRaw.unsafe_shutdowns,
                  )?.toLocaleString()}
                </TableCell>
              </TableRow>
            )}
            {getSmartNumber(nvmeHealthRaw.media_errors) !== null && (
              <TableRow>
                <TableCell>Media and Data Integrity Errors</TableCell>
                <TableCell
                  align="right"
                  sx={{
                    color:
                      (getSmartNumber(nvmeHealthRaw.media_errors) ?? 0) > 0
                        ? "error.main"
                        : "inherit",
                  }}
                >
                  {getSmartNumber(nvmeHealthRaw.media_errors)?.toLocaleString()}
                </TableCell>
              </TableRow>
            )}
            {getSmartNumber(nvmeHealthRaw.num_err_log_entries) !== null && (
              <TableRow>
                <TableCell>Error Information Log Entries</TableCell>
                <TableCell align="right">
                  {getSmartNumber(
                    nvmeHealthRaw.num_err_log_entries,
                  )?.toLocaleString()}
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </TableContainer>
    );
  }

  if (ataAttrs && ataAttrs.length > 0) {
    return (
      <TableContainer className="custom-scrollbar" sx={{ maxHeight: 400 }}>
        <Table
          size="small"
          stickyHeader
          sx={{ "& .MuiTableCell-root": { borderColor: "divider" } }}
        >
          <TableHead>
            <TableRow>
              <TableCell sx={{ fontWeight: 600 }}>#</TableCell>
              <TableCell sx={{ fontWeight: 600 }}>Attribute</TableCell>
              <TableCell sx={{ fontWeight: 600 }} align="right">
                Value
              </TableCell>
              <TableCell sx={{ fontWeight: 600 }} align="right">
                Worst
              </TableCell>
              <TableCell sx={{ fontWeight: 600 }} align="right">
                Thresh
              </TableCell>
              <TableCell sx={{ fontWeight: 600 }} align="right">
                Raw
              </TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {ataAttrs.map((attr) => (
              <TableRow key={attr.id}>
                <TableCell>{attr.id}</TableCell>
                <TableCell>{attr.name}</TableCell>
                <TableCell align="right">{attr.value}</TableCell>
                <TableCell align="right">{attr.worst}</TableCell>
                <TableCell align="right">{attr.thresh}</TableCell>
                <TableCell
                  align="right"
                  sx={{
                    color:
                      [5, 196, 197, 198].includes(attr.id) &&
                      attr.raw?.value &&
                      attr.raw.value > 0
                        ? "warning.main"
                        : "inherit",
                  }}
                >
                  {attr.raw?.string || attr.raw?.value?.toLocaleString()}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableContainer>
    );
  }

  return (
    <Typography color="text.secondary">
      No SMART attributes available for this drive.
    </Typography>
  );
};
