import { Icon } from "@iconify/react";
import type { CSSProperties } from "react";

import {
  linuxio,
  useCallMutation,
  type BtrfsFilesystem,
  type BtrfsOpProgress,
  type BtrfsScrubStatus,
  type TaskProgress,
} from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";
import { STATUS_COLOR } from "@/routes/_authenticated/vm/-components/vmCards";
import { formatDate } from "@/utils/formaters";

import { deviceErrorCount } from "./BtrfsDevicesCard";
import { BTRFS_TOAST_META } from "./btrfsShared";

const bannerStyle: CSSProperties = {
  alignItems: "center",
  display: "flex",
  flexWrap: "wrap",
  gap: "var(--app-space-16)",
  padding: "var(--app-space-16)",
};

const scrubSummary = (scrub: BtrfsScrubStatus) => {
  switch (scrub.status) {
    case "none":
      return "Never checked for errors";
    case "running":
      return `Checking for errors · ${scrub.percent}%`;
    default:
      return `Last check: ${formatDate(scrub.finishedAt)}, ${scrub.errorCount} errors`;
  }
};

const healthOf = (filesystem: BtrfsFilesystem, name: string) => {
  if (filesystem.devices.some((d) => deviceErrorCount(d) > 0))
    return {
      color: STATUS_COLOR.error,
      icon: "mdi:shield-alert",
      text: `A device of ${name} has reported errors. Check its disks on the Disks tab.`,
      title: "Errors",
    };
  if (filesystem.degraded)
    return {
      color: STATUS_COLOR.warning,
      icon: "mdi:shield-alert-outline",
      text: `${name} is running without one of its devices. Replace it before another one fails.`,
      title: "Degraded",
    };
  return {
    color: STATUS_COLOR.ok,
    icon: "mdi:shield-check",
    text: `${name} is working normally.`,
    title: "Healthy",
  };
};

/** Health of the selected filesystem, its last error check and a button to run one. */
const BtrfsHealthBanner = ({ filesystem }: { filesystem: BtrfsFilesystem }) => {
  const { uuid } = filesystem;
  const mountpoint = filesystem.mountpoints[0]?.mountpoint;
  const name = filesystem.label
    ? `${filesystem.label}${mountpoint ? ` at ${mountpoint}` : ""}`
    : `The filesystem${mountpoint ? ` at ${mountpoint}` : ""}`;
  const health = healthOf(filesystem, name);
  const scrubRunning = filesystem.scrub.status === "running";

  const scrub = linuxio.storage.btrfs_scrub_start.useTaskStreamAction<
    BtrfsFilesystem,
    TaskProgress<BtrfsOpProgress>
  >({
    success: "Check finished",
    error: "Failed to check filesystem",
    toast: BTRFS_TOAST_META,
  });
  const { mutate: cancelScrub } = useCallMutation(
    linuxio.storage.btrfs_scrub_cancel,
    { error: "Failed to stop the check", toast: BTRFS_TOAST_META },
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
    </FrostedCard>
  );
};

export default BtrfsHealthBanner;
