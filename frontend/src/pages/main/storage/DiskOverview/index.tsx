import { Icon } from "@iconify/react";
import {
  Box,
  Chip,
  Collapse,
  Divider,
  Fade,
  Grid,
  LinearProgress,
  Tab,
  Tabs,
  Tooltip,
  Typography,
} from "@mui/material";
import { useTheme } from "@mui/material/styles";
import { AnimatePresence, motion } from "framer-motion";
import React, { useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";

import {
  DriveInfoTab,
  OverviewTab,
  PowerStatesTab,
  SelfTestsTab,
  SmartAttributesTab,
  TabPanel,
} from "./components";
import type {
  DriveInfo,
  SmartTestProgressEvent,
  SmartTestResult,
} from "./types";
import {
  getHealthColor,
  getTemperature,
  getTemperatureColor,
  parseSizeToBytes,
} from "./utils";

import {
  linuxio,
  openSmartTestStream,
  type ResultFrame,
  type Stream,
  type ApiDisk,
} from "@/api";
import FrostedCard from "@/components/cards/RootCard";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import { formatFileSize } from "@/utils/formaters";
import { getMutationErrorMessage } from "@/utils/mutations";

interface DriveDetailsProps {
  drive: DriveInfo;
  expanded: boolean;
  rawDrive: ApiDisk | null;
  refetchDrives: () => void;
}

const DriveDetails: React.FC<DriveDetailsProps> = ({
  drive,
  expanded,
  rawDrive,
  refetchDrives,
}) => {
  const [tabIndex, setTabIndex] = useState(0);
  const [startPending, setStartPending] = useState<"short" | "long" | null>(
    null,
  );
  const [testProgress, setTestProgress] =
    useState<SmartTestProgressEvent | null>(null);
  const streamRef = useRef<Stream | null>(null);

  const { mutate: runSmartTest } = linuxio.storage.run_smart_test.useMutation({
    onSuccess: () => {
      refetchDrives();
    },
    onError: (error: Error) => {
      toast.error(getMutationErrorMessage(error, "Failed to start SMART test"));
    },
  });

  useEffect(() => {
    return () => {
      if (streamRef.current) {
        streamRef.current.close();
        streamRef.current = null;
      }
    };
  }, []);

  const handleRunTest = (testType: "short" | "long") => {
    if (!rawDrive) return;

    setStartPending(testType);
    setTestProgress({
      type: "status",
      status: "starting",
      test_type: testType,
      device: rawDrive.name,
      message: `Starting SMART ${testType} self-test`,
    });

    if (streamRef.current) {
      streamRef.current.close();
    }

    const stream = openSmartTestStream(rawDrive.name, testType);
    if (!stream) {
      runSmartTest([rawDrive.name, testType], {
        onSuccess: () => {
          toast.success(
            `${testType === "short" ? "Short" : "Extended"} self-test started on /dev/${rawDrive.name}`,
          );
          setStartPending(null);
        },
        onError: () => {
          setTestProgress((prev) =>
            prev
              ? { ...prev, status: "error", message: "Failed to start test" }
              : null,
          );
          setStartPending(null);
        },
      });
      return;
    }

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
  const isNvme = drive.transport === "nvme";
  const ataAttrs = smart?.ata_smart_attributes?.table;

  const smartData = rawDrive?.smart as Record<string, unknown> | undefined;
  const deviceInfo = smartData?.device as Record<string, unknown> | undefined;
  const smartHealth = smartData?.smart_status as
    | { passed?: boolean }
    | undefined;
  const nvmeHealthRaw = smartData?.nvme_smart_health_information_log as
    | Record<string, unknown>
    | undefined;
  const selfTestLog = smartData?.ata_smart_self_test_log as
    | { standard?: { table?: unknown[] } }
    | undefined;
  const nvmeSelfTestLog = smartData?.nvme_self_test_log as
    | { table?: unknown[] }
    | undefined;

  if (!expanded) return null;

  return (
    <Collapse in={expanded} timeout="auto" unmountOnExit>
      <Box onClick={(e) => e.stopPropagation()}>
        <Divider sx={{ my: 2 }} />

        <Box sx={{ borderBottom: 1, borderColor: "divider" }}>
          <Tabs
            value={tabIndex}
            onChange={handleTabChange}
            variant="scrollable"
            scrollButtons="auto"
            sx={{
              "& .MuiTabs-scroller": {
                "&::-webkit-scrollbar": {
                  height: 8,
                },
                "&::-webkit-scrollbar-thumb": {
                  backgroundColor: "rgba(100, 100, 100, 0.2)",
                  borderRadius: 8,
                  border: "2px solid transparent",
                  backgroundClip: "content-box",
                },
                "&::-webkit-scrollbar-track": {
                  background: "transparent",
                  borderRadius: 8,
                },
                "&::-webkit-scrollbar-thumb:hover": {
                  backgroundColor: "rgba(100, 100, 100, 0.45)",
                },
              },
            }}
          >
            <Tab label="Overview" />
            <Tab label="SMART Attributes" />
            <Tab label="Drive Information" />
            {isNvme && power && <Tab label="Power States" />}
            <Tab label="Self-Tests" />
          </Tabs>
        </Box>

        <TabPanel value={tabIndex} index={0}>
          <OverviewTab drive={drive} />
        </TabPanel>

        <TabPanel value={tabIndex} index={1}>
          <SmartAttributesTab
            isNvme={isNvme}
            nvmeHealthRaw={nvmeHealthRaw}
            ataAttrs={ataAttrs}
          />
        </TabPanel>

        <TabPanel value={tabIndex} index={2}>
          <DriveInfoTab
            drive={drive}
            rawDriveSize={rawDrive?.size}
            smartData={smartData}
            deviceInfo={deviceInfo}
            smartHealth={smartHealth}
          />
        </TabPanel>

        {isNvme && power && (
          <TabPanel value={tabIndex} index={3}>
            <PowerStatesTab power={power} />
          </TabPanel>
        )}

        <TabPanel value={tabIndex} index={isNvme && power ? 4 : 3}>
          <SelfTestsTab
            startPending={startPending}
            onRunTest={handleRunTest}
            selfTestLog={selfTestLog}
            nvmeSelfTestLog={nvmeSelfTestLog}
          />
        </TabPanel>
      </Box>
    </Collapse>
  );
};

const DiskOverview: React.FC = () => {
  const theme = useTheme();
  const [expanded, setExpanded] = useState<string | null>(null);

  const {
    data: rawDrives = [],
    isPending: drivesLoading,
    refetch: refetchDrives,
  } = linuxio.storage.get_drive_info.useQuery({ refetchInterval: 30000 });

  const { data: filesystems = [], isPending: fsLoading } =
    linuxio.system.get_fs_info.useQuery({ refetchInterval: 10000 });

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
                      refetchDrives={refetchDrives}
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
