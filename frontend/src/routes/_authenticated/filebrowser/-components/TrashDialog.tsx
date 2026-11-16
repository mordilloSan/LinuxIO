import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";

import {
  type CountProgress,
  type FileBatchResult,
  getMutationErrorMessage,
  isTaskCancellationError,
  linuxio,
  type TaskProgress,
} from "@/api";
import FileBrowserDialog from "@/components/dialog/GeneralDialog";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppCheckbox from "@/components/ui/AppCheckbox";
import AppConfirmDialog from "@/components/ui/AppConfirmDialog";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppSkeleton from "@/components/ui/AppSkeleton";
import AppTypography from "@/components/ui/AppTypography";
import { useScopedToast } from "@/hooks/useScopedToast";
import { batchFailureOpts } from "@/tasks/batchFailures";
import { markTerminalFeedbackEmitted } from "@/tasks/terminalTaskFeedback";
import { formatDate, formatFileSize } from "@/utils/formatters";
import { toastFileError } from "@/utils/linkRefusal";
import { parentPath } from "@/utils/path";

import { FILES_TOAST_META } from "../-hooks/useFileMutations";
import FileIcon from "./FileIcon";

import "./TrashDialog.css";

interface TrashDialogProps {
  onClose: () => void;
  open: boolean;
}

const itemCount = (count: number) => `${count} item${count === 1 ? "" : "s"}`;

const TrashDialog = ({ open, onClose }: TrashDialogProps) => {
  const toast = useScopedToast(FILES_TOAST_META);
  const navigate = useNavigate();
  const [selected, setSelected] = useState<Set<string>>(() => new Set());
  const [confirm, setConfirm] = useState<"selected" | "all" | "restore" | null>(
    null,
  );
  const [progress, setProgress] = useState<TaskProgress<CountProgress> | null>(
    null,
  );
  const { data, error, isLoading } = useQuery({
    ...linuxio.filebrowser.trash_list,
    enabled: open,
  });

  const entries = data?.entries ?? [];
  // Derived from the live listing, so entries that vanished after a refetch
  // drop out of the selection.
  const selectedPaths = entries
    .map((entry) => entry.path)
    .filter((path) => selected.has(path));
  const allSelected =
    entries.length > 0 && selectedPaths.length === entries.length;
  const purgePaths =
    confirm === "all" ? entries.map((entry) => entry.path) : selectedPaths;
  // Items another program trashed carry no record of where they came from,
  // so a restore into a folder the user does not own names its destination
  // and asks first; the server refuses it unconfirmed.
  const restoreConfirmations = entries.filter(
    (entry) =>
      selected.has(entry.path) && entry.confirmRestore && entry.originalPath,
  );
  const restoreDestinations = restoreConfirmations.map(
    (entry) => entry.originalPath,
  );

  // A restore or a purge is a job and does not hold the dialog open: the
  // task tray shows its progress and can cancel it, and its result is
  // toasted below whether the dialog is open or not. A toast's "Open the real
  // folder" closes the dialog with this too.
  const handleClose = () => {
    setSelected(new Set());
    setConfirm(null);
    onClose();
  };
  const report = (
    result: FileBatchResult | undefined,
    paths: string[],
    done: "Restored" | "Deleted",
    action: "restore" | "delete",
  ) => {
    setProgress(null);
    // Only the job's own items leave the selection: the dialog may have been
    // closed while the job ran and opened again with other items selected.
    const jobPaths = new Set(paths);
    setSelected(
      (prev) => new Set([...prev].filter((path) => !jobPaths.has(path))),
    );
    // No confirm opens while a job runs, so an open one is the job's own.
    setConfirm(null);
    const failed = result?.failed ?? [];
    if (failed.length > 0) {
      toastFileError(
        toast,
        navigate,
        `Failed to ${action} ${itemCount(failed.length)}`,
        batchFailureOpts(failed),
        handleClose,
      );
      return;
    }
    toast.success(`${done} ${itemCount(result?.succeeded ?? 0)}`);
  };
  const reportError = (err: unknown, fallback: string) => {
    setProgress(null);
    setConfirm(null);
    // A cancel is not a failure: the task tray that cancelled it says so.
    if (isTaskCancellationError(err)) return;
    toastFileError(
      toast,
      navigate,
      getMutationErrorMessage(err, fallback),
      undefined,
      handleClose,
    );
  };

  // The dialog reports the jobs it starts, so the task tray, which lists them
  // and can cancel them, does not report them as well.
  const restore = linuxio.filebrowser.trash_restore.useMutation({
    onStart: (task) => void markTerminalFeedbackEmitted(task.id),
    onProgress: setProgress,
    success: (result, request) =>
      report(result, request.paths, "Restored", "restore"),
    error: (err) => reportError(err, "Failed to restore items"),
  });
  const purge = linuxio.filebrowser.trash_purge.useMutation({
    onStart: (task) => void markTerminalFeedbackEmitted(task.id),
    onProgress: setProgress,
    success: (result, request) =>
      report(result, request.paths, "Deleted", "delete"),
    error: (err) => reportError(err, "Failed to delete items"),
  });
  // Holds every control that would start a second job, also in a dialog
  // opened again while the job runs.
  const busy = restore.isPending || purge.isPending;
  const toggle = (path: string, checked: boolean) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (checked) next.add(path);
      else next.delete(path);
      return next;
    });
  const toggleAll = (checked: boolean) =>
    setSelected(new Set(checked ? entries.map((entry) => entry.path) : []));

  const restoreSelected = () => {
    if (restoreDestinations.length > 0) {
      setConfirm("restore");
      return;
    }
    setProgress(null);
    restore.mutate({ paths: selectedPaths });
  };
  const confirmRestore = () => {
    setProgress(null);
    restore.mutate({
      paths: selectedPaths,
      // Each destination is confirmed for its own item only.
      confirmedDestinations: Object.fromEntries(
        restoreConfirmations.map((entry) => [entry.path, entry.originalPath]),
      ),
    });
  };
  const confirmPurge = () => {
    setProgress(null);
    purge.mutate({ paths: purgePaths });
  };

  let body;
  if (isLoading) {
    body = (
      <div aria-busy="true" className="trash-dialog__skeletons">
        {[0, 1, 2].map((row) => (
          <AppSkeleton height={40} key={row} variant="rectangular" />
        ))}
      </div>
    );
  } else if (error) {
    body = (
      <AppAlert severity="error">
        {getMutationErrorMessage(error, "Failed to load the Trash")}
      </AppAlert>
    );
  } else if (entries.length === 0) {
    body = (
      <AppTypography color="text.secondary" variant="body2">
        Trash is empty
      </AppTypography>
    );
  } else {
    body = (
      <ul className="trash-dialog__list">
        {entries.map((entry) => (
          <li key={entry.path}>
            <label className="trash-dialog__row">
              <AppCheckbox
                aria-label={`Select ${entry.name}`}
                checked={selected.has(entry.path)}
                onChange={(_event, checked) => toggle(entry.path, checked)}
                size="small"
              />
              <FileIcon
                filename={entry.name}
                isDirectory={entry.isDir}
                size={24}
              />
              <span className="trash-dialog__text">
                <AppTypography noWrap variant="body2">
                  {entry.name}
                </AppTypography>
                <AppTypography color="text.secondary" noWrap variant="caption">
                  {entry.originalPath
                    ? parentPath(entry.originalPath)
                    : "Original location unknown"}
                </AppTypography>
              </span>
              <span className="trash-dialog__meta">
                {entry.deletedAt && (
                  <AppTypography color="text.secondary" variant="caption">
                    {formatDate(entry.deletedAt)}
                  </AppTypography>
                )}
                {!entry.isDir && (
                  <AppTypography color="text.secondary" variant="caption">
                    {formatFileSize(entry.size)}
                  </AppTypography>
                )}
              </span>
            </label>
          </li>
        ))}
      </ul>
    );
  }

  return (
    <>
      <FileBrowserDialog
        fullWidth
        maxWidth="md"
        onClose={handleClose}
        open={open}
      >
        <AppDialogTitle>
          {data ? `Trash (${entries.length})` : "Trash"}
        </AppDialogTitle>
        <AppDialogContent>
          <div className="trash-dialog__toolbar">
            <AppCheckbox
              aria-label="Select all"
              checked={allSelected}
              disabled={entries.length === 0 || busy}
              indeterminate={selectedPaths.length > 0 && !allSelected}
              onChange={(_event, checked) => toggleAll(checked)}
              size="small"
            />
            <AppButton
              disabled={selectedPaths.length === 0 || busy}
              onClick={restoreSelected}
            >
              {restore.isPending
                ? "Restoring…"
                : selectedPaths.length > 0
                  ? `Restore ${selectedPaths.length}`
                  : "Restore"}
            </AppButton>
            <AppButton
              color="error"
              disabled={selectedPaths.length === 0 || busy}
              onClick={() => setConfirm("selected")}
            >
              {selectedPaths.length > 0
                ? `Delete permanently ${selectedPaths.length}`
                : "Delete permanently"}
            </AppButton>
            <AppButton
              color="error"
              disabled={entries.length === 0 || busy}
              onClick={() => setConfirm("all")}
            >
              Empty trash
            </AppButton>
          </div>
          {busy && (
            <div aria-live="polite" role="status">
              {progress?.message ??
                (restore.isPending ? "Restoring items" : "Deleting items")}
            </div>
          )}
          {body}
        </AppDialogContent>
        <AppDialogActions>
          <AppButton onClick={handleClose}>Close</AppButton>
        </AppDialogActions>
      </FileBrowserDialog>
      <AppConfirmDialog
        closeWhilePending
        confirmText="Delete"
        destructive
        isPending={purge.isPending}
        message={
          confirm === "all"
            ? `Permanently delete everything in the Trash (${itemCount(purgePaths.length)})? This cannot be undone.`
            : `Permanently delete ${itemCount(purgePaths.length)}? This cannot be undone.`
        }
        onClose={() => setConfirm(null)}
        onConfirm={confirmPurge}
        open={confirm === "selected" || confirm === "all"}
        progress={progress}
        title={confirm === "all" ? "Empty trash" : "Delete permanently"}
      />
      <AppConfirmDialog
        closeWhilePending
        confirmText="Restore"
        isPending={restore.isPending}
        message={`Restore to ${restoreDestinations.join(", ")}? Another program moved ${restoreDestinations.length === 1 ? "this item" : "these items"} to the Trash, so LinuxIO cannot vouch for where ${restoreDestinations.length === 1 ? "it" : "they"} came from.`}
        onClose={() => setConfirm(null)}
        onConfirm={confirmRestore}
        open={confirm === "restore"}
        pendingText="Restoring…"
        progress={progress}
        title="Restore to a system folder"
      />
    </>
  );
};

export default TrashDialog;
