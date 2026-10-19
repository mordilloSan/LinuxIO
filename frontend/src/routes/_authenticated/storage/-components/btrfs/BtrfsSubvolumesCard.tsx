import { Icon } from "@iconify/react";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";

import {
  linuxio,
  useCallMutation,
  type BtrfsSubvolume,
  type CallRequest,
} from "@/api";
import CardIconHeader from "@/components/cards/CardIconHeader";
import FrostedCard from "@/components/cards/FrostedCard";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppCardContent from "@/components/ui/AppCardContent";
import AppChip from "@/components/ui/AppChip";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppSelect from "@/components/ui/AppSelect";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import { formatFileSize } from "@/utils/formaters";

import { BTRFS_TOAST_META } from "./btrfsShared";

const GIB = 1024 ** 3;

type SubvolumeRequest = CallRequest<"storage.btrfs_subvolume_delete">;

interface BtrfsSubvolumesCardProps {
  /** Wired by the Snapshots card; the row action is hidden until then. */
  onSnapshot?: (subvolume: BtrfsSubvolume) => void;
  uuid: string;
}

const BtrfsSubvolumesCard = ({
  uuid,
  onSnapshot,
}: BtrfsSubvolumesCardProps) => {
  // Lazy: the route loader cannot know the selected filesystem's uuid.
  const {
    data = [],
    error,
    isPending,
  } = useQuery(linuxio.storage.btrfs_subvolumes({ uuid }));
  const [creating, setCreating] = useState(false);
  const [newPath, setNewPath] = useState("");
  const [deleting, setDeleting] = useState<BtrfsSubvolume | null>(null);
  const [limiting, setLimiting] = useState<BtrfsSubvolume | null>(null);
  const [limitGiB, setLimitGiB] = useState("");
  const [compressing, setCompressing] = useState<BtrfsSubvolume | null>(null);
  const [algorithm, setAlgorithm] = useState("none");

  const { mutate: createSubvolume, isPending: isCreating } = useCallMutation(
    linuxio.storage.btrfs_subvolume_create,
    {
      success: "Subvolume created",
      error: "Failed to create subvolume",
      toast: BTRFS_TOAST_META,
    },
  );
  const { mutate: deleteSubvolume, isPending: isDeleting } = useCallMutation(
    linuxio.storage.btrfs_subvolume_delete,
    {
      success: "Subvolume deleted",
      error: "Failed to delete subvolume",
      toast: BTRFS_TOAST_META,
    },
  );
  const { mutate: setReadOnly } = useCallMutation(
    linuxio.storage.btrfs_subvolume_set_readonly,
    { error: "Failed to change read-only state", toast: BTRFS_TOAST_META },
  );
  const { mutate: setDefault } = useCallMutation(
    linuxio.storage.btrfs_subvolume_set_default,
    {
      success: "Default subvolume updated",
      error: "Failed to set default subvolume",
      toast: BTRFS_TOAST_META,
    },
  );

  const { mutate: setLimit, isPending: isLimiting } = useCallMutation(
    linuxio.storage.btrfs_qgroup_limit,
    {
      success: "Size limit updated",
      error: "Failed to update size limit",
      toast: BTRFS_TOAST_META,
    },
  );
  const { mutate: setCompression, isPending: isCompressing } = useCallMutation(
    linuxio.storage.btrfs_subvolume_set_compression,
    {
      success: "Compression updated",
      error: "Failed to update compression",
      toast: BTRFS_TOAST_META,
    },
  );

  const subvolumes = data.filter((subvolume) => !subvolume.isSnapshot);
  const hasSizes = subvolumes.some(
    (subvolume) => subvolume.referencedBytes !== undefined,
  );
  const request = (subvolume: BtrfsSubvolume): SubvolumeRequest => ({
    uuid,
    path: subvolume.path,
  });

  const closeCreate = () => {
    setCreating(false);
    setNewPath("");
  };
  const handleCreate = () =>
    createSubvolume({ uuid, path: newPath.trim() }, { onSuccess: closeCreate });
  const handleDelete = () => {
    if (!deleting) return;
    deleteSubvolume(request(deleting), { onSuccess: () => setDeleting(null) });
  };

  const openLimit = (subvolume: BtrfsSubvolume) => {
    setLimiting(subvolume);
    setLimitGiB(
      subvolume.maxReferencedBytes === undefined
        ? ""
        : String(Math.round((subvolume.maxReferencedBytes / GIB) * 100) / 100),
    );
  };
  const limitBytes = Math.round(Number(limitGiB) * GIB);
  const limitValid =
    limitGiB === "" || (Number.isSafeInteger(limitBytes) && limitBytes > 0);
  const handleLimit = () => {
    if (!limiting) return;
    setLimit(
      {
        uuid,
        path: limiting.path,
        ...(limitGiB !== "" && { maxReferencedBytes: limitBytes }),
      },
      { onSuccess: () => setLimiting(null) },
    );
  };
  const handleCompression = () => {
    if (!compressing) return;
    setCompression(
      { uuid, path: compressing.path, algorithm },
      { onSuccess: () => setCompressing(null) },
    );
  };

  const columns: AppVirtualTableColumnDef<BtrfsSubvolume>[] = [
    { accessorKey: "path", header: "Path" },
    { accessorKey: "id", header: "ID", meta: { align: "right" } },
    {
      accessorKey: "mountpoint",
      header: "Mounted at",
      cell: ({ row }) => row.original.mountpoint ?? "-",
    },
    {
      id: "readOnly",
      header: "Read-only",
      cell: ({ row }) => (
        <AppSwitch
          aria-label={`Read-only ${row.original.path}`}
          checked={row.original.readOnly}
          onChange={(_, readOnly) =>
            setReadOnly({ ...request(row.original), readOnly })
          }
          size="small"
        />
      ),
    },
    {
      id: "default",
      header: "Default",
      cell: ({ row }) =>
        row.original.isDefault && (
          <AppChip label="Default" size="small" variant="soft" />
        ),
    },
    ...(hasSizes
      ? [
          {
            id: "size",
            header: "Size",
            cell: ({ row }) =>
              row.original.referencedBytes === undefined ? (
                "-"
              ) : (
                <>
                  {formatFileSize(row.original.referencedBytes)}
                  {row.original.maxReferencedBytes !== undefined && (
                    <AppTypography
                      color="text.secondary"
                      style={{ display: "block" }}
                      variant="caption"
                    >
                      Limit {formatFileSize(row.original.maxReferencedBytes)}
                    </AppTypography>
                  )}
                </>
              ),
            meta: { align: "right" },
          } satisfies AppVirtualTableColumnDef<BtrfsSubvolume>,
        ]
      : []),
    {
      id: "actions",
      header: "Actions",
      cell: ({ row }) => (
        <div style={{ display: "flex", gap: 8, justifyContent: "flex-end" }}>
          {onSnapshot && (
            <AppButton
              onClick={() => onSnapshot(row.original)}
              size="small"
              variant="outlined"
            >
              Snapshot
            </AppButton>
          )}
          {hasSizes && (
            <AppButton
              onClick={() => openLimit(row.original)}
              size="small"
              variant="outlined"
            >
              Limit
            </AppButton>
          )}
          <AppButton
            onClick={() => {
              setCompressing(row.original);
              setAlgorithm("none");
            }}
            size="small"
            variant="outlined"
          >
            Compression
          </AppButton>
          <AppButton
            disabled={row.original.isDefault}
            onClick={() => setDefault(request(row.original))}
            size="small"
            variant="outlined"
          >
            Set default
          </AppButton>
          <AppButton
            color="error"
            disabled={row.original.isDefault || !!row.original.mountpoint}
            onClick={() => setDeleting(row.original)}
            size="small"
            variant="outlined"
          >
            Delete
          </AppButton>
        </div>
      ),
      meta: { align: "right" },
    },
  ];

  return (
    <FrostedCard>
      <AppCardContent>
        <CardIconHeader
          icon={<Icon height={24} icon="mdi:folder-multiple" width={24} />}
          right={
            <AppButton
              onClick={() => setCreating(true)}
              size="small"
              variant="outlined"
            >
              Create subvolume
            </AppButton>
          }
          subtitle={`${subvolumes.length} subvolume(s)`}
          title="Subvolumes"
        />
        {error && <AppAlert severity="error">{error.message}</AppAlert>}
        <AppVirtualTable
          ariaLabel="Btrfs subvolumes"
          columns={columns}
          data={subvolumes}
          density="compact"
          emptyMessage={isPending ? "Loading..." : "No subvolumes found"}
          fillAvailable={false}
          getRowId={(subvolume) => String(subvolume.id)}
          maxHeight={320}
          variant="embedded"
        />
      </AppCardContent>

      <GeneralDialog
        fullWidth
        maxWidth="sm"
        onClose={closeCreate}
        open={creating}
      >
        <AppDialogTitle>Create subvolume</AppDialogTitle>
        <AppDialogContent>
          <AppTextField
            fullWidth
            helperText="Relative to the top level of the filesystem"
            label="Subvolume path"
            onChange={(event) => setNewPath(event.target.value)}
            placeholder="data/photos"
            size="small"
            value={newPath}
          />
        </AppDialogContent>
        <AppDialogActions>
          <AppButton disabled={isCreating} onClick={closeCreate}>
            Cancel
          </AppButton>
          <AppButton
            disabled={isCreating || newPath.trim() === ""}
            onClick={handleCreate}
            variant="contained"
          >
            {isCreating ? "Creating..." : "Create"}
          </AppButton>
        </AppDialogActions>
      </GeneralDialog>

      <GeneralDialog
        fullWidth
        maxWidth="sm"
        onClose={() => setDeleting(null)}
        open={deleting !== null}
      >
        <AppDialogTitle>Delete subvolume</AppDialogTitle>
        <AppDialogContent>
          <AppDialogContentText>
            Delete the subvolume <strong>{deleting?.path}</strong>? All data in
            it is lost and this cannot be undone.
          </AppDialogContentText>
        </AppDialogContent>
        <AppDialogActions>
          <AppButton disabled={isDeleting} onClick={() => setDeleting(null)}>
            Cancel
          </AppButton>
          <AppButton
            color="error"
            disabled={isDeleting}
            onClick={handleDelete}
            variant="contained"
          >
            {isDeleting ? "Deleting..." : "Delete"}
          </AppButton>
        </AppDialogActions>
      </GeneralDialog>

      <GeneralDialog
        fullWidth
        maxWidth="sm"
        onClose={() => setLimiting(null)}
        open={limiting !== null}
      >
        <AppDialogTitle>Size limit for {limiting?.path}</AppDialogTitle>
        <AppDialogContent>
          <AppTextField
            error={!limitValid}
            fullWidth
            helperText="Leave empty for no limit"
            label="Max referenced (GiB)"
            onChange={(event) => setLimitGiB(event.target.value)}
            size="small"
            type="number"
            value={limitGiB}
          />
        </AppDialogContent>
        <AppDialogActions>
          <AppButton disabled={isLimiting} onClick={() => setLimiting(null)}>
            Cancel
          </AppButton>
          <AppButton
            disabled={isLimiting || !limitValid}
            onClick={handleLimit}
            variant="contained"
          >
            {isLimiting ? "Saving..." : "Save"}
          </AppButton>
        </AppDialogActions>
      </GeneralDialog>

      <GeneralDialog
        fullWidth
        maxWidth="sm"
        onClose={() => setCompressing(null)}
        open={compressing !== null}
      >
        <AppDialogTitle>Compression for {compressing?.path}</AppDialogTitle>
        <AppDialogContent>
          <AppSelect
            fullWidth
            label="Compression"
            onChange={(event) => setAlgorithm(event.target.value)}
            value={algorithm}
          >
            <option value="none">Default</option>
            <option value="zstd">zstd</option>
            <option value="lzo">lzo</option>
            <option value="zlib">zlib</option>
          </AppSelect>
        </AppDialogContent>
        <AppDialogActions>
          <AppButton
            disabled={isCompressing}
            onClick={() => setCompressing(null)}
          >
            Cancel
          </AppButton>
          <AppButton
            disabled={isCompressing}
            onClick={handleCompression}
            variant="contained"
          >
            {isCompressing ? "Saving..." : "Save"}
          </AppButton>
        </AppDialogActions>
      </GeneralDialog>
    </FrostedCard>
  );
};

export default BtrfsSubvolumesCard;
