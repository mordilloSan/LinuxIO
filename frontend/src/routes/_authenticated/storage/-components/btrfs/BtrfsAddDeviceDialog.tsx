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

import { FORCE_LABEL, poolDeviceChoices } from "../storageMember";

interface BtrfsAddDeviceDialogProps {
  confirmLabel: string;
  /** Paths of this filesystem's own devices, which are never offered. */
  filesystemDevices: string[];
  isPending?: boolean;
  label: string;
  onClose: () => void;
  onConfirm: (device: string, force: boolean) => void;
  title: string;
  /** Lets the user pick devices that already carry an old filesystem. */
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

  // A device of another pool, array or volume group is listed disabled with
  // its reason, force or not.
  const choices = poolDeviceChoices(
    data?.devices ?? [],
    filesystemDevices,
    force,
  );
  const free = choices.filter(({ reason }) => reason === null);
  const selected = free.some((entry) => entry.device.path === device)
    ? device
    : "";

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
          {choices.map(({ device: entry, reason }) => (
            <option
              disabled={reason !== null}
              key={entry.path}
              value={entry.path}
            >
              {[
                entry.path,
                formatFileSize(entry.sizeBytes),
                entry.model || entry.fsType,
                reason,
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
            label={FORCE_LABEL}
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
