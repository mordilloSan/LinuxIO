import { Icon } from "@iconify/react";
import { useQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";

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
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTypography from "@/components/ui/AppTypography";
import { CARD_PADDING_MD } from "@/theme/constants";
import {
  formatDate,
  formatFileSize,
  formatRelativeAge,
} from "@/utils/formaters";

import { deviceErrorCount } from "./BtrfsDevicesCard";
import { BTRFS_TOAST_META, isSystemSubvolume } from "./btrfsShared";

type Tone = "success" | "error" | "primary" | "warning";

interface StatTileProps {
  action?: ReactNode;
  children?: ReactNode;
  detail: ReactNode;
  icon: string;
  label: string;
  tone: Tone;
  value: ReactNode;
}

const StatTile = ({
  icon,
  label,
  value,
  detail,
  tone,
  action,
  children,
}: StatTileProps) => (
  <FrostedCard style={{ padding: CARD_PADDING_MD, height: "100%" }}>
    <div style={{ display: "flex", gap: 14, alignItems: "center" }}>
      <span
        aria-hidden="true"
        style={{
          alignItems: "center",
          background: `color-mix(in srgb, var(--app-palette-${tone}-main), transparent 86%)`,
          borderRadius: 12,
          color: `var(--app-palette-${tone}-main)`,
          display: "flex",
          flexShrink: 0,
          height: 40,
          justifyContent: "center",
          width: 40,
        }}
      >
        <Icon height={22} icon={icon} width={22} />
      </span>
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          flex: 1,
          minWidth: 0,
        }}
      >
        <AppTypography color="text.secondary" variant="overline">
          {label}
        </AppTypography>
        <AppTypography fontWeight={700} noWrap variant="h6">
          {value}
        </AppTypography>
        <AppTypography color="text.secondary" noWrap variant="caption">
          {detail}
        </AppTypography>
        {children}
      </div>
      {action}
    </div>
  </FrostedCard>
);

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

export const BtrfsHealthTile = ({
  filesystem,
}: {
  filesystem: BtrfsFilesystem;
}) => {
  const { uuid } = filesystem;
  const hasErrors = filesystem.devices.some((d) => deviceErrorCount(d) > 0);
  const status = hasErrors
    ? "Errors"
    : filesystem.degraded
      ? "Degraded"
      : "Healthy";
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
    <StatTile
      action={
        scrubRunning ? (
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
        )
      }
      detail={scrubSummary(filesystem.scrub)}
      icon={status === "Healthy" ? "mdi:check" : "mdi:alert"}
      label="Health"
      tone={status === "Healthy" ? "success" : "error"}
      value={status}
    />
  );
};

export const BtrfsSpaceTile = ({
  filesystem,
}: {
  filesystem: BtrfsFilesystem;
}) => {
  const percent =
    filesystem.totalBytes > 0
      ? (filesystem.usedBytes / filesystem.totalBytes) * 100
      : 0;
  return (
    <StatTile
      detail={`${formatFileSize(filesystem.totalBytes - filesystem.usedBytes)} free · ${filesystem.devices.length} disk(s)`}
      icon="mdi:chart-donut"
      label="Space"
      tone={percent > 90 ? "error" : "primary"}
      value={`${formatFileSize(filesystem.usedBytes)} of ${formatFileSize(filesystem.totalBytes)}`}
    >
      <AppLinearProgress
        aria-label="Space used"
        color={percent > 90 ? "error" : "primary"}
        style={{ marginTop: 6 }}
        value={percent}
        variant="determinate"
      />
    </StatTile>
  );
};

export const BtrfsProtectionTile = ({ uuid }: { uuid: string }) => {
  // Lazy: the route loader cannot know the selected filesystem's uuid.
  const { data: subvolumes = [] } = useQuery(
    linuxio.storage.btrfs_subvolumes({ uuid }),
  );
  const { data: rules } = useQuery(linuxio.storage.btrfs_snapshot_rules);
  const { data: snapshots = [] } = useQuery(
    linuxio.storage.btrfs_snapshots({ uuid }),
  );

  const folders = subvolumes.filter(
    (s) => !s.isSnapshot && !isSystemSubvolume(s.path),
  );
  const protectedCount = folders.filter((folder) =>
    rules?.rules.some(
      (rule) => rule.uuid === uuid && rule.path === folder.path && rule.enabled,
    ),
  ).length;
  const newest = snapshots
    .filter((snapshot) => snapshot.kind === "timeline")
    .map((snapshot) => new Date(snapshot.createdAt).getTime())
    .sort((a, b) => b - a)[0];

  return (
    <StatTile
      detail={
        folders.length === 0
          ? "No folders can be snapshotted"
          : newest
            ? `Last automatic snapshot ${formatRelativeAge(newest)}`
            : "No automatic snapshot taken yet"
      }
      icon="mdi:shield-check-outline"
      label="Protection"
      tone={folders.length > 0 && protectedCount === 0 ? "warning" : "primary"}
      value={`${protectedCount} of ${folders.length} folders`}
    />
  );
};
