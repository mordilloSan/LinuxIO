import { useSuspenseQueries } from "@tanstack/react-query";
import { getRouteApi } from "@tanstack/react-router";
import { AnimatePresence, motion } from "motion/react";
import { useEffect, useMemo, useRef, useState } from "react";

import { type ApiDisk, type FilesystemInfo, linuxio, type Stream } from "@/api";
import DriveCard from "@/components/cards/DriveCard";
import FilesystemCard from "@/components/cards/FilesystemCard";
import TabSelector from "@/components/tabbar/TabSelector";
import AppCollapse from "@/components/ui/AppCollapse";
import AppDivider from "@/components/ui/AppDivider";
import AppGrid from "@/components/ui/AppGrid";
import AppTypography from "@/components/ui/AppTypography";
import { JOB_TYPE_STORAGE_SMART_TEST } from "@/constants/backgroundJobTypes";
import { useActiveJobRecovery } from "@/hooks/backgroundJobs/useActiveJobRecovery";
import { useCapability } from "@/hooks/useCapabilities";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useAppTheme } from "@/theme";

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
  SmartData,
  SmartTestProgressEvent,
  SmartTestResult,
} from "./types";
import { parseSizeToBytes } from "./utils";

const storageRouteApi = getRouteApi("/_authenticated/storage/");
const STORAGE_TOAST_META = {
  label: "Open storage",
  to: "/storage",
} as const;

interface DriveDetailsProps {
  drive: DriveInfo;
  expanded: boolean;
  rawDrive: ApiDisk | null;
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

const DriveDetails = ({
  drive,
  expanded,
  rawDrive,
  smartmontoolsAvailable,
  smartmontoolsReason,
}: DriveDetailsProps) => {
  const theme = useAppTheme();
  const toast = useScopedToast(STORAGE_TOAST_META);
  const [tabIndex, setTabIndex] = useState(0);
  const [startPending, setStartPending] = useState<"short" | "long" | null>(
    null,
  );
  const [testProgress, setTestProgress] =
    useState<SmartTestProgressEvent | null>(null);
  const streamRef = useRef<Stream | null>(null);
  // One config drives both lifecycles: fresh runs via smartTest.mutate() and
  // page-reload recovery via smartTest.attach() (see useActiveJobRecovery
  // below). Drive-info refresh comes from the invalidation manifest.
  const smartTest = linuxio.storage.run_smart_test.useJobStreamAction<
    SmartTestResult,
    SmartTestProgressEvent
  >({
    closeMessage: "SMART self-test stream closed unexpectedly",
    onOpen: (stream) => {
      streamRef.current = stream;
    },
    onProgress: (data, _job, variables) => {
      const testType: "short" | "long" =
        variables.testType === "long" ? "long" : "short";
      setTestProgress((prev) => ({
        ...(prev || {}),
        ...data,
        test_type: data.test_type ?? prev?.test_type ?? testType,
        device: data.device ?? prev?.device ?? variables.device,
      }));
    },
    success: (data, variables) => {
      const testType: "short" | "long" =
        variables.testType === "long" ? "long" : "short";
      const finalStatus = data?.status ?? "completed";
      setTestProgress((prev) => ({
        ...(prev || {}),
        type: "status",
        status: finalStatus as SmartTestProgressEvent["status"],
        message: data?.message ?? prev?.message,
        test_type: data?.test_type ?? prev?.test_type ?? testType,
        device: data?.device ?? prev?.device ?? variables.device,
      }));
      const label = testType === "short" ? "Short" : "Extended";
      if (finalStatus === "completed") {
        toast.success(
          `${label} self-test completed on /dev/${variables.device}`,
        );
      } else if (finalStatus === "aborted") {
        toast.error(`${label} self-test aborted on /dev/${variables.device}`);
      } else {
        const detail = data?.message ? `: ${data.message}` : "";
        toast.error(
          `${label} self-test failed on /dev/${variables.device}${detail}`,
        );
      }
    },
    error: (error, variables) => {
      if (error.name === "AbortError") return;
      const testType: "short" | "long" =
        variables.testType === "long" ? "long" : "short";
      const errorMessage = error.message || "SMART self-test failed";
      setTestProgress((prev) => ({
        ...(prev || {}),
        type: "status",
        status: "error",
        message: errorMessage,
        test_type: prev?.test_type ?? testType,
        device: prev?.device ?? variables.device,
      }));
      toast.error(errorMessage);
    },
    options: {
      onSettled: () => {
        streamRef.current = null;
        setStartPending(null);
      },
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
  // (e.g. user refreshed the page mid-test), adopt it into the same action —
  // progress, toasts, and cleanup all come from the shared config above.
  useActiveJobRecovery({
    type: JOB_TYPE_STORAGE_SMART_TEST,
    scanKey: rawDrive?.name ?? null,
    match: (job) => {
      const metadata = job.metadata as { device?: string } | undefined;
      return metadata?.device === rawDrive?.name;
    },
    onRecover: (job) => {
      const deviceName = rawDrive?.name;
      if (!deviceName) return;
      const metadata = job.metadata as { testType?: string } | undefined;
      const testType: "short" | "long" =
        metadata?.testType === "long" ? "long" : "short";
      setStartPending(testType);
      setTestProgress({
        type: "status",
        status: "in_progress",
        test_type: testType,
        device: deviceName,
        message: "Resuming SMART self-test",
      });
      smartTest.attach(job, { device: deviceName, testType });
    },
  });

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
    smartTest.mutate({ device: rawDrive.name, testType });
  };
  const handleTabChange = (newValue: number) => {
    setTabIndex(newValue);
  };
  const smart = (rawDrive?.smart ?? drive.smart) as SmartData | undefined;
  const power = drive.power;
  const isNvme =
    drive.transport === "nvme" ||
    drive.name.startsWith("nvme") ||
    rawDrive?.name.startsWith("nvme") === true;
  const smartData = smart;
  const ataAttrs = smartData?.ata_smart_attributes?.table;
  const smartError = rawDrive?.smartError;
  const deviceInfo = smartData?.device;
  const smartHealth = smartData?.smart_status;
  const nvmeHealthRaw = smartData?.nvme_smart_health_information_log;
  const selfTestLog = smartData?.ata_smart_self_test_log;
  const nvmeSelfTestLog = smartData?.nvme_self_test_log;
  return (
    <AppCollapse in={expanded} unmountOnExit>
      <div>
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
            smartData={smartData}
            smartError={smartError}
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

const DiskOverview = () => {
  const navigate = storageRouteApi.useNavigate();
  const search = storageRouteApi.useSearch();
  const toast = useScopedToast(STORAGE_TOAST_META);
  const expanded = typeof search.drive === "string" ? search.drive : undefined;
  const selectedMountpoint =
    typeof search.fs === "string" ? search.fs : undefined;
  const [creatingSubvolumeMountpoint, setCreatingSubvolumeMountpoint] =
    useState<string | null>(null);
  const [subvolumeDrafts, setSubvolumeDrafts] = useState<
    Record<string, string>
  >({});
  const { isEnabled: smartmontoolsAvailable, reason: smartmontoolsReason } =
    useCapability("smartmontoolsAvailable");
  const [
    { data: rawDrivesData },
    { data: filesystemsData },
    { data: nfsMountsData },
  ] = useSuspenseQueries({
    queries: [
      linuxio.storage.get_drive_info.queryOptions({
        refetchInterval: 30000,
      }),
      linuxio.system.get_fs_info.queryOptions({
        refetchInterval: 10000,
      }),
      linuxio.storage.list_nfs_mounts.queryOptions({
        refetchInterval: 10000,
      }),
    ],
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
    linuxio.storage.unmount_filesystem.useAction({
      success: () => {
        toast.success("Filesystem unmounted");
        navigate({
          to: "/storage",
          search: (previous) => ({
            ...previous,
            fs: undefined,
          }),
        });
      },
      error: "Failed to unmount filesystem",
      toast: STORAGE_TOAST_META,
    });
  const { mutate: createBtrfsSubvolume, isPending: isCreatingSubvolume } =
    linuxio.storage.create_btrfs_subvolume.useAction({
      success: (result) => {
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
      error: "Failed to create btrfs subvolume",
      toast: STORAGE_TOAST_META,
    });
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        navigate({
          to: "/storage",
          search: (previous) => ({
            ...previous,
            drive: undefined,
            fs: undefined,
          }),
        });
        setCreatingSubvolumeMountpoint(null);
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [navigate]);
  const handleToggle = (driveName: string) => {
    navigate({
      to: "/storage",
      search: (previous) => ({
        ...previous,
        drive: expanded === driveName ? undefined : driveName,
      }),
    });
  };
  const drives = useMemo<DriveInfo[]>(
    () =>
      rawDrives.map((d) => ({
        name: d.name,
        model: d.model,
        sizeBytes: parseSizeToBytes(d.size),
        transport: d.type ?? (d.name.startsWith("nvme") ? "nvme" : "unknown"),
        vendor: d.vendor,
        serial: d.serial,
        ro: d.ro,
        smart: d.smart as SmartData | undefined,
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
    navigate({
      to: "/storage",
      search: (previous) => ({
        ...previous,
        fs:
          selectedMountpoint === filesystem.mountpoint
            ? undefined
            : filesystem.mountpoint,
      }),
    });
  };
  const handleBrowseFilesystem = (mountpoint: string) => {
    navigate({
      to: "/filebrowser/$",
      params: { _splat: mountpoint.replace(/^\/+/, "") },
    });
  };
  const handleInspectDrive = (driveName: string) => {
    navigate({
      to: "/storage",
      search: (previous) => ({
        ...previous,
        drive: driveName,
        fs: undefined,
      }),
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

export default DiskOverview;
