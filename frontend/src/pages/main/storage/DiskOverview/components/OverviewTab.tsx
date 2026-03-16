import React from "react";

import type { DriveInfo } from "../types";
import { formatDataUnits, formatPowerOnTime } from "../utils";

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

  const temperature =
    nvmeHealth?.temperature ?? smart?.temperature?.current ?? null;
  const powerOnHours =
    nvmeHealth?.power_on_hours ?? smart?.power_on_time?.hours ?? null;
  const powerCycles =
    nvmeHealth?.power_cycles ?? smart?.power_cycle_count ?? null;
  const percentageUsed = nvmeHealth?.percentage_used;
  const dataRead = nvmeHealth?.data_units_read;
  const dataWritten = nvmeHealth?.data_units_written;

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
          <AppTypography variant="body2" color="text.secondary">
            Serial
          </AppTypography>
          <AppTypography variant="body2" fontWeight={500} noWrap>
            {drive.serial || "N/A"}
          </AppTypography>
        </div>
        <div>
          <AppTypography variant="body2" color="text.secondary">
            Vendor
          </AppTypography>
          <AppTypography variant="body2" fontWeight={500}>
            {drive.vendor || "N/A"}
          </AppTypography>
        </div>
        <div>
          <AppTypography variant="body2" color="text.secondary">
            Read Only
          </AppTypography>
          <AppTypography variant="body2" fontWeight={500}>
            {drive.ro ? "Yes" : "No"}
          </AppTypography>
        </div>
        <div>
          <AppTypography variant="body2" color="text.secondary">
            Transport
          </AppTypography>
          <AppTypography variant="body2" fontWeight={500}>
            {drive.transport.toUpperCase()}
          </AppTypography>
        </div>
      </div>

      {smart && (
        <>
          <AppTypography
            variant="caption"
            color="text.secondary"
            fontWeight={600}
            style={{ textTransform: "uppercase" }}
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
                <AppTypography variant="body2" color="text.secondary">
                  Temperature
                </AppTypography>
                <AppTypography
                  variant="body2"
                  fontWeight={500}
                  color={
                    temperature > 70
                      ? "error"
                      : temperature > 50
                        ? "warning"
                        : "text.primary"
                  }
                >
                  {temperature}°C
                </AppTypography>
              </div>
            )}
            {powerOnHours !== null && (
              <div>
                <AppTypography variant="body2" color="text.secondary">
                  Power On Time
                </AppTypography>
                <AppTypography variant="body2" fontWeight={500}>
                  {formatPowerOnTime(powerOnHours)}
                </AppTypography>
              </div>
            )}
            {powerCycles !== null && (
              <div>
                <AppTypography variant="body2" color="text.secondary">
                  Power Cycles
                </AppTypography>
                <AppTypography variant="body2" fontWeight={500}>
                  {powerCycles.toLocaleString()}
                </AppTypography>
              </div>
            )}
            {isNvme && percentageUsed !== undefined && (
              <div>
                <AppTypography variant="body2" color="text.secondary">
                  Life Used
                </AppTypography>
                <AppTypography
                  variant="body2"
                  fontWeight={500}
                  color={
                    percentageUsed > 90
                      ? "error"
                      : percentageUsed > 70
                        ? "warning"
                        : "text.primary"
                  }
                >
                  {percentageUsed}%
                </AppTypography>
              </div>
            )}
            {isNvme && dataRead !== undefined && (
              <div>
                <AppTypography variant="body2" color="text.secondary">
                  Data Read
                </AppTypography>
                <AppTypography variant="body2" fontWeight={500}>
                  {formatDataUnits(dataRead)}
                </AppTypography>
              </div>
            )}
            {isNvme && dataWritten !== undefined && (
              <div>
                <AppTypography variant="body2" color="text.secondary">
                  Data Written
                </AppTypography>
                <AppTypography variant="body2" fontWeight={500}>
                  {formatDataUnits(dataWritten)}
                </AppTypography>
              </div>
            )}
            {!isNvme && reallocatedSectors && (
              <div>
                <AppTypography variant="body2" color="text.secondary">
                  Reallocated Sectors
                </AppTypography>
                <AppTypography
                  variant="body2"
                  fontWeight={500}
                  color={
                    reallocatedSectors.raw.value > 0
                      ? "warning"
                      : "text.primary"
                  }
                >
                  {reallocatedSectors.raw.value}
                </AppTypography>
              </div>
            )}
            {!isNvme && pendingSectors && (
              <div>
                <AppTypography variant="body2" color="text.secondary">
                  Pending Sectors
                </AppTypography>
                <AppTypography
                  variant="body2"
                  fontWeight={500}
                  color={
                    pendingSectors.raw.value > 0 ? "warning" : "text.primary"
                  }
                >
                  {pendingSectors.raw.value}
                </AppTypography>
              </div>
            )}
          </div>
        </>
      )}

      {power && (
        <>
          <AppTypography
            variant="caption"
            color="text.secondary"
            fontWeight={600}
            style={{ textTransform: "uppercase" }}
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
                label={`State ${power.currentState}`}
                size="small"
                color="primary"
                variant="soft"
              />
              <AppTypography variant="body2" color="text.secondary">
                ~{power.estimatedW.toFixed(2)}W
              </AppTypography>
            </div>
          </div>
        </>
      )}

      {!smart && !power && (
        <AppTypography variant="body2" color="text.secondary">
          No detailed information available for this drive.
        </AppTypography>
      )}
    </div>
  );
};
