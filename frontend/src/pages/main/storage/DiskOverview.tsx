import { Icon } from "@iconify/react";
import { Box, Chip, Grid, LinearProgress, Typography } from "@mui/material";
import { useTheme } from "@mui/material/styles";
import React, { useMemo } from "react";

import linuxio from "@/api/react-query";
import FrostedCard from "@/components/cards/RootCard";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import { formatFileSize } from "@/utils/formaters";

interface DriveInfo {
  name: string;
  model: string;
  sizeBytes: number;
  transport: string;
  vendor?: string;
  serial?: string;
  smart?: { smart_status?: { passed?: boolean } };
}

function parseSizeToBytes(input: string | undefined | null): number {
  if (!input) return 0;
  const s = String(input).trim().toUpperCase();
  const m = s.match(/^([\d.]+)\s*([KMGTPE]?)(B)?$/);
  if (!m) return 0;
  const value = parseFloat(m[1]);
  if (!isFinite(value) || value < 0) return 0;
  const unit = m[2] || "B";
  const pow =
    unit === "B"
      ? 0
      : unit === "K"
        ? 1
        : unit === "M"
          ? 2
          : unit === "G"
            ? 3
            : unit === "T"
              ? 4
              : unit === "P"
                ? 5
                : 0;
  return Math.floor(value * Math.pow(1024, pow));
}

const getHealthColor = (
  smart: DriveInfo["smart"] | undefined,
): "success" | "error" | "warning" | "default" => {
  if (!smart?.smart_status) return "default";
  const passed = smart.smart_status.passed;
  if (passed === true) return "success";
  if (passed === false) return "error";
  return "warning";
};

const DiskOverview: React.FC = () => {
  const theme = useTheme();

  const { data: rawDrives = [], isPending: drivesLoading } =
    linuxio.system.get_drive_info.useQuery({ refetchInterval: 30000 });

  const { data: filesystems = [], isPending: fsLoading } =
    linuxio.system.get_fs_info.useQuery({ refetchInterval: 10000 });

  const drives = useMemo<DriveInfo[]>(
    () =>
      rawDrives.map((d) => ({
        name: d.name,
        model: d.model,
        sizeBytes: parseSizeToBytes(d.size),
        transport: d.type ?? "unknown",
        vendor: d.vendor,
        serial: d.serial,
        smart: d.smart as DriveInfo["smart"],
      })),
    [rawDrives],
  );

  // Filter out pseudo filesystems
  const relevantFS = useMemo(
    () =>
      filesystems.filter((fs) => {
        const mount = fs.mountpoint;
        return (
          fs.total > 0 &&
          mount !== "" &&
          !mount.startsWith("/var/lib/docker/") &&
          !mount.startsWith("/sys/firmware/") &&
          !mount.startsWith("/dev") &&
          !mount.startsWith("/run") &&
          !mount.startsWith("/proc") &&
          !mount.startsWith("/sys/fs")
        );
      }),
    [filesystems],
  );

  if (drivesLoading || fsLoading) {
    return <ComponentLoader />;
  }

  return (
    <Box>
      <Typography variant="h6" sx={{ mb: 2, fontWeight: 600 }}>
        Physical Drives
      </Typography>
      <Grid container spacing={3} sx={{ mb: 4 }}>
        {drives.length === 0 ? (
          <Grid size={{ xs: 12 }}>
            <Typography color="text.secondary">No drives found.</Typography>
          </Grid>
        ) : (
          drives.map((drive) => (
            <Grid key={drive.name} size={{ xs: 12, sm: 6, md: 4, lg: 3 }}>
              <FrostedCard sx={{ p: 2 }}>
                <Box display="flex" alignItems="center" mb={1.5}>
                  <Icon
                    icon={
                      drive.transport === "nvme"
                        ? "mdi:harddisk"
                        : "mdi:harddisk-plus"
                    }
                    width={32}
                    color={theme.palette.primary.main}
                  />
                  <Box ml={1.5} flexGrow={1} minWidth={0}>
                    <Typography variant="subtitle1" fontWeight={600} noWrap>
                      /dev/{drive.name}
                    </Typography>
                    <Typography
                      variant="body2"
                      color="text.secondary"
                      noWrap
                      title={drive.model || "Unknown Model"}
                    >
                      {drive.model || "Unknown Model"}
                    </Typography>
                  </Box>
                </Box>
                <Box display="flex" gap={1} flexWrap="wrap">
                  <Chip
                    label={formatFileSize(drive.sizeBytes)}
                    size="small"
                    color="primary"
                    variant="outlined"
                  />
                  <Chip
                    label={drive.transport.toUpperCase()}
                    size="small"
                    variant="outlined"
                  />
                  {drive.smart?.smart_status && (
                    <Chip
                      label={
                        getHealthColor(drive.smart) === "success"
                          ? "Healthy"
                          : getHealthColor(drive.smart) === "error"
                            ? "Failing"
                            : "Unknown"
                      }
                      size="small"
                      color={getHealthColor(drive.smart)}
                      variant="filled"
                    />
                  )}
                </Box>
              </FrostedCard>
            </Grid>
          ))
        )}
      </Grid>

      <Typography variant="h6" sx={{ mb: 2, fontWeight: 600 }}>
        Filesystems
      </Typography>
      <Grid container spacing={3}>
        {relevantFS.length === 0 ? (
          <Grid size={{ xs: 12 }}>
            <Typography color="text.secondary">
              No filesystems found.
            </Typography>
          </Grid>
        ) : (
          relevantFS.map((fs) => (
            <Grid key={fs.mountpoint} size={{ xs: 12, sm: 6, md: 4 }}>
              <FrostedCard sx={{ p: 2 }}>
                <Typography
                  variant="subtitle2"
                  fontWeight={600}
                  noWrap
                  title={fs.mountpoint}
                >
                  {fs.mountpoint}
                </Typography>
                <Typography
                  variant="body2"
                  color="text.secondary"
                  noWrap
                  sx={{ mb: 1.5 }}
                  title={`${fs.device} (${fs.fstype})`}
                >
                  {fs.device} ({fs.fstype})
                </Typography>
                <LinearProgress
                  variant="determinate"
                  value={fs.usedPercent}
                  sx={{ height: 8, borderRadius: 4, mb: 1 }}
                  color={
                    fs.usedPercent > 90
                      ? "error"
                      : fs.usedPercent > 70
                        ? "warning"
                        : "primary"
                  }
                />
                <Typography variant="body2" color="text.secondary">
                  {formatFileSize(fs.used)} / {formatFileSize(fs.total)} (
                  {fs.usedPercent.toFixed(1)}%)
                </Typography>
              </FrostedCard>
            </Grid>
          ))
        )}
      </Grid>
    </Box>
  );
};

export default DiskOverview;
