import { useState } from "react";

import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import { useScopedToast } from "@/hooks/useScopedToast";
import { getMutationErrorMessage } from "@/utils/mutations";

export interface BatchDeleteItem {
  key: string;
  label: string;
}

interface BatchDeleteDialogProps<T extends BatchDeleteItem> {
  /** Singular, lowercase noun: "network", "image", "volume". */
  noun: string;
  items: T[];
  /** Deletes one item; a rejected promise marks it as failed. */
  onDeleteOne: (item: T) => Promise<unknown>;
  /** Appended to the "cannot be undone" warning. */
  warning?: string;
  /** Appended to the failure toast, e.g. "(likely in use)". */
  failureHint?: string;
  onClose: () => void;
  onSuccess: () => void;
  open: boolean;
}

const BatchDeleteDialog = <T extends BatchDeleteItem>({
  noun,
  items,
  onDeleteOne,
  warning,
  failureHint,
  onClose,
  onSuccess,
  open,
}: BatchDeleteDialogProps<T>) => {
  const toast = useScopedToast({ label: "Open Docker", to: "/docker" });
  const [isDeleting, setIsDeleting] = useState(false);
  const Noun = noun.charAt(0).toUpperCase() + noun.slice(1);
  const plural = items.length === 1 ? "" : "s";

  const handleDelete = async () => {
    setIsDeleting(true);
    const failures: Array<{ error: unknown; label: string }> = [];
    // Sequential: Docker rejects concurrent removals of related resources.
    for (const item of items) {
      try {
        await onDeleteOne(item);
      } catch (error) {
        failures.push({ error, label: item.label });
      }
    }
    setIsDeleting(false);
    if (failures.length > 0) {
      const message =
        failures.length === 1 && items.length === 1
          ? getMutationErrorMessage(
              failures[0].error,
              `Failed to delete ${noun} "${failures[0].label}"`,
            )
          : `Failed to delete ${failures.length} of ${items.length} ${noun}${plural}`;
      toast.error(`${message}${failureHint ? ` ${failureHint}` : ""}`);
    } else {
      toast.success(
        items.length === 1
          ? `${Noun} "${items[0].label}" deleted successfully`
          : `${items.length} ${noun}s deleted successfully`,
      );
    }
    onSuccess();
    onClose();
  };

  return (
    <GeneralDialog fullWidth maxWidth="sm" onClose={onClose} open={open}>
      <AppDialogTitle>
        Delete {Noun}
        {plural}
      </AppDialogTitle>
      <AppDialogContent>
        <AppDialogContentText>
          Are you sure you want to delete the following {noun}
          {plural}?
        </AppDialogContentText>
        <div
          style={{
            display: "flex",
            flexWrap: "wrap",
            marginTop: "var(--app-space-8)",
            marginBottom: "var(--app-space-4)",
          }}
        >
          {items.map((item) => (
            <Chip
              key={item.key}
              label={item.label}
              size="small"
              style={{ marginRight: 4, marginBottom: 4 }}
              variant="soft"
            />
          ))}
        </div>
        <AppDialogContentText
          style={{ marginTop: 8, color: "var(--app-palette-warning-main)" }}
        >
          This action cannot be undone.{warning ? ` ${warning}` : ""}
        </AppDialogContentText>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={isDeleting} onClick={onClose}>
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
  );
};

export default BatchDeleteDialog;
