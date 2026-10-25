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
import AppTypography from "@/components/ui/AppTypography";

import {
  METADATA_HINT,
  metadataProfiles,
  PROFILE_MIN_DEVICES,
  recommendedMetadata,
} from "./btrfsShared";

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
  const data = dataProfile || currentProfile("data");
  const metadataOptions = metadataProfiles(available, data);
  const currentMetadata = currentProfile("metadata");
  // Metadata weaker than the resulting data profile is converted too, unless
  // nothing stronger fits on the present devices.
  const keepMetadata =
    currentMetadata === undefined ||
    metadataOptions.length === 0 ||
    metadataProfiles([currentMetadata], data).length > 0;
  const keptChoice =
    metadataProfile === KEEP_CURRENT
      ? keepMetadata
      : metadataOptions.includes(metadataProfile);
  const metadataValue = keptChoice
    ? metadataProfile
    : (recommendedMetadata(metadataOptions, present) ?? KEEP_CURRENT);

  const profileSelect = (
    label: string,
    kind: string,
    options: string[],
    keep: boolean,
    value: string,
    onChange: (profile: string) => void,
  ) => (
    <AppSelect
      fullWidth
      label={label}
      onChange={(event) => onChange(event.target.value)}
      value={value}
    >
      {keep && (
        <option value={KEEP_CURRENT}>
          {`Keep current (${currentProfile(kind) ?? "unknown"})`}
        </option>
      )}
      {options.map((profile) => (
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
        {profileSelect(
          "Data profile",
          "data",
          available,
          true,
          dataProfile,
          setDataProfile,
        )}
        {profileSelect(
          "Metadata profile",
          "metadata",
          metadataOptions,
          keepMetadata,
          metadataValue,
          setMetadataProfile,
        )}
        <AppTypography color="text.secondary" variant="caption">
          {METADATA_HINT}
        </AppTypography>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton onClick={onClose}>Cancel</AppButton>
        <AppButton
          disabled={
            dataProfile === KEEP_CURRENT && metadataValue === KEEP_CURRENT
          }
          onClick={() =>
            onConfirm({
              ...(dataProfile !== KEEP_CURRENT && { dataProfile }),
              ...(metadataValue !== KEEP_CURRENT && {
                metadataProfile: metadataValue,
              }),
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
