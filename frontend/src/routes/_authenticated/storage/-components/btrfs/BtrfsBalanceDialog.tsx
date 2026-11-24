import { useState } from "react";

import type { BtrfsBalanceRequest, BtrfsFilesystem } from "@/api";
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
  profilesFor,
  recommendedMetadata,
} from "./btrfsShared";

const KEEP_CURRENT = "";

type BalanceMode = "usage" | "all" | "convert";

const MODES: Record<BalanceMode, { label: string; help: string }> = {
  usage: {
    label: "Lightly used data chunks",
    help: "Rewrites only the data chunks filled up to the limit below, so their space can be used again. Quick; use it when the pool reports full while free space still shows.",
  },
  all: {
    label: "Everything",
    help: "Rewrites every chunk, which spreads data and metadata over all disks, as after adding one. Takes the longest.",
  },
  convert: {
    label: "Change profile",
    help: "Rewrites chunks into another profile, such as raid1 after adding a second disk.",
  },
};

// The fill limits offered for a light balance; 50% is Reclaim space's.
const USAGE_LIMITS = [10, 25, 50, 75];

/** What to balance: nothing set balances everything. */
export type BalanceChoice = Omit<BtrfsBalanceRequest, "uuid">;

interface BtrfsBalanceDialogProps {
  filesystem: BtrfsFilesystem;
  onClose: () => void;
  onConfirm: (choice: BalanceChoice) => void;
}

const BtrfsBalanceDialog = ({
  filesystem,
  onClose,
  onConfirm,
}: BtrfsBalanceDialogProps) => {
  const [chosenMode, setMode] = useState<BalanceMode>("usage");
  // A balance that is not a convert writes what it moves with one copy while
  // a disk is missing, so a degraded pool only changes profile.
  const mode = filesystem.degraded ? "convert" : chosenMode;
  const modes = filesystem.degraded
    ? (["convert"] as const)
    : (Object.keys(MODES) as BalanceMode[]);
  const [usage, setUsage] = useState(50);
  const [dataProfile, setDataProfile] = useState(KEEP_CURRENT);
  const [metadataProfile, setMetadataProfile] = useState(KEEP_CURRENT);

  const present = filesystem.devices.filter((device) => !device.missing).length;
  const available = profilesFor(present);
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

  // A profile change needs a profile to change to.
  const choice: BalanceChoice | null =
    mode === "all"
      ? {}
      : mode === "usage"
        ? { dataUsage: usage }
        : dataProfile === KEEP_CURRENT && metadataValue === KEEP_CURRENT
          ? null
          : {
              ...(dataProfile !== KEEP_CURRENT && { dataProfile }),
              ...(metadataValue !== KEEP_CURRENT && {
                metadataProfile: metadataValue,
              }),
            };

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
      <AppDialogTitle>Balance</AppDialogTitle>
      <AppDialogContent>
        <AppAlert severity="warning">
          A balance rewrites chunks and can take hours on a large pool. The
          filesystem stays usable, and the Disks card can cancel the balance.
        </AppAlert>
        <AppSelect
          fullWidth
          label="What to balance"
          onChange={(event) => setMode(event.target.value as BalanceMode)}
          value={mode}
        >
          {modes.map((key) => (
            <option key={key} value={key}>
              {MODES[key].label}
            </option>
          ))}
        </AppSelect>
        <AppTypography color="text.secondary" variant="caption">
          {filesystem.degraded
            ? "The pool is missing a disk, so it can only change profile: any other balance would rewrite chunks with fewer copies. Replace the disk first."
            : MODES[mode].help}
        </AppTypography>
        {mode === "usage" && (
          <AppSelect
            fullWidth
            label="Fill limit"
            onChange={(event) => setUsage(Number(event.target.value))}
            value={String(usage)}
          >
            {USAGE_LIMITS.map((limit) => (
              <option key={limit} value={String(limit)}>
                {`Chunks up to ${limit}% full`}
              </option>
            ))}
          </AppSelect>
        )}
        {mode === "convert" && (
          <>
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
          </>
        )}
      </AppDialogContent>
      <AppDialogActions>
        <AppButton onClick={onClose}>Cancel</AppButton>
        <AppButton
          disabled={choice === null}
          onClick={() => choice && onConfirm(choice)}
          variant="contained"
        >
          Balance
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default BtrfsBalanceDialog;
