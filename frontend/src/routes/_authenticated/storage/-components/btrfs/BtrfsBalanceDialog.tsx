import { useState } from "react";

import type { BtrfsFilesystem } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppSelect from "@/components/ui/AppSelect";

// Mirrors btrfs.AllowedProfiles and btrfs.MinDevices; raid5/raid6 are never
// offered.
const PROFILE_MIN_DEVICES: Record<string, number> = {
  single: 1,
  dup: 1,
  raid0: 2,
  raid1: 2,
  raid1c3: 3,
  raid1c4: 4,
  raid10: 4,
};

const KEEP_CURRENT = "";

interface BtrfsBalanceDialogProps {
  filesystem: BtrfsFilesystem;
  onClose: () => void;
  onConfirm: (profiles: {
    dataProfile?: string;
    metadataProfile?: string;
  }) => void;
}

const BtrfsBalanceDialog = ({
  filesystem,
  onClose,
  onConfirm,
}: BtrfsBalanceDialogProps) => {
  const [dataProfile, setDataProfile] = useState(KEEP_CURRENT);
  const [metadataProfile, setMetadataProfile] = useState(KEEP_CURRENT);

  const present = filesystem.devices.filter((device) => !device.missing).length;
  const available = Object.keys(PROFILE_MIN_DEVICES).filter(
    (profile) => PROFILE_MIN_DEVICES[profile] <= present,
  );
  const currentProfile = (kind: string) =>
    filesystem.allocation.find((entry) => entry.kind === kind)?.profile;

  const profileSelect = (
    label: string,
    kind: string,
    value: string,
    onChange: (profile: string) => void,
  ) => (
    <AppSelect
      fullWidth
      label={label}
      onChange={(event) => onChange(event.target.value)}
      value={value}
    >
      <option value={KEEP_CURRENT}>
        {`Keep current (${currentProfile(kind) ?? "unknown"})`}
      </option>
      {available.map((profile) => (
        <option key={profile} value={profile}>
          {profile}
        </option>
      ))}
    </AppSelect>
  );

  return (
    <GeneralDialog fullWidth maxWidth="sm" onClose={onClose} open>
      <AppDialogTitle>Change profile</AppDialogTitle>
      <AppDialogContent>
        <AppAlert severity="warning">
          Balance rewrites allocated chunks and can take hours. The filesystem
          stays usable.
        </AppAlert>
        {profileSelect("Data profile", "data", dataProfile, setDataProfile)}
        {profileSelect(
          "Metadata profile",
          "metadata",
          metadataProfile,
          setMetadataProfile,
        )}
      </AppDialogContent>
      <AppDialogActions>
        <AppButton onClick={onClose}>Cancel</AppButton>
        <AppButton
          disabled={
            dataProfile === KEEP_CURRENT && metadataProfile === KEEP_CURRENT
          }
          onClick={() =>
            onConfirm({
              ...(dataProfile !== KEEP_CURRENT && { dataProfile }),
              ...(metadataProfile !== KEEP_CURRENT && { metadataProfile }),
            })
          }
          variant="contained"
        >
          Balance
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default BtrfsBalanceDialog;
