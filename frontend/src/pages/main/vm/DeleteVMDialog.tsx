import { Icon } from "@iconify/react";
import { useState } from "react";
import type { CSSProperties } from "react";

import type { VirtualMachine } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppButton from "@/components/ui/AppButton";
import AppCheckbox from "@/components/ui/AppCheckbox";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import { type AppTheme, useAppTheme } from "@/theme";

const checkboxLineStyle = (theme: AppTheme): CSSProperties => ({
  alignItems: "center",
  display: "inline-flex",
  gap: theme.spacing(2),
  margin: theme.spacing(3.5, 0),
});

const ownedDisksStyle = (theme: AppTheme): CSSProperties => ({
  background: theme.codeBlock.background,
  borderRadius: 6,
  display: "grid",
  gap: theme.spacing(1.5),
  marginTop: theme.spacing(2.5),
  padding: theme.spacing(2.5),
});

const wrappingCodeStyle: CSSProperties = {
  overflowWrap: "anywhere",
};

export default function DeleteVMDialog({
  isDeleting,
  onClose,
  onDelete,
  open,
  vm,
}: {
  isDeleting: boolean;
  onClose: () => void;
  onDelete: (deleteDisks: boolean) => void;
  open: boolean;
  vm: VirtualMachine | null;
}) {
  const theme = useAppTheme();
  const [deleteDisks, setDeleteDisks] = useState(true);

  return (
    <GeneralDialog fullWidth maxWidth="sm" onClose={onClose} open={open}>
      <AppDialogTitle>Delete VM</AppDialogTitle>
      <AppDialogContent>
        <AppDialogContentText>
          Delete {vm?.name ?? "this VM"} from libvirt.
        </AppDialogContentText>
        <label style={checkboxLineStyle(theme)}>
          <AppCheckbox
            checked={deleteDisks}
            onChange={(_, checked) => setDeleteDisks(checked)}
          />
          <span>Delete LinuxIO-managed disks</span>
        </label>
        {vm && vm.ownedDisks.length > 0 && (
          <div style={ownedDisksStyle(theme)}>
            {vm.ownedDisks.map((disk) => (
              <code key={disk} style={wrappingCodeStyle}>
                {disk}
              </code>
            ))}
          </div>
        )}
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={isDeleting} onClick={onClose} variant="text">
          Cancel
        </AppButton>
        <AppButton
          color="error"
          disabled={isDeleting || !vm}
          onClick={() => onDelete(deleteDisks)}
          startIcon={
            isDeleting ? (
              <AppCircularProgress color="inherit" size={16} />
            ) : (
              <Icon height={18} icon="mdi:trash-can-outline" width={18} />
            )
          }
          variant="contained"
        >
          Delete
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
}
