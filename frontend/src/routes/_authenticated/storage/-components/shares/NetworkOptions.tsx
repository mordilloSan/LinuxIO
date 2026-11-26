import AppCheckbox from "@/components/ui/AppCheckbox";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";

/** What a shared folder offers Windows and Macs beyond its files. */
export interface NetworkValue {
  previousVersions: boolean;
  recycleBin: boolean;
  timeMachine: boolean;
  /** What Time Machine's backups may take, in GiB as typed; empty for no limit. */
  timeMachineGiB: string;
}

const BYTES_PER_GIB = 1024 ** 3;
const BYTES_PER_MIB = 1024 ** 2;

/** Why the tab's values cannot be saved, or null. */
export const networkProblem = (value: NetworkValue): string | null => {
  const size = value.timeMachineGiB.trim();
  if (!value.timeMachine || size === "") return null;
  // A size the server can hold and Samba can be told, in whole MiB.
  const bytes = Math.round(Number(size) * BYTES_PER_GIB);
  return Number.isSafeInteger(bytes) && bytes >= BYTES_PER_MIB
    ? null
    : "Time Machine size limit must be a positive number of GiB";
};

/** The size limit in bytes, undefined for none. */
export const timeMachineMaxBytes = (
  value: NetworkValue,
): number | undefined => {
  const size = value.timeMachineGiB.trim();
  return value.timeMachine && size !== ""
    ? Math.round(Number(size) * BYTES_PER_GIB)
    : undefined;
};

/** The tab's values of a folder as it is saved. */
export const networkOf = (folder: {
  previousVersions: boolean;
  recycleBin: boolean;
  timeMachine: boolean;
  timeMachineMaxBytes?: number;
}): NetworkValue => ({
  previousVersions: folder.previousVersions,
  recycleBin: folder.recycleBin,
  timeMachine: folder.timeMachine,
  // To the MiB, which is what Samba is told: 0.3 GiB reads back as 0.3.
  timeMachineGiB:
    folder.timeMachineMaxBytes === undefined
      ? ""
      : String(Number((folder.timeMachineMaxBytes / BYTES_PER_GIB).toFixed(4))),
});

interface NetworkOptionsProps {
  /**
   * Without SMB there is nobody to offer them to: a switch can then only be
   * turned off, which a folder shared over NFS alone needs for its bin.
   */
  smb: boolean;
  onChange: (value: NetworkValue) => void;
  value: NetworkValue;
}

/** The Network tab of a shared folder: what Windows and Macs are offered. */
const NetworkOptions = ({ smb, onChange, value }: NetworkOptionsProps) => (
  <>
    {!smb && (
      <AppTypography color="text.secondary" variant="body2">
        These apply when the folder is shared with Windows and Mac (SMB).
      </AppTypography>
    )}
    <AppFormControlLabel
      control={
        <AppCheckbox
          checked={value.previousVersions}
          onChange={(event) =>
            onChange({ ...value, previousVersions: event.target.checked })
          }
        />
      }
      disabled={!smb && !value.previousVersions}
      label="Windows and Mac can open earlier versions from snapshots"
    />
    <AppFormControlLabel
      control={
        <AppCheckbox
          checked={value.recycleBin}
          onChange={(event) =>
            onChange({ ...value, recycleBin: event.target.checked })
          }
        />
      }
      disabled={!smb && !value.recycleBin}
      label="Keep files deleted over the network in a recycle bin for 30 days"
    />
    {value.recycleBin && (
      <AppTypography color="text.secondary" variant="caption">
        They go to the folder #recycle at its top, which everyone with access
        can open. Files deleted on the server itself or over NFS are not kept.
      </AppTypography>
    )}
    <AppFormControlLabel
      control={
        <AppCheckbox
          checked={value.timeMachine}
          onChange={(event) =>
            onChange({ ...value, timeMachine: event.target.checked })
          }
        />
      }
      disabled={!smb && !value.timeMachine}
      label="Use as a Time Machine backup destination"
    />
    {value.timeMachine && (
      <AppTextField
        fullWidth
        helperText="Optional. A Mac removes its oldest backups to stay below it."
        label="Time Machine size limit (GiB)"
        onChange={(event) =>
          onChange({ ...value, timeMachineGiB: event.target.value })
        }
        size="small"
        type="number"
        value={value.timeMachineGiB}
      />
    )}
  </>
);

export default NetworkOptions;
