import { Icon } from "@iconify/react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";

import {
  linuxio,
  type BtrfsRollbackProgress,
  type BtrfsRollbackResult,
  type BtrfsSnapshot,
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
import AppMenu, { AppMenuItem } from "@/components/ui/AppMenu";
import AppSelect from "@/components/ui/AppSelect";
import AppTypography from "@/components/ui/AppTypography";
import StatusDot from "@/components/ui/StatusDot";
import { TASK_TYPE_STORAGE_BTRFS_ROLLBACK } from "@/tasks/taskTypes";
import { useActiveTaskRecovery } from "@/tasks/useActiveTaskRecovery";
import {
  formatDate,
  formatFileSize,
  formatRelativeAge,
} from "@/utils/formatters";

import BtrfsRollbackDialog from "./BtrfsRollbackDialog";
import { BTRFS_TOAST_META, isSystemSubvolume } from "./btrfsShared";
import { groupSnapshotsByDay } from "./snapshotDays";

const ALL_SOURCES = "";

interface KindInfo {
  label: string;
  /** How the legend names the kind, when it says more than the label. */
  legend?: string;
  color: string;
  detail: string;
}

// In the legend's order.
const KINDS: Record<string, KindInfo> = {
  timeline: {
    label: "Scheduled",
    color: "var(--app-palette-text-secondary)",
    detail: "automatic",
  },
  "pre-update": {
    label: "Before an update",
    color: "var(--app-palette-warning-main)",
    detail: "taken before an update",
  },
  manual: {
    label: "Taken by hand",
    legend: "Taken by hand, never pruned",
    color: "var(--app-palette-primary-main)",
    detail: "taken by you, never pruned",
  },
  "pre-rollback": {
    label: "Before a rollback",
    color: "var(--app-palette-info-main)",
    detail: "taken before a rollback",
  },
};

const kindColor = (kind: string) =>
  KINDS[kind]?.color ?? "var(--app-palette-text-disabled)";

const timeOfDay = (createdAt: string) =>
  new Date(createdAt).toLocaleTimeString(undefined, {
    hour: "2-digit",
    minute: "2-digit",
  });


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
  const [deleting, setDeleting] = useState<BtrfsSnapshot | null>(null);
  const [menu, setMenu] = useState<{
    anchor: HTMLElement;
    snapshot: BtrfsSnapshot;
  } | null>(null);
  // Held as a path so a rollback recovered after a reload resolves once the
  // list has loaded.
  const [rollbackPath, setRollbackPath] = useState<string | null>(null);
  const [progress, setProgress] = useState<BtrfsRollbackProgress | null>(null);
  const [result, setResult] = useState<BtrfsRollbackResult | null>(null);

  const { mutate: createSnapshot, isPending: isTaking } =
    linuxio.storage.btrfs_snapshot_create.useMutation({
      success: "Snapshot created",
      error: "Failed to create snapshot",
      toast: BTRFS_TOAST_META,
    });
  const { mutate: deleteSnapshot, isPending: isDeleting } =
    linuxio.storage.btrfs_snapshot_delete.useMutation({
      success: "Snapshot deleted",
      error: "Failed to delete snapshot",
      toast: BTRFS_TOAST_META,
    });
  // One config drives fresh runs (mutate) and reload recovery (watch).
  const rollback = linuxio.storage.btrfs_snapshot_rollback.useMutation({
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
      rollback.watch(task.id, { uuid, snapshotPath });
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

  const hasSizes = snapshots.some(
    (snapshot) => snapshot.referencedBytes !== undefined,
  );

  const days = groupSnapshotsByDay(snapshots);
  const kindsPresent = Object.keys(KINDS).filter((kind) =>
    data.some((snapshot) => snapshot.kind === kind),
  );

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
          subtitle="Go back to an earlier state, or fetch single files from it"
          title="Snapshots"
        />
        {error && <AppAlert severity="error">{error.message}</AppAlert>}
        {kindsPresent.length > 0 && (
          <ul
            aria-label="Snapshot kinds"
            style={{
              display: "flex",
              flexWrap: "wrap",
              gap: "var(--app-space-4) var(--app-space-16)",
              listStyle: "none",
              margin: "0 0 var(--app-space-8)",
              padding: 0,
            }}
          >
            {kindsPresent.map((kind) => (
              <li
                key={kind}
                style={{
                  alignItems: "center",
                  display: "flex",
                  gap: "var(--app-space-6)",
                }}
              >
                <StatusDot color={kindColor(kind)} />
                <AppTypography color="text.secondary" variant="caption">
                  {KINDS[kind].legend ?? KINDS[kind].label}
                </AppTypography>
              </li>
            ))}
          </ul>
        )}
        <section
          aria-label="Btrfs snapshots"
          style={{
            maxHeight: 520,
            maxWidth: 880,
            overflowX: "hidden",
            overflowY: "auto",
          }}
        >
          {days.length === 0 && (
            <AppTypography color="text.secondary" variant="body2">
              {isPending ? "Loading..." : "No snapshots yet"}
            </AppTypography>
          )}
          {days.map((day) => {
            const dayId = `btrfs-snapshot-day-${uuid}-${day.key}`;
            return (
              <div key={day.key}>
                <AppTypography
                  color="text.secondary"
                  fontWeight={600}
                  id={dayId}
                  style={{
                    borderBottom: "1px solid var(--app-palette-divider)",
                    margin: "var(--app-space-12) 0 0",
                    padding: "var(--app-space-6) var(--app-space-4)",
                  }}
                  variant="overline"
                >
                  {day.label}
                </AppTypography>
                <ul
                  aria-labelledby={dayId}
                  style={{ listStyle: "none", margin: 0, padding: 0 }}
                >
                  {day.items.map((snapshot) => {
                    const kind = KINDS[snapshot.kind]?.label ?? snapshot.kind;
                    const time = timeOfDay(snapshot.createdAt);
                    const whenId = `btrfs-snapshot-${uuid}-${snapshot.id}`;
                    return (
                      <li
                        key={snapshot.path}
                        style={{
                          alignItems: "center",
                          borderBottom: "1px solid var(--app-palette-divider)",
                          display: "flex",
                          flexWrap: "wrap",
                          gap: "var(--app-space-4) var(--app-space-12)",
                          padding: "var(--app-space-4)",
                        }}
                      >
                        <StatusDot
                          color={kindColor(snapshot.kind)}
                          tooltip={KINDS[snapshot.kind]?.detail ?? snapshot.kind}
                        />
                        <AppTypography
                          id={whenId}
                          style={{ flex: "0 0 150px" }}
                          variant="body2"
                        >
                          <strong>{time}</strong>
                          {day.recent &&
                            ` · ${formatRelativeAge(new Date(snapshot.createdAt).getTime())}`}
                        </AppTypography>
                        <div style={{ flex: "1 1 120px", minWidth: 0 }}>
                          <AppTypography variant="body2">{kind}</AppTypography>
                          {sources.length > 1 && (
                            <AppTypography
                              color="text.secondary"
                              style={{ display: "block" }}
                              variant="caption"
                            >
                              {snapshot.source}
                            </AppTypography>
                          )}
                        </div>
                        {hasSizes && (
                          <AppTypography color="text.secondary" variant="caption">
                            {snapshot.referencedBytes === undefined
                              ? ""
                              : formatFileSize(snapshot.referencedBytes)}
                          </AppTypography>
                        )}
                        <div
                          style={{
                            display: "flex",
                            gap: "var(--app-space-4)",
                            marginInlineStart: "auto",
                          }}
                        >
                          <AppButton
                            aria-describedby={whenId}
                            keepTextOnMobile
                            onClick={() => handleBrowse(snapshot)}
                            size="small"
                            startIcon={
                              <Icon
                                height={18}
                                icon="mdi:folder-open-outline"
                                width={18}
                              />
                            }
                            variant="text"
                          >
                            Browse
                          </AppButton>
                          <AppActionIconButton
                            ariaLabel={`More actions for the snapshot of ${snapshot.source}, ${day.label} at ${time}`}
                            icon="mdi:dots-horizontal"
                            label="More actions"
                            onClick={(event) =>
                              setMenu({ anchor: event.currentTarget, snapshot })
                            }
                          />
                        </div>
                      </li>
                    );
                  })}
                </ul>
              </div>
            );
          })}
        </section>
        <AppMenu
          anchorEl={menu?.anchor ?? null}
          anchorOrigin={{ vertical: "bottom", horizontal: "right" }}
          onClose={() => setMenu(null)}
          open={menu !== null}
          transformOrigin={{ vertical: "top", horizontal: "right" }}
        >
          <AppMenuItem
            onClick={() => {
              if (menu) openRollback(menu.snapshot);
              setMenu(null);
            }}
            startAdornment={
              <Icon height={18} icon="mdi:backup-restore" width={18} />
            }
          >
            Roll back to this snapshot…
          </AppMenuItem>
          <AppMenuItem
            danger
            onClick={() => {
              if (menu) setDeleting(menu.snapshot);
              setMenu(null);
            }}
            startAdornment={<Icon height={18} icon="mdi:delete" width={18} />}
          >
            Delete…
          </AppMenuItem>
        </AppMenu>
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
