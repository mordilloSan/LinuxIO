import { useSuspenseQueries } from "@tanstack/react-query";
import { useState, type CSSProperties } from "react";

import {
  linuxio,
  useCallMutation,
  type VMStoragePool,
  type VMStorageVolume,
  type VMUnusedDisk,
  type VirtualMachine,
} from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppButton from "@/components/ui/AppButton";
import AppChip from "@/components/ui/AppChip";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTypography from "@/components/ui/AppTypography";
import { useScopedToast } from "@/hooks/useScopedToast";
import { formatFileSize } from "@/utils/formaters";
import { getMutationErrorMessage } from "@/utils/mutations";

import { VM_TOAST } from "./vmShared";

const pageStyle: CSSProperties = {
  display: "flex",
  flexDirection: "column",
  gap: "var(--app-space-16)",
  minHeight: 0,
};

const tableCardStyle: CSSProperties = {
  minWidth: 0,
  overflow: "hidden",
};

const poolHeaderStyle: CSSProperties = {
  alignItems: "center",
  display: "flex",
  flexWrap: "wrap",
  gap: "var(--app-space-8)",
  padding: "var(--app-space-12) var(--app-space-12) 0",
};

const poolUsageStyle: CSSProperties = {
  display: "flex",
  flexDirection: "column",
  gap: 4,
  padding: "var(--app-space-8) var(--app-space-12)",
};

type VolumeRow = VMStorageVolume & { vmName: string };

const volumeColumns: AppVirtualTableColumnDef<VolumeRow>[] = [
  {
    accessorKey: "name",
    header: "Volume",
    cell: ({ row }) => row.original.name,
    meta: { width: "minmax(200px, 1fr)" },
  },
  {
    accessorKey: "vmName",
    header: "Used by",
    cell: ({ row }) => row.original.vmName || "—",
    meta: { width: "160px" },
  },
  {
    accessorKey: "capacityBytes",
    header: "Virtual size",
    cell: ({ row }) => formatFileSize(row.original.capacityBytes, 1),
    meta: { align: "right", width: "120px" },
  },
  {
    accessorKey: "allocationBytes",
    header: "Allocated",
    cell: ({ row }) => formatFileSize(row.original.allocationBytes, 1),
    meta: { align: "right", width: "120px" },
  },
];

function StoragePoolCard({
  pool,
  vms,
}: {
  pool: VMStoragePool;
  vms: VirtualMachine[];
}) {
  const rows = pool.volumes.map((volume) => ({
    ...volume,
    vmName:
      vms.find((vm) => (vm.disks ?? []).some((d) => d.path === volume.path))
        ?.name ?? "",
  }));
  const usedPercent = pool.capacityBytes
    ? (pool.allocationBytes / pool.capacityBytes) * 100
    : 0;
  return (
    <FrostedCard style={tableCardStyle}>
      <div style={poolHeaderStyle}>
        <AppTypography component="h2" variant="subtitle1">
          {pool.name}
        </AppTypography>
        <AppChip label={pool.type} size="small" variant="soft" />
        <AppChip
          color={pool.active ? "success" : "warning"}
          label={pool.active ? "active" : "inactive"}
          size="small"
          variant="soft"
        />
        <AppTypography color="text.secondary" noWrap variant="caption">
          {pool.path}
        </AppTypography>
      </div>
      {pool.active && (
        <div style={poolUsageStyle}>
          <AppTypography variant="body2">
            {formatFileSize(pool.allocationBytes, 1)} used of{" "}
            {formatFileSize(pool.capacityBytes, 1)} ·{" "}
            {formatFileSize(pool.availableBytes, 1)} free
          </AppTypography>
          <AppLinearProgress
            aria-label={`${pool.name} usage`}
            value={Math.min(usedPercent, 100)}
            variant="determinate"
          />
        </div>
      )}
      <AppVirtualTable
        ariaLabel={`${pool.name} volumes`}
        columns={volumeColumns}
        data={rows}
        emptyMessage={pool.active ? "No volumes." : "Pool is inactive."}
        enableSorting={false}
        fillAvailable={false}
        getRowId={(row) => row.path}
        maxHeight={300}
        variant="embedded"
      />
    </FrostedCard>
  );
}

const VMDisksPage = () => {
  // The VM list is already polled by the /vm layout; it is only joined here
  // to name the VM each volume belongs to.
  const [{ data: pools }, { data: vms }, { data: disks }] = useSuspenseQueries({
    queries: [
      linuxio.virt.pools,
      { ...linuxio.virt.list, refetchOnMount: false },
      linuxio.virt.unused_disks,
    ],
  });
  const toast = useScopedToast(VM_TOAST);
  const [deleting, setDeleting] = useState<VMUnusedDisk | null>(null);
  const remove = useCallMutation(linuxio.virt.unused_disk_delete, {
    success: (_, request) => {
      toast.success(`Deleted ${request.path}`);
      setDeleting(null);
    },
    error: (error) =>
      toast.error(getMutationErrorMessage(error, "Failed to delete disk")),
  });
  const totalBytes = disks.reduce((sum, disk) => sum + disk.sizeBytes, 0);

  const columns: AppVirtualTableColumnDef<VMUnusedDisk>[] = [
    {
      accessorKey: "name",
      header: "Disk",
      cell: ({ row }) => row.original.name,
      meta: { width: "minmax(200px, 1fr)" },
    },
    {
      accessorKey: "vmName",
      header: "From VM",
      cell: ({ row }) => row.original.vmName,
      meta: { width: "160px" },
    },
    {
      accessorKey: "sizeBytes",
      header: "Size",
      cell: ({ row }) => formatFileSize(row.original.sizeBytes, 1),
      meta: { align: "right", width: "110px" },
    },
    {
      accessorKey: "modifiedAt",
      header: "Last used",
      cell: ({ row }) => new Date(row.original.modifiedAt).toLocaleString(),
      meta: { width: "200px" },
    },
    {
      id: "actions",
      header: "",
      cell: ({ row }) => (
        <AppActionIconButton
          disabled={remove.isPending}
          icon="mdi:delete"
          label={`Delete ${row.original.name}`}
          onClick={() => setDeleting(row.original)}
        />
      ),
      meta: { align: "right", width: "64px" },
    },
  ];

  return (
    <div style={pageStyle}>
      {pools.map((pool) => (
        <StoragePoolCard key={pool.name} pool={pool} vms={vms} />
      ))}
      <AppTypography component="h2" variant="subtitle1">
        Unused disks
      </AppTypography>
      <AppTypography color="text.secondary" variant="body2">
        Disks LinuxIO created for VMs that no longer exist, usually kept when a
        VM was deleted without its disks. No VM uses them.
        {disks.length > 0
          ? ` Deleting all of them frees ${formatFileSize(totalBytes, 1)}.`
          : ""}
      </AppTypography>
      <FrostedCard style={tableCardStyle}>
        <AppVirtualTable
          ariaLabel="Unused VM disks"
          columns={columns}
          data={disks}
          emptyMessage="No unused disks."
          enableSorting={false}
          fillAvailable={false}
          getRowId={(row) => row.path}
          maxHeight={400}
          variant="embedded"
        />
      </FrostedCard>
      <GeneralDialog
        open={deleting !== null}
        onClose={() => {
          if (!remove.isPending) setDeleting(null);
        }}
        fullWidth
        maxWidth="sm"
      >
        <AppDialogTitle>Delete unused disk</AppDialogTitle>
        <AppDialogContent>
          <AppTypography variant="body2">
            Delete the disk left by VM {deleting?.vmName}? Its data cannot be
            recovered.
          </AppTypography>
          <AppTypography
            component="div"
            variant="caption"
            style={{
              overflowWrap: "anywhere",
              marginTop: "var(--app-space-12)",
            }}
          >
            {deleting?.path}
          </AppTypography>
        </AppDialogContent>
        <AppDialogActions>
          <AppButton
            disabled={remove.isPending}
            onClick={() => setDeleting(null)}
            variant="text"
          >
            Cancel
          </AppButton>
          <AppButton
            color="error"
            disabled={remove.isPending || !deleting}
            onClick={() => {
              if (deleting) remove.mutate({ path: deleting.path });
            }}
            variant="contained"
          >
            Delete disk
          </AppButton>
        </AppDialogActions>
      </GeneralDialog>
    </div>
  );
};

export default VMDisksPage;
