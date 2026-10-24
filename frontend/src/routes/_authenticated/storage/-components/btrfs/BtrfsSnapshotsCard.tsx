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
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
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
import { TASK_TYPE_STORAGE_BTRFS_ROLLBACK } from "@/tasks/taskTypes";
import { useActiveTaskRecovery } from "@/tasks/useActiveTaskRecovery";
import {
  formatDate,
  formatFileSize,
  formatRelativeAge,
} from "@/utils/formatters";

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

  const hasSizes = snapshots.some(
    (snapshot) => snapshot.referencedBytes !== undefined,
  );

  const columns: AppVirtualTableColumnDef<BtrfsSnapshot>[] = [
    {
      id: "kind",
      header: "",
      cell: ({ row }) => (
        <StatusDot
          color={
            KINDS[row.original.kind]?.color ??
            "var(--app-palette-text-disabled)"
          }
          tooltip={KINDS[row.original.kind]?.detail ?? row.original.kind}
        />
      ),
      meta: { width: 28 },
    },
    {
      accessorKey: "createdAt",
      header: "Taken",
      cell: ({ row }) => (
        <div style={{ minWidth: 0 }}>
          {formatRelativeAge(new Date(row.original.createdAt).getTime())}
          <AppTypography
            color="text.secondary"
            style={{ display: "block" }}
            variant="caption"
          >
            {formatDate(row.original.createdAt)}
          </AppTypography>
        </div>
      ),
    },
    {
      id: "what",
      header: "What",
      cell: ({ row }) => (
        <div style={{ minWidth: 0 }}>
          {KINDS[row.original.kind]?.label ?? row.original.kind} snapshot
          <AppTypography
            color="text.secondary"
            style={{ display: "block" }}
            variant="caption"
          >
            {row.original.source}
          </AppTypography>
        </div>
      ),
    },
    ...(hasSizes
      ? [
          {
            id: "size",
            header: "Size",
            cell: ({ row }) => (
              <AppTypography color="text.secondary" variant="caption">
                {row.original.referencedBytes === undefined
                  ? "-"
                  : formatFileSize(row.original.referencedBytes)}
              </AppTypography>
            ),
            meta: { align: "right" },
          } satisfies AppVirtualTableColumnDef<BtrfsSnapshot>,
        ]
      : []),
    {
      id: "actions",
      header: "",
      cell: ({ row }) => (
        <div style={{ display: "flex", justifyContent: "flex-end" }}>
          <AppActionIconButton
            icon="mdi:folder-open-outline"
            label="Browse"
            onClick={() => handleBrowse(row.original)}
          />
          <AppActionIconButton
            icon="mdi:backup-restore"
            label="Restore"
            onClick={() => openRollback(row.original)}
          />
          <AppActionIconButton
            icon="mdi:delete"
            label="Delete"
            onClick={() => setDeleting(row.original)}
          />
        </div>
      ),
      meta: { align: "right" },
    },
  ];

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
        <AppVirtualTable
          ariaLabel="Btrfs snapshots"
          columns={columns}
          data={snapshots}
          density="compact"
          emptyMessage={isPending ? "Loading..." : "No snapshots yet"}
          fillAvailable={false}
          getRowId={(snapshot) => snapshot.path}
          maxHeight={420}
          variant="embedded"
        />
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
