import { Icon } from "@iconify/react";
import {
  Box,
  Button,
  Chip,
  CircularProgress,
  Collapse,
  Divider,
  Fade,
  Grid,
  LinearProgress,
  Tab,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Tabs,
  Tooltip,
  Typography,
} from "@mui/material";
import { useTheme } from "@mui/material/styles";
import { AnimatePresence, motion } from "framer-motion";
import React, { useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";

import {
  encodeString,
  getStreamMux,
  type ResultFrame,
  type Stream,
} from "@/api/linuxio";
import type { ApiDisk } from "@/api/linuxio-types";
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

interface SmartTestProgressEvent {
  type: "status" | "progress";
  device?: string;
  test_type?: "short" | "long";
  status?:
    | "starting"
    | "running"
    | "completed"
    | "aborted"
    | "failed"
    | "error"
    | "unknown";
  message?: string;
  percentage?: number;
  remaining_percent?: number;
  remaining_minutes?: number;
}

interface SmartTestResult {
  device?: string;
  test_type?: "short" | "long";
  status?: string;
  message?: string;
  duration_ms?: number;
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
  return `${units.toLocaleString()} [${formatFileSize(bytes)}]`;
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

// Helper functions for SMART data
const getSmartValue = (
  val: unknown,
  preferString = true,
): string | number | null => {
  if (val === undefined || val === null) return null;
  if (typeof val === "string" || typeof val === "number") return val;
  if (typeof val === "object") {
    const obj = val as { string?: string; value?: number };
    if (preferString && obj.string !== undefined) return obj.string;
    if (obj.value !== undefined) return obj.value;
    if (obj.string !== undefined) return obj.string;
  }
  return null;
};

const getSmartNumber = (val: unknown): number | null => {
  const result = getSmartValue(val, false);
  if (typeof result === "number") return result;
  if (typeof result === "string") {
    const parsed = parseFloat(result);
    return isNaN(parsed) ? null : parsed;
  }
  return null;
};

const getSmartString = (val: unknown): string | null => {
  const result = getSmartValue(val, true);
  return result !== null ? String(result) : null;
};

// Tab Panel Component
interface TabPanelProps {
  children?: React.ReactNode;
  index: number;
  value: number;
}

const TabPanel: React.FC<TabPanelProps> = ({ children, value, index }) => {
  return (
    <Box
      role="tabpanel"
      hidden={value !== index}
      sx={{ py: 2, display: value === index ? "block" : "none" }}
    >
      {children}
    </Box>
  );
};

// Info Row Component
const InfoRow: React.FC<{
  label: string;
  value: React.ReactNode;
  valueColor?: string;
}> = ({ label, value, valueColor }) => (
  <Box
    sx={{
      display: "flex",
      justifyContent: "space-between",
      py: 1,
      borderBottom: "1px solid",
      borderColor: "divider",
    }}
  >
    <Typography variant="body2" color="text.secondary">
      {label}
    </Typography>
    <Typography
      variant="body2"
      fontWeight={500}
      color={valueColor || "text.primary"}
    >
      {value}
    </Typography>
  </Box>
);

interface DriveDetailsProps {
  drive: DriveInfo;
  expanded: boolean;
  rawDrive: ApiDisk | null;
}

const STREAM_TYPE_SMART_TEST = "smart-test";

const DriveDetails: React.FC<DriveDetailsProps> = ({
  drive,
  expanded,
  rawDrive,
}) => {
  const [tabIndex, setTabIndex] = useState(0);
  const [startPending, setStartPending] = useState<"short" | "long" | null>(
    null,
  );
  const [testProgress, setTestProgress] =
    useState<SmartTestProgressEvent | null>(null);
  const streamRef = useRef<Stream | null>(null);

  const { mutateAsync: runSmartTest } =
    linuxio.storage.run_smart_test.useMutation();

  useEffect(() => {
    return () => {
      if (streamRef.current) {
        streamRef.current.close();
        streamRef.current = null;
      }
    };
  }, []);

  const handleRunTest = async (testType: "short" | "long") => {
    if (!rawDrive) return;

    setStartPending(testType);
    setTestProgress({
      type: "status",
      status: "starting",
      test_type: testType,
      device: rawDrive.name,
      message: `Starting SMART ${testType} self-test`,
    });

    const mux = getStreamMux();
    if (!mux || mux.status !== "open") {
      try {
        await runSmartTest([rawDrive.name, testType]);
        toast.success(
          `${testType === "short" ? "Short" : "Extended"} self-test started on /dev/${rawDrive.name}`,
        );
      } catch (err) {
        toast.error(
          `Failed to start test: ${err instanceof Error ? err.message : "Unknown error"}`,
        );
        setTestProgress((prev) =>
          prev
            ? { ...prev, status: "error", message: "Failed to start test" }
            : null,
        );
      } finally {
        setStartPending(null);
      }
      return;
    }

    if (streamRef.current) {
      streamRef.current.close();
    }

    const payload = encodeString(
      `${STREAM_TYPE_SMART_TEST}\0${rawDrive.name}\0${testType}`,
    );
    const stream = mux.openStream(STREAM_TYPE_SMART_TEST, payload);
    streamRef.current = stream;

    stream.onProgress = (progressData: unknown) => {
      const data = progressData as SmartTestProgressEvent;
      setTestProgress((prev) => ({
        ...(prev || {}),
        ...data,
        test_type: data.test_type ?? prev?.test_type ?? testType,
        device: data.device ?? prev?.device ?? rawDrive.name,
      }));
      if (data.status && data.status !== "starting") {
        setStartPending(null);
      }
    };

    stream.onResult = (result: ResultFrame) => {
      streamRef.current = null;
      setStartPending(null);

      if (result.status === "ok") {
        const data = result.data as SmartTestResult;
        const finalStatus = data?.status ?? "completed";
        setTestProgress((prev) => ({
          ...(prev || {}),
          type: "status",
          status: finalStatus as SmartTestProgressEvent["status"],
          message: data?.message ?? prev?.message,
          test_type: data?.test_type ?? prev?.test_type ?? testType,
          device: data?.device ?? prev?.device ?? rawDrive.name,
        }));

        if (finalStatus === "completed") {
          toast.success(
            `${testType === "short" ? "Short" : "Extended"} self-test completed on /dev/${rawDrive.name}`,
          );
        } else {
          toast.error(
            `${testType === "short" ? "Short" : "Extended"} self-test ${finalStatus}`,
          );
        }
      } else {
        setTestProgress((prev) => ({
          ...(prev || {}),
          type: "status",
          status: "error",
          message: result.error || "SMART self-test failed",
          test_type: prev?.test_type ?? testType,
          device: prev?.device ?? rawDrive.name,
        }));
        toast.error(result.error || "SMART self-test failed");
      }
    };

    stream.onClose = () => {
      streamRef.current = null;
      setStartPending(null);
      if (
        testProgress?.status === "running" ||
        testProgress?.status === "starting"
      ) {
        setTestProgress((prev) => ({
          ...(prev || {}),
          type: "status",
          status: "error",
          message: "SMART self-test stream closed unexpectedly",
        }));
        toast.error("SMART self-test stream closed unexpectedly");
      }
    };
  };

  const handleTabChange = (_: React.SyntheticEvent, newValue: number) => {
    setTabIndex(newValue);
  };

  const smart = drive.smart;
  const power = drive.power;

  // Extract SMART data based on drive type (NVMe vs ATA)
  const isNvme = drive.transport === "nvme";
  const nvmeHealth = smart?.nvme_smart_health_information_log;
  const ataAttrs = smart?.ata_smart_attributes?.table;

  const smartData = rawDrive?.smart as Record<string, unknown> | undefined;
  const deviceInfo = smartData?.device as Record<string, unknown> | undefined;
  const smartHealth = smartData?.smart_status as
    | { passed?: boolean }
    | undefined;

  // Access full SMART data for detailed attributes
  const nvmeHealthRaw = smartData?.nvme_smart_health_information_log as
    | Record<string, unknown>
    | undefined;

  const selfTestLog = smartData?.ata_smart_self_test_log as
    | { standard?: { table?: unknown[] } }
    | undefined;
  const nvmeSelfTestLog = smartData?.nvme_self_test_log as
    | { table?: unknown[] }
    | undefined;

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

  if (!expanded) return null;

  return (
    <Collapse in={expanded} timeout="auto" unmountOnExit>
      <Box onClick={(e) => e.stopPropagation()}>
        <Divider sx={{ my: 2 }} />

        {/* Tabs */}
        <Box sx={{ borderBottom: 1, borderColor: "divider" }}>
          <Tabs value={tabIndex} onChange={handleTabChange}>
            <Tab label="Overview" />
            <Tab label="SMART Attributes" />
            <Tab label="Drive Information" />
            {isNvme && power && <Tab label="Power States" />}
            <Tab label="Self-Tests" />
          </Tabs>
        </Box>

        {/* Tab Panels */}

        {/* Overview Tab */}
        <TabPanel value={tabIndex} index={0}>
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

            {/* NVMe Power States Summary */}
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
        </TabPanel>

        {/* SMART Attributes Tab */}
        <TabPanel value={tabIndex} index={1}>
          {isNvme && nvmeHealthRaw ? (
            <TableContainer>
              <Table size="small">
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
                            (getSmartNumber(nvmeHealthRaw.temperature) ?? 0) >
                            70
                              ? "error.main"
                              : (getSmartNumber(nvmeHealthRaw.temperature) ??
                                    0) > 50
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
                        {getSmartNumber(
                          nvmeHealthRaw.available_spare_threshold,
                        )}
                        %
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
                            (getSmartNumber(nvmeHealthRaw.percentage_used) ??
                              0) > 90
                              ? "error.main"
                              : (getSmartNumber(
                                    nvmeHealthRaw.percentage_used,
                                  ) ?? 0) > 70
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
                          getSmartNumber(nvmeHealthRaw.data_units_read) ??
                            undefined,
                        )}
                      </TableCell>
                    </TableRow>
                  )}
                  {getSmartNumber(nvmeHealthRaw.data_units_written) !==
                    null && (
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
                        {getSmartNumber(
                          nvmeHealthRaw.host_reads,
                        )?.toLocaleString()}
                      </TableCell>
                    </TableRow>
                  )}
                  {getSmartNumber(nvmeHealthRaw.host_writes) !== null && (
                    <TableRow>
                      <TableCell>Host Write Commands</TableCell>
                      <TableCell align="right">
                        {getSmartNumber(
                          nvmeHealthRaw.host_writes,
                        )?.toLocaleString()}
                      </TableCell>
                    </TableRow>
                  )}
                  {getSmartNumber(nvmeHealthRaw.controller_busy_time) !==
                    null && (
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
                        {getSmartNumber(
                          nvmeHealthRaw.power_cycles,
                        )?.toLocaleString()}
                      </TableCell>
                    </TableRow>
                  )}
                  {getSmartNumber(nvmeHealthRaw.power_on_hours) !== null && (
                    <TableRow>
                      <TableCell>Power On Hours</TableCell>
                      <TableCell align="right">
                        {formatPowerOnTime(
                          getSmartNumber(nvmeHealthRaw.power_on_hours) ??
                            undefined,
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
                            (getSmartNumber(nvmeHealthRaw.media_errors) ?? 0) >
                            0
                              ? "error.main"
                              : "inherit",
                        }}
                      >
                        {getSmartNumber(
                          nvmeHealthRaw.media_errors,
                        )?.toLocaleString()}
                      </TableCell>
                    </TableRow>
                  )}
                  {getSmartNumber(nvmeHealthRaw.num_err_log_entries) !==
                    null && (
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
          ) : ataAttrs && ataAttrs.length > 0 ? (
            <TableContainer>
              <Table size="small">
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
                            // Highlight concerning attributes
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
          ) : (
            <Typography color="text.secondary">
              No SMART attributes available for this drive.
            </Typography>
          )}
        </TabPanel>

        {/* Drive Information Tab */}
        <TabPanel value={tabIndex} index={2}>
          <Box sx={{ maxWidth: 600 }}>
            <InfoRow label="Model" value={drive.model || "N/A"} />
            <InfoRow label="Serial Number" value={drive.serial || "N/A"} />
            <InfoRow label="Vendor" value={drive.vendor || "N/A"} />
            <InfoRow
              label="Firmware Version"
              value={getSmartString(smartData?.firmware_version) || "N/A"}
            />
            <InfoRow label="Capacity" value={rawDrive?.size || "N/A"} />
            <InfoRow
              label="Transport"
              value={drive.transport?.toUpperCase() || "N/A"}
            />
            <InfoRow label="Read Only" value={drive.ro ? "Yes" : "No"} />
            {isNvme && (
              <>
                <InfoRow
                  label="NVMe Version"
                  value={getSmartString(smartData?.nvme_version) || "N/A"}
                />
                <InfoRow
                  label="Number of Namespaces"
                  value={
                    getSmartNumber(
                      smartData?.nvme_number_of_namespaces,
                    )?.toString() || "N/A"
                  }
                />
              </>
            )}
            {deviceInfo && (
              <>
                <InfoRow
                  label="Device Type"
                  value={getSmartString(deviceInfo.type) || "N/A"}
                />
                <InfoRow
                  label="Protocol"
                  value={getSmartString(deviceInfo.protocol) || "N/A"}
                />
              </>
            )}
            <InfoRow
              label="SMART Health"
              value={
                smartHealth?.passed === true
                  ? "Passed"
                  : smartHealth?.passed === false
                    ? "Failed"
                    : "Unknown"
              }
              valueColor={
                smartHealth?.passed === true
                  ? "success.main"
                  : smartHealth?.passed === false
                    ? "error.main"
                    : undefined
              }
            />
          </Box>
        </TabPanel>

        {/* Power States Tab (NVMe only) */}
        {isNvme && power && (
          <TabPanel value={tabIndex} index={3}>
            <Box sx={{ mb: 3 }}>
              <Typography variant="subtitle2" gutterBottom>
                Current State
              </Typography>
              <Box display="flex" gap={2} alignItems="center">
                <Chip
                  label={`Power State ${power.currentState}`}
                  color="primary"
                />
                <Typography variant="body2" color="text.secondary">
                  Estimated Power: ~{power.estimatedW.toFixed(2)}W
                </Typography>
              </Box>
            </Box>

            <Typography variant="subtitle2" gutterBottom>
              Supported Power States
            </Typography>
            <TableContainer>
              <Table size="small">
                <TableHead>
                  <TableRow>
                    <TableCell sx={{ fontWeight: 600 }}>State</TableCell>
                    <TableCell sx={{ fontWeight: 600 }}>Op</TableCell>
                    <TableCell sx={{ fontWeight: 600 }} align="right">
                      Max Power
                    </TableCell>
                    <TableCell sx={{ fontWeight: 600 }}>Description</TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {power.states.map((ps) => (
                    <TableRow
                      key={ps.state}
                      selected={ps.state === power.currentState}
                    >
                      <TableCell>{ps.state}</TableCell>
                      <TableCell>+</TableCell>
                      <TableCell align="right">{ps.maxPowerW}W</TableCell>
                      <TableCell sx={{ fontSize: "0.75rem" }}>
                        {ps.description}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </TableContainer>
          </TabPanel>
        )}

        {/* Self-Tests Tab */}
        <TabPanel value={tabIndex} index={isNvme && power ? 4 : 3}>
          <Box sx={{ mb: 3 }}>
            <Typography variant="subtitle2" gutterBottom>
              Run SMART Self-Test
            </Typography>
            <Box display="flex" gap={2} alignItems="center">
              <Button
                variant="outlined"
                size="small"
                disabled={startPending !== null}
                onClick={(e) => {
                  e.stopPropagation();
                  handleRunTest("short");
                }}
                startIcon={
                  startPending === "short" ? (
                    <CircularProgress size={16} />
                  ) : undefined
                }
              >
                {startPending === "short" ? "Starting..." : "Short Test"}
              </Button>
              <Button
                variant="outlined"
                size="small"
                disabled={startPending !== null}
                onClick={(e) => {
                  e.stopPropagation();
                  handleRunTest("long");
                }}
                startIcon={
                  startPending === "long" ? (
                    <CircularProgress size={16} />
                  ) : undefined
                }
              >
                {startPending === "long" ? "Starting..." : "Extended Test"}
              </Button>
            </Box>
            <Typography
              variant="caption"
              color="text.secondary"
              sx={{ mt: 1, display: "block" }}
            >
              Short test takes ~2 minutes. Extended test can take hours
              depending on drive size.
            </Typography>
          </Box>

          <Typography variant="subtitle2" gutterBottom>
            Self-Test History
          </Typography>
          {selfTestLog?.standard?.table &&
          (selfTestLog.standard.table as unknown[]).length > 0 ? (
            <TableContainer>
              <Table size="small">
                <TableHead>
                  <TableRow>
                    <TableCell sx={{ fontWeight: 600 }}>#</TableCell>
                    <TableCell sx={{ fontWeight: 600 }}>Type</TableCell>
                    <TableCell sx={{ fontWeight: 600 }}>Status</TableCell>
                    <TableCell sx={{ fontWeight: 600 }} align="right">
                      Lifetime Hours
                    </TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {(
                    selfTestLog.standard.table as {
                      num?: number;
                      type?: { string?: string };
                      status?: { string?: string; passed?: boolean };
                      lifetime_hours?: number;
                    }[]
                  ).map((entry, idx) => (
                    <TableRow key={idx}>
                      <TableCell>{entry.num ?? idx + 1}</TableCell>
                      <TableCell>{entry.type?.string || "Unknown"}</TableCell>
                      <TableCell
                        sx={{
                          color: entry.status?.passed
                            ? "success.main"
                            : "error.main",
                        }}
                      >
                        {entry.status?.string || "Unknown"}
                      </TableCell>
                      <TableCell align="right">
                        {entry.lifetime_hours?.toLocaleString() || "N/A"}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </TableContainer>
          ) : nvmeSelfTestLog?.table &&
            (nvmeSelfTestLog.table as unknown[]).length > 0 ? (
            <TableContainer>
              <Table size="small">
                <TableHead>
                  <TableRow>
                    <TableCell sx={{ fontWeight: 600 }}>Type</TableCell>
                    <TableCell sx={{ fontWeight: 600 }}>Result</TableCell>
                    <TableCell sx={{ fontWeight: 600 }} align="right">
                      Power On Hours
                    </TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {(
                    nvmeSelfTestLog.table as {
                      self_test_code?: { string?: string };
                      self_test_result?: { string?: string; value?: number };
                      power_on_hours?: number;
                    }[]
                  ).map((entry, idx) => (
                    <TableRow key={idx}>
                      <TableCell>
                        {entry.self_test_code?.string || "Unknown"}
                      </TableCell>
                      <TableCell
                        sx={{
                          color:
                            entry.self_test_result?.value === 0
                              ? "success.main"
                              : "error.main",
                        }}
                      >
                        {entry.self_test_result?.string || "Unknown"}
                      </TableCell>
                      <TableCell align="right">
                        {entry.power_on_hours?.toLocaleString() || "N/A"}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </TableContainer>
          ) : (
            <Typography color="text.secondary">
              No self-test history available.
            </Typography>
          )}
        </TabPanel>
      </Box>
    </Collapse>
  );
};

const DiskOverview: React.FC = () => {
  const theme = useTheme();
  const [expanded, setExpanded] = useState<string | null>(null);

  const { data: rawDrives = [], isPending: drivesLoading } =
    linuxio.storage.get_drive_info.useQuery({ refetchInterval: 30000 });

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
                    {drive.transport.toLowerCase() === "usb" ? (
                      <Tooltip
                        title="Create Bootable USB"
                        placement="top"
                        arrow
                        slots={{ transition: Fade }}
                        slotProps={{ transition: { timeout: 300 } }}
                      >
                        <Box
                          sx={{
                            position: "absolute",
                            top: 8,
                            right: 8,
                            cursor: "pointer",
                            "&:hover": {
                              opacity: 0.7,
                            },
                          }}
                          onClick={(e) => {
                            e.stopPropagation();
                            // TODO: Add handler for bootable USB creation
                          }}
                        >
                          <Icon
                            icon="mdi:pencil"
                            width={20}
                            color={theme.palette.text.secondary}
                          />
                        </Box>
                      </Tooltip>
                    ) : getTemperature(drive.smart) !== null ? (
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
                            color={getTemperatureColor(
                              getTemperature(drive.smart),
                            )}
                          >
                            {getTemperature(drive.smart)}°C
                          </Typography>
                        </Box>
                      </Tooltip>
                    ) : null}
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
                      rawDrive={
                        rawDrives.find((d) => d.name === drive.name) || null
                      }
                    />
                  </FrostedCard>
                </Grid>
              ),
            )
          )}
        </AnimatePresence>
      </Grid>

      {!expanded && (
        <>
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
        </>
      )}
    </Box>
  );
};

export default DiskOverview;
