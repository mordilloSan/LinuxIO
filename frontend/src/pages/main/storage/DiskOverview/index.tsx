import { Icon } from "@iconify/react";
import {
  Button,
  Collapse,
  Divider,
  Fade,
  Grid,
  LinearProgress,
  Tab,
  Tabs,
  TextField,
  Tooltip,
  Typography,
} from "@mui/material";
import { alpha, useTheme } from "@mui/material/styles";
import { useQueryClient } from "@tanstack/react-query";
import { AnimatePresence, motion } from "framer-motion";
import React, { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
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
  type Stream,
  type ApiDisk,
  type NFSMount,
} from "@/api";
import Chip from "@/components/ui/AppChip";
import FrostedCard from "@/components/cards/RootCard";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import { useCapability } from "@/hooks/useCapabilities";
import { useStreamResult } from "@/hooks/useStreamResult";
import { FilesystemInfo } from "@/types/fs";
import { formatFileSize } from "@/utils/formaters";
import { getMutationErrorMessage } from "@/utils/mutations";

interface DriveDetailsProps {
  drive: DriveInfo;
  expanded: boolean;
  rawDrive: ApiDisk | null;
  refetchDrives: () => void;
  smartmontoolsAvailable: boolean;
  smartmontoolsReason: string;
}

interface FilesystemCardDetailsProps {
  filesystem: FilesystemInfo;
  backingDrive: DriveInfo | null;
  nfsMount: NFSMount | null;
  isUnmounting: boolean;
  isCreatingSubvolume: boolean;
  subvolumeName: string;
  onBrowse: (mountpoint: string) => void;
  onInspectDrive: (driveName: string) => void;
  onUnmount: (mountpoint: string) => void;
  onSubvolumeNameChange: (mountpoint: string, value: string) => void;
  onCreateSubvolume: (mountpoint: string) => void;
}

const SYSTEM_MOUNTPOINTS = new Set(["/", "/boot", "/boot/efi"]);

const encodeFilebrowserPath = (path: string): string => {
  if (path === "/") {
    return "/filebrowser";
  }

  const encodedSegments = path
    .split("/")
    .filter(Boolean)
    .map((segment) => encodeURIComponent(segment));

  return `/filebrowser/${encodedSegments.join("/")}`;
};

const normalizeDeviceName = (device: string): string =>
  device.replace(/^\/dev\//, "");

const findBackingDrive = (
  device: string,
  drives: DriveInfo[],
): DriveInfo | null => {
  const normalizedDevice = normalizeDeviceName(device);

  return (
    drives.find((drive) => {
      if (normalizedDevice === drive.name) {
        return true;
      }

      if (normalizedDevice.startsWith(`${drive.name}p`)) {
        return true;
      }

      const suffix = normalizedDevice.slice(drive.name.length);
      return (
        suffix.length > 0 &&
        normalizedDevice.startsWith(drive.name) &&
        /^\d+$/.test(suffix)
      );
    }) ?? null
  );
};

const canUnmountFilesystem = (filesystem: FilesystemInfo): boolean =>
  !SYSTEM_MOUNTPOINTS.has(filesystem.mountpoint);

const canCreateSubvolume = (filesystem: FilesystemInfo): boolean =>
  filesystem.fstype === "btrfs" && !filesystem.readOnly;

const FilesystemCardDetails: React.FC<FilesystemCardDetailsProps> = ({
  filesystem,
  backingDrive,
  nfsMount,
  isUnmounting,
  isCreatingSubvolume,
  subvolumeName,
  onBrowse,
  onInspectDrive,
  onUnmount,
  onSubvolumeNameChange,
  onCreateSubvolume,
}) => {
  const isSystemMount = SYSTEM_MOUNTPOINTS.has(filesystem.mountpoint);
  const isNfs = filesystem.fstype === "nfs" || filesystem.fstype === "nfs4";

  return (
    <Collapse in timeout="auto" unmountOnExit>
      <div onClick={(e: React.MouseEvent) => e.stopPropagation()}>
        <Divider sx={{ my: 2 }} />

        <div
          style={{
            display: "flex",
            flexWrap: "wrap",
            gap: 8,
            marginBottom: 16,
          }}
        >
          <Chip label={filesystem.fstype.toUpperCase()} size="small" variant="soft" />
          {filesystem.readOnly && (
            <Chip label="Read-only" size="small" color="warning" variant="soft" />
          )}
          {nfsMount && <Chip label="NFS mount" size="small" color="info" variant="soft" />}
          {isSystemMount && (
            <Chip label="System mount" size="small" color="default" variant="soft" />
          )}
        </div>

        <div
          style={{
            display: "grid",
            gap: 12,
            marginBottom: 16,
          }}
        >
          <div>
            <Typography variant="body2" color="text.secondary">
              Source
            </Typography>
            <Typography variant="body1">
              {nfsMount?.source || filesystem.device || "Unknown"}
            </Typography>
          </div>

          <div>
            <Typography variant="body2" color="text.secondary">
              Usage
            </Typography>
            <Typography variant="body1">
              {formatFileSize(filesystem.used)} used of{" "}
              {formatFileSize(filesystem.total)} (
              {filesystem.usedPercent.toFixed(1)}
              %)
            </Typography>
            <Typography variant="body2" color="text.secondary">
              {formatFileSize(filesystem.free)} free
            </Typography>
          </div>

          {typeof filesystem.inodesUsedPercent === "number" &&
            filesystem.inodesTotal &&
            filesystem.inodesTotal > 0 && (
              <div>
                <Typography variant="body2" color="text.secondary">
                  Inodes
                </Typography>
                <Typography variant="body1">
                  {(filesystem.inodesUsed ?? 0).toLocaleString()} used of{" "}
                  {filesystem.inodesTotal.toLocaleString()} (
                  {filesystem.inodesUsedPercent.toFixed(1)}%)
                </Typography>
              </div>
            )}

          {nfsMount && (
            <>
              <div>
                <Typography variant="body2" color="text.secondary">
                  Export
                </Typography>
                <Typography variant="body1">
                  {nfsMount.server}:{nfsMount.exportPath}
                </Typography>
              </div>

              <div>
                <Typography variant="body2" color="text.secondary">
                  Mount options
                </Typography>
                <Typography variant="body1">
                  {nfsMount.options.length > 0
                    ? nfsMount.options.join(", ")
                    : "Default options"}
                </Typography>
                <Typography variant="body2" color="text.secondary">
                  {nfsMount.inFstab
                    ? "Configured to mount at boot"
                    : "Not persisted in /etc/fstab"}
                </Typography>
              </div>
            </>
          )}

          <Typography variant="body2" color="text.secondary">
            {nfsMount
              ? "This filesystem supports direct unmount here because NFS management already exists in the backend."
              : isNfs
                ? "This looks like an NFS filesystem, but no matching NFS mount entry was loaded for direct actions."
                : canCreateSubvolume(filesystem)
                  ? "This btrfs filesystem can create subvolumes directly from the card."
                  : canUnmountFilesystem(filesystem)
                    ? "This filesystem can be unmounted directly from the card."
                    : "Protected system mounts stay visible here but do not expose unmount actions."}
          </Typography>

          {backingDrive && (
            <Typography variant="body2" color="text.secondary">
              Backing drive: /dev/{backingDrive.name}
              {backingDrive.model ? ` (${backingDrive.model})` : ""}
            </Typography>
          )}
        </div>

        <div
          style={{
            display: "flex",
            flexWrap: "wrap",
            gap: 8,
            alignItems: "center",
          }}
        >
          <Button
            variant="outlined"
            onClick={() => onBrowse(filesystem.mountpoint)}
          >
            Browse
          </Button>
          {backingDrive && (
            <Button
              variant="outlined"
              onClick={() => onInspectDrive(backingDrive.name)}
            >
              Inspect Drive
            </Button>
          )}
          {canUnmountFilesystem(filesystem) && (
            <Button
              color="error"
              variant="outlined"
              onClick={() => onUnmount(filesystem.mountpoint)}
              disabled={isUnmounting}
            >
              {isUnmounting ? "Unmounting..." : "Unmount"}
            </Button>
          )}
        </div>

        {canCreateSubvolume(filesystem) && (
          <div
            style={{
              display: "flex",
              flexWrap: "wrap",
              gap: 8,
              alignItems: "center",
              marginTop: 12,
            }}
          >
            <TextField
              size="small"
              label="Subvolume name"
              value={subvolumeName}
              onChange={(event) =>
                onSubvolumeNameChange(filesystem.mountpoint, event.target.value)
              }
              placeholder="@data"
              sx={{ minWidth: 220, flex: "1 1 220px" }}
              onClick={(event) => event.stopPropagation()}
            />
            <Button
              variant="outlined"
              onClick={() => onCreateSubvolume(filesystem.mountpoint)}
              disabled={
                isCreatingSubvolume || subvolumeName.trim().length === 0
              }
            >
              {isCreatingSubvolume ? "Creating..." : "Create subvolume"}
            </Button>
          </div>
        )}
      </div>
    </Collapse>
  );
};

const DriveDetails: React.FC<DriveDetailsProps> = ({
  drive,
  expanded,
  rawDrive,
  refetchDrives,
  smartmontoolsAvailable,
  smartmontoolsReason,
}) => {
  const theme = useTheme();
  const [tabIndex, setTabIndex] = useState(0);
  const [startPending, setStartPending] = useState<"short" | "long" | null>(
    null,
  );
  const [, setTestProgress] = useState<SmartTestProgressEvent | null>(null);
  const streamRef = useRef<Stream | null>(null);
  const { run: runStreamResult } = useStreamResult();

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
    if (!smartmontoolsAvailable) {
      toast.error(smartmontoolsReason);
      return;
    }

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

    void runStreamResult<SmartTestResult, SmartTestProgressEvent>({
      open: () => stream,
      onProgress: (data) => {
        setTestProgress((prev) => ({
          ...(prev || {}),
          ...data,
          test_type: data.test_type ?? prev?.test_type ?? testType,
          device: data.device ?? prev?.device ?? rawDrive.name,
        }));
        if (data.status && data.status !== "starting") {
          setStartPending(null);
        }
      },
      closeMessage: "SMART self-test stream closed unexpectedly",
    })
      .then((data) => {
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
      })
      .catch((error: unknown) => {
        if (error instanceof Error && error.name === "AbortError") {
          return;
        }

        const errorMessage =
          error instanceof Error ? error.message : "SMART self-test failed";
        setTestProgress((prev) => ({
          ...(prev || {}),
          type: "status",
          status: "error",
          message: errorMessage,
          test_type: prev?.test_type ?? testType,
          device: prev?.device ?? rawDrive.name,
        }));
        toast.error(errorMessage);
      })
      .finally(() => {
        streamRef.current = null;
        setStartPending(null);
      });
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
      <div onClick={(e: React.MouseEvent) => e.stopPropagation()}>
        <Divider sx={{ my: 2 }} />

        <div style={{ borderBottom: `1px solid ${theme.palette.divider}` }}>
          <Tabs
            value={tabIndex}
            onChange={handleTabChange}
            variant="scrollable"
            scrollButtons="auto"
            sx={{
              "& .MuiTab-root": {
                minWidth: "auto",
                px: 1.5,
              },
              "& .MuiTabs-scroller": {
                "&::-webkit-scrollbar": {
                  height: 8,
                },
                "&::-webkit-scrollbar-thumb": {
                  backgroundColor: alpha(theme.palette.text.secondary, 0.2),
                  borderRadius: 8,
                  border: "2px solid transparent",
                  backgroundClip: "content-box",
                },
                "&::-webkit-scrollbar-track": {
                  background: "transparent",
                  borderRadius: 8,
                },
                "&::-webkit-scrollbar-thumb:hover": {
                  backgroundColor: alpha(theme.palette.text.secondary, 0.45),
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
        </div>

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
            smartmontoolsAvailable={smartmontoolsAvailable}
            smartmontoolsReason={smartmontoolsReason}
          />
        </TabPanel>
      </div>
    </Collapse>
  );
};

const DiskOverview: React.FC = () => {
  const theme = useTheme();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const queryClient = useQueryClient();
  const expanded = searchParams.get("drive");
  const selectedMountpoint = searchParams.get("fs");
  const [creatingSubvolumeMountpoint, setCreatingSubvolumeMountpoint] =
    useState<string | null>(null);
  const [subvolumeDrafts, setSubvolumeDrafts] = useState<
    Record<string, string>
  >({});
  const { isEnabled: smartmontoolsAvailable, reason: smartmontoolsReason } =
    useCapability("smartmontoolsAvailable");

  const {
    data: rawDrives = [],
    isPending: drivesLoading,
    refetch: refetchDrives,
  } = linuxio.storage.get_drive_info.useQuery({ refetchInterval: 30000 });

  const { data: filesystems = [], isPending: fsLoading } =
    linuxio.system.get_fs_info.useQuery({ refetchInterval: 10000 });
  const { data: nfsMounts = [] } = linuxio.storage.list_nfs_mounts.useQuery({
    refetchInterval: 10000,
  });

  const { mutate: unmountFilesystem, isPending: isUnmounting } =
    linuxio.storage.unmount_filesystem.useMutation({
      onSuccess: async () => {
        await Promise.all([
          queryClient.invalidateQueries({
            queryKey: linuxio.storage.list_nfs_mounts.queryKey(),
          }),
          queryClient.invalidateQueries({
            queryKey: linuxio.system.get_fs_info.queryKey(),
          }),
        ]);

        toast.success("Filesystem unmounted");
        setSearchParams((prev) => {
          prev.delete("fs");
          return prev;
        });
      },
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to unmount filesystem"),
        );
      },
    });

  const { mutate: createBtrfsSubvolume, isPending: isCreatingSubvolume } =
    linuxio.storage.create_btrfs_subvolume.useMutation({
      onSuccess: async (result) => {
        await queryClient.invalidateQueries({
          queryKey: linuxio.system.get_fs_info.queryKey(),
        });

        if (result.path) {
          toast.success(`Created subvolume at ${result.path}`);
        } else {
          toast.success("Subvolume created");
        }

        if (result.mountpoint) {
          setSubvolumeDrafts((prev) => {
            const next = { ...prev };
            delete next[result.mountpoint!];
            return next;
          });
        }
      },
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to create btrfs subvolume"),
        );
      },
    });

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        setSearchParams((prev) => {
          prev.delete("drive");
          prev.delete("fs");
          return prev;
        });
        setCreatingSubvolumeMountpoint(null);
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [setSearchParams]);

  const handleToggle = (driveName: string) => {
    setSearchParams((prev) => {
      if (prev.get("drive") === driveName) {
        prev.delete("drive");
      } else {
        prev.set("drive", driveName);
      }
      return prev;
    });
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

  const nfsMountByMountpoint = useMemo(
    () => new Map(nfsMounts.map((mount) => [mount.mountpoint, mount])),
    [nfsMounts],
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

  const handleFilesystemToggle = (filesystem: FilesystemInfo) => {
    setCreatingSubvolumeMountpoint(null);
    setSearchParams((prev) => {
      if (prev.get("fs") === filesystem.mountpoint) {
        prev.delete("fs");
      } else {
        prev.set("fs", filesystem.mountpoint);
      }
      return prev;
    });
  };

  const handleBrowseFilesystem = (mountpoint: string) => {
    navigate(encodeFilebrowserPath(mountpoint));
  };

  const handleInspectDrive = (driveName: string) => {
    setSearchParams((prev) => {
      prev.set("drive", driveName);
      prev.delete("fs");
      return prev;
    });
    setCreatingSubvolumeMountpoint(null);
  };

  const handleUnmountFilesystem = (mountpoint: string) => {
    unmountFilesystem([mountpoint]);
  };

  const handleSubvolumeNameChange = (mountpoint: string, value: string) => {
    setSubvolumeDrafts((prev) => ({
      ...prev,
      [mountpoint]: value,
    }));
  };

  const handleCreateSubvolume = (mountpoint: string) => {
    const name = (subvolumeDrafts[mountpoint] ?? "").trim();
    if (!name) {
      toast.error("Subvolume name is required");
      return;
    }

    setCreatingSubvolumeMountpoint(mountpoint);
    createBtrfsSubvolume([mountpoint, name], {
      onSettled: () => {
        setCreatingSubvolumeMountpoint(null);
      },
    });
  };

  if (drivesLoading || fsLoading) {
    return <ComponentLoader />;
  }

  return (
    <div>
      {!selectedMountpoint && (
        <>
          <Typography variant="h6" sx={{ mb: 2, fontWeight: 600 }}>
            Physical Drives
          </Typography>
          <Grid container spacing={3} sx={{ mb: 4 }}>
            <AnimatePresence>
              {drives.length === 0 ? (
                <Grid size={{ xs: 12 }}>
                  <Typography color="text.secondary">
                    No drives found.
                  </Typography>
                </Grid>
              ) : (
                drives.map((drive) =>
                  expanded && expanded !== drive.name ? null : (
                    <Grid
                      key={drive.name}
                      size={{
                        xs: 12,
                        sm: expanded === drive.name ? 12 : 6,
                        md: expanded === drive.name ? 6 : 4,
                        lg: expanded === drive.name ? 4 : 3,
                      }}
                      component={motion.div}
                      layout
                      initial={{ opacity: 0, scale: 0.95 }}
                      animate={{ opacity: 1, scale: 1 }}
                      exit={{ opacity: 0, scale: 0.9 }}
                      transition={{ duration: 0.2 }}
                    >
                      <FrostedCard
                        hoverLift={expanded !== drive.name}
                        style={{
                          padding: 8,
                          position: "relative",
                          cursor: "pointer",
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
                            <div
                              className="fc-opacity-hover"
                              style={{
                                position: "absolute",
                                top: 8,
                                right: 8,
                                cursor: "pointer",
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
                            </div>
                          </Tooltip>
                        ) : getTemperature(drive.smart) !== null ? (
                          <Tooltip
                            title="Drive Temperature"
                            placement="top"
                            arrow
                            slots={{ transition: Fade }}
                            slotProps={{ transition: { timeout: 300 } }}
                          >
                            <div
                              style={{
                                position: "absolute",
                                top: 12,
                                right: 12,
                                display: "flex",
                                alignItems: "center",
                                gap: 2,
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
                            </div>
                          </Tooltip>
                        ) : null}
                        <div
                          style={{
                            display: "flex",
                            alignItems: "center",
                            marginBottom: 6,
                          }}
                        >
                          <Icon
                            icon={
                              drive.transport === "nvme"
                                ? "mdi:harddisk"
                                : "mdi:harddisk-plus"
                            }
                            width={32}
                            color={theme.palette.primary.main}
                          />
                          <div
                            style={{ marginLeft: 6, flexGrow: 1, minWidth: 0 }}
                          >
                            <Typography
                              variant="subtitle1"
                              fontWeight={600}
                              noWrap
                            >
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
                          </div>
                        </div>
                        <div
                          style={{ display: "flex", gap: 4, flexWrap: "wrap" }}
                        >
                          <Chip
                            label={formatFileSize(drive.sizeBytes)}
                            size="small"
                            color="primary"
                            variant="soft"
                          />
                          <Chip
                            label={drive.transport.toUpperCase()}
                            size="small"
                            variant="soft"
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
                              variant="soft"
                            />
                          )}
                        </div>
                        <DriveDetails
                          drive={drive}
                          expanded={expanded === drive.name}
                          rawDrive={
                            rawDrives.find((d) => d.name === drive.name) || null
                          }
                          refetchDrives={refetchDrives}
                          smartmontoolsAvailable={smartmontoolsAvailable}
                          smartmontoolsReason={smartmontoolsReason}
                        />
                      </FrostedCard>
                    </Grid>
                  ),
                )
              )}
            </AnimatePresence>
          </Grid>
        </>
      )}

      {!expanded && (
        <>
          <Typography variant="h6" sx={{ mb: 2, fontWeight: 600 }}>
            Filesystems
          </Typography>
          <Grid container spacing={3}>
            <AnimatePresence>
              {relevantFS.length === 0 ? (
                <Grid size={{ xs: 12 }}>
                  <Typography color="text.secondary">
                    No filesystems found.
                  </Typography>
                </Grid>
              ) : (
                relevantFS.map((fs) =>
                  selectedMountpoint &&
                  selectedMountpoint !== fs.mountpoint ? null : (
                    <Grid
                      key={fs.mountpoint}
                      size={{
                        xs: 12,
                        sm: selectedMountpoint === fs.mountpoint ? 12 : 6,
                        md: selectedMountpoint === fs.mountpoint ? 8 : 4,
                        lg: selectedMountpoint === fs.mountpoint ? 6 : 4,
                      }}
                      component={motion.div}
                      layout
                      initial={{ opacity: 0, scale: 0.95 }}
                      animate={{ opacity: 1, scale: 1 }}
                      exit={{ opacity: 0, scale: 0.9 }}
                      transition={{ duration: 0.2 }}
                    >
                      <FrostedCard
                        hoverLift={selectedMountpoint !== fs.mountpoint}
                        style={{ padding: 8, cursor: "pointer" }}
                        onClick={() => handleFilesystemToggle(fs)}
                        onKeyDown={(event) => {
                          if (event.key === "Enter" || event.key === " ") {
                            event.preventDefault();
                            handleFilesystemToggle(fs);
                          }
                        }}
                        role="button"
                        tabIndex={0}
                        aria-label={`Toggle details for ${fs.mountpoint}`}
                      >
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
                          {formatFileSize(fs.used)} / {formatFileSize(fs.total)}{" "}
                          ({fs.usedPercent.toFixed(1)}%)
                        </Typography>
                        {selectedMountpoint === fs.mountpoint && (
                          <FilesystemCardDetails
                            filesystem={fs}
                            backingDrive={findBackingDrive(fs.device, drives)}
                            nfsMount={
                              nfsMountByMountpoint.get(fs.mountpoint) ?? null
                            }
                            isUnmounting={isUnmounting}
                            isCreatingSubvolume={
                              creatingSubvolumeMountpoint === fs.mountpoint &&
                              isCreatingSubvolume
                            }
                            subvolumeName={subvolumeDrafts[fs.mountpoint] ?? ""}
                            onBrowse={handleBrowseFilesystem}
                            onInspectDrive={handleInspectDrive}
                            onUnmount={handleUnmountFilesystem}
                            onSubvolumeNameChange={handleSubvolumeNameChange}
                            onCreateSubvolume={handleCreateSubvolume}
                          />
                        )}
                      </FrostedCard>
                    </Grid>
                  ),
                )
              )}
            </AnimatePresence>
          </Grid>
        </>
      )}
    </div>
  );
};

export default DiskOverview;
