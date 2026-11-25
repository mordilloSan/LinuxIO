import { Icon } from "@iconify/react";
import type { ReactNode } from "react";

import type {
  BtrfsFilesystem,
  BtrfsMaintenanceSchedule,
  BtrfsReplicationJob,
  BtrfsSnapshot,
} from "@/api";
import CardIconHeader from "@/components/cards/CardIconHeader";
import { KeyValues, STATUS_COLOR } from "@/components/cards/EntityCardKit";
import FrostedCard from "@/components/cards/FrostedCard";
import AppButton from "@/components/ui/AppButton";
import AppCardContent from "@/components/ui/AppCardContent";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTypography from "@/components/ui/AppTypography";
import { formatFileSize, formatRelativeAge } from "@/utils/formatters";

import { periodLabel } from "../btrfs/BtrfsMaintenanceControls";
import type { PoolTab } from "./poolTabs";

type Tone = keyof typeof STATUS_COLOR;

/** How many disks each profile can lose and keep its data. */
const DISKS_IT_SURVIVES: Record<string, number> = {
  single: 0,
  dup: 0,
  raid0: 0,
  raid1: 1,
  raid10: 1,
  raid1c3: 2,
  raid1c4: 3,
  raid5: 1,
  raid6: 2,
};

const COUNT = ["", "one", "two", "three"];

const TONE_ICON: Record<Tone, string> = {
  error: "mdi:close-circle",
  muted: "mdi:minus-circle",
  ok: "mdi:check-circle",
  warning: "mdi:alert-circle",
};

interface Row {
  key: string;
  tone: Tone;
  title: string;
  detail?: string;
  action?: ReactNode;
}

/**
 * The weakest copy the pool keeps decides what a disk failure costs, less
 * the disks it already runs without.
 */
const diskFailureRow = (fs: BtrfsFilesystem): Row => {
  const survives = fs.allocation.map((a) => DISKS_IT_SURVIVES[a.profile] ?? 0);
  if (survives.length === 0)
    return { key: "disks", tone: "muted", title: "Copies unknown" };
  const missing = fs.devices.filter((d) => d.missing).length;
  const n = Math.min(...survives);
  if (missing > 0) {
    const left = Math.max(n - missing, 0);
    return left === 0
      ? {
          key: "disks",
          tone: "error",
          title: "Already missing a disk: another failure loses this pool",
          detail: "Replace the missing disk, or reconnect it.",
        }
      : {
          key: "disks",
          tone: "warning",
          title: `Missing a disk: survives ${COUNT[left] ?? left} more disk${left === 1 ? "" : "s"} failing`,
        };
  }
  return n === 0
    ? {
        key: "disks",
        tone: "warning",
        title: "A disk failure loses this pool",
        detail:
          "It keeps no second copy on another disk. Add a disk and keep two copies to survive one failing.",
      }
    : {
        key: "disks",
        tone: "ok",
        title: `Survives ${COUNT[n] ?? n} disk${n === 1 ? "" : "s"} failing`,
      };
};

const checkRow = (fs: BtrfsFilesystem): Row => {
  const { scrub } = fs;
  if (scrub.status === "running")
    return {
      key: "check",
      tone: "ok",
      title: `Checking now · ${Math.round(scrub.percent)}%`,
    };
  if (!scrub.finishedAt)
    return {
      key: "check",
      tone: "muted",
      title: "Never checked",
      detail: "A scrub reads every block to find silent corruption.",
    };
  const when = `Last check ${new Date(scrub.finishedAt).toLocaleDateString()}`;
  if (scrub.uncorrectableErrors > 0)
    return {
      key: "check",
      tone: "error",
      title: `${when}: ${scrub.uncorrectableErrors} errors it could not repair`,
      detail: "Restore the affected files from a snapshot or a backup.",
    };
  return {
    key: "check",
    tone: "ok",
    title:
      scrub.correctedErrors > 0
        ? `${when}: repaired ${scrub.correctedErrors} errors`
        : `${when}: no errors`,
  };
};

const targetName = (job: BtrfsReplicationJob) =>
  job.target.host ? `${job.target.host}:${job.target.path}` : job.target.path;

interface PoolOverviewProps {
  fs: BtrfsFilesystem;
  /** Enabled or not, any pool's; undefined while unknown. */
  jobs: BtrfsReplicationJob[] | undefined;
  /** Null when scheduled maintenance is not available here. */
  maintenance: BtrfsMaintenanceSchedule | null | undefined;
  onTab: (tab: PoolTab) => void;
  /** The pool's snapshots; undefined while unknown. */
  snapshots: BtrfsSnapshot[] | undefined;
}

const MAINTENANCE_TASKS = [
  ["scrub", "Scrub"],
  ["balance", "Balance"],
  ["trim", "Trim"],
  ["defrag", "Defrag"],
] as const;

/**
 * A pool's Overview tab: what protects its data, its disks, and its
 * maintenance. Space and health are on the pool's card beside it.
 */
const PoolOverview = ({
  fs,
  jobs,
  maintenance,
  onTab,
  snapshots,
}: PoolOverviewProps) => {
  const newest = snapshots
    ?.map((s) => new Date(s.createdAt).getTime())
    .reduce((a, b) => Math.max(a, b), 0);
  const copies = (jobs ?? []).filter(
    (job) => job.enabled && job.uuid === fs.uuid,
  );
  const failed = copies.find((job) => job.lastError);
  const mountpoint = fs.mountpoints[0]?.mountpoint ?? "";

  const rows: Row[] = [
    diskFailureRow(fs),
    checkRow(fs),
    {
      key: "snapshots",
      tone: snapshots === undefined ? "muted" : newest ? "ok" : "warning",
      title:
        snapshots === undefined
          ? "Snapshots unknown"
          : newest
            ? `Newest ${formatRelativeAge(newest)}`
            : "No snapshots",
      action: (
        <AppButton
          onClick={() => onTab("snapshots")}
          size="small"
          variant="text"
        >
          See snapshots
        </AppButton>
      ),
    },
    jobs === undefined
      ? { key: "copies", tone: "muted", title: "Copies unknown" }
      : copies.length === 0
        ? {
            key: "copies",
            tone: "warning",
            title: "No copy anywhere else",
            detail:
              "Snapshots live on the same disks. A copy on another pool or server survives losing this one.",
          }
        : {
            key: "copies",
            tone: failed ? "error" : "ok",
            title: `Copied to ${copies.map(targetName).join(", ")}`,
            detail: failed
              ? `The last copy failed: ${failed.lastError}`
              : copies[0].lastRun
                ? `Last copied ${formatRelativeAge(new Date(copies[0].lastRun).getTime())}`
                : undefined,
          },
  ];

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: "var(--app-space-16)",
      }}
    >
      <FrostedCard aria-label="Protection" role="region">
        <AppCardContent>
          <CardIconHeader
            iconTint
            icon={<Icon height={24} icon="mdi:shield-outline" width={24} />}
            title="Protection"
          />
          <div
            style={{
              display: "flex",
              flexDirection: "column",
              gap: "var(--app-space-12)",
            }}
          >
            {rows.map((row) => (
              <div
                key={row.key}
                style={{
                  alignItems: "flex-start",
                  display: "flex",
                  gap: "var(--app-space-12)",
                }}
              >
                <Icon
                  aria-hidden="true"
                  color={STATUS_COLOR[row.tone]}
                  height={20}
                  icon={TONE_ICON[row.tone]}
                  style={{ flexShrink: 0, marginTop: 2 }}
                  width={20}
                />
                <div style={{ flex: 1, minWidth: 0 }}>
                  <AppTypography fontWeight={600} variant="body2">
                    {row.title}
                  </AppTypography>
                  {row.detail && (
                    <AppTypography color="text.secondary" variant="caption">
                      {row.detail}
                    </AppTypography>
                  )}
                </div>
                {row.action}
              </div>
            ))}
          </div>
        </AppCardContent>
      </FrostedCard>

      <FrostedCard aria-label="Disks" role="region">
        <AppCardContent>
          <CardIconHeader
            iconTint
            icon={<Icon height={24} icon="mdi:harddisk" width={24} />}
            title="Disks"
          />
          <div
            style={{
              display: "grid",
              gap: "var(--app-space-12)",
              gridTemplateColumns: "repeat(auto-fill, minmax(200px, 1fr))",
            }}
          >
            {fs.devices.map((device) => (
              <div
                key={device.id}
                style={{
                  border: "1px solid var(--app-palette-divider)",
                  borderRadius: "var(--app-radius-lg)",
                  display: "flex",
                  flexDirection: "column",
                  gap: "var(--app-space-6)",
                  padding: "var(--app-space-12)",
                }}
              >
                <AppTypography fontWeight={600} noWrap variant="body2">
                  {device.path || "Missing disk"}
                </AppTypography>
                <AppTypography color="text.secondary" variant="caption">
                  {device.missing
                    ? device.droppedOut
                      ? "Dropped out"
                      : "Missing"
                    : formatFileSize(device.sizeBytes)}
                </AppTypography>
                {!device.missing && (
                  <>
                    <AppLinearProgress
                      aria-label={`${device.path} allocated`}
                      value={
                        device.sizeBytes > 0
                          ? (device.usedBytes / device.sizeBytes) * 100
                          : 0
                      }
                      variant="determinate"
                    />
                    <AppTypography color="text.secondary" variant="caption">
                      {`${formatFileSize(device.usedBytes)} allocated`}
                    </AppTypography>
                  </>
                )}
              </div>
            ))}
            <AppButton
              onClick={() => onTab("disks")}
              startIcon={<Icon height={20} icon="mdi:plus" width={20} />}
              style={{
                border: "1px dashed var(--app-palette-divider)",
                borderRadius: "var(--app-radius-lg)",
                minHeight: 96,
              }}
              variant="text"
            >
              Add a disk
            </AppButton>
          </div>
        </AppCardContent>
      </FrostedCard>

      <FrostedCard aria-label="Maintenance" role="region">
        <AppCardContent>
          <CardIconHeader
            iconTint
            icon={<Icon height={24} icon="mdi:calendar-sync" width={24} />}
            title="Maintenance"
          />
          {maintenance === undefined ? (
            <AppTypography color="text.secondary" variant="body2">
              Reading the schedule…
            </AppTypography>
          ) : maintenance ? (
            <KeyValues
              items={MAINTENANCE_TASKS.map(([task, label]) => {
                const entry = maintenance.tasks.find((t) => t.task === task);
                const period =
                  entry && entry.mountpoints.includes(mountpoint)
                    ? entry.period
                    : "none";
                return [label, periodLabel(period)];
              })}
            />
          ) : (
            <AppTypography color="text.secondary" variant="body2">
              Scheduled maintenance is not set up here; Settings says what it
              needs.
            </AppTypography>
          )}
          <AppButton
            onClick={() => onTab("settings")}
            size="small"
            style={{ alignSelf: "flex-start", marginTop: "var(--app-space-8)" }}
            variant="text"
          >
            Change in Settings
          </AppButton>
        </AppCardContent>
      </FrostedCard>
    </div>
  );
};

export default PoolOverview;
