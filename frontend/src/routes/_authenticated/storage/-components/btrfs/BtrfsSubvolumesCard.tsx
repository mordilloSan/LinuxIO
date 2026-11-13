import { Icon } from "@iconify/react";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";

import {
  linuxio,
  type BtrfsFilesystem,
  type BtrfsSubvolume,
  type BtrfsSubvolumeRequest,
} from "@/api";
import CardIconHeader from "@/components/cards/CardIconHeader";
import FrostedCard from "@/components/cards/FrostedCard";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
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
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppSelect from "@/components/ui/AppSelect";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import { useDockerSettings } from "@/hooks/useConfig";
import { formatFileSize } from "@/utils/formatters";

import { BTRFS_TOAST_META, isSystemSubvolume } from "./btrfsShared";
import {
  consumers,
  snapshotRuleFor,
  subvolumeHostPath,
  subvolumeUses,
} from "./subvolumeUsage";

const GIB = 1024 ** 3;

type SubvolumeRequest = BtrfsSubvolumeRequest;

/** The subvolumes of a filesystem: where they are, how big, what uses them and how they are snapshotted. */
const BtrfsSubvolumesCard = ({
  filesystem,
}: {
  filesystem: BtrfsFilesystem;
}) => {
  const { uuid } = filesystem;
  // Lazy: the route loader cannot know the selected filesystem's uuid.
  const {
    data = [],
    error,
    isPending,
  } = useQuery(linuxio.storage.btrfs_subvolumes({ uuid }));
  // The same queries the Snapshots view and the Shares tab use.
  const { data: snapshots = [] } = useQuery(
    linuxio.storage.btrfs_snapshots({ uuid }),
  );
  const { data: rulesData } = useQuery(linuxio.storage.btrfs_snapshot_rules);
  const { data: samba = [] } = useQuery(linuxio.shares.list_samba_shares);
  const { data: nfs = [] } = useQuery(linuxio.shares.list_nfs_shares);
  const { folders: dockerFolders } = useDockerSettings();
  const [creating, setCreating] = useState(false);
  const [showSystem, setShowSystem] = useState(false);
  const [newPath, setNewPath] = useState("");
  const [deleting, setDeleting] = useState<BtrfsSubvolume | null>(null);
  const [limiting, setLimiting] = useState<BtrfsSubvolume | null>(null);
  const [limitGiB, setLimitGiB] = useState("");
  const [compressing, setCompressing] = useState<BtrfsSubvolume | null>(null);
  const [algorithm, setAlgorithm] = useState("none");

  const { mutate: createSubvolume, isPending: isCreating } =
    linuxio.storage.btrfs_subvolume_create.useMutation({
      success: "Subvolume created",
      error: "Failed to create subvolume",
      toast: BTRFS_TOAST_META,
    });
  const { mutate: deleteSubvolume, isPending: isDeleting } =
    linuxio.storage.btrfs_subvolume_delete.useMutation({
      success: "Subvolume deleted",
      error: "Failed to delete subvolume",
      toast: BTRFS_TOAST_META,
    });
  const { mutate: setReadOnly } =
    linuxio.storage.btrfs_subvolume_set_readonly.useMutation({
      error: "Failed to change read-only state",
      toast: BTRFS_TOAST_META,
    });
  const { mutate: setDefault } =
    linuxio.storage.btrfs_subvolume_set_default.useMutation({
      success: "Default subvolume updated",
      error: "Failed to set default subvolume",
      toast: BTRFS_TOAST_META,
    });

  const { mutate: takeSnapshot } =
    linuxio.storage.btrfs_snapshot_create.useMutation({
      success: "Snapshot created",
      error: "Failed to create snapshot",
      toast: BTRFS_TOAST_META,
    });
  const { mutate: setLimit, isPending: isLimiting } =
    linuxio.storage.btrfs_qgroup_limit.useMutation({
      success: "Size limit updated",
      error: "Failed to update size limit",
      toast: BTRFS_TOAST_META,
    });
  const { mutate: setCompression, isPending: isCompressing } =
    linuxio.storage.btrfs_subvolume_set_compression.useMutation({
      success: "Compression updated",
      error: "Failed to update compression",
      toast: BTRFS_TOAST_META,
    });

  const subvolumes = data.filter(
    (subvolume) =>
      !subvolume.isSnapshot &&
      (showSystem || !isSystemSubvolume(subvolume.path)),
  );
  const hasSystem = data.some(
    (subvolume) => !subvolume.isSnapshot && isSystemSubvolume(subvolume.path),
  );
  const hasSizes = subvolumes.some(
    (subvolume) => subvolume.referencedBytes !== undefined,
  );
  const request = (subvolume: BtrfsSubvolume): SubvolumeRequest => ({
    uuid,
    path: subvolume.path,
  });
  const hostPaths = new Map(
    data.flatMap((subvolume) => {
      const hostPath = subvolume.isSnapshot
        ? undefined
        : subvolumeHostPath(filesystem, subvolume.path);
      return hostPath ? [[subvolume.path, hostPath] as const] : [];
    }),
  );
  const uses = subvolumeUses(
    hostPaths,
    consumers(
      Array.isArray(samba) ? samba : [],
      Array.isArray(nfs) ? nfs : [],
      dockerFolders,
    ),
  );
  const rules = rulesData?.rules ?? [];

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
    {
      accessorKey: "path",
      header: "Name",
      cell: ({ row }) => {
        const { path, isDefault, readOnly } = row.original;
        return (
          <div style={{ minWidth: 0 }}>
            <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
              <AppTypography style={{ fontWeight: 600 }} variant="body2">
                {path.split("/").pop()}
              </AppTypography>
              {isDefault && (
                <AppChip label="Default" size="small" variant="soft" />
              )}
              {readOnly && (
                <AppChip label="Read-only" size="small" variant="soft" />
              )}
            </div>
            <AppTypography color="text.secondary" noWrap variant="caption">
              {hostPaths.get(path) ?? path}
            </AppTypography>
          </div>
        );
      },
    },
    {
      id: "size",
      header: "Size",
      cell: ({ row }) => {
        const used = row.original.referencedBytes;
        const limit = row.original.maxReferencedBytes;
        if (used === undefined) {
          return (
            <AppTypography color="text.secondary" variant="caption">
              —
            </AppTypography>
          );
        }
        return (
          <div style={{ minWidth: 120 }}>
            <AppTypography variant="caption">
              {limit === undefined
                ? formatFileSize(used)
                : `${formatFileSize(used)} of ${formatFileSize(limit)}`}
            </AppTypography>
            {limit !== undefined && (
              <AppLinearProgress
                aria-label={`${row.original.path} space used`}
                value={Math.min(100, (used / limit) * 100)}
                variant="determinate"
              />
            )}
          </div>
        );
      },
    },
    {
      id: "usedBy",
      header: "Used by",
      cell: ({ row }) => {
        const list = uses.get(row.original.path) ?? [];
        return list.length === 0 ? (
          <AppTypography color="text.secondary" variant="caption">
            —
          </AppTypography>
        ) : (
          <div style={{ display: "flex", flexWrap: "wrap", gap: 4 }}>
            {list.map((use) => (
              <AppChip
                color={use === "Docker" ? "primary" : "success"}
                key={use}
                label={use}
                size="small"
                variant="soft"
              />
            ))}
          </div>
        );
      },
    },
    {
      id: "snapshots",
      header: "Snapshots",
      cell: ({ row }) => {
        const count = snapshots.filter(
          (snapshot) => snapshot.source === row.original.path,
        ).length;
        const rule = snapshotRuleFor(rules, uuid, row.original.path);
        return (
          <AppTypography color="text.secondary" variant="caption">
            {[count > 0 ? String(count) : "None", rule?.every]
              .filter(Boolean)
              .join(" · ")}
          </AppTypography>
        );
      },
    },
    {
      id: "actions",
      header: "Actions",
      cell: ({ row }) => (
        <div style={{ display: "flex", justifyContent: "flex-end" }}>
          <AppActionIconButton
            icon="mdi:camera-plus-outline"
            label="Take snapshot"
            onClick={() => takeSnapshot(request(row.original))}
          />
          {hasSizes && (
            <AppActionIconButton
              icon="mdi:gauge"
              label="Limit"
              onClick={() => openLimit(row.original)}
            />
          )}
          <AppActionIconButton
            icon="mdi:zip-box-outline"
            label="Compression"
            onClick={() => {
              setCompressing(row.original);
              setAlgorithm("none");
            }}
          />
          <AppActionIconButton
            icon={
              row.original.readOnly
                ? "mdi:lock-open-variant-outline"
                : "mdi:lock-outline"
            }
            label={row.original.readOnly ? "Make writable" : "Make read-only"}
            onClick={() =>
              setReadOnly({
                ...request(row.original),
                readOnly: !row.original.readOnly,
              })
            }
          />
          <AppActionIconButton
            disabled={row.original.isDefault}
            icon="mdi:star-outline"
            label="Set default"
            onClick={() => setDefault(request(row.original))}
          />
          <AppActionIconButton
            disabled={row.original.isDefault || !!row.original.mountpoint}
            icon="mdi:delete"
            label="Delete"
            onClick={() => setDeleting(row.original)}
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
          icon={<Icon height={24} icon="mdi:folder-multiple" width={24} />}
          right={
            <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
              {hasSystem && (
                <AppFormControlLabel
                  control={
                    <AppSwitch
                      checked={showSystem}
                      onChange={(_, checked) => setShowSystem(checked)}
                      size="small"
                    />
                  }
                  label="Show system subvolumes"
                />
              )}
              <AppButton
                onClick={() => setCreating(true)}
                size="small"
                variant="outlined"
              >
                Create subvolume
              </AppButton>
            </div>
          }
          subtitle={
            hasSizes
              ? "Folders of the pool that are sized, shared and snapshotted on their own"
              : "Folders of the pool that are shared and snapshotted on their own. Turn on quotas in the pool settings to see their sizes."
          }
          title="Subvolumes"
        />
        {error && <AppAlert severity="error">{error.message}</AppAlert>}
        <AppVirtualTable
          ariaLabel="Btrfs subvolumes"
          columns={columns}
          data={subvolumes}
          density="compact"
          emptyMessage={isPending ? "Loading..." : "No subvolumes found"}
          estimateRowHeight={56}
          fillAvailable={false}
          getRowId={(subvolume) => String(subvolume.id)}
          maxHeight={640}
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
            Delete the subvolume <strong>{deleting?.path}</strong>? Every file
            in it is lost. Its snapshots are kept. This cannot be undone.
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
