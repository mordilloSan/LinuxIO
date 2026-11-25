import { useQuery } from "@tanstack/react-query";

import { linuxio, type VMUnusedDisk } from "@/api";
import BatchDeleteDialog from "@/components/dialog/BatchDeleteDialog";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import { formatFileSize } from "@/utils/formatters";

import { VM_TOAST } from "./vmShared";

// The Disks tab's prune is a header broom and a confirmation the VM layout
// renders apart: the dialog stays outside the header's action host, which on
// small screens is a menu whose arrow-key handling would reach into it, and
// stays mounted for the whole section, so a prune still running keeps its
// state when the tab changes.

/** Hands over the unused disks it shows, which are the ones a prune deletes. */
export const VMDisksPruneButton = ({
  onClick,
}: {
  onClick: (disks: VMUnusedDisk[]) => void;
}) => {
  const { data: disks = [] } = useQuery(linuxio.virt.unused_disks);
  return (
    <AppActionIconButton
      ariaLabel="Prune unused disks"
      disabled={disks.length === 0}
      icon="mdi:broom"
      iconSize={20}
      label="Prune unused disks"
      onClick={() => onClick(disks)}
    />
  );
};

/**
 * Deletes the listed disks one at a time. `virt.unused_disk_delete` checks
 * each again under its VM's lock, so a disk a VM took in the meantime fails
 * and stays.
 */
export const VMDisksPruneDialog = ({
  disks,
  onClose,
  open,
}: {
  disks: VMUnusedDisk[];
  onClose: () => void;
  open: boolean;
}) => {
  // Configless: the dialog owns the per-disk results and the toast.
  const { mutateAsync: remove } = linuxio.virt.unused_disk_delete.useMutation();
  const totalBytes = disks.reduce((sum, disk) => sum + disk.sizeBytes, 0);
  return (
    <BatchDeleteDialog
      items={disks.map((disk) => ({ key: disk.path, label: disk.name }))}
      noun="disk"
      onClose={onClose}
      onDeleteOne={(item) => remove({ path: item.key })}
      open={open}
      toastMeta={VM_TOAST}
      warning={`Their data cannot be recovered. This frees ${formatFileSize(totalBytes, 1)}.`}
    />
  );
};
