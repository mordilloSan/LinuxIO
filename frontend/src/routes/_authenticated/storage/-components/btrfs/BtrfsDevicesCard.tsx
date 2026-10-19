import { Icon } from "@iconify/react";
import { useState } from "react";

import {
  linuxio,
  useCallMutation,
  type BtrfsDevice,
  type BtrfsFilesystem,
  type BtrfsOpProgress,
  type TaskProgress,
} from "@/api";
import CardIconHeader from "@/components/cards/CardIconHeader";
import FrostedCard from "@/components/cards/FrostedCard";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppButton from "@/components/ui/AppButton";
import AppCardContent from "@/components/ui/AppCardContent";
import AppChip from "@/components/ui/AppChip";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTypography from "@/components/ui/AppTypography";
import { TASK_TYPE_STORAGE_BTRFS_BALANCE } from "@/constants/backgroundTaskTypes";
import { useActiveTaskRecovery } from "@/hooks/backgroundTasks/useActiveTaskRecovery";
import { formatFileSize } from "@/utils/formaters";

import BtrfsAddDeviceDialog from "./BtrfsAddDeviceDialog";
import BtrfsBalanceDialog from "./BtrfsBalanceDialog";
import { BTRFS_TOAST_META } from "./btrfsShared";

type DeviceDialog =
  | { kind: "add" }
  | { kind: "balance" }
  | { kind: "remove"; device: BtrfsDevice }
  | { kind: "replace"; device: BtrfsDevice };

const deviceName = ({ id, path }: BtrfsDevice) => path || `device ${id}`;

export const deviceErrorCount = ({ errors }: BtrfsDevice) =>
  errors.write +
  errors.read +
  errors.flush +
  errors.corruption +
  errors.generation;

const BtrfsDevicesCard = ({ filesystem }: { filesystem: BtrfsFilesystem }) => {
  const { uuid } = filesystem;
  const [dialog, setDialog] = useState<DeviceDialog | null>(null);
  const [progress, setProgress] = useState<BtrfsOpProgress | null>(null);

  const { mutate: addDevice, isPending: isAdding } = useCallMutation(
    linuxio.storage.btrfs_device_add,
    {
      success: "Device added",
      error: "Failed to add device",
      toast: BTRFS_TOAST_META,
    },
  );
  const { mutate: cancelBalance } = useCallMutation(
    linuxio.storage.btrfs_balance_cancel,
    { error: "Failed to cancel balance", toast: BTRFS_TOAST_META },
  );
  const { mutate: cancelScrub } = useCallMutation(
    linuxio.storage.btrfs_scrub_cancel,
    { error: "Failed to cancel scrub", toast: BTRFS_TOAST_META },
  );
  const taskConfig = (success: string, error: string) => ({
    onProgress: (taskProgress: TaskProgress<BtrfsOpProgress>) =>
      setProgress(taskProgress.detail ?? null),
    success,
    error,
    toast: BTRFS_TOAST_META,
    options: { onSettled: () => setProgress(null) },
  });
  const removeDevice = linuxio.storage.btrfs_device_remove.useTaskStreamAction<
    BtrfsFilesystem,
    TaskProgress<BtrfsOpProgress>
  >(taskConfig("Device removed", "Failed to remove device"));
  const replaceDevice =
    linuxio.storage.btrfs_device_replace.useTaskStreamAction<
      BtrfsFilesystem,
      TaskProgress<BtrfsOpProgress>
    >(taskConfig("Device replaced", "Failed to replace device"));
  const balance = linuxio.storage.btrfs_balance.useTaskStreamAction<
    BtrfsFilesystem,
    TaskProgress<BtrfsOpProgress>
  >(taskConfig("Balance finished", "Failed to balance filesystem"));
  const scrub = linuxio.storage.btrfs_scrub_start.useTaskStreamAction<
    BtrfsFilesystem,
    TaskProgress<BtrfsOpProgress>
  >(taskConfig("Scrub finished", "Failed to scrub filesystem"));

  // Balance is the long task, so it is the only one re-attached after a reload.
  useActiveTaskRecovery({
    type: TASK_TYPE_STORAGE_BTRFS_BALANCE,
    scanKey: uuid,
    match: (task) => task.metadata?.identity?.[0] === uuid,
    onRecover: (task) => balance.watch(task, { uuid }),
  });

  const pending =
    removeDevice.isPending ||
    replaceDevice.isPending ||
    balance.isPending ||
    scrub.isPending;
  const scrubRunning = filesystem.scrub.status === "running";
  const closeDialog = () => setDialog(null);
  const start = (action: () => void) => {
    closeDialog();
    action();
  };

  const columns: AppVirtualTableColumnDef<BtrfsDevice>[] = [
    {
      accessorKey: "path",
      header: "Path",
      cell: ({ row }) => (
        <>
          {row.original.path || `Device ${row.original.id}`}
          {row.original.missing && (
            <AppChip
              color="error"
              label="Missing"
              size="small"
              variant="soft"
            />
          )}
        </>
      ),
    },
    {
      accessorKey: "sizeBytes",
      header: "Size",
      cell: ({ row }) => formatFileSize(row.original.sizeBytes),
      meta: { align: "right" },
    },
    {
      accessorKey: "usedBytes",
      header: "Used",
      cell: ({ row }) => formatFileSize(row.original.usedBytes),
      meta: { align: "right" },
    },
    {
      id: "errors",
      header: "Errors",
      cell: ({ row }) => deviceErrorCount(row.original),
      meta: { align: "right" },
    },
    {
      id: "actions",
      header: "Actions",
      cell: ({ row }) => (
        <div style={{ display: "flex", gap: 8, justifyContent: "flex-end" }}>
          <AppActionIconButton
            disabled={pending}
            icon="mdi:swap-horizontal"
            label="Replace"
            onClick={() => setDialog({ kind: "replace", device: row.original })}
          />
          <AppActionIconButton
            disabled={pending}
            icon="mdi:delete"
            label="Remove"
            onClick={() => setDialog({ kind: "remove", device: row.original })}
          />
        </div>
      ),
      meta: { align: "right" },
    },
  ];

  return (
    <FrostedCard>
      <AppCardContent>
        <CardIconHeader
          icon={<Icon height={24} icon="mdi:harddisk" width={24} />}
          right={
            <div style={{ display: "flex", gap: 4 }}>
              <AppActionIconButton
                disabled={pending}
                icon="mdi:plus"
                label="Add device"
                onClick={() => setDialog({ kind: "add" })}
              />
              <AppActionIconButton
                disabled={pending}
                icon="mdi:tune"
                label="Change profile"
                onClick={() => setDialog({ kind: "balance" })}
              />
              {scrubRunning ? (
                <AppActionIconButton
                  icon="mdi:stop"
                  label="Cancel scrub"
                  onClick={() => cancelScrub({ uuid })}
                />
              ) : (
                <AppActionIconButton
                  disabled={pending}
                  icon="mdi:shield-search"
                  label="Scrub now"
                  onClick={() => scrub.mutate({ uuid })}
                />
              )}
            </div>
          }
          subtitle={`${filesystem.devices.length} device(s)`}
          title="Devices"
        />
        {pending && (
          <div>
            <AppLinearProgress
              value={progress?.percent}
              variant={progress ? "determinate" : "indeterminate"}
            />
            <AppTypography variant="body2">
              {progress?.message ?? "Working..."}
            </AppTypography>
            {balance.isPending && (
              <AppButton
                onClick={() => cancelBalance({ uuid })}
                size="small"
                variant="outlined"
              >
                Cancel
              </AppButton>
            )}
          </div>
        )}
        <AppVirtualTable
          ariaLabel="Btrfs devices"
          columns={columns}
          data={filesystem.devices}
          density="compact"
          emptyMessage="No devices found"
          fillAvailable={false}
          getRowId={(device) => device.path || String(device.id)}
          maxHeight={320}
          variant="embedded"
        />
      </AppCardContent>

      {dialog?.kind === "add" && (
        <BtrfsAddDeviceDialog
          confirmLabel="Add"
          filesystemDevices={filesystem.devices.map(({ path }) => path)}
          isPending={isAdding}
          label="Device"
          onClose={closeDialog}
          onConfirm={(device, force) =>
            addDevice({ uuid, device, force }, { onSuccess: closeDialog })
          }
          title="Add device"
          withForce
        />
      )}
      {dialog?.kind === "replace" && (
        <BtrfsAddDeviceDialog
          confirmLabel="Replace"
          filesystemDevices={filesystem.devices.map(({ path }) => path)}
          label="New device"
          onClose={closeDialog}
          onConfirm={(newDevice) =>
            start(() =>
              replaceDevice.mutate({
                uuid,
                deviceId: dialog.device.id,
                newDevice,
              }),
            )
          }
          title={`Replace ${deviceName(dialog.device)}`}
        />
      )}
      {dialog?.kind === "balance" && (
        <BtrfsBalanceDialog
          filesystem={filesystem}
          onClose={closeDialog}
          onConfirm={(profiles) =>
            start(() => balance.mutate({ uuid, ...profiles }))
          }
        />
      )}
      <GeneralDialog
        fullWidth
        maxWidth="sm"
        onClose={closeDialog}
        open={dialog?.kind === "remove"}
      >
        <AppDialogTitle>Remove device</AppDialogTitle>
        <AppDialogContent>
          <AppDialogContentText>
            {dialog?.kind === "remove" &&
              `Remove ${deviceName(dialog.device)} from the filesystem? Data is migrated to the remaining devices first.`}
          </AppDialogContentText>
        </AppDialogContent>
        <AppDialogActions>
          <AppButton onClick={closeDialog}>Cancel</AppButton>
          <AppButton
            color="error"
            onClick={() => {
              if (dialog?.kind !== "remove") return;
              start(() =>
                removeDevice.mutate({ uuid, deviceId: dialog.device.id }),
              );
            }}
            variant="contained"
          >
            Remove
          </AppButton>
        </AppDialogActions>
      </GeneralDialog>
    </FrostedCard>
  );
};

export default BtrfsDevicesCard;
