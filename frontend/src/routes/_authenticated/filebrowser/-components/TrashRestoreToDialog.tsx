import { useState } from "react";

import FileBrowserDialog from "@/components/dialog/GeneralDialog";
import PathPickerField from "@/components/pathpicker/PathPickerField";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppTypography from "@/components/ui/AppTypography";

interface TrashRestoreToDialogProps {
  /** How many items go; the dialog names them by count. */
  count: number;
  /** The folder the picker starts in. */
  initialFolder: string;
  onClose: () => void;
  onRestore: (destination: string) => void;
  open: boolean;
}

/**
 * Picks the folder trashed items are restored into. Restoring is a job: the
 * dialog closes as it starts and holds nothing.
 */
const TrashRestoreToDialog = ({
  count,
  initialFolder,
  onClose,
  onRestore,
  open,
}: TrashRestoreToDialogProps) => {
  const [folder, setFolder] = useState(initialFolder);
  const destination = folder.trim();

  return (
    <FileBrowserDialog fullWidth maxWidth="sm" onClose={onClose} open={open}>
      <form
        className="app-dialog-form"
        onSubmit={(event) => {
          event.preventDefault();
          if (destination) onRestore(destination);
        }}
      >
        <AppDialogTitle>Restore to a folder</AppDialogTitle>
        <AppDialogContent>
          <AppTypography style={{ marginBottom: 12 }} variant="body2">
            {count === 1
              ? "The item goes into this folder under the name it is shown with."
              : `The ${count} items go into this folder under the names they are shown with.`}
          </AppTypography>
          <PathPickerField
            editable
            label="Folder"
            onChange={setFolder}
            selectableTypes={["directory"]}
            value={folder}
          />
        </AppDialogContent>
        <AppDialogActions>
          <AppButton onClick={onClose} type="button">
            Cancel
          </AppButton>
          <AppButton disabled={!destination} type="submit" variant="contained">
            Restore
          </AppButton>
        </AppDialogActions>
      </form>
    </FileBrowserDialog>
  );
};

export default TrashRestoreToDialog;
