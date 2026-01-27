import { Icon } from "@iconify/react";
import {
  Box,
  Chip,
  Collapse,
  Divider,
  Fade,
  Grid,
  LinearProgress,
  Tooltip,
  Typography,
} from "@mui/material";
import { useTheme } from "@mui/material/styles";
import { AnimatePresence, motion } from "framer-motion";
import React, { useEffect, useMemo, useState } from "react";

import linuxio from "@/api/react-query";
import FrostedCard from "@/components/cards/RootCard";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import { formatFileSize } from "@/utils/formaters";

interface SmartAttribute {
  id: number;
  name: string;
  value: number;
  worst: number;
  thresh: number;
  raw: { value: number; string?: string };
}

interface SmartData {
  smart_status?: { passed?: boolean };
  temperature?: { current?: number };
  power_on_time?: { hours?: number };
  power_cycle_count?: number;
  ata_smart_attributes?: { table?: SmartAttribute[] };
  nvme_smart_health_information_log?: {
    temperature?: number;
    power_on_hours?: number;
    power_cycles?: number;
    percentage_used?: number;
    data_units_read?: number;
    data_units_written?: number;
  };
}

interface PowerState {
  state: number;
  maxPowerW: number;
  description: string;
}

interface PowerData {
  currentState: number;
  estimatedW: number;
  states: PowerState[];
}

interface DriveInfo {
  name: string;
  model: string;
  sizeBytes: number;
  transport: string;
  vendor?: string;
  serial?: string;
  ro?: boolean;
  smart?: SmartData;
  power?: PowerData;
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

const formatPowerOnTime = (hours?: number): string => {
  if (hours === undefined) return "N/A";
  const days = Math.floor(hours / 24);
  const remainingHours = hours % 24;
  if (days > 0) {
    return `${days}d ${remainingHours}h`;
  }
  return `${hours}h`;
};

const formatDataUnits = (units?: number): string => {
  if (units === undefined) return "N/A";
  // NVMe data units are in 512KB blocks
  const bytes = units * 512 * 1000;
  return formatFileSize(bytes);
};

const getTemperature = (smart?: SmartData): number | null => {
  if (!smart) return null;
  return (
    smart.nvme_smart_health_information_log?.temperature ??
    smart.temperature?.current ??
    null
  );
};

const getTemperatureColor = (temp: number | null): string => {
  if (temp === null) return "text.secondary";
  if (temp > 70) return "error.main";
  if (temp > 50) return "warning.main";
  return "success.main";
};

interface DriveDetailsProps {
  drive: DriveInfo;
  expanded: boolean;
}

const DriveDetails: React.FC<DriveDetailsProps> = ({ drive, expanded }) => {
  const smart = drive.smart;
  const power = drive.power;

  // Extract SMART data based on drive type (NVMe vs ATA)
  const isNvme = drive.transport === "nvme";
  const nvmeHealth = smart?.nvme_smart_health_information_log;
  const ataAttrs = smart?.ata_smart_attributes?.table;

  // Get temperature
  const temperature =
    nvmeHealth?.temperature ?? smart?.temperature?.current ?? null;

  // Get power-on hours
  const powerOnHours =
    nvmeHealth?.power_on_hours ?? smart?.power_on_time?.hours ?? null;

  // Get power cycles
  const powerCycles =
    nvmeHealth?.power_cycles ?? smart?.power_cycle_count ?? null;

  // NVMe-specific: percentage used
  const percentageUsed = nvmeHealth?.percentage_used;

  // NVMe-specific: data read/written
  const dataRead = nvmeHealth?.data_units_read;
  const dataWritten = nvmeHealth?.data_units_written;

  // Find specific ATA SMART attributes
  const findAtaAttr = (id: number) => ataAttrs?.find((a) => a.id === id);
  const reallocatedSectors = findAtaAttr(5);
  const pendingSectors = findAtaAttr(197);

  return (
    <Collapse in={expanded} timeout="auto" unmountOnExit>
      <Divider sx={{ my: 2 }} />
      <Box>
        {/* Basic Info */}
        <Typography
          variant="caption"
          color="text.secondary"
          sx={{ textTransform: "uppercase", fontWeight: 600 }}
        >
          Drive Information
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

        {/* SMART Data */}
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

        {/* NVMe Power States */}
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
              <Box display="flex" gap={0.5} flexWrap="wrap">
                {power.states.map((ps) => (
                  <Chip
                    key={ps.state}
                    label={`PS${ps.state}: ${ps.maxPowerW}W`}
                    size="small"
                    variant={
                      ps.state === power.currentState ? "filled" : "outlined"
                    }
                    color={ps.state === power.currentState ? "primary" : "default"}
                    sx={{
                      fontSize: "0.7rem",
                      height: 22,
                      opacity: ps.state === power.currentState ? 1 : 0.7,
                    }}
                  />
                ))}
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
    </Collapse>
  );
};

const DiskOverview: React.FC = () => {
  const theme = useTheme();
  const [expanded, setExpanded] = useState<string | null>(null);

  const { data: rawDrives = [], isPending: drivesLoading } =
    linuxio.system.get_drive_info.useQuery({ refetchInterval: 30000 });

  const { data: filesystems = [], isPending: fsLoading } =
    linuxio.system.get_fs_info.useQuery({ refetchInterval: 10000 });

  // Close expanded card on Escape key
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") setExpanded(null);
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, []);

  const handleToggle = (driveName: string) => {
    setExpanded((prev) => (prev === driveName ? null : driveName));
  };

  const drives = useMemo<DriveInfo[]>(
    () =>
      rawDrives.map((d) => ({
        name: d.name,
        model: d.model,
        sizeBytes: parseSizeToBytes(d.size),
        transport: d.type ?? "unknown",
        vendor: d.vendor,
        serial: d.serial,
        ro: d.ro,
        smart: d.smart as DriveInfo["smart"],
        power: d.power as DriveInfo["power"],
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
        <AnimatePresence>
          {drives.length === 0 ? (
            <Grid size={{ xs: 12 }}>
              <Typography color="text.secondary">No drives found.</Typography>
            </Grid>
          ) : (
            drives.map((drive) =>
              expanded && expanded !== drive.name ? null : (
                <Grid
                  key={drive.name}
                  size={{
                    xs: 12,
                    sm: expanded === drive.name ? 12 : 6,
                    md: expanded === drive.name ? 8 : 4,
                    lg: expanded === drive.name ? 6 : 3,
                  }}
                  component={motion.div}
                  layout
                  initial={{ opacity: 0, scale: 0.95 }}
                  animate={{ opacity: 1, scale: 1 }}
                  exit={{ opacity: 0, scale: 0.9 }}
                  transition={{ duration: 0.2 }}
                >
                  <FrostedCard
                    sx={{
                      p: 2,
                      position: "relative",
                      transition: "transform 0.2s, box-shadow 0.2s",
                      cursor: "pointer",
                      ...(expanded !== drive.name && {
                        "&:hover": {
                          transform: "translateY(-4px)",
                          boxShadow: "0 8px 24px rgba(0,0,0,0.35)",
                        },
                      }),
                    }}
                    onClick={() => handleToggle(drive.name)}
                  >
                    {getTemperature(drive.smart) !== null && (
                      <Tooltip
                        title="Drive Temperature"
                        placement="top"
                        arrow
                        slots={{ transition: Fade }}
                        slotProps={{ transition: { timeout: 300 } }}
                      >
                        <Box
                          sx={{
                            position: "absolute",
                            top: 12,
                            right: 12,
                            display: "flex",
                            alignItems: "center",
                            gap: 0.5,
                          }}
                        >
                          <Typography
                            variant="body2"
                            fontWeight={600}
                            color={getTemperatureColor(getTemperature(drive.smart))}
                          >
                            {getTemperature(drive.smart)}°C
                          </Typography>
                        </Box>
                      </Tooltip>
                    )}
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
                    <DriveDetails
                      drive={drive}
                      expanded={expanded === drive.name}
                    />
                  </FrostedCard>
                </Grid>
              ),
            )
          )}
        </AnimatePresence>
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
