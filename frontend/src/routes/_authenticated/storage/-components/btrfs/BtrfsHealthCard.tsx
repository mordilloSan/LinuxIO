import { Icon } from "@iconify/react";

import {
  linuxio,
  useCallMutation,
  type BtrfsFilesystem,
  type BtrfsOpProgress,
  type BtrfsScrubStatus,
  type TaskProgress,
} from "@/api";
import CardIconHeader from "@/components/cards/CardIconHeader";
import FrostedCard from "@/components/cards/FrostedCard";
import AppButton from "@/components/ui/AppButton";
import AppCardContent from "@/components/ui/AppCardContent";
import AppChip from "@/components/ui/AppChip";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import InfoRow from "@/components/ui/InfoRow";
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

const BtrfsHealthCard = ({ filesystem }: { filesystem: BtrfsFilesystem }) => {
  const { uuid } = filesystem;
  const hasErrors = filesystem.devices.some((d) => deviceErrorCount(d) > 0);
  const status = hasErrors
    ? "Errors"
    : filesystem.degraded
      ? "Degraded"
      : "Healthy";
  const scrubRunning = filesystem.scrub.status === "running";
  const usedPercent =
    filesystem.totalBytes > 0
      ? (filesystem.usedBytes / filesystem.totalBytes) * 100
      : 0;

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
    <FrostedCard>
      <AppCardContent>
        <CardIconHeader
          icon={<Icon height={24} icon="mdi:heart-pulse" width={24} />}
          right={
            <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
              <AppChip
                color={status === "Healthy" ? "success" : "error"}
                label={status}
                size="small"
                variant="soft"
              />
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
          }
          subtitle={`${formatFileSize(filesystem.usedBytes)} used of ${formatFileSize(filesystem.totalBytes)}`}
          title="Health"
        />
        <AppLinearProgress
          aria-label="Space used"
          color={usedPercent > 90 ? "error" : undefined}
          value={usedPercent}
          variant="determinate"
        />
        <InfoRow label="Integrity check" noBorder>
          {scrubSummary(filesystem.scrub)}
        </InfoRow>
      </AppCardContent>
    </FrostedCard>
  );
};

export default BtrfsHealthCard;
