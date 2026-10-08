import { useSuspenseQuery } from "@tanstack/react-query";
import { useState, type CSSProperties } from "react";

import { linuxio, useCallMutation, type VMUnusedDisk } from "@/api";
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

const VMDisksPage = () => {
  const { data: disks } = useSuspenseQuery(linuxio.virt.unused_disks);
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
