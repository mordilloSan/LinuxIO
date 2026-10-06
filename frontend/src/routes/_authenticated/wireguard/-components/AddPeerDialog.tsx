import { useState } from "react";

import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppTextField from "@/components/ui/AppTextField";

interface AddPeerDialogProps {
  interfaceName: string;
  loading: boolean;
  onClose: () => void;
  onCreate: (name: string) => void;
  open: boolean;
}

const AddPeerDialog = ({
  interfaceName,
  loading,
  onClose,
  onCreate,
  open,
}: AddPeerDialogProps) => {
  const [name, setName] = useState("");

  const handleClose = () => {
    if (loading) return;
    setName("");
    onClose();
  };

  const handleCreate = () => {
    onCreate(name.trim());
    setName("");
  };

  return (
    <GeneralDialog
      aria-busy={loading}
      disableEscapeKeyDown={loading}
      fullWidth
      maxWidth="xs"
      onClose={handleClose}
      open={open}
    >
      <AppDialogTitle>Add peer to {interfaceName}</AppDialogTitle>
      <AppDialogContent>
        <div className="app-dialog-fields">
          <AppTextField
            autoFocus
            disabled={loading}
            fullWidth
            helperText="Leave empty to use the generated id."
            label="Name"
            onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !loading) handleCreate();
            }}
            placeholder="e.g. Alice's phone"
            value={name}
          />
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={loading} onClick={handleClose}>
          Cancel
        </AppButton>
        <AppButton
          disabled={loading}
          onClick={handleCreate}
          variant="contained"
        >
          {loading ? "Adding…" : "Add peer"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default AddPeerDialog;
