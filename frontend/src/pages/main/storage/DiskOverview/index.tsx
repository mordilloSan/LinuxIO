import { useQueryClient } from "@tanstack/react-query";
import { AnimatePresence, motion } from "framer-motion";
import React, { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";

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

import {
  type ApiDisk,
  type FilesystemInfo,
  jobSnapshotResult,
  linuxio,
  openJobAttachStream,
  type Stream,
} from "@/api";
import DriveCard from "@/components/cards/DriveCard";
import FilesystemCard from "@/components/cards/FilesystemCard";
import PageLoader from "@/components/loaders/PageLoader";
import TabSelector from "@/components/tabbar/TabSelector";
import AppCollapse from "@/components/ui/AppCollapse";
import AppDivider from "@/components/ui/AppDivider";
import AppGrid from "@/components/ui/AppGrid";
import AppTypography from "@/components/ui/AppTypography";
import { useCapability } from "@/hooks/useCapabilities";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useStreamResult } from "@/hooks/useStreamResult";
import { useAppTheme } from "@/theme";
import { getMutationErrorMessage } from "@/utils/mutations";

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
  const toast = useScopedToast({ href: "/storage", label: "Open storage" });
  const [tabIndex, setTabIndex] = useState(0);
  const [startPending, setStartPending] = useState<"short" | "long" | null>(
    null,
  );
  const [testProgress, setTestProgress] =
    useState<SmartTestProgressEvent | null>(null);
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

  // Refresh recovery: if a SMART self-test job for this drive is already running
  // (e.g. user refreshed the page mid-test), find it and re-attach so the UI
  // keeps the buttons disabled and progress bar updating.
  useEffect(() => {
    const deviceName = rawDrive?.name;
    if (!deviceName) return;
    let canceled = false;
    void (async () => {
      try {
        const jobs = await linuxio.jobs.list({ status: "active" });
        if (canceled) return;
        const mine = jobs.find((j) => {
          const request = j.request as
            | { device?: string; testType?: string }
            | undefined;
          return (
            j.type === "storage.run_smart_test" &&
            request?.device === deviceName &&
            (j.state === "running" || j.state === "queued")
          );
        });
        if (!mine) return;
        const request = mine.request as
          | { device?: string; testType?: string }
          | undefined;
        const testType: "short" | "long" =
          request?.testType === "long" ? "long" : "short";
        setStartPending(testType);
        setTestProgress({
          type: "status",
          status: "in_progress",
          test_type: testType,
          device: deviceName,
          message: "Resuming SMART self-test",
        });
        const label = testType === "short" ? "Short" : "Extended";
        void runStreamResult<SmartTestResult, SmartTestProgressEvent>({
          open: () => openJobAttachStream(mine.id),
          onOpen: (stream) => {
            streamRef.current = stream;
          },
          onProgress: (data) => {
            setTestProgress((prev) => ({
              ...(prev || {}),
              ...data,
              test_type: data.test_type ?? prev?.test_type ?? testType,
              device: data.device ?? prev?.device ?? deviceName,
            }));
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
              device: data?.device ?? prev?.device ?? deviceName,
            }));
            if (finalStatus === "completed") {
              toast.success(
                `${label} self-test completed on /dev/${deviceName}`,
              );
            } else if (finalStatus === "aborted") {
              toast.error(`${label} self-test aborted on /dev/${deviceName}`);
            } else {
              const detail = data?.message ? `: ${data.message}` : "";
              toast.error(
                `${label} self-test failed on /dev/${deviceName}${detail}`,
              );
            }
            return;
          })
          .catch((error: unknown) => {
            if (error instanceof Error && error.name === "AbortError") return;
            const errorMessage =
              error instanceof Error ? error.message : "SMART self-test failed";
            toast.error(errorMessage);
          })
          .finally(() => {
            streamRef.current = null;
            setStartPending(null);
          });
      } catch {
        // ignore — refresh recovery is best-effort
      }
    })();
    return () => {
      canceled = true;
    };
  }, [rawDrive?.name, runStreamResult, toast]);

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
        const job = await linuxio.storage.run_smart_test({
          device: rawDrive.name,
          testType,
        });
        jobId = job.id;
      } catch {
        runSmartTest(
          { device: rawDrive.name, testType },
          {
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
          },
        );
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
          const label = testType === "short" ? "Short" : "Extended";
          if (finalStatus === "completed") {
            toast.success(
              `${label} self-test completed on /dev/${rawDrive.name}`,
            );
          } else if (finalStatus === "aborted") {
            toast.error(`${label} self-test aborted on /dev/${rawDrive.name}`);
          } else {
            const detail = data?.message ? `: ${data.message}` : "";
            toast.error(
              `${label} self-test failed on /dev/${rawDrive.name}${detail}`,
            );
          }
          return;
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
  const smartData = rawDrive?.smart;
  const deviceInfo = smartData?.device;
  const smartHealth = smartData?.smart_status;
  const nvmeHealthRaw = smartData?.nvme_smart_health_information_log;
  const selfTestLog = smartData?.ata_smart_self_test_log;
  const nvmeSelfTestLog = smartData?.nvme_self_test_log;
  return (
    <AppCollapse in={expanded} unmountOnExit>
      <div onClick={(e: React.MouseEvent) => e.stopPropagation()}>
        <AppDivider style={{ margin: "16px 0" }} />

        <div
          style={{
            borderBottom: `1px solid ${theme.palette.divider}`,
          }}
        >
          <TabSelector
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
            value={String(tabIndex)}
          />
        </div>

        <TabPanel index={0} value={tabIndex}>
          <OverviewTab drive={drive} />
        </TabPanel>

        <TabPanel index={1} value={tabIndex}>
          <SmartAttributesTab
            ataAttrs={ataAttrs}
            isNvme={isNvme}
            nvmeHealthRaw={nvmeHealthRaw}
          />
        </TabPanel>

        <TabPanel index={2} value={tabIndex}>
          <DriveInfoTab
            deviceInfo={deviceInfo}
            drive={drive}
            rawDriveSize={rawDrive?.size}
            smartData={smartData}
            smartHealth={smartHealth}
          />
        </TabPanel>

        {isNvme && power && (
          <TabPanel index={3} value={tabIndex}>
            <PowerStatesTab power={power} />
          </TabPanel>
        )}

        <TabPanel index={isNvme && power ? 4 : 3} value={tabIndex}>
          <SelfTestsTab
            nvmeSelfTestLog={nvmeSelfTestLog}
            onRunTest={handleRunTest}
            percentage={testProgress?.percentage}
            selfTestLog={selfTestLog}
            smartmontoolsAvailable={smartmontoolsAvailable}
            smartmontoolsReason={smartmontoolsReason}
            startPending={startPending}
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
  const toast = useScopedToast({ href: "/storage", label: "Open storage" });
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
        const subvolumeResult = jobSnapshotResult(result);
        await queryClient.invalidateQueries({
          queryKey: linuxio.system.get_fs_info.queryKey(),
        });
        if (subvolumeResult.path) {
          toast.success(`Created subvolume at ${subvolumeResult.path}`);
        } else {
          toast.success("Subvolume created");
        }
        if (subvolumeResult.mountpoint) {
          setSubvolumeDrafts((prev) => {
            const next = {
              ...prev,
            };
            delete next[subvolumeResult.mountpoint!];
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
        smart: d.smart,
        power: d.power,
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
    unmountFilesystem({ mountpoint });
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
    createBtrfsSubvolume(
      { mountpoint, name },
      {
        onSettled: () => {
          setCreatingSubvolumeMountpoint(null);
        },
      },
    );
  };
  if (drivesLoading || fsLoading) {
    return <PageLoader />;
  }
  return (
    <div>
      {!selectedMountpoint && (
        <>
          <AppTypography
            style={{
              marginBottom: 8,
              fontWeight: 600,
            }}
            variant="h6"
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
                      animate={{ opacity: 1, scale: 1 }}
                      component={motion.div}
                      exit={{ opacity: 0, scale: 0.9 }}
                      initial={{ opacity: 0, scale: 0.95 }}
                      key={drive.name}
                      layout
                      size={{
                        xs: 12,
                        sm: expanded === drive.name ? 12 : 6,
                        md: expanded === drive.name ? 6 : 4,
                        lg: expanded === drive.name ? 4 : 3,
                      }}
                      transition={{ duration: 0.2 }}
                    >
                      <DriveCard
                        expanded={expanded === drive.name}
                        model={drive.model}
                        name={drive.name}
                        onClick={() => handleToggle(drive.name)}
                        sizeBytes={drive.sizeBytes}
                        smart={drive.smart}
                        transport={drive.transport}
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
            style={{
              marginBottom: 8,
              fontWeight: 600,
            }}
            variant="h6"
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
                      animate={{ opacity: 1, scale: 1 }}
                      component={motion.div}
                      exit={{ opacity: 0, scale: 0.9 }}
                      initial={{ opacity: 0, scale: 0.95 }}
                      key={fs.mountpoint}
                      layout
                      size={{
                        xs: 12,
                        sm: selectedMountpoint === fs.mountpoint ? 12 : 6,
                        md: selectedMountpoint === fs.mountpoint ? 8 : 4,
                        lg: selectedMountpoint === fs.mountpoint ? 6 : 4,
                      }}
                      transition={{ duration: 0.2 }}
                    >
                      <FilesystemCard
                        backingDrive={(() => {
                          const bd = findBackingDrive(fs.device, drives);
                          return bd ? { name: bd.name, model: bd.model } : null;
                        })()}
                        filesystem={fs}
                        isCreatingSubvolume={
                          creatingSubvolumeMountpoint === fs.mountpoint &&
                          isCreatingSubvolume
                        }
                        isUnmounting={isUnmounting}
                        nfsMount={
                          nfsMountByMountpoint.get(fs.mountpoint) ?? null
                        }
                        onBrowse={handleBrowseFilesystem}
                        onClick={() => handleFilesystemToggle(fs)}
                        onCreateSubvolume={handleCreateSubvolume}
                        onInspectDrive={handleInspectDrive}
                        onSubvolumeNameChange={handleSubvolumeNameChange}
                        onUnmount={handleUnmountFilesystem}
                        selected={selectedMountpoint === fs.mountpoint}
                        subvolumeName={subvolumeDrafts[fs.mountpoint] ?? ""}
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
