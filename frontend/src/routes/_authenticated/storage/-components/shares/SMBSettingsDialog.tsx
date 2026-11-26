import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";

import { getMutationErrorMessage, linuxio, type SMBSettings } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppCheckbox from "@/components/ui/AppCheckbox";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppSelect from "@/components/ui/AppSelect";
import AppTypography from "@/components/ui/AppTypography";
import { useCapability } from "@/hooks/useCapabilities";

// The protocols and the encryption LinuxIO offers; smb.conf can hold others.
const PROTOCOLS = ["SMB2", "SMB3"];
const ENCRYPTION: { value: string; label: string }[] = [
  { value: "off", label: "Off" },
  { value: "default", label: "When the client asks" },
  { value: "desired", label: "Asked of every client" },
  { value: "required", label: "Required" },
];

interface SMBSettingsDialogProps {
  onClose: () => void;
  onExited?: () => void;
  open: boolean;
}

/** Where a missing discovery service is installed. */
const InstallHint = ({ what }: { what: string }) => (
  <AppTypography color="text.secondary" variant="caption">
    {what}{" "}
    <Link search={{ tab: "capabilities" }} to="/settings">
      Install it in Settings
    </Link>
  </AppTypography>
);

/**
 * The settings of the SMB service as a whole: the oldest protocol it speaks,
 * whether it encrypts, and whether the server shows up by itself on Macs and
 * on Windows. The caller mounts it afresh each time it opens.
 */
const SMBSettingsDialog = ({
  onClose,
  onExited,
  open,
}: SMBSettingsDialogProps) => {
  const avahi = useCapability("avahiAvailable");
  const wsdd = useCapability("wsddAvailable");
  const { data: saved } = useQuery({
    ...linuxio.shares.get_smb_settings,
    enabled: open,
  });
  // Null until it is edited: the fields start from what the server has.
  const [edited, setEdited] = useState<SMBSettings | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const value = edited ?? saved;
  const { mutate: save, isPending } =
    linuxio.shares.set_smb_settings.useMutation({
      success: "SMB settings saved",
      error: (error) =>
        setProblem(
          getMutationErrorMessage(error, "Failed to save the SMB settings"),
        ),
      options: { onSuccess: onClose },
    });
  // A protocol set by hand in smb.conf that LinuxIO does not offer.
  const offered = value ? PROTOCOLS.includes(value.minProtocol) : false;

  return (
    <GeneralDialog
      fullWidth
      maxWidth="sm"
      onClose={onClose}
      open={open}
      slotProps={{ transition: { onExited } }}
    >
      <AppDialogTitle>SMB service</AppDialogTitle>
      <AppDialogContent>
        {value && (
          <div className="app-dialog-fields">
            <AppSelect
              fullWidth
              label="Oldest protocol"
              onChange={(event) =>
                setEdited({ ...value, minProtocol: event.target.value })
              }
              value={value.minProtocol}
            >
              {!offered && (
                <option value={value.minProtocol}>{value.minProtocol}</option>
              )}
              {PROTOCOLS.map((protocol) => (
                <option key={protocol} value={protocol}>
                  {protocol}
                </option>
              ))}
            </AppSelect>
            <AppTypography color="text.secondary" variant="caption">
              Windows 7 and later and every current Mac speak SMB2; SMB3 adds
              encryption.
            </AppTypography>
            {!offered && (
              <AppAlert severity="warning">
                {`${value.minProtocol} is older than LinuxIO offers, and was set in smb.conf by hand. Choose SMB2 or SMB3 to save.`}
              </AppAlert>
            )}
            <AppSelect
              fullWidth
              label="Encryption"
              onChange={(event) =>
                setEdited({ ...value, encryption: event.target.value })
              }
              value={value.encryption}
            >
              {ENCRYPTION.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </AppSelect>
            <AppTypography color="text.secondary" variant="caption">
              Required keeps out clients that cannot encrypt, which need SMB3.
            </AppTypography>
            <AppFormControlLabel
              control={
                <AppCheckbox
                  checked={value.macDiscovery}
                  onChange={(event) =>
                    setEdited({ ...value, macDiscovery: event.target.checked })
                  }
                />
              }
              disabled={avahi.status === "unavailable"}
              label="Show this server to Macs in Finder"
            />
            {avahi.status === "unavailable" && (
              <InstallHint what="Macs find it through Avahi, which is not running." />
            )}
            {saved && value.macDiscovery !== saved.macDiscovery && (
              <AppTypography color="text.secondary" variant="caption">
                Saving this restarts the SMB service, which is when it tells
                Macs. Open connections are cut for a moment.
              </AppTypography>
            )}
            <AppFormControlLabel
              control={
                <AppCheckbox
                  checked={value.windowsDiscovery}
                  onChange={(event) =>
                    setEdited({
                      ...value,
                      windowsDiscovery: event.target.checked,
                    })
                  }
                />
              }
              disabled={wsdd.status === "unavailable"}
              label="Show this server to Windows in Network"
            />
            {wsdd.status === "unavailable" && (
              <InstallHint what="Windows finds it through the WS-Discovery service, which is not installed." />
            )}
            {problem && <AppAlert severity="error">{problem}</AppAlert>}
          </div>
        )}
      </AppDialogContent>
      <AppDialogActions>
        <AppButton onClick={onClose}>Cancel</AppButton>
        <AppButton
          disabled={isPending || !value || !offered}
          onClick={() => {
            if (!value) return;
            setProblem(null);
            save(value);
          }}
          variant="contained"
        >
          Save
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default SMBSettingsDialog;
