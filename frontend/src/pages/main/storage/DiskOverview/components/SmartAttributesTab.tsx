import React from "react";

import type { SmartAttribute } from "../types";
import { formatDataUnits, formatPowerOnTime, getSmartNumber } from "../utils";

import {
  AppTable,
  AppTableBody,
  AppTableCell,
  AppTableContainer,
  AppTableHead,
  AppTableRow,
} from "@/components/ui/AppTable";
import AppTypography from "@/components/ui/AppTypography";
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
      <AppTableContainer
        className="custom-scrollbar"
        style={{ maxHeight: 400 }}
      >
        <AppTable className="app-table--sticky">
          <AppTableHead>
            <AppTableRow>
              <AppTableCell style={{ fontWeight: 600 }}>Attribute</AppTableCell>
              <AppTableCell style={{ fontWeight: 600 }} align="right">
                Value
              </AppTableCell>
            </AppTableRow>
          </AppTableHead>
          <AppTableBody>
            {getSmartNumber(nvmeHealthRaw.critical_warning) !== null && (
              <AppTableRow>
                <AppTableCell>Critical Warning</AppTableCell>
                <AppTableCell align="right">
                  0x
                  {(getSmartNumber(nvmeHealthRaw.critical_warning) ?? 0)
                    .toString(16)
                    .padStart(2, "0")
                    .toUpperCase()}
                </AppTableCell>
              </AppTableRow>
            )}
            {getSmartNumber(nvmeHealthRaw.temperature) !== null && (
              <AppTableRow>
                <AppTableCell>Temperature</AppTableCell>
                <AppTableCell
                  align="right"
                  style={{
                    color:
                      (getSmartNumber(nvmeHealthRaw.temperature) ?? 0) > 70
                        ? "var(--mui-palette-error-main)"
                        : (getSmartNumber(nvmeHealthRaw.temperature) ?? 0) > 50
                          ? "var(--mui-palette-warning-main)"
                          : "inherit",
                  }}
                >
                  {getSmartNumber(nvmeHealthRaw.temperature)} Celsius
                </AppTableCell>
              </AppTableRow>
            )}
            {getSmartNumber(nvmeHealthRaw.available_spare) !== null && (
              <AppTableRow>
                <AppTableCell>Available Spare</AppTableCell>
                <AppTableCell align="right">
                  {getSmartNumber(nvmeHealthRaw.available_spare)}%
                </AppTableCell>
              </AppTableRow>
            )}
            {getSmartNumber(nvmeHealthRaw.available_spare_threshold) !==
              null && (
              <AppTableRow>
                <AppTableCell>Available Spare Threshold</AppTableCell>
                <AppTableCell align="right">
                  {getSmartNumber(nvmeHealthRaw.available_spare_threshold)}%
                </AppTableCell>
              </AppTableRow>
            )}
            {getSmartNumber(nvmeHealthRaw.percentage_used) !== null && (
              <AppTableRow>
                <AppTableCell>Percentage Used</AppTableCell>
                <AppTableCell
                  align="right"
                  style={{
                    color:
                      (getSmartNumber(nvmeHealthRaw.percentage_used) ?? 0) > 90
                        ? "var(--mui-palette-error-main)"
                        : (getSmartNumber(nvmeHealthRaw.percentage_used) ?? 0) >
                            70
                          ? "var(--mui-palette-warning-main)"
                          : "inherit",
                  }}
                >
                  {getSmartNumber(nvmeHealthRaw.percentage_used)}%
                </AppTableCell>
              </AppTableRow>
            )}
            {getSmartNumber(nvmeHealthRaw.data_units_read) !== null && (
              <AppTableRow>
                <AppTableCell>Data Units Read</AppTableCell>
                <AppTableCell align="right">
                  {formatDataUnits(
                    getSmartNumber(nvmeHealthRaw.data_units_read) ?? undefined,
                  )}
                </AppTableCell>
              </AppTableRow>
            )}
            {getSmartNumber(nvmeHealthRaw.data_units_written) !== null && (
              <AppTableRow>
                <AppTableCell>Data Units Written</AppTableCell>
                <AppTableCell align="right">
                  {formatDataUnits(
                    getSmartNumber(nvmeHealthRaw.data_units_written) ??
                      undefined,
                  )}
                </AppTableCell>
              </AppTableRow>
            )}
            {getSmartNumber(nvmeHealthRaw.host_reads) !== null && (
              <AppTableRow>
                <AppTableCell>Host Read Commands</AppTableCell>
                <AppTableCell align="right">
                  {getSmartNumber(nvmeHealthRaw.host_reads)?.toLocaleString()}
                </AppTableCell>
              </AppTableRow>
            )}
            {getSmartNumber(nvmeHealthRaw.host_writes) !== null && (
              <AppTableRow>
                <AppTableCell>Host Write Commands</AppTableCell>
                <AppTableCell align="right">
                  {getSmartNumber(nvmeHealthRaw.host_writes)?.toLocaleString()}
                </AppTableCell>
              </AppTableRow>
            )}
            {getSmartNumber(nvmeHealthRaw.controller_busy_time) !== null && (
              <AppTableRow>
                <AppTableCell>Controller Busy Time</AppTableCell>
                <AppTableCell align="right">
                  {getSmartNumber(
                    nvmeHealthRaw.controller_busy_time,
                  )?.toLocaleString()}
                </AppTableCell>
              </AppTableRow>
            )}
            {getSmartNumber(nvmeHealthRaw.power_cycles) !== null && (
              <AppTableRow>
                <AppTableCell>Power Cycles</AppTableCell>
                <AppTableCell align="right">
                  {getSmartNumber(nvmeHealthRaw.power_cycles)?.toLocaleString()}
                </AppTableCell>
              </AppTableRow>
            )}
            {getSmartNumber(nvmeHealthRaw.power_on_hours) !== null && (
              <AppTableRow>
                <AppTableCell>Power On Hours</AppTableCell>
                <AppTableCell align="right">
                  {formatPowerOnTime(
                    getSmartNumber(nvmeHealthRaw.power_on_hours) ?? undefined,
                  )}
                </AppTableCell>
              </AppTableRow>
            )}
            {getSmartNumber(nvmeHealthRaw.unsafe_shutdowns) !== null && (
              <AppTableRow>
                <AppTableCell>Unsafe Shutdowns</AppTableCell>
                <AppTableCell align="right">
                  {getSmartNumber(
                    nvmeHealthRaw.unsafe_shutdowns,
                  )?.toLocaleString()}
                </AppTableCell>
              </AppTableRow>
            )}
            {getSmartNumber(nvmeHealthRaw.media_errors) !== null && (
              <AppTableRow>
                <AppTableCell>Media and Data Integrity Errors</AppTableCell>
                <AppTableCell
                  align="right"
                  style={{
                    color:
                      (getSmartNumber(nvmeHealthRaw.media_errors) ?? 0) > 0
                        ? "var(--mui-palette-error-main)"
                        : "inherit",
                  }}
                >
                  {getSmartNumber(nvmeHealthRaw.media_errors)?.toLocaleString()}
                </AppTableCell>
              </AppTableRow>
            )}
            {getSmartNumber(nvmeHealthRaw.num_err_log_entries) !== null && (
              <AppTableRow>
                <AppTableCell>Error Information Log Entries</AppTableCell>
                <AppTableCell align="right">
                  {getSmartNumber(
                    nvmeHealthRaw.num_err_log_entries,
                  )?.toLocaleString()}
                </AppTableCell>
              </AppTableRow>
            )}
          </AppTableBody>
        </AppTable>
      </AppTableContainer>
    );
  }
  if (ataAttrs && ataAttrs.length > 0) {
    return (
      <AppTableContainer
        className="custom-scrollbar"
        style={{ maxHeight: 400 }}
      >
        <AppTable className="app-table--sticky">
          <AppTableHead>
            <AppTableRow>
              <AppTableCell style={{ fontWeight: 600 }}>#</AppTableCell>
              <AppTableCell style={{ fontWeight: 600 }}>Attribute</AppTableCell>
              <AppTableCell style={{ fontWeight: 600 }} align="right">
                Value
              </AppTableCell>
              <AppTableCell style={{ fontWeight: 600 }} align="right">
                Worst
              </AppTableCell>
              <AppTableCell style={{ fontWeight: 600 }} align="right">
                Thresh
              </AppTableCell>
              <AppTableCell style={{ fontWeight: 600 }} align="right">
                Raw
              </AppTableCell>
            </AppTableRow>
          </AppTableHead>
          <AppTableBody>
            {ataAttrs.map((attr) => (
              <AppTableRow key={attr.id}>
                <AppTableCell>{attr.id}</AppTableCell>
                <AppTableCell>{attr.name}</AppTableCell>
                <AppTableCell align="right">{attr.value}</AppTableCell>
                <AppTableCell align="right">{attr.worst}</AppTableCell>
                <AppTableCell align="right">{attr.thresh}</AppTableCell>
                <AppTableCell
                  align="right"
                  style={{
                    color:
                      [5, 196, 197, 198].includes(attr.id) &&
                      attr.raw?.value &&
                      attr.raw.value > 0
                        ? "var(--mui-palette-warning-main)"
                        : "inherit",
                  }}
                >
                  {attr.raw?.string || attr.raw?.value?.toLocaleString()}
                </AppTableCell>
              </AppTableRow>
            ))}
          </AppTableBody>
        </AppTable>
      </AppTableContainer>
    );
  }
  return (
    <AppTypography color="text.secondary">
      No SMART attributes available for this drive.
    </AppTypography>
  );
};
