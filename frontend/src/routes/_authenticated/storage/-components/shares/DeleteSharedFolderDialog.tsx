import { useQuery } from "@tanstack/react-query";
import { useState } from "react";

import { getMutationErrorMessage, linuxio, type SharedFolder } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppTextField from "@/components/ui/AppTextField";
import { formatFileSize } from "@/utils/formatters";

import { SMB_RESTART_NOTE, timeMachineName } from "./sharedFolders";

interface DeleteSharedFolderDialogProps {
  folder: SharedFolder;
  onClose: () => void;
  onExited?: () => void;
  open: boolean;
}

const plural = (count: number, one: string, many: string) =>
  `${count} ${count === 1 ? one : many}`;

/**
 * Deletes a shared folder with what is in it and its snapshots, once its
 * name is typed. The caller mounts it afresh for each folder.
 */
const DeleteSharedFolderDialog = ({
  folder,
  onClose,
  onExited,
  open,
}: DeleteSharedFolderDialogProps) => {
  const [typed, setTyped] = useState("");
  // A folder whose pool is not mounted cannot be reached: the server removes
  // its share and its record, and leaves what is on the pool.
  const reachable = Boolean(folder.folder);
  const action = reachable
    ? "Delete folder and its data"
    : "Remove shared folder";
  const [problem, setProblem] = useState<string | null>(null);
  const target = { uuid: folder.uuid, path: folder.path };
  const { data: subvolumes } = useQuery({
    ...linuxio.storage.btrfs_subvolumes({ uuid: target.uuid }),
    enabled: open && reachable,
  });
  const { data: snapshots } = useQuery({
    ...linuxio.storage.btrfs_snapshots(target),
    enabled: open && reachable,
  });
  const { data: jobs } = useQuery({
    ...linuxio.storage.btrfs_replication_jobs,
    enabled: open,
  });
  // Known only while quotas are on for the pool.
  const stored = subvolumes?.find(
    (subvolume) => subvolume.path === folder.path,
  )?.referencedBytes;
  const copied = jobs?.some(
    (job) => job.uuid === folder.uuid && job.path === folder.path,
  );
  const { mutate: deleteFolder, isPending } =
    linuxio.shares.delete_folder.useMutation({
      success: reachable ? "Shared folder deleted" : "Shared folder removed",
      error: (error) =>
        setProblem(
          getMutationErrorMessage(error, "Failed to delete the shared folder"),
        ),
      options: { onSuccess: onClose },
    });

  return (
    <GeneralDialog
      fullWidth
      maxWidth="sm"
      onClose={onClose}
      open={open}
      slotProps={{ transition: { onExited } }}
    >
      <AppDialogTitle>{action}</AppDialogTitle>
      <AppDialogContent>
        <div className="app-dialog-fields">
          <AppDialogContentText>
            {reachable
              ? `${folder.folder} is deleted with everything in it${
                  stored == null ? "" : ` (${formatFileSize(stored)})`
                }${
                  snapshots
                    ? ` and its ${plural(snapshots.length, "snapshot", "snapshots")}`
                    : " and its snapshots"
                }. This cannot be undone.`
              : `The pool of ${folder.name} is not mounted, so only its share, its snapshot schedule and its record are removed. What is on the pool stays, as a folder that is no longer shared.`}
            {copied &&
              " Its copies on another pool or server stay; the job that makes them is removed."}
          </AppDialogContentText>
          {timeMachineName(folder) !== "" && (
            <AppDialogContentText>{SMB_RESTART_NOTE}</AppDialogContentText>
          )}
          <AppTextField
            autoFocus
            fullWidth
            label={`Type ${folder.name} to confirm`}
            onChange={(event) => setTyped(event.target.value)}
            size="small"
            value={typed}
          />
          {problem && <AppAlert severity="error">{problem}</AppAlert>}
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton onClick={onClose}>Cancel</AppButton>
        <AppButton
          color="error"
          disabled={isPending || typed !== folder.name}
          onClick={() => {
            setProblem(null);
            deleteFolder({ ...target, confirmName: typed });
          }}
          variant="contained"
        >
          {action}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default DeleteSharedFolderDialog;
