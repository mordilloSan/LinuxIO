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
import { parseSizeToBytes } from "./utils";

import { linuxio, openJobAttachStream, type Stream, type ApiDisk } from "@/api";
import DriveCard from "@/components/cards/DriveCard";
import FilesystemCard from "@/components/cards/FilesystemCard";
import PageLoader from "@/components/loaders/PageLoader";
import TabSelector from "@/components/tabbar/TabSelector";
import AppCollapse from "@/components/ui/AppCollapse";
import AppDivider from "@/components/ui/AppDivider";
import AppGrid from "@/components/ui/AppGrid";
import AppTypography from "@/components/ui/AppTypography";
import { useCapability } from "@/hooks/useCapabilities";
import { useStreamResult } from "@/hooks/useStreamResult";
import { useAppTheme } from "@/theme";
import { FilesystemInfo } from "@/types/fs";
import { getMutationErrorMessage } from "@/utils/mutations";

const JOB_TYPE_STORAGE_SMART_TEST = "storage.smart_test";

interface DriveDetailsProps {
  drive: DriveInfo;
  expanded: boolean;
  rawDrive: ApiDisk | null;
  refetchDrives: () => void;
  smartmontoolsAvailable: boolean;
  smartmontoolsReason: string;
}

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

const DriveDetails: React.FC<DriveDetailsProps> = ({
  drive,
  expanded,
  rawDrive,
  refetchDrives,
  smartmontoolsAvailable,
  smartmontoolsReason,
}) => {
  const theme = useAppTheme();
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
    void (async () => {
      let jobId: string | null = null;
      try {
        const job = await linuxio.jobs.start.call(
          JOB_TYPE_STORAGE_SMART_TEST,
          rawDrive.name,
          testType,
        );
        jobId = job.id;
      } catch {
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
                ? {
                    ...prev,
                    status: "error",
                    message: "Failed to start test",
                  }
                : null,
            );
            setStartPending(null);
          },
        });
        return;
      }

      void runStreamResult<SmartTestResult, SmartTestProgressEvent>({
        open: () => (jobId ? openJobAttachStream(jobId) : null),
        onOpen: (stream) => {
          streamRef.current = stream;
        },
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
    })();
  };
  const handleTabChange = (newValue: number) => {
    setTabIndex(newValue);
  };
  const smart = drive.smart;
  const power = drive.power;
  const isNvme = drive.transport === "nvme";
  const ataAttrs = smart?.ata_smart_attributes?.table;
  const smartData = rawDrive?.smart as Record<string, unknown> | undefined;
  const deviceInfo = smartData?.device as Record<string, unknown> | undefined;
  const smartHealth = smartData?.smart_status as
    | {
        passed?: boolean;
      }
    | undefined;
  const nvmeHealthRaw = smartData?.nvme_smart_health_information_log as
    | Record<string, unknown>
    | undefined;
  const selfTestLog = smartData?.ata_smart_self_test_log as
    | {
        standard?: {
          table?: unknown[];
        };
      }
    | undefined;
  const nvmeSelfTestLog = smartData?.nvme_self_test_log as
    | {
        table?: unknown[];
      }
    | undefined;
  if (!expanded) return null;
  return (
    <AppCollapse in={expanded} timeout="auto" unmountOnExit>
      <div onClick={(e: React.MouseEvent) => e.stopPropagation()}>
        <AppDivider style={{ margin: "16px 0" }} />

        <div
          style={{
            borderBottom: `1px solid ${theme.palette.divider}`,
          }}
        >
          <TabSelector
            value={String(tabIndex)}
            onChange={(nextValue) => handleTabChange(Number(nextValue))}
            options={[
              { value: "0", label: "Overview" },
              { value: "1", label: "SMART Attributes" },
              { value: "2", label: "Drive Information" },
              ...(isNvme && power
                ? [{ value: "3", label: "Power States" }]
                : []),
              {
                value: isNvme && power ? "4" : "3",
                label: "Self-Tests",
              },
            ]}
            style={{ marginBottom: 0 }}
          />
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
    </AppCollapse>
  );
};

const DiskOverview: React.FC = () => {
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
    data: rawDrivesData,
    isPending: drivesLoading,
    refetch: refetchDrives,
  } = linuxio.storage.get_drive_info.useQuery({
    refetchInterval: 30000,
  });
  const { data: filesystemsData, isPending: fsLoading } =
    linuxio.system.get_fs_info.useQuery({
      refetchInterval: 10000,
    });
  const { data: nfsMountsData } = linuxio.storage.list_nfs_mounts.useQuery({
    refetchInterval: 10000,
  });
  const rawDrives = useMemo(
    () => (Array.isArray(rawDrivesData) ? rawDrivesData : []),
    [rawDrivesData],
  );
  const filesystems = useMemo(
    () => (Array.isArray(filesystemsData) ? filesystemsData : []),
    [filesystemsData],
  );
  const nfsMounts = useMemo(
    () => (Array.isArray(nfsMountsData) ? nfsMountsData : []),
    [nfsMountsData],
  );
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
            const next = {
              ...prev,
            };
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
    return <PageLoader />;
  }
  return (
    <div>
      {!selectedMountpoint && (
        <>
          <AppTypography
            variant="h6"
            style={{
              marginBottom: 8,
              fontWeight: 600,
            }}
          >
            Physical Drives
          </AppTypography>
          <AppGrid
            container
            spacing={3}
            style={{
              marginBottom: 16,
            }}
          >
            <AnimatePresence>
              {drives.length === 0 ? (
                <AppGrid
                  size={{
                    xs: 12,
                  }}
                >
                  <AppTypography color="text.secondary">
                    No drives found.
                  </AppTypography>
                </AppGrid>
              ) : (
                drives.map((drive) =>
                  expanded && expanded !== drive.name ? null : (
                    <AppGrid
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
                      <DriveCard
                        name={drive.name}
                        model={drive.model}
                        transport={drive.transport}
                        sizeBytes={drive.sizeBytes}
                        smart={drive.smart}
                        expanded={expanded === drive.name}
                        onClick={() => handleToggle(drive.name)}
                      >
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
                      </DriveCard>
                    </AppGrid>
                  ),
                )
              )}
            </AnimatePresence>
          </AppGrid>
        </>
      )}

      {!expanded && (
        <>
          <AppTypography
            variant="h6"
            style={{
              marginBottom: 8,
              fontWeight: 600,
            }}
          >
            Filesystems
          </AppTypography>
          <AppGrid container spacing={3}>
            <AnimatePresence>
              {relevantFS.length === 0 ? (
                <AppGrid
                  size={{
                    xs: 12,
                  }}
                >
                  <AppTypography color="text.secondary">
                    No filesystems found.
                  </AppTypography>
                </AppGrid>
              ) : (
                relevantFS.map((fs) =>
                  selectedMountpoint &&
                  selectedMountpoint !== fs.mountpoint ? null : (
                    <AppGrid
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
                      <FilesystemCard
                        filesystem={fs}
                        selected={selectedMountpoint === fs.mountpoint}
                        backingDrive={(() => {
                          const bd = findBackingDrive(fs.device, drives);
                          return bd ? { name: bd.name, model: bd.model } : null;
                        })()}
                        nfsMount={
                          nfsMountByMountpoint.get(fs.mountpoint) ?? null
                        }
                        isUnmounting={isUnmounting}
                        isCreatingSubvolume={
                          creatingSubvolumeMountpoint === fs.mountpoint &&
                          isCreatingSubvolume
                        }
                        subvolumeName={subvolumeDrafts[fs.mountpoint] ?? ""}
                        onClick={() => handleFilesystemToggle(fs)}
                        onBrowse={handleBrowseFilesystem}
                        onInspectDrive={handleInspectDrive}
                        onUnmount={handleUnmountFilesystem}
                        onSubvolumeNameChange={handleSubvolumeNameChange}
                        onCreateSubvolume={handleCreateSubvolume}
                      />
                    </AppGrid>
                  ),
                )
              )}
            </AnimatePresence>
          </AppGrid>
        </>
      )}
    </div>
  );
};

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

export default DiskOverview;
