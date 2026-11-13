import { Icon } from "@iconify/react";
import { useState } from "react";
import { toast } from "sonner";

import {
  call,
  getMutationErrorMessage,
  linuxio,
  type BtrfsDevice,
  type BtrfsFilesystem,
  type BtrfsOpProgress,
  type TaskProgress,
  type TaskSnapshot,
} from "@/api";
import type { RouteName } from "@/api/generated/route-metadata";
import CardIconHeader from "@/components/cards/CardIconHeader";
import FrostedCard from "@/components/cards/FrostedCard";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppButton from "@/components/ui/AppButton";
import AppCardContent from "@/components/ui/AppCardContent";
import AppChip from "@/components/ui/AppChip";
import AppConfirmDialog from "@/components/ui/AppConfirmDialog";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTypography from "@/components/ui/AppTypography";
import StatusDot from "@/components/ui/StatusDot";
import { TASK_TYPE_STORAGE_BTRFS_BALANCE } from "@/tasks/taskTypes";
import { useActiveTaskRecovery } from "@/tasks/useActiveTaskRecovery";
import { formatFileSize } from "@/utils/formatters";

import BtrfsAddDeviceDialog from "./BtrfsAddDeviceDialog";
import BtrfsBalanceDialog from "./BtrfsBalanceDialog";
import { BTRFS_POOL_TOAST_META } from "./btrfsShared";

type DeviceDialog =
  | { kind: "add" }
  | { kind: "balance" }
  | { kind: "remove"; device: BtrfsDevice }
  | { kind: "replace"; device: BtrfsDevice };

const deviceName = ({ id, path }: BtrfsDevice) => path || `device ${id}`;

// The device jobs, named [uuid, device id] by their task metadata.
const TASK_TYPE_DEVICE_REMOVE: RouteName = "storage.btrfs_device_remove";
const TASK_TYPE_DEVICE_REPLACE: RouteName = "storage.btrfs_device_replace";

type DeviceOp = "remove" | "replace";

// What a cancel leaves behind, said before the job is canceled. The kernel
// stops a removal between chunks and keeps the device a member; it stops a
// replace by destroying the target and keeping the source (docs/btrfs.md,
// Cancel).
const CANCEL_PROMPT: Record<
  DeviceOp,
  { title: string; confirm: string; message: (device: string) => string }
> = {
  remove: {
    title: "Stop removing device",
    confirm: "Stop removal",
    message: (device) =>
      `Stop removing ${device}? It stays in the pool with less data on it: what was already moved off stays on the other devices, so removing it again continues where this one stopped. A kernel older than 5.14 cannot stop a removal, which then runs to the end.`,
  },
  replace: {
    title: "Stop replacing device",
    confirm: "Stop replace",
    message: (device) =>
      `Stop replacing ${device}? It stays in the pool as it was, and the copy made so far on the new disk is discarded: that disk leaves the pool and can be used again. Replacing it again starts the copy from the beginning. A replace already in its last step finishes anyway.`,
  },
};

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

  const { mutate: addDevice, isPending: isAdding } =
    linuxio.storage.btrfs_device_add.useMutation({
      success: "Device added",
      error: "Failed to add device",
      toast: BTRFS_POOL_TOAST_META,
    });
  // A canceled job runs on until the kernel stops at the next chunk.
  const [canceling, setCanceling] = useState(false);
  const [cancelPrompt, setCancelPrompt] = useState<{
    op: DeviceOp;
    taskId: string;
    device: string;
  } | null>(null);
  const { mutate: cancelBalance } =
    linuxio.storage.btrfs_balance_cancel.useMutation({
      error: "Failed to cancel balance",
      toast: BTRFS_POOL_TOAST_META,
      options: { onError: () => setCanceling(false) },
    });
  // This pool's running job of type: a pool runs one balance, removal or
  // replace at a time.
  const findJob = async (type: string) => {
    const jobs = await call("tasks.list", { status: "active" }).catch(() => []);
    return jobs.find(
      (task) =>
        task.type === type &&
        task.state === "running" &&
        task.metadata?.identity?.[0] === uuid,
    );
  };
  // Canceled through the job, so the record gets cancel_requested and a
  // shutdown right after does not resume it.
  const cancelJob = async (taskId: string, failure: string) => {
    setCanceling(true);
    try {
      await call("tasks.cancel", { taskId });
    } catch (error) {
      setCanceling(false);
      toast.error(getMutationErrorMessage(error, failure), {
        meta: BTRFS_POOL_TOAST_META,
      });
    }
  };
  // Only a balance without a running job falls back to the kernel cancel.
  const cancelBalanceJob = async () => {
    setCanceling(true);
    const job = await findJob(TASK_TYPE_STORAGE_BTRFS_BALANCE);
    if (!job) {
      cancelBalance({ uuid });
      return;
    }
    await cancelJob(job.id, "Failed to cancel balance");
  };
  // A removal or replace is canceled only through its job, after a
  // confirmation that names the device and says what the cancel leaves.
  const promptDeviceCancel = async (op: DeviceOp) => {
    const job = await findJob(
      op === "remove" ? TASK_TYPE_DEVICE_REMOVE : TASK_TYPE_DEVICE_REPLACE,
    );
    if (!job) {
      toast.error(
        `No running ${op === "remove" ? "removal" : "replace"} of this pool to cancel`,
        { meta: BTRFS_POOL_TOAST_META },
      );
      return;
    }
    const id = Number(job.metadata?.identity?.[1]);
    const device = filesystem.devices.find((entry) => entry.id === id);
    setCancelPrompt({
      op,
      taskId: job.id,
      device: device ? deviceName(device) : `device ${id}`,
    });
  };
  const { mutate: growDevice, isPending: isGrowing } =
    linuxio.storage.btrfs_device_grow.useMutation({
      success: "Device grown to its disk size",
      error: "Failed to grow device",
      toast: BTRFS_POOL_TOAST_META,
    });
  const taskConfig = (success: string, error: string) => ({
    onProgress: (taskProgress: TaskProgress<BtrfsOpProgress>) =>
      setProgress(taskProgress.detail ?? null),
    success,
    error,
    toast: BTRFS_POOL_TOAST_META,
    options: {
      onSettled: () => {
        setProgress(null);
        setCanceling(false);
        setCancelPrompt(null); // nothing left to cancel
      },
    },
  });
  const removeDevice = linuxio.storage.btrfs_device_remove.useMutation(
    taskConfig("Device removed", "Failed to remove device"),
  );
  const replaceDevice = linuxio.storage.btrfs_device_replace.useMutation(
    taskConfig("Device replaced", "Failed to replace device"),
  );
  const balance = linuxio.storage.btrfs_balance.useMutation(
    taskConfig("Balance finished", "Failed to balance filesystem"),
  );

  // The long jobs are re-attached after a reload, with their cancel.
  const ofThisPool = (task: TaskSnapshot) =>
    task.metadata?.identity?.[0] === uuid;
  useActiveTaskRecovery({
    type: TASK_TYPE_STORAGE_BTRFS_BALANCE,
    scanKey: uuid,
    match: ofThisPool,
    onRecover: (task) => {
      setCanceling(task.cancel_requested === true);
      balance.watch(task.id, { uuid });
    },
  });
  useActiveTaskRecovery({
    type: TASK_TYPE_DEVICE_REMOVE,
    scanKey: uuid,
    match: ofThisPool,
    onRecover: (task) => {
      setCanceling(task.cancel_requested === true);
      removeDevice.watch(task.id, {
        uuid,
        deviceId: Number(task.metadata?.identity?.[1]),
      });
    },
  });
  useActiveTaskRecovery({
    type: TASK_TYPE_DEVICE_REPLACE,
    scanKey: uuid,
    match: ofThisPool,
    onRecover: (task) => {
      setCanceling(task.cancel_requested === true);
      replaceDevice.watch(task.id, {
        uuid,
        deviceId: Number(task.metadata?.identity?.[1]),
        newDevice: "",
      });
    },
  });

  const pending =
    removeDevice.isPending || replaceDevice.isPending || balance.isPending;
  // No profile can live on zero devices, so the last present one must stay.
  const lastDevice =
    filesystem.devices.filter((device) => !device.missing).length <= 1;
  const closeDialog = () => setDialog(null);
  const start = (action: () => void) => {
    closeDialog();
    action();
  };

  const columns: AppVirtualTableColumnDef<BtrfsDevice>[] = [
    {
      accessorKey: "path",
      header: "Disk",
      cell: ({ row }) => {
        const bad = row.original.missing || deviceErrorCount(row.original) > 0;
        return (
          <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
            <StatusDot
              color={
                bad
                  ? "var(--app-palette-error-main)"
                  : "var(--app-palette-success-main)"
              }
              tooltip={bad ? "Needs attention" : "OK"}
            />
            <Icon
              color="var(--app-palette-text-secondary)"
              height={22}
              icon="mdi:harddisk"
              width={22}
            />
            {row.original.path || `Device ${row.original.id}`}
            {row.original.missing && (
              <AppChip
                color="error"
                label="Missing"
                size="small"
                variant="soft"
              />
            )}
          </div>
        );
      },
    },
    {
      accessorKey: "sizeBytes",
      header: "Size",
      cell: ({ row }) => (
        <AppTypography color="text.secondary" variant="caption">
          {formatFileSize(row.original.sizeBytes)}
        </AppTypography>
      ),
      meta: { align: "right" },
    },
    {
      accessorKey: "usedBytes",
      header: "Usage",
      cell: ({ row }) => {
        const { sizeBytes, usedBytes } = row.original;
        return (
          <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
            <AppLinearProgress
              aria-label={`${row.original.path || row.original.id} usage`}
              value={sizeBytes > 0 ? (usedBytes / sizeBytes) * 100 : 0}
              variant="determinate"
            />
            <AppTypography color="text.secondary" variant="caption">
              {formatFileSize(usedBytes)} used ·{" "}
              {formatFileSize(Math.max(sizeBytes - usedBytes, 0))} free
            </AppTypography>
          </div>
        );
      },
      meta: { width: "260px" },
    },
    {
      id: "errors",
      header: "Errors",
      cell: ({ row }) => {
        const errors = deviceErrorCount(row.original);
        return (
          <AppTypography
            color={errors > 0 ? "error" : "text.secondary"}
            variant="caption"
          >
            {errors > 0 ? errors : "—"}
          </AppTypography>
        );
      },
      meta: { align: "right" },
    },
    {
      id: "actions",
      header: "Actions",
      cell: ({ row }) => (
        <div style={{ display: "flex", gap: 8, justifyContent: "flex-end" }}>
          {row.original.growable && (
            <AppActionIconButton
              disabled={pending || isGrowing}
              icon="mdi:arrow-expand-horizontal"
              label="Grow to disk size"
              onClick={() => growDevice({ uuid, deviceId: row.original.id })}
            />
          )}
          <AppActionIconButton
            disabled={pending}
            icon="mdi:swap-horizontal"
            label="Replace"
            onClick={() => setDialog({ kind: "replace", device: row.original })}
          />
          <AppActionIconButton
            disabled={pending || lastDevice}
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
          iconTint
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
            </div>
          }
          subtitle={`${filesystem.devices.length} disk(s) in this filesystem`}
          title="Disks"
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
            <AppButton
              disabled={canceling}
              onClick={() =>
                void (balance.isPending
                  ? cancelBalanceJob()
                  : promptDeviceCancel(
                      removeDevice.isPending ? "remove" : "replace",
                    ))
              }
              size="small"
              variant="outlined"
            >
              {canceling ? "Cancelling…" : "Cancel"}
            </AppButton>
          </div>
        )}
        <AppVirtualTable
          ariaLabel="Btrfs disks"
          columns={columns}
          data={filesystem.devices}
          density="compact"
          emptyMessage="No disks found"
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
      <AppConfirmDialog
        confirmText={cancelPrompt ? CANCEL_PROMPT[cancelPrompt.op].confirm : ""}
        destructive
        message={
          cancelPrompt
            ? CANCEL_PROMPT[cancelPrompt.op].message(cancelPrompt.device)
            : ""
        }
        onClose={() => setCancelPrompt(null)}
        onConfirm={() => {
          if (!cancelPrompt) return;
          setCancelPrompt(null);
          void cancelJob(
            cancelPrompt.taskId,
            `Failed to cancel the ${cancelPrompt.op === "remove" ? "removal" : "replace"}`,
          );
        }}
        open={cancelPrompt !== null}
        title={cancelPrompt ? CANCEL_PROMPT[cancelPrompt.op].title : ""}
      />
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
              `Remove ${deviceName(dialog.device)} from the filesystem? Data is migrated to the remaining devices first, which can take a long time.`}
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
