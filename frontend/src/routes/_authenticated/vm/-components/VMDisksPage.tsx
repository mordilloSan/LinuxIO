import { useSuspenseQueries } from "@tanstack/react-query";
import { useState, type CSSProperties } from "react";

import {
  CACHE_TTL_MS,
  getMutationErrorMessage,
  linuxio,
  type VirtualMachine,
  type VMStoragePool,
  type VMUnusedDisk,
} from "@/api";
import { EntityCard, KeyValues } from "@/components/cards/EntityCardKit";
import FrostedCard from "@/components/cards/FrostedCard";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTypography from "@/components/ui/AppTypography";
import { useScopedToast } from "@/hooks/useScopedToast";
import { formatFileSize } from "@/utils/formatters";

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

type VMDiskRow = {
  allocatedBytes: number;
  id: string;
  path: string;
  sizeGB: number;
  vmName: string;
};

const vmDiskColumns: AppVirtualTableColumnDef<VMDiskRow>[] = [
  {
    accessorKey: "vmName",
    header: "Name",
    cell: ({ row }) => row.original.vmName,
    meta: { width: "minmax(140px, 1fr)" },
  },
  {
    accessorKey: "path",
    header: "Disk",
    cell: ({ row }) =>
      row.original.path.slice(row.original.path.lastIndexOf("/") + 1),
    meta: { width: "minmax(200px, 1.4fr)" },
  },
  {
    id: "usage",
    header: "Usage",
    cell: ({ row }) => {
      const { allocatedBytes, sizeGB } = row.original;
      const virtualBytes = sizeGB * 1024 ** 3;
      if (!virtualBytes) return formatFileSize(allocatedBytes, 2);
      const percent = ((allocatedBytes / virtualBytes) * 100).toFixed(1);
      return `${formatFileSize(allocatedBytes, 2)} / ${formatFileSize(virtualBytes, 2)} (${percent}%)`;
    },
    meta: { width: "minmax(220px, 1fr)" },
  },
];

const inPool = (pool: VMStoragePool, path: string) =>
  Boolean(pool.path) && path.startsWith(`${pool.path}/`);

function StoragePoolCard({
  hostname,
  pool,
  vms,
}: {
  hostname: string;
  pool: VMStoragePool;
  vms: VirtualMachine[];
}) {
  const rows: VMDiskRow[] = vms.flatMap((vm) =>
    (vm.disks ?? [])
      .filter((disk) => disk.device !== "cdrom" && inPool(pool, disk.path))
      .map((disk) => ({
        allocatedBytes: disk.allocatedBytes ?? 0,
        id: `${vm.name}-${disk.path}`,
        path: disk.path,
        sizeGB: disk.sizeGB ?? 0,
        vmName: vm.name,
      })),
  );
  const allocatedGB = rows.reduce((sum, row) => sum + row.sizeGB, 0);
  const usedPercent = pool.capacityBytes
    ? (pool.allocationBytes / pool.capacityBytes) * 100
    : 0;
  return (
    <EntityCard
      icon="mdi:harddisk"
      name={pool.name}
      status={pool.active ? "Normal" : "Inactive"}
      summary={
        pool.active ? (
          <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
            <AppTypography fontWeight={600} noWrap variant="body2">
              Used / Total: {formatFileSize(pool.allocationBytes, 2)} /{" "}
              {formatFileSize(pool.capacityBytes, 2)}
            </AppTypography>
            <AppLinearProgress
              aria-label={`${pool.name} usage`}
              value={Math.min(usedPercent, 100)}
              variant="determinate"
            />
            <AppTypography color="text.secondary" variant="caption">
              Usage: {usedPercent.toFixed(2)}%
            </AppTypography>
          </div>
        ) : null
      }
      tone={pool.active ? "ok" : "muted"}
    >
      <KeyValues
        items={[
          ["Host", hostname],
          ["Type", pool.type],
          ["Path", pool.path ?? "-"],
          [
            "Total space",
            pool.active ? formatFileSize(pool.capacityBytes, 2) : "-",
          ],
          [
            "Free space",
            pool.active ? formatFileSize(pool.availableBytes, 2) : "-",
          ],
          ["Allocated to virtual disks", `${allocatedGB} GB`],
        ]}
      />
      <AppTypography component="h3" variant="subtitle2">
        Virtual machine usage
      </AppTypography>
      <AppVirtualTable
        ariaLabel={`${pool.name} virtual machine usage`}
        columns={vmDiskColumns}
        data={rows}
        emptyMessage="No virtual machine disk lives in this pool."
        enableSorting={false}
        fillAvailable={false}
        getRowId={(row) => row.id}
        maxHeight={300}
        variant="embedded"
      />
    </EntityCard>
  );
}

const VMDisksPage = () => {
  // The VM list is already polled by the /vm layout; it is only joined here
  // to list each VM's disks under a pool.
  const [{ data: pools }, { data: vms }, { data: host }, { data: disks }] =
    useSuspenseQueries({
      queries: [
        linuxio.virt.pools,
        { ...linuxio.virt.list, refetchOnMount: false },
        { ...linuxio.system.get_host_info, staleTime: CACHE_TTL_MS.ONE_DAY },
        linuxio.virt.unused_disks,
      ],
    });
  const toast = useScopedToast(VM_TOAST);
  const [deleting, setDeleting] = useState<VMUnusedDisk | null>(null);
  const remove = linuxio.virt.unused_disk_delete.useMutation({
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
        <StoragePoolCard
          hostname={host.hostname}
          key={pool.name}
          pool={pool}
          vms={vms}
        />
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
