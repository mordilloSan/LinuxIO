import { linuxio, type NFSExport, type SambaShare } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppTypography from "@/components/ui/AppTypography";
import { useScopedToast } from "@/hooks/useScopedToast";

const SHARES_TOAST_META = { label: "Open shares", to: "/shares" } as const;

interface DeleteNFSShareDialogProps {
  onClose: () => void;
  onSuccess: () => void;
  open: boolean;
  share: NFSExport | null;
}

export const DeleteNFSShareDialog = ({
  open,
  onClose,
  share,
  onSuccess,
}: DeleteNFSShareDialogProps) => {
  const toast = useScopedToast(SHARES_TOAST_META);
  const { mutate: deleteShare, isPending } =
    linuxio.shares.delete_nfs_share.useAction({
      success: () => {
        toast.success(`Removed NFS export for ${share?.path}`);
        onSuccess();
        onClose();
      },
      error: "Failed to remove NFS export",
      toast: SHARES_TOAST_META,
    });

  return (
    <GeneralDialog fullWidth maxWidth="sm" onClose={onClose} open={open}>
      <AppDialogTitle>Remove NFS Export</AppDialogTitle>
      <AppDialogContent>
        <AppDialogContentText>
          Are you sure you want to remove this NFS export?
        </AppDialogContentText>
        {share ? (
          <div style={{ marginTop: 8, marginBottom: 8 }}>
            <AppTypography variant="body2">
              <strong>Path:</strong> {share.path}
            </AppTypography>
            <AppTypography variant="body2">
              <strong>Clients:</strong>{" "}
              {share.clients.map((client) => client.host).join(", ")}
            </AppTypography>
          </div>
        ) : null}
        <AppAlert severity="warning">
          This will remove the export from /etc/exports and re-export.
        </AppAlert>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={isPending} onClick={onClose}>
          Cancel
        </AppButton>
        <AppButton
          color="error"
          disabled={isPending}
          onClick={() => {
            if (share) deleteShare({ path: share.path });
          }}
          variant="contained"
        >
          {isPending ? "Removing..." : "Remove"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

interface DeleteSambaShareDialogProps {
  onClose: () => void;
  onSuccess: () => void;
  open: boolean;
  share: SambaShare | null;
}

export const DeleteSambaShareDialog = ({
  open,
  onClose,
  share,
  onSuccess,
}: DeleteSambaShareDialogProps) => {
  const toast = useScopedToast(SHARES_TOAST_META);
  const { mutate: deleteShare, isPending } =
    linuxio.shares.delete_samba_share.useAction({
      success: () => {
        toast.success(`Removed Samba share "${share?.name}"`);
        onSuccess();
        onClose();
      },
      error: "Failed to remove Samba share",
      toast: SHARES_TOAST_META,
    });

  return (
    <GeneralDialog fullWidth maxWidth="sm" onClose={onClose} open={open}>
      <AppDialogTitle>Remove Samba Share</AppDialogTitle>
      <AppDialogContent>
        <AppDialogContentText>
          Are you sure you want to remove this Samba share?
        </AppDialogContentText>
        {share ? (
          <div style={{ marginTop: 8, marginBottom: 8 }}>
            <AppTypography variant="body2">
              <strong>Name:</strong> {share.name}
            </AppTypography>
            <AppTypography variant="body2">
              <strong>Path:</strong> {share.properties.path}
            </AppTypography>
          </div>
        ) : null}
        <AppAlert severity="warning">
          This will remove the share from smb.conf and reload Samba.
        </AppAlert>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={isPending} onClick={onClose}>
          Cancel
        </AppButton>
        <AppButton
          color="error"
          disabled={isPending}
          onClick={() => {
            if (share) deleteShare({ name: share.name });
          }}
          variant="contained"
        >
          {isPending ? "Removing..." : "Remove"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};
