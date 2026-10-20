import { Icon } from "@iconify/react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";

import {
  linuxio,
  useCallMutation,
  type BtrfsRollbackProgress,
  type BtrfsRollbackResult,
  type BtrfsSnapshot,
  type TaskProgress,
} from "@/api";
import CardIconHeader from "@/components/cards/CardIconHeader";
import FrostedCard from "@/components/cards/FrostedCard";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppCardContent from "@/components/ui/AppCardContent";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppSelect from "@/components/ui/AppSelect";
import AppTypography from "@/components/ui/AppTypography";
import StatusDot from "@/components/ui/StatusDot";
import { TASK_TYPE_STORAGE_BTRFS_ROLLBACK } from "@/constants/backgroundTaskTypes";
import { useActiveTaskRecovery } from "@/hooks/backgroundTasks/useActiveTaskRecovery";
import { formatDate, formatFileSize } from "@/utils/formaters";

import BtrfsRollbackDialog from "./BtrfsRollbackDialog";
import { BTRFS_TOAST_META, isSystemSubvolume } from "./btrfsShared";

const ALL_SOURCES = "";

const KINDS: Record<string, { label: string; color: string; detail: string }> =
  {
    manual: {
      label: "Manual",
      color: "var(--app-palette-primary-main)",
      detail: "taken by you",
    },
    timeline: {
      label: "Scheduled",
      color: "var(--app-palette-text-secondary)",
      detail: "automatic",
    },
    "pre-update": {
      label: "Before update",
      color: "var(--app-palette-warning-main)",
      detail: "taken before an update",
    },
    "pre-rollback": {
      label: "Before restore",
      color: "var(--app-palette-info-main)",
      detail: "taken before a restore",
    },
  };

const BtrfsSnapshotsCard = ({ uuid }: { uuid: string }) => {
  // Lazy: the route loader cannot know the selected filesystem's uuid.
  const {
    data = [],
    error,
    isPending,
  } = useQuery(linuxio.storage.btrfs_snapshots({ uuid }));
  const { data: subvolumeData = [] } = useQuery(
    linuxio.storage.btrfs_subvolumes({ uuid }),
  );
  const navigate = useNavigate();
  const [source, setSource] = useState(ALL_SOURCES);
  // null: dialog closed; "": open with nothing picked yet.
  const [taking, setTaking] = useState<string | null>(null);
  // Read once per mount: day headings only need to be right for the session.
  const [today] = useState(() => new Date());
  const [deleting, setDeleting] = useState<BtrfsSnapshot | null>(null);
  // Held as a path so a rollback recovered after a reload resolves once the
  // list has loaded.
  const [rollbackPath, setRollbackPath] = useState<string | null>(null);
  const [progress, setProgress] = useState<BtrfsRollbackProgress | null>(null);
  const [result, setResult] = useState<BtrfsRollbackResult | null>(null);

  const { mutate: createSnapshot, isPending: isTaking } = useCallMutation(
    linuxio.storage.btrfs_snapshot_create,
    {
      success: "Snapshot created",
      error: "Failed to create snapshot",
      toast: BTRFS_TOAST_META,
    },
  );
  const { mutate: deleteSnapshot, isPending: isDeleting } = useCallMutation(
    linuxio.storage.btrfs_snapshot_delete,
    {
      success: "Snapshot deleted",
      error: "Failed to delete snapshot",
      toast: BTRFS_TOAST_META,
    },
  );
  // One config drives fresh runs (mutate) and reload recovery (watch).
  const rollback = linuxio.storage.btrfs_snapshot_rollback.useTaskStreamAction<
    BtrfsRollbackResult,
    TaskProgress<BtrfsRollbackProgress>
  >({
    closeMessage: "Rollback stream closed unexpectedly",
    onProgress: (taskProgress) => setProgress(taskProgress.detail ?? null),
    success: setResult,
    error: "Failed to roll back snapshot",
    toast: BTRFS_TOAST_META,
  });

  useActiveTaskRecovery({
    type: TASK_TYPE_STORAGE_BTRFS_ROLLBACK,
    scanKey: uuid,
    match: (task) => task.metadata?.identity?.[0] === uuid,
    onRecover: (task) => {
      const snapshotPath = task.metadata?.identity?.[1];
      if (!snapshotPath) return;
      setRollbackPath(snapshotPath);
      rollback.watch(task, { uuid, snapshotPath });
    },
  });

  const subvolumes = subvolumeData
    .filter((s) => !s.isSnapshot && !isSystemSubvolume(s.path))
    .map((s) => s.path);
  const sources = [...new Set(data.map((snapshot) => snapshot.source))].sort();
  const activeSource = sources.includes(source) ? source : ALL_SOURCES;
  const snapshots = data.filter(
    (snapshot) =>
      activeSource === ALL_SOURCES || snapshot.source === activeSource,
  );
  const rollingBack = data.find((snapshot) => snapshot.path === rollbackPath);

  const handleBrowse = (snapshot: BtrfsSnapshot) =>
    navigate({
      to: "/filebrowser/$",
      params: { _splat: `run/linuxio/btrfs/${uuid}/${snapshot.path}` },
    });
  const handleDelete = () => {
    if (!deleting) return;
    deleteSnapshot(
      { uuid, snapshotPath: deleting.path },
      { onSuccess: () => setDeleting(null) },
    );
  };
  const openTake = () =>
    setTaking(subvolumes.length === 1 ? subvolumes[0] : "");
  const handleTake = () => {
    if (!taking) return;
    createSnapshot(
      { uuid, path: taking },
      { onSuccess: () => setTaking(null) },
    );
  };
  const openRollback = (snapshot: BtrfsSnapshot) => {
    setProgress(null);
    setResult(null);
    setRollbackPath(snapshot.path);
  };

  const dayLabel = (iso: string) => {
    const date = new Date(iso);
    const yesterday = new Date(today.getTime() - 24 * 60 * 60 * 1000);
    const same = (a: Date, b: Date) => a.toDateString() === b.toDateString();
    if (same(date, today)) return "Today";
    if (same(date, yesterday)) return "Yesterday";
    return date.toLocaleDateString(undefined, {
      day: "numeric",
      month: "long",
      year: "numeric",
    });
  };
  const timeLabel = (iso: string) =>
    new Date(iso).toLocaleTimeString([], {
      hour: "2-digit",
      minute: "2-digit",
    });
  const groups: { day: string; items: BtrfsSnapshot[] }[] = [];
  for (const snapshot of snapshots) {
    const day = dayLabel(snapshot.createdAt);
    const group = groups.at(-1);
    if (group?.day === day) group.items.push(snapshot);
    else groups.push({ day, items: [snapshot] });
  }

  return (
    <FrostedCard>
      <AppCardContent>
        <CardIconHeader
          iconTint
          icon={<Icon height={24} icon="mdi:camera-burst" width={24} />}
          right={
            <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
              {sources.length > 1 && (
                <AppSelect
                  label="Source"
                  onChange={(event) => setSource(event.target.value)}
                  size="small"
                  style={{ minWidth: 160 }}
                  value={activeSource}
                >
                  <option value={ALL_SOURCES}>All folders</option>
                  {sources.map((name) => (
                    <option key={name} value={name}>
                      {name}
                    </option>
                  ))}
                </AppSelect>
              )}
              <AppButton
                disabled={subvolumes.length === 0}
                onClick={openTake}
                size="small"
                variant="outlined"
              >
                Take snapshot
              </AppButton>
            </div>
          }
          subtitle="Every point you can go back to. Browse to fetch single files, Restore to go back entirely"
          title="History"
        />
        {error && <AppAlert severity="error">{error.message}</AppAlert>}
        {!isPending && snapshots.length === 0 && (
          <AppTypography color="text.secondary" variant="body2">
            No snapshots yet
          </AppTypography>
        )}
        <ul
          aria-label="Btrfs snapshots"
          style={{ listStyle: "none", margin: 0, padding: 0 }}
        >
          {groups.map((group) => (
            <li key={group.day} style={{ listStyle: "none" }}>
              <AppTypography
                color="text.secondary"
                style={{
                  display: "block",
                  padding: "var(--app-space-8) 0 2px",
                }}
                variant="overline"
              >
                {group.day}
              </AppTypography>
              <ul
                style={{
                  borderLeft: "2px solid var(--app-palette-divider)",
                  listStyle: "none",
                  margin: 0,
                  marginLeft: 5,
                  padding: 0,
                }}
              >
                {group.items.map((snapshot) => {
                  const kind = KINDS[snapshot.kind];
                  return (
                    <li
                      key={snapshot.path}
                      style={{
                        alignItems: "center",
                        display: "flex",
                        gap: 12,
                        padding: "var(--app-space-8) 0",
                      }}
                    >
                      <StatusDot
                        color={
                          kind?.color ?? "var(--app-palette-text-disabled)"
                        }
                        style={{
                          boxShadow:
                            "0 0 0 3px var(--app-palette-background-paper)",
                          marginLeft: -6,
                        }}
                        tooltip={kind?.label ?? snapshot.kind}
                      />
                      <AppTypography
                        fontWeight={600}
                        style={{ width: 56, flexShrink: 0 }}
                        variant="body2"
                      >
                        {timeLabel(snapshot.createdAt)}
                      </AppTypography>
                      <div
                        style={{
                          display: "flex",
                          flex: 1,
                          flexDirection: "column",
                          minWidth: 0,
                        }}
                      >
                        <AppTypography noWrap variant="body2">
                          {kind?.label ?? snapshot.kind} snapshot of{" "}
                          <strong>{snapshot.source}</strong>
                        </AppTypography>
                        <AppTypography color="text.secondary" variant="caption">
                          {[
                            snapshot.referencedBytes === undefined
                              ? null
                              : formatFileSize(snapshot.referencedBytes),
                            kind?.detail ?? snapshot.kind,
                          ]
                            .filter(Boolean)
                            .join(" · ")}
                        </AppTypography>
                      </div>
                      <AppButton
                        onClick={() => handleBrowse(snapshot)}
                        size="small"
                        variant="outlined"
                      >
                        Browse
                      </AppButton>
                      <AppButton
                        onClick={() => openRollback(snapshot)}
                        size="small"
                        variant="outlined"
                      >
                        Restore
                      </AppButton>
                      <AppActionIconButton
                        icon="mdi:delete"
                        label="Delete"
                        onClick={() => setDeleting(snapshot)}
                      />
                    </li>
                  );
                })}
              </ul>
            </li>
          ))}
        </ul>
      </AppCardContent>

      <GeneralDialog
        fullWidth
        maxWidth="sm"
        onClose={() => setTaking(null)}
        open={taking !== null}
      >
        <AppDialogTitle>Take snapshot</AppDialogTitle>
        <AppDialogContent>
          <AppDialogContentText>
            Keeps a read-only copy of the current state that you can browse or
            restore later. It takes no extra space until files change.
          </AppDialogContentText>
          {subvolumes.length > 1 && (
            <AppSelect
              fullWidth
              label="Subvolume"
              onChange={(event) => setTaking(event.target.value)}
              value={taking ?? ""}
            >
              <option disabled hidden value="">
                Choose…
              </option>
              {subvolumes.map((path) => (
                <option key={path} value={path}>
                  {path}
                </option>
              ))}
            </AppSelect>
          )}
        </AppDialogContent>
        <AppDialogActions>
          <AppButton disabled={isTaking} onClick={() => setTaking(null)}>
            Cancel
          </AppButton>
          <AppButton
            disabled={isTaking || !taking}
            onClick={handleTake}
            variant="contained"
          >
            {isTaking ? "Taking..." : "Take snapshot"}
          </AppButton>
        </AppDialogActions>
      </GeneralDialog>

      {rollingBack && (
        <BtrfsRollbackDialog
          isPending={rollback.isPending}
          onClose={() => setRollbackPath(null)}
          onConfirm={() =>
            rollback.mutate({ uuid, snapshotPath: rollingBack.path })
          }
          progress={progress}
          result={result}
          snapshot={rollingBack}
        />
      )}

      <GeneralDialog
        fullWidth
        maxWidth="sm"
        onClose={() => setDeleting(null)}
        open={deleting !== null}
      >
        <AppDialogTitle>Delete snapshot</AppDialogTitle>
        <AppDialogContent>
          <AppDialogContentText>
            Delete the snapshot of <strong>{deleting?.source}</strong> taken{" "}
            {formatDate(deleting?.createdAt)}? Files that only exist in this
            snapshot are lost. This cannot be undone.
          </AppDialogContentText>
        </AppDialogContent>
        <AppDialogActions>
          <AppButton disabled={isDeleting} onClick={() => setDeleting(null)}>
            Cancel
          </AppButton>
          <AppButton
            color="error"
            disabled={isDeleting}
            onClick={handleDelete}
            variant="contained"
          >
            {isDeleting ? "Deleting..." : "Delete"}
          </AppButton>
        </AppDialogActions>
      </GeneralDialog>
    </FrostedCard>
  );
};

export default BtrfsSnapshotsCard;
