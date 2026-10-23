import { Icon } from "@iconify/react";
import { useState, type CSSProperties } from "react";

import {
  linuxio,
  useCallMutation,
  type BtrfsFilesystem,
  type BtrfsOpProgress,
  type BtrfsScrubStatus,
  type TaskProgress,
} from "@/api";
import { STATUS_COLOR } from "@/components/cards/EntityCardKit";
import FrostedCard from "@/components/cards/FrostedCard";
import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";
import { formatDate } from "@/utils/formatters";

import BtrfsAddDeviceDialog from "./BtrfsAddDeviceDialog";
import { deviceErrorCount } from "./BtrfsDevicesCard";
import { BTRFS_POOL_TOAST_META } from "./btrfsShared";

const bannerStyle: CSSProperties = {
  alignItems: "center",
  display: "flex",
  flexWrap: "wrap",
  gap: "var(--app-space-16)",
  padding: "var(--app-space-16)",
};

// Balance data chunks at most this full to turn slack into unallocated space.
const RECLAIM_USAGE = 50;

const scrubSummary = (scrub: BtrfsScrubStatus) => {
  switch (scrub.status) {
    case "none":
      return "Never checked for errors";
    case "running":
      return `Checking for errors · ${scrub.percent}%`;
    default:
      return `Last check: ${formatDate(scrub.finishedAt)}, ${scrub.errorCount} errors${
        scrub.uncorrectableErrors > 0
          ? ` (${scrub.uncorrectableErrors} uncorrectable)`
          : ""
      }`;
  }
};

interface Health {
  action?: "replace" | "reclaim";
  color: string;
  icon: string;
  text: string;
  title: string;
}

/** The worst condition first: it is the one to act on. */
const healthOf = (filesystem: BtrfsFilesystem, name: string): Health => {
  if (filesystem.devices.some((d) => deviceErrorCount(d) > 0))
    return {
      color: STATUS_COLOR.error,
      icon: "mdi:shield-alert",
      text: `A device of ${name} has reported errors. Check its disks below.`,
      title: "Errors",
    };
  if (filesystem.degraded)
    return {
      action: "replace",
      color: STATUS_COLOR.warning,
      icon: "mdi:shield-alert-outline",
      text: `${name} is running without one of its devices. Replace it before another one fails.`,
      title: "Degraded",
    };
  if (
    filesystem.scrub.status !== "running" &&
    filesystem.scrub.uncorrectableErrors > 0
  )
    return {
      color: STATUS_COLOR.error,
      icon: "mdi:shield-alert",
      text: `The last check of ${name} found ${filesystem.scrub.uncorrectableErrors} uncorrectable errors. Restore the affected files from a snapshot or backup.`,
      title: "Data errors",
    };
  if (filesystem.lowMetadataSpace)
    return {
      action: "reclaim",
      color: STATUS_COLOR.warning,
      icon: "mdi:shield-half-full",
      text: `${name} is nearly out of metadata space, so writes will start failing. Reclaim space by balancing lightly used data chunks.`,
      title: "Low space",
    };
  return {
    color: STATUS_COLOR.ok,
    icon: "mdi:shield-check",
    text: `${name} is working normally.`,
    title: "Healthy",
  };
};

/** Health of the selected filesystem, its last error check and the one action that fixes the current problem. */
const BtrfsHealthBanner = ({ filesystem }: { filesystem: BtrfsFilesystem }) => {
  const { uuid } = filesystem;
  const [replacing, setReplacing] = useState(false);
  const mountpoint = filesystem.mountpoints[0]?.mountpoint;
  const name = filesystem.label
    ? `${filesystem.label}${mountpoint ? ` at ${mountpoint}` : ""}`
    : `The filesystem${mountpoint ? ` at ${mountpoint}` : ""}`;
  const health = healthOf(filesystem, name);
  const scrubRunning = filesystem.scrub.status === "running";
  const missing = filesystem.devices.find((d) => d.missing);

  const taskConfig = (success: string, error: string) => ({
    success,
    error,
    toast: BTRFS_POOL_TOAST_META,
  });
  const scrub = linuxio.storage.btrfs_scrub_start.useTaskStreamAction<
    BtrfsFilesystem,
    TaskProgress<BtrfsOpProgress>
  >(taskConfig("Check finished", "Failed to check filesystem"));
  const replace = linuxio.storage.btrfs_device_replace.useTaskStreamAction<
    BtrfsFilesystem,
    TaskProgress<BtrfsOpProgress>
  >(taskConfig("Device replaced", "Failed to replace device"));
  const reclaim = linuxio.storage.btrfs_balance.useTaskStreamAction<
    BtrfsFilesystem,
    TaskProgress<BtrfsOpProgress>
  >(taskConfig("Space reclaimed", "Failed to reclaim space"));
  const { mutate: cancelScrub } = useCallMutation(
    linuxio.storage.btrfs_scrub_cancel,
    { error: "Failed to stop the check", toast: BTRFS_POOL_TOAST_META },
  );

  return (
    <FrostedCard style={bannerStyle}>
      <Icon color={health.color} height={56} icon={health.icon} width={56} />
      <div style={{ flex: "1 1 240px", minWidth: 0 }}>
        <AppTypography
          component="h2"
          fontWeight={600}
          style={{ color: health.color }}
          variant="h5"
        >
          {health.title}
        </AppTypography>
        <AppTypography variant="body2">{health.text}</AppTypography>
        <AppTypography color="text.secondary" variant="caption">
          {scrubSummary(filesystem.scrub)}
        </AppTypography>
      </div>
      {health.action === "replace" && missing && (
        <AppButton
          disabled={replace.isPending}
          onClick={() => setReplacing(true)}
          size="small"
          variant="contained"
        >
          {replace.isPending ? "Replacing…" : "Replace missing disk"}
        </AppButton>
      )}
      {health.action === "reclaim" && (
        <AppButton
          disabled={reclaim.isPending}
          onClick={() => reclaim.mutate({ uuid, dataUsage: RECLAIM_USAGE })}
          size="small"
          variant="contained"
        >
          {reclaim.isPending ? "Reclaiming…" : "Reclaim space"}
        </AppButton>
      )}
      {scrubRunning ? (
        <AppButton
          onClick={() => cancelScrub({ uuid })}
          size="small"
          variant="outlined"
        >
          Stop check
        </AppButton>
      ) : (
        <AppButton
          disabled={scrub.isPending}
          onClick={() => scrub.mutate({ uuid })}
          size="small"
          variant="outlined"
        >
          Check for errors
        </AppButton>
      )}
      {replacing && missing && (
        <BtrfsAddDeviceDialog
          confirmLabel="Replace"
          filesystemDevices={filesystem.devices.map(({ path }) => path)}
          label="New device"
          onClose={() => setReplacing(false)}
          onConfirm={(newDevice) => {
            setReplacing(false);
            replace.mutate({ uuid, deviceId: missing.id, newDevice });
          }}
          title="Replace missing disk"
        />
      )}
    </FrostedCard>
  );
};

export default BtrfsHealthBanner;
