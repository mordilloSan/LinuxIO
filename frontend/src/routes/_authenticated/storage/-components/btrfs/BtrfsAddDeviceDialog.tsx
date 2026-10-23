import { useQuery } from "@tanstack/react-query";
import { useState } from "react";

import { linuxio } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppSelect from "@/components/ui/AppSelect";
import AppSwitch from "@/components/ui/AppSwitch";
import { formatFileSize } from "@/utils/formatters";

interface BtrfsAddDeviceDialogProps {
  confirmLabel: string;
  /** Paths of this filesystem's own devices, which are never offered. */
  filesystemDevices: string[];
  isPending?: boolean;
  label: string;
  onClose: () => void;
  onConfirm: (device: string, force: boolean) => void;
  title: string;
  /** Lets the user pick devices that already carry a filesystem signature. */
  withForce?: boolean;
}

/** Picks a free block device for adding to, or replacing a member of, a filesystem. */
const BtrfsAddDeviceDialog = ({
  confirmLabel,
  filesystemDevices,
  isPending = false,
  label,
  onClose,
  onConfirm,
  title,
  withForce = false,
}: BtrfsAddDeviceDialogProps) => {
  const { data } = useQuery(linuxio.storage.get_topology);
  const [device, setDevice] = useState("");
  const [force, setForce] = useState(false);

  const all = data?.devices ?? [];
  const partitioned = new Set(all.flatMap((entry) => entry.parents));
  const free = all.filter(
    (entry) =>
      entry.mountpoints.length === 0 &&
      !filesystemDevices.includes(entry.path) &&
      !partitioned.has(entry.path) &&
      (entry.fsType === "" || force),
  );
  const selected = free.some((entry) => entry.path === device) ? device : "";

  return (
    <GeneralDialog fullWidth maxWidth="sm" onClose={onClose} open>
      <AppDialogTitle>{title}</AppDialogTitle>
      <AppDialogContent>
        <AppSelect
          fullWidth
          label={label}
          onChange={(event) => setDevice(event.target.value)}
          value={selected}
        >
          <option value="">
            {free.length === 0 ? "No free devices" : "Select a device"}
          </option>
          {free.map((entry) => (
            <option key={entry.path} value={entry.path}>
              {[
                entry.path,
                formatFileSize(entry.sizeBytes),
                entry.model || entry.fsType,
              ]
                .filter(Boolean)
                .join(" · ")}
            </option>
          ))}
        </AppSelect>
        {withForce && (
          <AppFormControlLabel
            control={
              <AppSwitch
                checked={force}
                onChange={(_, checked) => setForce(checked)}
              />
            }
            label="Force (wipe existing filesystem signature)"
          />
        )}
      </AppDialogContent>
      <AppDialogActions>
        <AppButton onClick={onClose}>Cancel</AppButton>
        <AppButton
          disabled={isPending || selected === ""}
          onClick={() => onConfirm(selected, force)}
          variant="contained"
        >
          {confirmLabel}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

export default BtrfsAddDeviceDialog;
