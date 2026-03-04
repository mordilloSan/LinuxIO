import { Chip, Typography } from "@mui/material";
import React from "react";

import type { DriveInfo } from "../types";
import { formatDataUnits, formatPowerOnTime } from "../utils";

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
          <Typography variant="body2" color="text.secondary">
            Serial
          </Typography>
          <Typography variant="body2" fontWeight={500} noWrap>
            {drive.serial || "N/A"}
          </Typography>
        </div>
        <div>
          <Typography variant="body2" color="text.secondary">
            Vendor
          </Typography>
          <Typography variant="body2" fontWeight={500}>
            {drive.vendor || "N/A"}
          </Typography>
        </div>
        <div>
          <Typography variant="body2" color="text.secondary">
            Read Only
          </Typography>
          <Typography variant="body2" fontWeight={500}>
            {drive.ro ? "Yes" : "No"}
          </Typography>
        </div>
        <div>
          <Typography variant="body2" color="text.secondary">
            Transport
          </Typography>
          <Typography variant="body2" fontWeight={500}>
            {drive.transport.toUpperCase()}
          </Typography>
        </div>
      </div>

      {smart && (
        <>
          <Typography
            variant="caption"
            color="text.secondary"
            sx={{ textTransform: "uppercase", fontWeight: 600 }}
          >
            Health & Statistics
          </Typography>
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
                <Typography variant="body2" color="text.secondary">
                  Temperature
                </Typography>
                <Typography
                  variant="body2"
                  fontWeight={500}
                  color={
                    temperature > 70
                      ? "error.main"
                      : temperature > 50
                        ? "warning.main"
                        : "text.primary"
                  }
                >
                  {temperature}°C
                </Typography>
              </div>
            )}
            {powerOnHours !== null && (
              <div>
                <Typography variant="body2" color="text.secondary">
                  Power On Time
                </Typography>
                <Typography variant="body2" fontWeight={500}>
                  {formatPowerOnTime(powerOnHours)}
                </Typography>
              </div>
            )}
            {powerCycles !== null && (
              <div>
                <Typography variant="body2" color="text.secondary">
                  Power Cycles
                </Typography>
                <Typography variant="body2" fontWeight={500}>
                  {powerCycles.toLocaleString()}
                </Typography>
              </div>
            )}
            {isNvme && percentageUsed !== undefined && (
              <div>
                <Typography variant="body2" color="text.secondary">
                  Life Used
                </Typography>
                <Typography
                  variant="body2"
                  fontWeight={500}
                  color={
                    percentageUsed > 90
                      ? "error.main"
                      : percentageUsed > 70
                        ? "warning.main"
                        : "text.primary"
                  }
                >
                  {percentageUsed}%
                </Typography>
              </div>
            )}
            {isNvme && dataRead !== undefined && (
              <div>
                <Typography variant="body2" color="text.secondary">
                  Data Read
                </Typography>
                <Typography variant="body2" fontWeight={500}>
                  {formatDataUnits(dataRead)}
                </Typography>
              </div>
            )}
            {isNvme && dataWritten !== undefined && (
              <div>
                <Typography variant="body2" color="text.secondary">
                  Data Written
                </Typography>
                <Typography variant="body2" fontWeight={500}>
                  {formatDataUnits(dataWritten)}
                </Typography>
              </div>
            )}
            {!isNvme && reallocatedSectors && (
              <div>
                <Typography variant="body2" color="text.secondary">
                  Reallocated Sectors
                </Typography>
                <Typography
                  variant="body2"
                  fontWeight={500}
                  color={
                    reallocatedSectors.raw.value > 0
                      ? "warning.main"
                      : "text.primary"
                  }
                >
                  {reallocatedSectors.raw.value}
                </Typography>
              </div>
            )}
            {!isNvme && pendingSectors && (
              <div>
                <Typography variant="body2" color="text.secondary">
                  Pending Sectors
                </Typography>
                <Typography
                  variant="body2"
                  fontWeight={500}
                  color={
                    pendingSectors.raw.value > 0
                      ? "warning.main"
                      : "text.primary"
                  }
                >
                  {pendingSectors.raw.value}
                </Typography>
              </div>
            )}
          </div>
        </>
      )}

      {power && (
        <>
          <Typography
            variant="caption"
            color="text.secondary"
            sx={{ textTransform: "uppercase", fontWeight: 600 }}
          >
            Power
          </Typography>
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
                variant="outlined"
              />
              <Typography variant="body2" color="text.secondary">
                ~{power.estimatedW.toFixed(2)}W
              </Typography>
            </div>
          </div>
        </>
      )}

      {!smart && !power && (
        <Typography variant="body2" color="text.secondary">
          No detailed information available for this drive.
        </Typography>
      )}
    </div>
  );
};
