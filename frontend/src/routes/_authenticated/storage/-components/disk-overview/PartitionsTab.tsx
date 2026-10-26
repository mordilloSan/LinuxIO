import { useQuery } from "@tanstack/react-query";
import { useState } from "react";

import {
  linuxio,
  useCallMutation,
  getMutationErrorMessage,
  type BtrfsOpProgress,
  type StorageBlockDevice,
  type SuccessPathResponse,
  type SuccessResponse,
  type TaskProgress,
} from "@/api";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppButton from "@/components/ui/AppButton";
import AppConfirmDialog from "@/components/ui/AppConfirmDialog";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import useAuth from "@/hooks/useAuth";
import { useCapability } from "@/hooks/useCapabilities";
import {
  TASK_TYPE_STORAGE_PARTITION_CREATE,
  TASK_TYPE_STORAGE_PARTITION_FORMAT,
} from "@/tasks/taskTypes";
import { useActiveTaskRecovery } from "@/tasks/useActiveTaskRecovery";
import { formatFileSize } from "@/utils/formatters";

import {
  CreatePartitionDialog,
  FormatPartitionDialog,
  MountPartitionDialog,
} from "./PartitionDialogs";
import {
  diskLayout,
  initializeBlocker,
  type LayoutRow,
} from "./partitionLayout";

type PartitionDialog =
  | { kind: "create"; startBytes: number; sizeBytes: number }
  | { kind: "format"; device: StorageBlockDevice }
  | { kind: "mount"; device: StorageBlockDevice }
  | { kind: "delete"; device: StorageBlockDevice }
  | { kind: "initialize" };

// Filesystems with nothing to mount: swap, containers for other layers.
const NOT_MOUNTABLE = new Set([
  "swap",
  "crypto_LUKS",
  "LVM2_member",
  "linux_raid_member",
  "zfs_member",
]);

const TABLE_NAMES: Record<string, string> = { gpt: "GPT", dos: "MBR" };

const describe = (device: StorageBlockDevice) =>
  [formatFileSize(device.sizeBytes), device.fsType].filter(Boolean).join(", ");

/** The partitions of one disk, with what can be done to each. */
const PartitionsTab = ({ driveName }: { driveName: string }) => {
  const { privileged } = useAuth();
  const { isEnabled } = useCapability("btrfsAvailable");
  const poolsEnabled = privileged && isEnabled;
  const { data: topology } = useQuery(linuxio.storage.get_topology);
  const { data: btrfs = [] } = useQuery({
    ...linuxio.storage.btrfs_list,
    enabled: poolsEnabled,
  });
  const { data: offline = [] } = useQuery({
    ...linuxio.storage.btrfs_pools_offline,
    enabled: poolsEnabled,
  });
  const [dialog, setDialog] = useState<PartitionDialog | null>(null);
  const [step, setStep] = useState<BtrfsOpProgress | null>(null);
  // The last create or format failure, shown in its dialog; the bridge's
  // message says what to change, e.g. to tick Overwrite.
  const [error, setError] = useState<string | null>(null);
  const closeDialog = () => {
    setDialog(null);
    setError(null);
  };
  // A task dismissed with its dialog, or recovered, can fail with none open.
  const openDialog = (next: PartitionDialog) => {
    setError(null);
    setDialog(next);
  };

  const devices = topology?.devices ?? [];
  const disk = devices.find(
    (device) => device.type === "disk" && device.name === driveName,
  );
  const rows = disk ? diskLayout(disk, devices, btrfs, offline) : [];

  // Tasks, not calls: the bridge trims the whole partition before mkfs,
  // which can take minutes on a large SSD.
  const taskConfig = (success: string, failure: string) => ({
    onProgress: (progress: TaskProgress<BtrfsOpProgress>) =>
      setStep(progress.detail ?? null),
    success,
    error: failure,
    options: {
      onMutate: () => setError(null),
      onError: (err: Error) => setError(getMutationErrorMessage(err, failure)),
      onSettled: () => setStep(null),
    },
  });
  const create = linuxio.storage.partition_create.useTaskStreamAction<
    SuccessPathResponse,
    TaskProgress<BtrfsOpProgress>
  >(taskConfig("Partition created", "Failed to create partition"));
  const format = linuxio.storage.partition_format.useTaskStreamAction<
    SuccessResponse,
    TaskProgress<BtrfsOpProgress>
  >(taskConfig("Partition formatted", "Failed to format partition"));
  const { mutate: deletePartition, isPending: isDeleting } = useCallMutation(
    linuxio.storage.partition_delete,
    { success: "Partition deleted", error: "Failed to delete partition" },
  );
  const { mutate: mountPartition, isPending: isMounting } = useCallMutation(
    linuxio.storage.partition_mount,
    { success: "Filesystem mounted", error: "Failed to mount filesystem" },
  );
  const { mutate: initializeDisk, isPending: isInitializing } = useCallMutation(
    linuxio.storage.disk_initialize,
    { success: "Disk initialized", error: "Failed to initialize disk" },
  );

  // After a reload, adopt a running create or format of this disk so its
  // progress and completion toast come back.
  useActiveTaskRecovery({
    type: TASK_TYPE_STORAGE_PARTITION_CREATE,
    scanKey: disk?.path ?? null,
    match: (task) => task.metadata?.device === disk?.path,
    onRecover: (task) =>
      create.watch(task, {
        device: task.metadata?.device ?? "",
        startBytes: 0,
        sizeBytes: 0,
      }),
  });
  useActiveTaskRecovery({
    type: TASK_TYPE_STORAGE_PARTITION_FORMAT,
    scanKey: disk?.path ?? null,
    match: (task) =>
      rows.some(
        (row) =>
          row.kind === "partition" && row.device.path === task.metadata?.device,
      ),
    onRecover: (task) =>
      format.watch(task, {
        device: task.metadata?.device ?? "",
        fsType: "",
        label: "",
        overwrite: false,
      }),
  });

  if (!disk) return null;
  if (disk.sizeBytes === 0) {
    return (
      <AppTypography color="text.secondary">
        No media in this drive.
      </AppTypography>
    );
  }

  const running = create.isPending || format.isPending;
  const working = running || isDeleting || isMounting || isInitializing;
  const gpt = disk.ptType === "gpt";
  const blocker = initializeBlocker(disk, rows, btrfs, offline);

  const columns: AppVirtualTableColumnDef<LayoutRow>[] = [
    {
      id: "name",
      header: "Partition",
      cell: ({ row: { original } }) =>
        original.kind === "free" ? (
          <AppTypography color="text.secondary" variant="body2">
            Free space
          </AppTypography>
        ) : (
          original.device.name
        ),
    },
    {
      id: "size",
      header: "Size",
      cell: ({ row: { original } }) =>
        formatFileSize(
          original.kind === "free"
            ? original.sizeBytes
            : original.device.sizeBytes,
        ),
      meta: { align: "right" },
    },
    {
      id: "filesystem",
      header: "Filesystem",
      cell: ({ row: { original } }) =>
        original.kind === "free"
          ? ""
          : [original.device.fsType, original.device.label]
              .filter(Boolean)
              .join(" · "),
    },
    {
      id: "usedBy",
      header: "Used by",
      cell: ({ row: { original } }) =>
        original.kind === "free"
          ? ""
          : original.device.mountpoints.join(", ") || original.reason || "",
    },
    {
      id: "actions",
      header: "Actions",
      cell: ({ row: { original } }) => {
        if (!privileged) return null;
        if (original.kind === "free") {
          return (
            <AppActionIconButton
              ariaLabel="Create partition"
              disabled={working}
              icon="mdi:plus"
              label="Create partition"
              onClick={() =>
                openDialog({
                  kind: "create",
                  startBytes: original.startBytes,
                  sizeBytes: original.sizeBytes,
                })
              }
            />
          );
        }
        const { device, reason, pool } = original;
        const mountable =
          device.type === "part" &&
          device.fsType !== "" &&
          !NOT_MOUNTABLE.has(device.fsType) &&
          device.mountpoints.length === 0 &&
          pool === null;
        return (
          <div
            style={{
              display: "flex",
              gap: "var(--app-space-8)",
              justifyContent: "flex-end",
            }}
          >
            {mountable && (
              <AppActionIconButton
                ariaLabel="Mount"
                disabled={working || Boolean(device.busy)}
                icon="mdi:folder-open"
                label={device.busy ?? "Mount"}
                onClick={() => openDialog({ kind: "mount", device })}
              />
            )}
            <AppActionIconButton
              ariaLabel="Format"
              disabled={working || reason !== null}
              icon="mdi:eraser"
              label={reason ?? "Format"}
              onClick={() => openDialog({ kind: "format", device })}
            />
            {gpt && (
              <AppActionIconButton
                ariaLabel="Delete"
                disabled={working || reason !== null}
                icon="mdi:delete"
                label={reason ?? "Delete"}
                onClick={() => openDialog({ kind: "delete", device })}
              />
            )}
          </div>
        );
      },
      meta: { align: "right" },
    },
  ];

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: "var(--app-space-12)",
      }}
    >
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
        }}
      >
        <AppTypography color="text.secondary" variant="body2">
          {disk.ptType
            ? `Partition table: ${TABLE_NAMES[disk.ptType] ?? disk.ptType.toUpperCase()}`
            : "No partition table"}
        </AppTypography>
        {privileged && (
          <AppTooltip title={blocker ?? ""}>
            <span>
              <AppButton
                color="error"
                disabled={working || blocker !== null}
                onClick={() => openDialog({ kind: "initialize" })}
                size="small"
                variant="outlined"
              >
                Initialize disk
              </AppButton>
            </span>
          </AppTooltip>
        )}
      </div>
      {running && !dialog && (
        <div role="status">
          <AppLinearProgress
            aria-label="Working"
            value={step?.percent}
            variant={step?.percent ? "determinate" : "indeterminate"}
          />
          <AppTypography color="text.secondary" variant="caption">
            {step?.message ?? "Working..."}
          </AppTypography>
        </div>
      )}
      <AppVirtualTable
        ariaLabel="Partitions"
        columns={columns}
        data={rows}
        density="compact"
        emptyMessage="No partitions"
        fillAvailable={false}
        getRowId={(row) =>
          row.kind === "free" ? `free-${row.startBytes}` : row.device.path
        }
        maxHeight={320}
        variant="embedded"
      />

      {dialog?.kind === "create" && (
        <CreatePartitionDialog
          disk={disk}
          error={error}
          isPending={create.isPending}
          onClose={closeDialog}
          onConfirm={(request) =>
            create.mutate(request, { onSuccess: closeDialog })
          }
          region={dialog}
          step={step}
        />
      )}
      {dialog?.kind === "format" && (
        <FormatPartitionDialog
          device={dialog.device}
          error={error}
          isPending={format.isPending}
          onClose={closeDialog}
          onConfirm={(request) =>
            format.mutate(request, { onSuccess: closeDialog })
          }
          step={step}
        />
      )}
      {dialog?.kind === "mount" && (
        <MountPartitionDialog
          device={dialog.device}
          isPending={isMounting}
          onClose={closeDialog}
          onConfirm={(request) =>
            mountPartition(request, { onSuccess: closeDialog })
          }
        />
      )}
      <AppConfirmDialog
        confirmText="Delete"
        destructive
        isPending={isDeleting}
        message={
          dialog?.kind === "delete"
            ? `Delete ${dialog.device.path} (${describe(dialog.device)})? Everything on it is lost.`
            : ""
        }
        onClose={closeDialog}
        onConfirm={() => {
          if (dialog?.kind !== "delete") return;
          deletePartition(
            { device: dialog.device.path },
            { onSuccess: closeDialog },
          );
        }}
        open={dialog?.kind === "delete"}
        pendingText="Deleting..."
        title="Delete partition"
      />
      <AppConfirmDialog
        confirmText="Initialize"
        destructive
        isPending={isInitializing}
        message={`Initialize ${disk.path}${disk.fsType ? ` (${disk.fsType})` : ""}? It gets a new empty GPT, and everything on it is lost${
          rows.some((row) => row.kind === "partition")
            ? `, including ${rows
                .flatMap((row) =>
                  row.kind === "partition"
                    ? [`${row.device.path} (${describe(row.device)})`]
                    : [],
                )
                .join(", ")}`
            : ""
        }.`}
        onClose={closeDialog}
        onConfirm={() =>
          initializeDisk({ device: disk.path }, { onSuccess: closeDialog })
        }
        open={dialog?.kind === "initialize"}
        pendingText="Initializing..."
        title="Initialize disk"
      />
    </div>
  );
};

export default PartitionsTab;
