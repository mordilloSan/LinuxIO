import { useQuery } from "@tanstack/react-query";
import { useState } from "react";

import { linuxio, type BtrfsPoolCreateRequest } from "@/api";
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
import AppSwitch from "@/components/ui/AppSwitch";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import { formatFileSize } from "@/utils/formaters";

import { PROFILE_MIN_DEVICES } from "./btrfsShared";

interface BtrfsCreatePoolDialogProps {
  isPending?: boolean;
  onClose: () => void;
  onConfirm: (request: BtrfsPoolCreateRequest) => void;
  /** Paths of disks in a mounted btrfs filesystem, which are never offered. */
  pooledDevices: string[];
}

// Mirrors the mkfs.btrfs defaults: redundancy as soon as there is a second disk.
const defaultProfile = (kind: "data" | "metadata", disks: number) =>
  disks >= 2 ? "raid1" : kind === "data" ? "single" : "dup";

/** Formats free disks as one btrfs filesystem with the chosen profiles. */
const BtrfsCreatePoolDialog = ({
  isPending = false,
  onClose,
  onConfirm,
  pooledDevices,
}: BtrfsCreatePoolDialogProps) => {
  const { data } = useQuery(linuxio.storage.get_topology);
  const [label, setLabel] = useState("");
  // null follows the name until the mount point is edited.
  const [mountpoint, setMountpoint] = useState<string | null>(null);
  const [dataProfile, setDataProfile] = useState("");
  const [metadataProfile, setMetadataProfile] = useState("");
  const [picked, setPicked] = useState<string[]>([]);
  const [force, setForce] = useState(false);

  const all = data?.devices ?? [];
  const partitioned = new Set(all.flatMap((entry) => entry.parents));
  const free = all.filter(
    (entry) =>
      entry.mountpoints.length === 0 &&
      !pooledDevices.includes(entry.path) &&
      !partitioned.has(entry.path) &&
      (entry.fsType === "" || force),
  );
  const devices = picked.filter((path) =>
    free.some((entry) => entry.path === path),
  );
  const profiles = Object.keys(PROFILE_MIN_DEVICES).filter(
    (profile) => PROFILE_MIN_DEVICES[profile] <= Math.max(devices.length, 1),
  );
  // A profile the disk count no longer allows falls back to the default.
  const chosen = (profile: string, kind: "data" | "metadata") =>
    profiles.includes(profile) ? profile : defaultProfile(kind, devices.length);
  const dataValue = chosen(dataProfile, "data");
  const metadataValue = chosen(metadataProfile, "metadata");
  const name = label.trim();
  const target = mountpoint ?? `/mnt/${name}`;
  const valid = name !== "" && devices.length > 0 && target.startsWith("/");

  const toggle = (path: string, checked: boolean) =>
    setPicked((current) =>
      checked ? [...current, path] : current.filter((entry) => entry !== path),
    );

  const profileSelect = (
    title: string,
    value: string,
    onChange: (profile: string) => void,
  ) => (
    <AppSelect
      fullWidth
      label={title}
      onChange={(event) => onChange(event.target.value)}
      value={value}
    >
      {profiles.map((profile) => (
        <option key={profile} value={profile}>
          {profile}
        </option>
      ))}
    </AppSelect>
  );

  return (
    <GeneralDialog fullWidth maxWidth="sm" onClose={onClose} open>
      <AppDialogTitle>Create pool</AppDialogTitle>
      <AppDialogContent>
        <div
          style={{
            display: "flex",
            flexDirection: "column",
            gap: "var(--app-space-12)",
          }}
        >
          <AppAlert severity="warning">
            Everything on the selected disks is erased.
          </AppAlert>
          <AppTextField
            fullWidth
            label="Name"
            onChange={(event) => setLabel(event.target.value)}
            value={label}
          />
          <AppTextField
            fullWidth
            helperText="Mounted here and added to /etc/fstab"
            label="Mount point"
            onChange={(event) => setMountpoint(event.target.value)}
            value={target}
          />
          {profileSelect("Data profile", dataValue, setDataProfile)}
          {profileSelect("Metadata profile", metadataValue, setMetadataProfile)}
          <div>
            <AppTypography variant="subtitle2">Disks</AppTypography>
            {free.length === 0 && (
              <AppTypography color="text.secondary" variant="body2">
                No free disks
              </AppTypography>
            )}
            {free.map((entry) => (
              <AppFormControlLabel
                control={
                  <AppCheckbox
                    checked={devices.includes(entry.path)}
                    onChange={(_, checked) => toggle(entry.path, checked)}
                  />
                }
                key={entry.path}
                label={[
                  entry.path,
                  formatFileSize(entry.sizeBytes),
                  entry.model || entry.fsType,
                ]
                  .filter(Boolean)
                  .join(" · ")}
              />
            ))}
          </div>
          <AppFormControlLabel
            control={
              <AppSwitch
                checked={force}
                onChange={(_, checked) => setForce(checked)}
              />
            }
            label="Force (wipe existing filesystem signatures)"
          />
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton onClick={onClose}>Cancel</AppButton>
        <AppButton
          disabled={isPending || !valid}
          onClick={() =>
            onConfirm({
              label: name,
              mountpoint: target,
              dataProfile: dataValue,
              metadataProfile: metadataValue,
              devices,
              force,
            })
          }
          variant="contained"
        >
          Create pool
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default BtrfsCreatePoolDialog;
