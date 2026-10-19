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
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppCardContent from "@/components/ui/AppCardContent";
import AppChip from "@/components/ui/AppChip";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppSelect from "@/components/ui/AppSelect";
import { TASK_TYPE_STORAGE_BTRFS_ROLLBACK } from "@/constants/backgroundTaskTypes";
import { useActiveTaskRecovery } from "@/hooks/backgroundTasks/useActiveTaskRecovery";
import { formatDate, formatFileSize } from "@/utils/formaters";

import BtrfsRollbackDialog from "./BtrfsRollbackDialog";
import { BTRFS_TOAST_META } from "./btrfsShared";

const ALL_SOURCES = "";

const BtrfsSnapshotsCard = ({ uuid }: { uuid: string }) => {
  // Lazy: the route loader cannot know the selected filesystem's uuid.
  const {
    data = [],
    error,
    isPending,
  } = useQuery(linuxio.storage.btrfs_snapshots({ uuid }));
  const navigate = useNavigate();
  const [source, setSource] = useState(ALL_SOURCES);
  const [deleting, setDeleting] = useState<BtrfsSnapshot | null>(null);
  // Held as a path so a rollback recovered after a reload resolves once the
  // list has loaded.
  const [rollbackPath, setRollbackPath] = useState<string | null>(null);
  const [progress, setProgress] = useState<BtrfsRollbackProgress | null>(null);
  const [result, setResult] = useState<BtrfsRollbackResult | null>(null);

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

  const sources = [...new Set(data.map((snapshot) => snapshot.source))].sort();
  const activeSource = sources.includes(source) ? source : ALL_SOURCES;
  const snapshots = data.filter(
    (snapshot) =>
      activeSource === ALL_SOURCES || snapshot.source === activeSource,
  );
  const hasSizes = snapshots.some(
    (snapshot) => snapshot.referencedBytes !== undefined,
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
  const openRollback = (snapshot: BtrfsSnapshot) => {
    setProgress(null);
    setResult(null);
    setRollbackPath(snapshot.path);
  };

  const columns: AppVirtualTableColumnDef<BtrfsSnapshot>[] = [
    { accessorKey: "source", header: "Source" },
    {
      accessorKey: "createdAt",
      header: "Taken",
      cell: ({ row }) => formatDate(row.original.createdAt),
    },
    {
      accessorKey: "kind",
      header: "Kind",
      cell: ({ row }) => (
        <AppChip label={row.original.kind} size="small" variant="soft" />
      ),
    },
    ...(hasSizes
      ? [
          {
            id: "size",
            header: "Size",
            cell: ({ row }) =>
              row.original.referencedBytes === undefined
                ? "-"
                : formatFileSize(row.original.referencedBytes),
            meta: { align: "right" },
          } satisfies AppVirtualTableColumnDef<BtrfsSnapshot>,
        ]
      : []),
    {
      id: "actions",
      header: "Actions",
      cell: ({ row }) => (
        <div style={{ display: "flex", gap: 8, justifyContent: "flex-end" }}>
          <AppButton
            onClick={() => handleBrowse(row.original)}
            size="small"
            variant="outlined"
          >
            Browse
          </AppButton>
          <AppButton
            onClick={() => openRollback(row.original)}
            size="small"
            variant="outlined"
          >
            Roll back
          </AppButton>
          <AppButton
            color="error"
            onClick={() => setDeleting(row.original)}
            size="small"
            variant="outlined"
          >
            Delete
          </AppButton>
        </div>
      ),
      meta: { align: "right" },
    },
  ];

  return (
    <FrostedCard>
      <AppCardContent>
        <CardIconHeader
          icon={<Icon height={24} icon="mdi:camera-burst" width={24} />}
          right={
            <AppSelect
              label="Source"
              onChange={(event) => setSource(event.target.value)}
              value={activeSource}
            >
              <option value={ALL_SOURCES}>All</option>
              {sources.map((name) => (
                <option key={name} value={name}>
                  {name}
                </option>
              ))}
            </AppSelect>
          }
          subtitle={`${snapshots.length} snapshot(s)`}
          title="Snapshots"
        />
        {error && <AppAlert severity="error">{error.message}</AppAlert>}
        <AppVirtualTable
          ariaLabel="Btrfs snapshots"
          columns={columns}
          data={snapshots}
          density="compact"
          emptyMessage={isPending ? "Loading..." : "No snapshots found"}
          fillAvailable={false}
          getRowId={(snapshot) => snapshot.path}
          maxHeight={320}
          variant="embedded"
        />
      </AppCardContent>

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
            {formatDate(deleting?.createdAt)}? This cannot be undone.
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
