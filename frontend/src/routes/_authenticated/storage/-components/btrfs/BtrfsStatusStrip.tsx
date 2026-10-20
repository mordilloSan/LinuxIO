import {
  linuxio,
  useCallMutation,
  type BtrfsFilesystem,
  type BtrfsOpProgress,
  type BtrfsScrubStatus,
  type TaskProgress,
} from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import MetricBar from "@/components/gauge/MetricBar";
import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";
import StatusDot from "@/components/ui/StatusDot";
import { CARD_PADDING_MD } from "@/theme/constants";
import { formatDate, formatFileSize } from "@/utils/formaters";

import { deviceErrorCount } from "./BtrfsDevicesCard";
import { BTRFS_TOAST_META } from "./btrfsShared";

const scrubSummary = (scrub: BtrfsScrubStatus) => {
  switch (scrub.status) {
    case "none":
      return "Never scrubbed";
    case "running":
      return `Scrub running · ${scrub.percent}%`;
    default:
      return `Last scrub: ${formatDate(scrub.finishedAt)}, ${scrub.errorCount} errors`;
  }
};

/** One-line state of the selected filesystem: health, space and a scrub button. */
const BtrfsStatusStrip = ({ filesystem }: { filesystem: BtrfsFilesystem }) => {
  const { uuid } = filesystem;
  const hasErrors = filesystem.devices.some((d) => deviceErrorCount(d) > 0);
  const status = hasErrors
    ? "Errors"
    : filesystem.degraded
      ? "Degraded"
      : "Healthy";
  const healthy = status === "Healthy";
  const scrubRunning = filesystem.scrub.status === "running";
  const percent =
    filesystem.totalBytes > 0
      ? (filesystem.usedBytes / filesystem.totalBytes) * 100
      : 0;
  const mountpoint = filesystem.mountpoints[0]?.mountpoint;

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
    <FrostedCard style={{ padding: CARD_PADDING_MD }}>
      <div
        style={{
          alignItems: "center",
          display: "flex",
          flexWrap: "wrap",
          gap: "var(--app-space-16)",
        }}
      >
        <StatusDot
          color={
            healthy
              ? "var(--app-palette-success-main)"
              : "var(--app-palette-error-main)"
          }
          size={12}
          tooltip={status}
        />
        <div
          style={{
            display: "flex",
            flex: "1 1 260px",
            flexDirection: "column",
            minWidth: 0,
          }}
        >
          <AppTypography fontWeight={700} noWrap variant="subtitle1">
            {filesystem.label || "Btrfs filesystem"}
            {mountpoint && (
              <AppTypography color="text.secondary" component="span">
                {" "}
                · {mountpoint}
              </AppTypography>
            )}
          </AppTypography>
          <AppTypography color="text.secondary" variant="caption">
            <span>{status}</span> ·{" "}
            <span>{scrubSummary(filesystem.scrub)}</span>
          </AppTypography>
        </div>
        <div style={{ flex: "1 1 320px", minWidth: 240 }}>
          <MetricBar
            color={
              percent > 90
                ? "var(--app-palette-error-main)"
                : "var(--app-palette-primary-main)"
            }
            label="Space"
            percent={percent}
            rightLabel={`${formatFileSize(filesystem.usedBytes)} used of ${formatFileSize(filesystem.totalBytes)}`}
          />
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
      </div>
    </FrostedCard>
  );
};

export default BtrfsStatusStrip;
