import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppTypography from "@/components/ui/AppTypography";

interface PeerQrDialogProps {
  downloading: boolean;
  loading: boolean;
  onClose: () => void;
  onDownload: () => void;
  open: boolean;
  peerName: string;
  qrcode: string | undefined;
}

/**
 * A peer's configuration as a QR code for the WireGuard app on a phone, and
 * as a file for a computer.
 */
const PeerQrDialog = ({
  downloading,
  loading,
  onClose,
  onDownload,
  open,
  peerName,
  qrcode,
}: PeerQrDialogProps) => (
  <GeneralDialog onClose={onClose} open={open}>
    <AppDialogTitle>Connect {peerName}</AppDialogTitle>
    <AppDialogContent>
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          gap: "var(--app-space-8)",
        }}
      >
        <AppTypography variant="body1">
          In the WireGuard app, tap + and choose Scan from QR code.
        </AppTypography>
        <AppTypography color="text.secondary" variant="body2">
          On a computer, download the configuration file and import it.
        </AppTypography>
        {loading ? (
          <AppTypography>Loading QR code...</AppTypography>
        ) : qrcode ? (
          <img
            alt={`QR code for ${peerName}`}
            src={qrcode}
            style={{
              width: 300,
              height: 300,
              maxWidth: "100%",
              display: "block",
              alignSelf: "center",
            }}
          />
        ) : (
          <AppTypography>Failed to load QR code</AppTypography>
        )}
      </div>
    </AppDialogContent>
    <AppDialogActions>
      <AppButton disabled={downloading} onClick={onDownload}>
        {downloading ? "Downloading…" : "Download configuration"}
      </AppButton>
      <AppButton onClick={onClose} variant="contained">
        Done
      </AppButton>
    </AppDialogActions>
  </GeneralDialog>
);

export default PeerQrDialog;
