import { Box, Chip, Typography } from "@mui/material";
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
    <Box>
      <Box
        sx={{
          display: "grid",
          gridTemplateColumns: "1fr 1fr",
          gap: 1,
          mt: 1,
          mb: 2,
        }}
      >
        <Box>
          <Typography variant="body2" color="text.secondary">
            Serial
          </Typography>
          <Typography variant="body2" fontWeight={500} noWrap>
            {drive.serial || "N/A"}
          </Typography>
        </Box>
        <Box>
          <Typography variant="body2" color="text.secondary">
            Vendor
          </Typography>
          <Typography variant="body2" fontWeight={500}>
            {drive.vendor || "N/A"}
          </Typography>
        </Box>
        <Box>
          <Typography variant="body2" color="text.secondary">
            Read Only
          </Typography>
          <Typography variant="body2" fontWeight={500}>
            {drive.ro ? "Yes" : "No"}
          </Typography>
        </Box>
        <Box>
          <Typography variant="body2" color="text.secondary">
            Transport
          </Typography>
          <Typography variant="body2" fontWeight={500}>
            {drive.transport.toUpperCase()}
          </Typography>
        </Box>
      </Box>

      {smart && (
        <>
          <Typography
            variant="caption"
            color="text.secondary"
            sx={{ textTransform: "uppercase", fontWeight: 600 }}
          >
            Health & Statistics
          </Typography>
          <Box
            sx={{
              display: "grid",
              gridTemplateColumns: "1fr 1fr",
              gap: 1,
              mt: 1,
              mb: 2,
            }}
          >
            {temperature !== null && (
              <Box>
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
              </Box>
            )}
            {powerOnHours !== null && (
              <Box>
                <Typography variant="body2" color="text.secondary">
                  Power On Time
                </Typography>
                <Typography variant="body2" fontWeight={500}>
                  {formatPowerOnTime(powerOnHours)}
                </Typography>
              </Box>
            )}
            {powerCycles !== null && (
              <Box>
                <Typography variant="body2" color="text.secondary">
                  Power Cycles
                </Typography>
                <Typography variant="body2" fontWeight={500}>
                  {powerCycles.toLocaleString()}
                </Typography>
              </Box>
            )}
            {isNvme && percentageUsed !== undefined && (
              <Box>
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
              </Box>
            )}
            {isNvme && dataRead !== undefined && (
              <Box>
                <Typography variant="body2" color="text.secondary">
                  Data Read
                </Typography>
                <Typography variant="body2" fontWeight={500}>
                  {formatDataUnits(dataRead)}
                </Typography>
              </Box>
            )}
            {isNvme && dataWritten !== undefined && (
              <Box>
                <Typography variant="body2" color="text.secondary">
                  Data Written
                </Typography>
                <Typography variant="body2" fontWeight={500}>
                  {formatDataUnits(dataWritten)}
                </Typography>
              </Box>
            )}
            {!isNvme && reallocatedSectors && (
              <Box>
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
              </Box>
            )}
            {!isNvme && pendingSectors && (
              <Box>
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
              </Box>
            )}
          </Box>
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
          <Box sx={{ mt: 1 }}>
            <Box display="flex" gap={1} alignItems="center" mb={1}>
              <Chip
                label={`State ${power.currentState}`}
                size="small"
                color="primary"
                variant="outlined"
              />
              <Typography variant="body2" color="text.secondary">
                ~{power.estimatedW.toFixed(2)}W
              </Typography>
            </Box>
          </Box>
        </>
      )}

      {!smart && !power && (
        <Typography variant="body2" color="text.secondary">
          No detailed information available for this drive.
        </Typography>
      )}
    </Box>
  );
};
