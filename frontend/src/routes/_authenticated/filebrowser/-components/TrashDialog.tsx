import { useQuery } from "@tanstack/react-query";
import { useState } from "react";

import {
  type CountProgress,
  type FileBatchResult,
  getMutationErrorMessage,
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
import { describeBatchFailures } from "@/tasks/batchFailures";
import { formatDate, formatFileSize } from "@/utils/formatters";
import { parentPath } from "@/utils/path";

import FileIcon from "./FileIcon";
import { FILES_TOAST_META } from "../-hooks/useFileMutations";

import "./TrashDialog.css";

interface TrashDialogProps {
  onClose: () => void;
  open: boolean;
}

const itemCount = (count: number) => `${count} item${count === 1 ? "" : "s"}`;

const TrashDialog = ({ open, onClose }: TrashDialogProps) => {
  const toast = useScopedToast(FILES_TOAST_META);
  const [selected, setSelected] = useState<Set<string>>(() => new Set());
  const [confirm, setConfirm] = useState<"selected" | "all" | null>(null);
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

  const report = (
    result: FileBatchResult | undefined,
    done: "Restored" | "Deleted",
    action: "restore" | "delete",
  ) => {
    setProgress(null);
    setSelected(new Set());
    setConfirm(null);
    const failed = result?.failed ?? [];
    if (failed.length > 0) {
      toast.error(`Failed to ${action} ${itemCount(failed.length)}`, {
        description: describeBatchFailures(failed),
      });
      return;
    }
    toast.success(`${done} ${itemCount(result?.succeeded ?? 0)}`);
  };
  const reportError = (err: unknown, fallback: string) => {
    setProgress(null);
    setConfirm(null);
    toast.error(getMutationErrorMessage(err, fallback));
  };

  const restore = linuxio.filebrowser.trash_restore.useTaskStreamAction({
    closeMessage: "Restore task stream closed before completion",
    onProgress: setProgress,
    success: (result) => report(result, "Restored", "restore"),
    error: (err) => reportError(err, "Failed to restore items"),
  });
  const purge = linuxio.filebrowser.trash_purge.useTaskStreamAction({
    closeMessage: "Delete task stream closed before completion",
    onProgress: setProgress,
    success: (result) => report(result, "Deleted", "delete"),
    error: (err) => reportError(err, "Failed to delete items"),
  });
  const busy = restore.isPending || purge.isPending;

  const handleClose = () => {
    if (busy) return;
    setSelected(new Set());
    setConfirm(null);
    onClose();
  };
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
    setProgress(null);
    restore.mutate({ paths: selectedPaths });
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
          {restore.isPending && (
            <div aria-live="polite" role="status">
              {progress?.message ?? "Restoring items"}
            </div>
          )}
          {body}
        </AppDialogContent>
        <AppDialogActions>
          <AppButton disabled={busy} onClick={handleClose}>
            Close
          </AppButton>
        </AppDialogActions>
      </FileBrowserDialog>
      <AppConfirmDialog
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
        open={confirm !== null}
        progress={progress}
        title={confirm === "all" ? "Empty trash" : "Delete permanently"}
      />
    </>
  );
};

export default TrashDialog;
