import { Icon } from "@iconify/react";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";

import {
  linuxio,
  type BtrfsOpProgress,
  type BtrfsReplicationJob,
  type BtrfsReplicationResult,
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
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { useScopedToast } from "@/hooks/useScopedToast";
import { TASK_TYPE_STORAGE_BTRFS_REPLICATION } from "@/tasks/taskTypes";
import { useActiveTaskRecovery } from "@/tasks/useActiveTaskRecovery";
import { formatDate, formatFileSize } from "@/utils/formatters";

import BtrfsReplicationJobDialog from "./BtrfsReplicationJobDialog";
import { BTRFS_TOAST_META } from "./btrfsShared";

const targetLabel = ({ target }: BtrfsReplicationJob) =>
  target.kind === "ssh"
    ? `${target.user}@${target.host}:${target.path}`
    : `local:${target.path}`;

const BtrfsReplicationCard = ({ uuid }: { uuid: string }) => {
  const toast = useScopedToast(BTRFS_TOAST_META);
  // Lazy: the route loader cannot know the selected filesystem's uuid.
  const { data: subvolumeData = [], error: subvolumesError } = useQuery(
    linuxio.storage.btrfs_subvolumes({ uuid }),
  );
  const {
    data: jobData = [],
    error: jobsError,
    isPending,
  } = useQuery(linuxio.storage.btrfs_replication_jobs);
  const [editing, setEditing] = useState<BtrfsReplicationJob | null>(null);
  const [deleting, setDeleting] = useState<BtrfsReplicationJob | null>(null);
  const [progress, setProgress] = useState<BtrfsOpProgress | null>(null);

  const { mutate: saveJob, isPending: isSaving } =
    linuxio.storage.btrfs_replication_job_set.useMutation({
      success: "Replication job saved",
      error: "Failed to save replication job",
      toast: BTRFS_TOAST_META,
    });
  const { mutate: deleteJob, isPending: isDeleting } =
    linuxio.storage.btrfs_replication_job_delete.useMutation({
      success: "Replication job deleted",
      error: "Failed to delete replication job",
      toast: BTRFS_TOAST_META,
    });
  // One config drives fresh runs (mutate) and reload recovery (watch).
  const replicate = linuxio.storage.btrfs_replication_run.useTaskStreamAction<
    BtrfsReplicationResult,
    TaskProgress<BtrfsOpProgress>
  >({
    closeMessage: "Replication stream closed unexpectedly",
    onProgress: (taskProgress) => setProgress(taskProgress.detail ?? null),
    success: (result) =>
      toast.success(
        result.upToDate || !result.sent
          ? "Target is up to date"
          : `Replicated ${result.sent.path.split("/").pop()} (${formatFileSize(result.bytes)})`,
      ),
    error: "Failed to replicate",
    toast: BTRFS_TOAST_META,
    options: { onSettled: () => setProgress(null) },
  });

  useActiveTaskRecovery({
    type: TASK_TYPE_STORAGE_BTRFS_REPLICATION,
    scanKey: uuid,
    match: (task) =>
      task.metadata?.identity?.[0] === uuid &&
      task.metadata.identity[1] === "replication",
    onRecover: (task) => {
      const id = task.metadata?.identity?.[2];
      if (id) replicate.watch(task, { id });
    },
  });

  const sources = subvolumeData
    .filter((subvolume) => !subvolume.isSnapshot)
    .map((subvolume) => subvolume.path);
  const jobs = jobData.filter((job) => job.uuid === uuid);

  const handleSave = (job: BtrfsReplicationJob) =>
    saveJob({ job }, { onSuccess: () => setEditing(null) });
  const handleDelete = () => {
    if (!deleting) return;
    deleteJob({ id: deleting.id }, { onSuccess: () => setDeleting(null) });
  };

  const columns: AppVirtualTableColumnDef<BtrfsReplicationJob>[] = [
    { accessorKey: "path", header: "Source" },
    {
      id: "target",
      header: "Target",
      cell: ({ row }) => (
        <AppTypography color="text.secondary" variant="caption">
          {targetLabel(row.original)}
        </AppTypography>
      ),
    },
    {
      id: "lastRun",
      header: "Last run",
      cell: ({ row }) => (
        <AppTypography color="text.secondary" variant="caption">
          {row.original.lastRun ? formatDate(row.original.lastRun) : "Never"}
        </AppTypography>
      ),
    },
    {
      id: "status",
      header: "Status",
      cell: ({ row }) => {
        const { enabled, lastError } = row.original;
        if (lastError) {
          return (
            <AppTooltip title={lastError}>
              <AppChip
                color="error"
                label="Error"
                size="small"
                variant="soft"
              />
            </AppTooltip>
          );
        }
        return enabled ? (
          <AppChip color="success" label="OK" size="small" variant="soft" />
        ) : (
          <AppChip label="Disabled" size="small" variant="soft" />
        );
      },
    },
    {
      id: "actions",
      header: "Actions",
      cell: ({ row }) => (
        <div style={{ display: "flex", gap: 8, justifyContent: "flex-end" }}>
          <AppButton
            disabled={replicate.isPending}
            onClick={() => replicate.mutate({ id: row.original.id })}
            size="small"
            variant="outlined"
          >
            Run now
          </AppButton>
          <AppButton
            onClick={() => setEditing(row.original)}
            size="small"
            variant="outlined"
          >
            Edit
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

  const error = subvolumesError ?? jobsError;

  return (
    <FrostedCard>
      <AppCardContent>
        <CardIconHeader
          iconTint
          icon={<Icon height={24} icon="mdi:backup-restore" width={24} />}
          right={
            <AppButton
              onClick={() =>
                setEditing({
                  id: "",
                  uuid,
                  path: "",
                  target: { kind: "local", path: "" },
                  keepOnTarget: 5,
                  enabled: true,
                  after: "timeline",
                })
              }
              size="small"
              variant="outlined"
            >
              Add job
            </AppButton>
          }
          subtitle="Send snapshots to another btrfs filesystem"
          title="Replication"
        />
        {error && <AppAlert severity="error">{error.message}</AppAlert>}
        {replicate.isPending && (
          <>
            <AppLinearProgress
              value={progress?.percent}
              variant={progress ? "determinate" : "indeterminate"}
            />
            <AppTypography variant="body2">
              {progress?.message ?? "Working..."}
            </AppTypography>
          </>
        )}
        <AppVirtualTable
          ariaLabel="Btrfs replication jobs"
          columns={columns}
          data={jobs}
          density="compact"
          emptyMessage={isPending ? "Loading..." : "No replication jobs"}
          fillAvailable={false}
          getRowId={(job) => job.id}
          maxHeight={320}
          variant="embedded"
        />
      </AppCardContent>

      {editing && (
        <BtrfsReplicationJobDialog
          isSaving={isSaving}
          job={editing}
          key={editing.id}
          onClose={() => setEditing(null)}
          onSave={handleSave}
          sources={sources}
        />
      )}

      <GeneralDialog
        fullWidth
        maxWidth="sm"
        onClose={() => setDeleting(null)}
        open={deleting !== null}
      >
        <AppDialogTitle>Delete replication job</AppDialogTitle>
        <AppDialogContent>
          <AppDialogContentText>
            Delete the job replicating <strong>{deleting?.path}</strong> to{" "}
            <strong>{deleting && targetLabel(deleting)}</strong>? Snapshots
            already on the target are kept.
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

export default BtrfsReplicationCard;
