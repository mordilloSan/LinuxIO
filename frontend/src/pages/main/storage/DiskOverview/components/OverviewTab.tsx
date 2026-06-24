import React from "react";

import type { DriveInfo } from "../types";
import { formatDataUnits, formatPowerOnTime, getSmartNumber } from "../utils";

import Chip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";

interface OverviewTabProps {
  drive: DriveInfo;
}

export const OverviewTab: React.FC<OverviewTabProps> = ({ drive }) => {
  const smart = drive.smart;
  const power = drive.power;
  const isNvme = drive.transport === "nvme";
  const nvmeHealth = smart?.nvme_smart_health_information_log;
  const ataAttrs = smart?.ata_smart_attributes?.table;

  const temperature = getSmartNumber(
    nvmeHealth?.temperature ?? smart?.temperature?.current,
  );
  const powerOnHours = getSmartNumber(
    nvmeHealth?.power_on_hours ?? smart?.power_on_time?.hours,
  );
  const powerCycles = getSmartNumber(
    nvmeHealth?.power_cycles ?? smart?.power_cycle_count,
  );
  const percentageUsed = getSmartNumber(nvmeHealth?.percentage_used);
  const dataRead = getSmartNumber(nvmeHealth?.data_units_read);
  const dataWritten = getSmartNumber(nvmeHealth?.data_units_written);

  const findAtaAttr = (id: number) => ataAttrs?.find((a) => a.id === id);
  const reallocatedSectors = findAtaAttr(5);
  const pendingSectors = findAtaAttr(197);

  return (
    <div>
      <div
        style={{
          display: "grid",
          gridTemplateColumns: "1fr 1fr",
          gap: 4,
          marginTop: 4,
          marginBottom: 8,
        }}
      >
        <div>
          <AppTypography color="text.secondary" variant="body2">
            Serial
          </AppTypography>
          <AppTypography fontWeight={500} noWrap variant="body2">
            {drive.serial || "N/A"}
          </AppTypography>
        </div>
        <div>
          <AppTypography color="text.secondary" variant="body2">
            Vendor
          </AppTypography>
          <AppTypography fontWeight={500} variant="body2">
            {drive.vendor || "N/A"}
          </AppTypography>
        </div>
        <div>
          <AppTypography color="text.secondary" variant="body2">
            Read Only
          </AppTypography>
          <AppTypography fontWeight={500} variant="body2">
            {drive.ro ? "Yes" : "No"}
          </AppTypography>
        </div>
        <div>
          <AppTypography color="text.secondary" variant="body2">
            Transport
          </AppTypography>
          <AppTypography fontWeight={500} variant="body2">
            {drive.transport.toUpperCase()}
          </AppTypography>
        </div>
      </div>

      {smart && (
        <>
          <AppTypography
            color="text.secondary"
            fontWeight={600}
            style={{ textTransform: "uppercase" }}
            variant="caption"
          >
            Health & Statistics
          </AppTypography>
          <div
            style={{
              display: "grid",
              gridTemplateColumns: "1fr 1fr",
              gap: 4,
              marginTop: 4,
              marginBottom: 8,
            }}
          >
            {temperature !== null && (
              <div>
                <AppTypography color="text.secondary" variant="body2">
                  Temperature
                </AppTypography>
                <AppTypography
                  color={
                    temperature > 70
                      ? "error"
                      : temperature > 50
                        ? "warning"
                        : "text.primary"
                  }
                  fontWeight={500}
                  variant="body2"
                >
                  {temperature}°C
                </AppTypography>
              </div>
            )}
            {powerOnHours !== null && (
              <div>
                <AppTypography color="text.secondary" variant="body2">
                  Power On Time
                </AppTypography>
                <AppTypography fontWeight={500} variant="body2">
                  {formatPowerOnTime(powerOnHours)}
                </AppTypography>
              </div>
            )}
            {powerCycles !== null && (
              <div>
                <AppTypography color="text.secondary" variant="body2">
                  Power Cycles
                </AppTypography>
                <AppTypography fontWeight={500} variant="body2">
                  {powerCycles.toLocaleString()}
                </AppTypography>
              </div>
            )}
            {isNvme && percentageUsed !== null && (
              <div>
                <AppTypography color="text.secondary" variant="body2">
                  Life Used
                </AppTypography>
                <AppTypography
                  color={
                    percentageUsed > 90
                      ? "error"
                      : percentageUsed > 70
                        ? "warning"
                        : "text.primary"
                  }
                  fontWeight={500}
                  variant="body2"
                >
                  {percentageUsed}%
                </AppTypography>
              </div>
            )}
            {isNvme && dataRead !== null && (
              <div>
                <AppTypography color="text.secondary" variant="body2">
                  Data Read
                </AppTypography>
                <AppTypography fontWeight={500} variant="body2">
                  {formatDataUnits(dataRead)}
                </AppTypography>
              </div>
            )}
            {isNvme && dataWritten !== null && (
              <div>
                <AppTypography color="text.secondary" variant="body2">
                  Data Written
                </AppTypography>
                <AppTypography fontWeight={500} variant="body2">
                  {formatDataUnits(dataWritten)}
                </AppTypography>
              </div>
            )}
            {!isNvme && reallocatedSectors && (
              <div>
                <AppTypography color="text.secondary" variant="body2">
                  Reallocated Sectors
                </AppTypography>
                <AppTypography
                  color={
                    (getSmartNumber(reallocatedSectors.raw?.value) ?? 0) > 0
                      ? "warning"
                      : "text.primary"
                  }
                  fontWeight={500}
                  variant="body2"
                >
                  {getSmartNumber(reallocatedSectors.raw?.value) ?? "N/A"}
                </AppTypography>
              </div>
            )}
            {!isNvme && pendingSectors && (
              <div>
                <AppTypography color="text.secondary" variant="body2">
                  Pending Sectors
                </AppTypography>
                <AppTypography
                  color={
                    (getSmartNumber(pendingSectors.raw?.value) ?? 0) > 0
                      ? "warning"
                      : "text.primary"
                  }
                  fontWeight={500}
                  variant="body2"
                >
                  {getSmartNumber(pendingSectors.raw?.value) ?? "N/A"}
                </AppTypography>
              </div>
            )}
          </div>
        </>
      )}

      {power && (
        <>
          <AppTypography
            color="text.secondary"
            fontWeight={600}
            style={{ textTransform: "uppercase" }}
            variant="caption"
          >
            Power
          </AppTypography>
          <div style={{ marginTop: 4 }}>
            <div
              style={{
                display: "flex",
                gap: 4,
                alignItems: "center",
                marginBottom: 4,
              }}
            >
              <Chip
                color="primary"
                label={`State ${power.currentState}`}
                size="small"
                variant="soft"
              />
              <AppTypography color="text.secondary" variant="body2">
                ~{power.estimatedW.toFixed(2)}W
              </AppTypography>
            </div>
          </div>
        </>
      )}

      {!smart && !power && (
        <AppTypography color="text.secondary" variant="body2">
          No detailed information available for this drive.
        </AppTypography>
      )}
    </div>
  );
};
