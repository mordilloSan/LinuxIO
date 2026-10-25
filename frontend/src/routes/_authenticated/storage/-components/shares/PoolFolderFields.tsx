import type { BtrfsFilesystem } from "@/api";
import AppCheckbox from "@/components/ui/AppCheckbox";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppSelect from "@/components/ui/AppSelect";
import AppTextField from "@/components/ui/AppTextField";

import { TRANSIENT_TOP } from "../btrfs/btrfsShared";

/** A pool a new shared folder can live on: mounted at its top level. */
interface SharePool {
  uuid: string;
  name: string;
  mountpoint: string;
}

export interface PoolFolder {
  uuid: string;
  name: string;
  /** Size limit in GiB as typed; empty for none. */
  limitGiB: string;
  snapshots: boolean;
}

/** Pools with a permanent mount of their top level, where a new subvolume appears as a folder. */
export const sharePools = (filesystems: BtrfsFilesystem[]): SharePool[] =>
  filesystems.flatMap((fs) => {
    const top = fs.mountpoints.find(
      (m) => m.subvolId === 5 && !m.mountpoint.startsWith(TRANSIENT_TOP),
    );
    return top
      ? [
          {
            uuid: fs.uuid,
            name: fs.label || fs.uuid,
            mountpoint: top.mountpoint,
          },
        ]
      : [];
  });

/** Why a folder cannot be created, or null; names must stay valid share paths. */
export const poolFolderError = (folder: PoolFolder): string | null => {
  if (!folder.uuid) return "Choose a pool";
  if (!/^[A-Za-z0-9_.-]+$/.test(folder.name) || /^\.{1,2}$/.test(folder.name))
    return "Folder name may use letters, digits, dot, dash and underscore";
  if (folder.name === "linuxio-snapshots")
    return "That name is reserved for snapshots";
  const limit = folder.limitGiB.trim();
  if (limit !== "" && !(Number(limit) > 0))
    return "Size limit must be a positive number of GiB";
  return null;
};

interface PoolFolderFieldsProps {
  folder: PoolFolder;
  onChange: (folder: PoolFolder) => void;
  pools: SharePool[];
}

/** Pool, folder name, size limit and automatic snapshots for a new shared folder. */
const PoolFolderFields = ({
  folder,
  onChange,
  pools,
}: PoolFolderFieldsProps) => {
  const pool = pools.find((p) => p.uuid === folder.uuid);
  return (
    <>
      <AppSelect
        fullWidth
        label="Pool"
        onChange={(event) => onChange({ ...folder, uuid: event.target.value })}
        value={folder.uuid}
      >
        <option value="">Select a pool</option>
        {pools.map((p) => (
          <option key={p.uuid} value={p.uuid}>
            {`${p.name} · ${p.mountpoint}`}
          </option>
        ))}
      </AppSelect>
      <AppTextField
        fullWidth
        helperText={
          pool && folder.name
            ? `Created as ${pool.mountpoint}/${folder.name}`
            : undefined
        }
        label="Folder name"
        onChange={(event) => onChange({ ...folder, name: event.target.value })}
        size="small"
        value={folder.name}
      />
      <AppTextField
        fullWidth
        helperText="Optional. Turns on quotas for the pool."
        label="Size limit (GiB)"
        onChange={(event) =>
          onChange({ ...folder, limitGiB: event.target.value })
        }
        size="small"
        type="number"
        value={folder.limitGiB}
      />
      <AppFormControlLabel
        control={
          <AppCheckbox
            checked={folder.snapshots}
            onChange={(event) =>
              onChange({ ...folder, snapshots: event.target.checked })
            }
          />
        }
        label="Automatic snapshots (hourly, keeping 24 hourly, 7 daily, 4 weekly, 6 monthly)"
      />
    </>
  );
};

export default PoolFolderFields;
