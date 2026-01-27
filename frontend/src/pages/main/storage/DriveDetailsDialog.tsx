import { Icon } from "@iconify/react";
import CloseIcon from "@mui/icons-material/Close";
import {
  Box,
  Button,
  Chip,
  CircularProgress,
  Dialog,
  DialogContent,
  DialogTitle,
  IconButton,
  Tab,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Tabs,
  Typography,
} from "@mui/material";
import { useTheme } from "@mui/material/styles";
import React, { useState } from "react";
import { toast } from "sonner";

import type { ApiDisk } from "@/api/linuxio-types";
import linuxio from "@/api/react-query";
import { formatFileSize } from "@/utils/formaters";

interface DriveDetailsDialogProps {
  open: boolean;
  onClose: () => void;
  drive: ApiDisk | null;
}

// Helper to safely extract a value from SMART data
// Handles both plain values and {string, value} objects
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

// Helper to get numeric value from SMART data
const getSmartNumber = (val: unknown): number | null => {
  const result = getSmartValue(val, false);
  if (typeof result === "number") return result;
  if (typeof result === "string") {
    const parsed = parseFloat(result);
    return isNaN(parsed) ? null : parsed;
  }
  return null;
};

// Helper to get string value from SMART data
const getSmartString = (val: unknown): string | null => {
  const result = getSmartValue(val, true);
  return result !== null ? String(result) : null;
};

// Helper to format power-on time
const formatPowerOnTime = (hours?: number | null): string => {
  if (hours === undefined || hours === null) return "N/A";
  const years = Math.floor(hours / 8760);
  const days = Math.floor((hours % 8760) / 24);
  const remainingHours = hours % 24;
  if (years > 0) {
    return `${hours.toLocaleString()} (${years}y, ${days}d, ${remainingHours}h)`;
  }
  if (days > 0) {
    return `${hours.toLocaleString()} (${days}d, ${remainingHours}h)`;
  }
  return `${hours}h`;
};

// Helper to format data units (NVMe uses 512KB blocks)
const formatDataUnits = (units?: number | null): string => {
  if (units === undefined || units === null) return "N/A";
  const bytes = units * 512 * 1000;
  return `${units.toLocaleString()} [${formatFileSize(bytes)}]`;
};

// Tab panel component
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

// Info row component for key-value display
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

const DriveDetailsDialog: React.FC<DriveDetailsDialogProps> = ({
  open,
  onClose,
  drive,
}) => {
  const theme = useTheme();
  const [tabIndex, setTabIndex] = useState(0);
  const [runningTest, setRunningTest] = useState<"short" | "long" | null>(null);

  // Mutation for running SMART self-tests
  const { mutateAsync: runSmartTest } =
    linuxio.system.run_smart_test.useMutation();

  const handleRunTest = async (testType: "short" | "long") => {
    if (!drive) return;
    setRunningTest(testType);
    try {
      await runSmartTest([drive.name, testType]);
      toast.success(
        `${testType === "short" ? "Short" : "Extended"} self-test started on /dev/${drive.name}`,
      );
    } catch (err) {
      toast.error(
        `Failed to start test: ${err instanceof Error ? err.message : "Unknown error"}`,
      );
    } finally {
      setRunningTest(null);
    }
  };

  if (!drive) return null;

  const smart = drive.smart as Record<string, unknown> | undefined;
  const power = drive.power as
    | {
        currentState: number;
        estimatedW: number;
        states: { state: number; maxPowerW: number; description: string }[];
      }
    | undefined;

  const isNvme = drive.type === "nvme";

  // Extract SMART data
  const nvmeHealth = smart?.nvme_smart_health_information_log as
    | Record<string, unknown>
    | undefined;
  const ataAttrs = (
    smart?.ata_smart_attributes as { table?: unknown[] } | undefined
  )?.table as
    | {
        id: number;
        name: string;
        value: number;
        worst: number;
        thresh: number;
        flags?: { string?: string };
        raw?: { value: number; string?: string };
      }[]
    | undefined;

  // Device info from SMART
  const deviceInfo = smart?.device as Record<string, unknown> | undefined;
  const smartHealth = smart?.smart_status as { passed?: boolean } | undefined;

  // Self-test log
  const selfTestLog = smart?.ata_smart_self_test_log as
    | { standard?: { table?: unknown[] } }
    | undefined;
  const nvmeSelfTestLog = smart?.nvme_self_test_log as
    | { table?: unknown[] }
    | undefined;

  const handleTabChange = (_: React.SyntheticEvent, newValue: number) => {
    setTabIndex(newValue);
  };

  return (
    <Dialog
      open={open}
      onClose={onClose}
      maxWidth="md"
      fullWidth
      scroll="paper"
      sx={{
        "& .MuiDialog-container": {
          alignItems: "flex-start",
          paddingTop: "5vh",
        },
      }}
      slotProps={{
        transition: {
          onExited: () => setTabIndex(0),
        },
        paper: {
          sx: {
            backgroundColor:
              theme.palette.mode === "dark"
                ? "rgba(17,25,40,0.95)"
                : "rgba(255,255,255,0.95)",
            backgroundImage:
              theme.palette.mode === "dark"
                ? "linear-gradient(180deg, rgba(255,255,255,0.08) 0%, rgba(255,255,255,0.04) 100%)"
                : "linear-gradient(180deg, rgba(226,232,240,0.72) 0%, rgba(255,255,255,0.95) 100%)",
            backdropFilter: "blur(20px)",
            border:
              theme.palette.mode === "dark"
                ? "1px solid rgba(255,255,255,0.1)"
                : "1px solid rgba(0,0,0,0.1)",
            maxHeight: "85vh",
          },
        },
      }}
    >
      <DialogTitle
        sx={{
          display: "flex",
          alignItems: "center",
          gap: 2,
          pb: 1,
        }}
      >
        <Icon
          icon={isNvme ? "mdi:harddisk" : "mdi:harddisk-plus"}
          width={32}
          color={theme.palette.primary.main}
        />
        <Box flexGrow={1}>
          <Typography variant="h6" fontWeight={600}>
            /dev/{drive.name}
          </Typography>
          <Typography variant="body2" color="text.secondary">
            {drive.model || "Unknown Model"}
          </Typography>
        </Box>
        <Chip
          label={smartHealth?.passed ? "Healthy" : "Unknown"}
          color={smartHealth?.passed ? "success" : "default"}
          size="small"
        />
        <IconButton onClick={onClose} size="small">
          <CloseIcon />
        </IconButton>
      </DialogTitle>

      <Box sx={{ borderBottom: 1, borderColor: "divider", px: 3 }}>
        <Tabs value={tabIndex} onChange={handleTabChange}>
          <Tab label="SMART Attributes" />
          <Tab label="Drive Information" />
          {isNvme && power && <Tab label="Power States" />}
          <Tab label="Self-Tests" />
        </Tabs>
      </Box>

      <DialogContent sx={{ minHeight: 400 }}>
        {/* SMART Attributes Tab */}
        <TabPanel value={tabIndex} index={0}>
          {isNvme && nvmeHealth ? (
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
                  {getSmartNumber(nvmeHealth.critical_warning) !== null && (
                    <TableRow>
                      <TableCell>Critical Warning</TableCell>
                      <TableCell align="right">
                        0x
                        {(getSmartNumber(nvmeHealth.critical_warning) ?? 0)
                          .toString(16)
                          .padStart(2, "0")
                          .toUpperCase()}
                      </TableCell>
                    </TableRow>
                  )}
                  {getSmartNumber(nvmeHealth.temperature) !== null && (
                    <TableRow>
                      <TableCell>Temperature</TableCell>
                      <TableCell
                        align="right"
                        sx={{
                          color:
                            (getSmartNumber(nvmeHealth.temperature) ?? 0) > 70
                              ? "error.main"
                              : (getSmartNumber(nvmeHealth.temperature) ?? 0) >
                                  50
                                ? "warning.main"
                                : "inherit",
                        }}
                      >
                        {getSmartNumber(nvmeHealth.temperature)} Celsius
                      </TableCell>
                    </TableRow>
                  )}
                  {getSmartNumber(nvmeHealth.available_spare) !== null && (
                    <TableRow>
                      <TableCell>Available Spare</TableCell>
                      <TableCell align="right">
                        {getSmartNumber(nvmeHealth.available_spare)}%
                      </TableCell>
                    </TableRow>
                  )}
                  {getSmartNumber(nvmeHealth.available_spare_threshold) !==
                    null && (
                    <TableRow>
                      <TableCell>Available Spare Threshold</TableCell>
                      <TableCell align="right">
                        {getSmartNumber(nvmeHealth.available_spare_threshold)}%
                      </TableCell>
                    </TableRow>
                  )}
                  {getSmartNumber(nvmeHealth.percentage_used) !== null && (
                    <TableRow>
                      <TableCell>Percentage Used</TableCell>
                      <TableCell
                        align="right"
                        sx={{
                          color:
                            (getSmartNumber(nvmeHealth.percentage_used) ?? 0) >
                            90
                              ? "error.main"
                              : (getSmartNumber(nvmeHealth.percentage_used) ??
                                    0) > 70
                                ? "warning.main"
                                : "inherit",
                        }}
                      >
                        {getSmartNumber(nvmeHealth.percentage_used)}%
                      </TableCell>
                    </TableRow>
                  )}
                  {getSmartNumber(nvmeHealth.data_units_read) !== null && (
                    <TableRow>
                      <TableCell>Data Units Read</TableCell>
                      <TableCell align="right">
                        {formatDataUnits(
                          getSmartNumber(nvmeHealth.data_units_read),
                        )}
                      </TableCell>
                    </TableRow>
                  )}
                  {getSmartNumber(nvmeHealth.data_units_written) !== null && (
                    <TableRow>
                      <TableCell>Data Units Written</TableCell>
                      <TableCell align="right">
                        {formatDataUnits(
                          getSmartNumber(nvmeHealth.data_units_written),
                        )}
                      </TableCell>
                    </TableRow>
                  )}
                  {getSmartNumber(nvmeHealth.host_reads) !== null && (
                    <TableRow>
                      <TableCell>Host Read Commands</TableCell>
                      <TableCell align="right">
                        {getSmartNumber(
                          nvmeHealth.host_reads,
                        )?.toLocaleString()}
                      </TableCell>
                    </TableRow>
                  )}
                  {getSmartNumber(nvmeHealth.host_writes) !== null && (
                    <TableRow>
                      <TableCell>Host Write Commands</TableCell>
                      <TableCell align="right">
                        {getSmartNumber(
                          nvmeHealth.host_writes,
                        )?.toLocaleString()}
                      </TableCell>
                    </TableRow>
                  )}
                  {getSmartNumber(nvmeHealth.controller_busy_time) !== null && (
                    <TableRow>
                      <TableCell>Controller Busy Time</TableCell>
                      <TableCell align="right">
                        {getSmartNumber(
                          nvmeHealth.controller_busy_time,
                        )?.toLocaleString()}
                      </TableCell>
                    </TableRow>
                  )}
                  {getSmartNumber(nvmeHealth.power_cycles) !== null && (
                    <TableRow>
                      <TableCell>Power Cycles</TableCell>
                      <TableCell align="right">
                        {getSmartNumber(
                          nvmeHealth.power_cycles,
                        )?.toLocaleString()}
                      </TableCell>
                    </TableRow>
                  )}
                  {getSmartNumber(nvmeHealth.power_on_hours) !== null && (
                    <TableRow>
                      <TableCell>Power On Hours</TableCell>
                      <TableCell align="right">
                        {formatPowerOnTime(
                          getSmartNumber(nvmeHealth.power_on_hours),
                        )}
                      </TableCell>
                    </TableRow>
                  )}
                  {getSmartNumber(nvmeHealth.unsafe_shutdowns) !== null && (
                    <TableRow>
                      <TableCell>Unsafe Shutdowns</TableCell>
                      <TableCell align="right">
                        {getSmartNumber(
                          nvmeHealth.unsafe_shutdowns,
                        )?.toLocaleString()}
                      </TableCell>
                    </TableRow>
                  )}
                  {getSmartNumber(nvmeHealth.media_errors) !== null && (
                    <TableRow>
                      <TableCell>Media and Data Integrity Errors</TableCell>
                      <TableCell
                        align="right"
                        sx={{
                          color:
                            (getSmartNumber(nvmeHealth.media_errors) ?? 0) > 0
                              ? "error.main"
                              : "inherit",
                        }}
                      >
                        {getSmartNumber(
                          nvmeHealth.media_errors,
                        )?.toLocaleString()}
                      </TableCell>
                    </TableRow>
                  )}
                  {getSmartNumber(nvmeHealth.num_err_log_entries) !== null && (
                    <TableRow>
                      <TableCell>Error Information Log Entries</TableCell>
                      <TableCell align="right">
                        {getSmartNumber(
                          nvmeHealth.num_err_log_entries,
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
        <TabPanel value={tabIndex} index={1}>
          <Box sx={{ maxWidth: 600 }}>
            <InfoRow label="Model" value={drive.model || "N/A"} />
            <InfoRow label="Serial Number" value={drive.serial || "N/A"} />
            <InfoRow label="Vendor" value={drive.vendor || "N/A"} />
            <InfoRow
              label="Firmware Version"
              value={getSmartString(smart?.firmware_version) || "N/A"}
            />
            <InfoRow label="Capacity" value={drive.size || "N/A"} />
            <InfoRow
              label="Transport"
              value={drive.type?.toUpperCase() || "N/A"}
            />
            <InfoRow label="Read Only" value={drive.ro ? "Yes" : "No"} />
            {isNvme && (
              <>
                <InfoRow
                  label="NVMe Version"
                  value={getSmartString(smart?.nvme_version) || "N/A"}
                />
                <InfoRow
                  label="Number of Namespaces"
                  value={
                    getSmartNumber(
                      smart?.nvme_number_of_namespaces,
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
          <TabPanel value={tabIndex} index={2}>
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
        <TabPanel value={tabIndex} index={isNvme && power ? 3 : 2}>
          <Box sx={{ mb: 3 }}>
            <Typography variant="subtitle2" gutterBottom>
              Run SMART Self-Test
            </Typography>
            <Box display="flex" gap={2} alignItems="center">
              <Button
                variant="outlined"
                size="small"
                disabled={runningTest !== null}
                onClick={() => handleRunTest("short")}
                startIcon={
                  runningTest === "short" ? (
                    <CircularProgress size={16} />
                  ) : undefined
                }
              >
                {runningTest === "short" ? "Starting..." : "Short Test"}
              </Button>
              <Button
                variant="outlined"
                size="small"
                disabled={runningTest !== null}
                onClick={() => handleRunTest("long")}
                startIcon={
                  runningTest === "long" ? (
                    <CircularProgress size={16} />
                  ) : undefined
                }
              >
                {runningTest === "long" ? "Starting..." : "Extended Test"}
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
      </DialogContent>
    </Dialog>
  );
};

export default DriveDetailsDialog;
