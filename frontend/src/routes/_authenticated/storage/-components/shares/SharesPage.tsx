import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import {
  useCallback,
  useMemo,
  useState,
  type CSSProperties,
  type ReactNode,
} from "react";

import {
  getMutationErrorMessage,
  linuxio,
  type NFSExport,
  type SambaShare,
  type SharedFolder,
} from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import PathPickerField from "@/components/pathpicker/PathPickerField";
import ReorderableCardGrid from "@/components/reorder/ReorderableCardGrid";
import { useReorderableSurface } from "@/components/reorder/useReorderableSurface";
import { RoutedTabActions } from "@/components/tabbar";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableProps } from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import { useReorderableTableDnd } from "@/components/tables/useReorderableTableDnd";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppCheckbox from "@/components/ui/AppCheckbox";
import Chip from "@/components/ui/AppChip";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppMenu, { AppMenuItem } from "@/components/ui/AppMenu";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import HeaderActions from "@/components/ui/HeaderActions";
import ViewModeToggle from "@/components/ui/ViewModeToggle";
import useAuth from "@/hooks/useAuth";
import { useCapability } from "@/hooks/useCapabilities";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useViewMode } from "@/hooks/useViewMode";
import FolderShareCard from "@/routes/_authenticated/storage/-components/shares/FolderShareCard";
import { CARD_GRID_SIZE_DENSE, GAP_LG, GAP_XL } from "@/theme/constants";

import DeleteSharedFolderDialog from "./DeleteSharedFolderDialog";
import {
  DeleteNFSShareDialog,
  DeleteSambaShareDialog,
} from "./DeleteShareDialogs";
import NFSOptionsDropdown, {
  defaultNFSOptions,
  nfsOptionsFromStrings,
  parseNFSClients,
  type ClientOptions,
} from "./NFSClientOptions";
import SharedFolderDialog from "./SharedFolderDialog";
import { accessSummary } from "./sharedFolders";
import SMBSettingsDialog from "./SMBSettingsDialog";
import StopSharingDialog from "./StopSharingDialog";

type ShareGroup = {
  id: string;
  name: string;
  path: string;
  comment: string;
  samba: SambaShare | null;
  nfs: NFSExport | null;
  /** Set for a shared folder LinuxIO made, which has an access table. */
  folder: SharedFolder | null;
};

/** What can be done to a shared folder from its row. */
type FolderAction = "edit" | "stop" | "delete";

interface FolderShareDialogProps {
  /** `null` creates a new share; a group edits that share. */
  group: ShareGroup | null;
  onClose: () => void;
  onExited?: () => void;
  onSuccess: () => void;
  open: boolean;
}

const protocolSectionStyle: CSSProperties = {
  display: "flex",
  flexDirection: "column",
  gap: 6,
  padding: 10,
  borderRadius: 10,
  background: "rgba(255,255,255,0.03)",
};

const tableColumns: AppVirtualTableColumnDef<ShareGroup>[] = [
  {
    accessorKey: "name",
    header: "Name",
    cell: ({ row }) => (
      <AppTypography fontWeight={700} variant="body2">
        {row.original.name}
      </AppTypography>
    ),
    meta: {
      align: "left",
      getCellRenderKey: (row) => {
        const group = row as ShareGroup;
        return [group.id, group.name];
      },
    },
  },
  {
    accessorKey: "comment",
    header: "Comment",
    cell: ({ row }) => (
      <AppTypography color="text.secondary" variant="body2">
        {row.original.comment || "-"}
      </AppTypography>
    ),
    meta: {
      align: "left",
      getCellRenderKey: (row) => {
        const group = row as ShareGroup;
        return [group.id, group.comment];
      },
    },
  },
  {
    id: "smb",
    header: "SMB",
    accessorFn: (group) => getSambaAccessLabel(group.samba),
    cell: ({ row }) => (
      <AppTypography variant="body2">
        {getSambaAccessLabel(row.original.samba)}
      </AppTypography>
    ),
    meta: {
      align: "left",
      getCellRenderKey: (row) => {
        const group = row as ShareGroup;
        return [group.id, getSambaAccessLabel(group.samba)];
      },
      width: "110px",
    },
  },
  {
    id: "nfs",
    header: "NFS",
    accessorFn: (group) => getNFSAccessLabel(group.nfs),
    cell: ({ row }) => (
      <AppTypography variant="body2">
        {getNFSAccessLabel(row.original.nfs)}
      </AppTypography>
    ),
    meta: {
      align: "left",
      getCellRenderKey: (row) => {
        const group = row as ShareGroup;
        return [group.id, getNFSAccessLabel(group.nfs)];
      },
      width: "110px",
    },
  },
  {
    accessorKey: "path",
    header: "Path",
    cell: ({ row }) => (
      <AppTypography
        style={{ fontFamily: "var(--app-font-mono)" }}
        variant="body2"
      >
        {row.original.path}
      </AppTypography>
    ),
    meta: {
      align: "left",
      getCellRenderKey: (row) => {
        const group = row as ShareGroup;
        return [group.id, group.path];
      },
    },
  },
];

const getShareGroupId = (group: ShareGroup) => group.id;

function normalizeSharePath(path: string): string {
  if (!path || path === "/") {
    return path || "/";
  }
  return path.replace(/\/+$/, "");
}

function inferShareName(path: string): string {
  const normalized = normalizeSharePath(path);
  const parts = normalized.split("/").filter(Boolean);
  return parts[parts.length - 1] || normalized || "/";
}

function getSambaAccessLabel(share: SambaShare | null): string {
  if (!share) {
    return "-";
  }
  return share.properties["guest ok"] === "yes" ? "Public" : "Private";
}

function getNFSAccessLabel(share: NFSExport | null): string {
  if (!share) {
    return "-";
  }
  return share.clients.some((client) => client.host === "*")
    ? "Public"
    : "Private";
}

function buildShareGroups(
  sambaShares: SambaShare[],
  nfsShares: NFSExport[],
  folders: SharedFolder[],
): ShareGroup[] {
  const groups = new Map<string, ShareGroup>();

  for (const samba of sambaShares) {
    const path = normalizeSharePath(samba.properties["path"] ?? samba.name);
    const existing = groups.get(path) ?? {
      id: path,
      name: samba.name,
      path,
      comment: "",
      samba: null,
      nfs: null,
      folder: null,
    };

    existing.name = samba.name || existing.name;
    existing.comment = samba.properties["comment"] || existing.comment;
    existing.samba = samba;
    groups.set(path, existing);
  }

  for (const nfs of nfsShares) {
    const path = normalizeSharePath(nfs.path);
    const existing = groups.get(path) ?? {
      id: path,
      name: inferShareName(path),
      path,
      comment: "",
      samba: null,
      nfs: null,
      folder: null,
    };

    if (!existing.name) {
      existing.name = inferShareName(path);
    }
    existing.nfs = nfs;
    groups.set(path, existing);
  }

  // A shared folder joins the section and the export it is published as, or
  // stands alone while it is not shared; who can use it stands in for the
  // comment.
  for (const folder of folders) {
    const path = folder.folder
      ? normalizeSharePath(folder.folder)
      : `${folder.uuid}/${folder.path}`;
    const existing = groups.get(path) ?? {
      id: path,
      name: folder.name,
      path: folder.folder || folder.path,
      comment: "",
      samba: null,
      nfs: null,
      folder: null,
    };
    existing.name = folder.name;
    existing.comment = accessSummary(folder);
    existing.folder = folder;
    groups.set(path, existing);
  }

  return Array.from(groups.values()).sort((left, right) =>
    left.name.localeCompare(right.name),
  );
}

function buildFolderSambaProperties(
  path: string,
  comment: string,
  sambaPublic: boolean,
  baseProperties?: Record<string, string>,
): Record<string, string> {
  const properties: Record<string, string> = { ...(baseProperties ?? {}) };
  properties.path = path;
  properties.browseable ??= "yes";
  properties["read only"] ??= "no";
  properties["guest ok"] = sambaPublic ? "yes" : "no";

  if (comment.trim()) {
    properties.comment = comment.trim();
  } else {
    delete properties.comment;
  }

  return properties;
}

function renderProtocolSummary(group: ShareGroup): ReactNode {
  return (
    <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
      {group.samba ? (
        <Chip
          color="primary"
          label={`SMB ${getSambaAccessLabel(group.samba)}`}
          size="small"
          variant="soft"
        />
      ) : null}
      {group.nfs ? (
        <Chip
          color="primary"
          label={`NFS ${getNFSAccessLabel(group.nfs)}`}
          size="small"
          variant="soft"
        />
      ) : null}
      {group.folder && !group.samba && !group.nfs ? (
        <Chip label="Not shared" size="small" variant="soft" />
      ) : null}
    </div>
  );
}

const FolderShareDialog = ({
  group,
  open,
  onClose,
  onExited,
  onSuccess,
}: FolderShareDialogProps) => {
  const toast = useScopedToast({ label: "Open shares", to: "/storage/shares" });
  const { reason: nfsReason, status: nfsStatus } =
    useCapability("nfsServerAvailable");
  const nfsUnavailable = nfsStatus === "unavailable";
  const { reason: sambaReason, status: sambaStatus } = useCapability(
    "sambaServerAvailable",
  );
  const sambaUnavailable = sambaStatus === "unavailable";
  // Initialised from `group`; the caller keys this dialog by group id so a
  // different share remounts it with fresh state.
  const [path, setPath] = useState(group?.path ?? "");
  const [sambaEnabled, setSambaEnabled] = useState(
    group ? Boolean(group.samba) : true,
  );
  const [nfsEnabled, setNFSEnabled] = useState(Boolean(group?.nfs));
  const [sambaName, setSambaName] = useState(group?.samba?.name ?? "");
  const [comment, setComment] = useState(
    group?.samba?.properties["comment"] ?? group?.comment ?? "",
  );
  const [sambaPublic, setSambaPublic] = useState(
    group?.samba?.properties["guest ok"] === "yes",
  );
  const [nfsClients, setNFSClients] = useState(
    () => group?.nfs?.clients.map((client) => client.host).join(", ") || "*",
  );
  const [nfsOptions, setNFSOptions] = useState<ClientOptions>(
    group?.nfs?.clients[0]
      ? nfsOptionsFromStrings(group.nfs.clients[0].options ?? [])
      : { ...defaultNFSOptions },
  );
  const [validationError, setValidationError] = useState<string | null>(null);

  const sambaCreate = linuxio.shares.create_samba_share.useMutation();
  const sambaUpdate = linuxio.shares.update_samba_share.useMutation();
  const sambaDelete = linuxio.shares.delete_samba_share.useMutation();
  const nfsCreate = linuxio.shares.create_nfs_share.useMutation();
  const nfsUpdate = linuxio.shares.update_nfs_share.useMutation();
  const nfsDelete = linuxio.shares.delete_nfs_share.useMutation();

  const isPending =
    sambaCreate.isPending ||
    sambaUpdate.isPending ||
    sambaDelete.isPending ||
    nfsCreate.isPending ||
    nfsUpdate.isPending ||
    nfsDelete.isPending;

  const hasExistingSamba = Boolean(group?.samba);
  const effectiveSambaEnabled =
    sambaEnabled && (!sambaUnavailable || hasExistingSamba);

  const handleClose = () => {
    if (!group) {
      // Create mode is not keyed, so reset for the next open.
      setPath("");
      setSambaEnabled(true);
      setNFSEnabled(false);
      setSambaName("");
      setComment("");
      setSambaPublic(false);
      setNFSClients("*");
      setNFSOptions({ ...defaultNFSOptions });
    }
    setValidationError(null);
    onClose();
  };

  const handleSave = async () => {
    const normalizedPath = normalizeSharePath(path.trim());
    const resolvedName = sambaName.trim() || inferShareName(normalizedPath);
    const parsedNFSClients = parseNFSClients(nfsClients, nfsOptions);

    if (!normalizedPath) {
      setValidationError("Folder path is required");
      return;
    }
    if (!effectiveSambaEnabled && !nfsEnabled) {
      setValidationError("Enable SMB and/or NFS for this folder share");
      return;
    }
    if (effectiveSambaEnabled && !resolvedName) {
      setValidationError("Share name is required when SMB is enabled");
      return;
    }
    if (effectiveSambaEnabled && sambaUnavailable) {
      setValidationError(sambaReason);
      return;
    }
    if (nfsEnabled && nfsUnavailable) {
      setValidationError(nfsReason);
      return;
    }
    if (nfsEnabled && parsedNFSClients.length === 0) {
      setValidationError("At least one NFS client is required");
      return;
    }

    setValidationError(null);

    const verb = group ? "updated" : "created";
    let changedAny = false;

    try {
      if (effectiveSambaEnabled) {
        const sambaProperties = buildFolderSambaProperties(
          normalizedPath,
          comment,
          sambaPublic,
          group?.samba?.properties,
        );

        if (group?.samba) {
          await sambaUpdate.mutateAsync({
            oldName: group.samba.name,
            newName: resolvedName,
            properties: sambaProperties,
          });
        } else {
          await sambaCreate.mutateAsync({
            name: resolvedName,
            properties: sambaProperties,
          });
        }
        changedAny = true;
      } else if (group?.samba) {
        await sambaDelete.mutateAsync({ name: group.samba.name });
        changedAny = true;
      }

      if (nfsEnabled) {
        if (group?.nfs) {
          await nfsUpdate.mutateAsync({
            path: normalizedPath,
            clients: parsedNFSClients,
          });
        } else {
          await nfsCreate.mutateAsync({
            path: normalizedPath,
            clients: parsedNFSClients,
          });
        }
        changedAny = true;
      } else if (group?.nfs) {
        await nfsDelete.mutateAsync({ path: normalizedPath });
        changedAny = true;
      }

      toast.success(`Folder share ${verb} for ${normalizedPath}`);
      onSuccess();
      handleClose();
    } catch (error) {
      const message = getMutationErrorMessage(
        error,
        `Failed to ${group ? "update" : "create"} folder share`,
      );

      if (changedAny) {
        toast.error(`${message}. Some changes may already have been applied.`);
        onSuccess();
        handleClose();
        return;
      }

      setValidationError(message);
    }
  };

  return (
    <GeneralDialog
      fullWidth
      maxWidth="sm"
      onClose={handleClose}
      open={open}
      slotProps={{ transition: { onExited } }}
    >
      <AppDialogTitle>
        {group ? "Edit Folder Share" : "Share an existing folder"}
      </AppDialogTitle>
      <AppDialogContent>
        <div className="app-dialog-fields">
          {group ? (
            <AppTextField
              disabled
              fullWidth
              label="Folder Path"
              shrinkLabel
              size="small"
              value={group.path}
            />
          ) : (
            <PathPickerField
              label="Folder Path"
              onChange={setPath}
              value={path}
            />
          )}

          <div style={protocolSectionStyle}>
            <AppFormControlLabel
              control={
                <AppCheckbox
                  checked={effectiveSambaEnabled}
                  onChange={(event) => setSambaEnabled(event.target.checked)}
                />
              }
              disabled={sambaUnavailable && !hasExistingSamba}
              label="Enable SMB"
            />
            {sambaUnavailable ? (
              <AppAlert severity="warning">{sambaReason}</AppAlert>
            ) : null}
            {effectiveSambaEnabled ? (
              <>
                <AppTextField
                  className="app-text-field--compact-copy"
                  fullWidth
                  label="Share Name"
                  onChange={(event) => setSambaName(event.target.value)}
                  placeholder={inferShareName(path)}
                  size="small"
                  value={sambaName}
                />
                <AppTextField
                  className="app-text-field--compact-copy"
                  fullWidth
                  label="Comment"
                  onChange={(event) => setComment(event.target.value)}
                  placeholder="Optional description"
                  size="small"
                  value={comment}
                />
                <AppFormControlLabel
                  control={
                    <AppCheckbox
                      checked={sambaPublic}
                      onChange={(event) => setSambaPublic(event.target.checked)}
                    />
                  }
                  label="Public SMB access"
                />
              </>
            ) : null}
          </div>

          <div style={protocolSectionStyle}>
            <AppFormControlLabel
              control={
                <AppCheckbox
                  checked={nfsEnabled}
                  onChange={(event) => setNFSEnabled(event.target.checked)}
                />
              }
              disabled={nfsUnavailable && !nfsEnabled}
              label="Enable NFS"
            />
            {nfsUnavailable ? (
              <AppAlert severity="warning">{nfsReason}</AppAlert>
            ) : null}
            {nfsEnabled ? (
              <div
                style={{
                  display: "flex",
                  flexWrap: "wrap",
                  gap: 8,
                  alignItems: "flex-start",
                }}
              >
                <AppTextField
                  fullWidth
                  helperText="Use * for public access, or enter host/IP/CIDR values separated by commas."
                  label="Allowed NFS Clients"
                  onChange={(event) => setNFSClients(event.target.value)}
                  placeholder="* or 192.168.1.0/24"
                  size="small"
                  style={{ flex: "2 1 260px" }}
                  value={nfsClients}
                />
                <NFSOptionsDropdown
                  onChange={setNFSOptions}
                  options={nfsOptions}
                />
              </div>
            ) : null}
          </div>

          {validationError ? (
            <AppAlert severity="error">{validationError}</AppAlert>
          ) : null}
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={isPending} onClick={handleClose}>
          Cancel
        </AppButton>
        <AppButton
          disabled={isPending}
          onClick={handleSave}
          variant="contained"
        >
          {isPending
            ? group
              ? "Saving..."
              : "Creating..."
            : group
              ? "Save Share"
              : "Create Share"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

const FolderShareCardActions = ({
  group,
  onEditShare,
  onDeleteSamba,
  onDeleteNFS,
  onFolderAction,
}: {
  group: ShareGroup;
  onEditShare: (group: ShareGroup) => void;
  onDeleteSamba: (share: SambaShare) => void;
  onDeleteNFS: (share: NFSExport) => void;
  onFolderAction: (action: FolderAction, folder: SharedFolder) => void;
}) => {
  const folder = group.folder;
  const [removeAnchor, setRemoveAnchor] = useState<HTMLButtonElement | null>(
    null,
  );

  return (
    <div style={{ display: "flex", gap: 2 }}>
      <AppActionIconButton
        color="var(--app-palette-primary-main)"
        icon="mdi:pencil-outline"
        iconSize={18}
        label="Edit Share"
        onClick={() => onEditShare(group)}
      />
      <AppActionIconButton
        color="var(--app-palette-error-main)"
        icon="mdi:trash-can-outline"
        iconSize={18}
        label="Remove Share"
        onClick={(event) => setRemoveAnchor(event.currentTarget)}
      />

      <AppMenu
        anchorEl={removeAnchor}
        anchorOrigin={{ vertical: "bottom", horizontal: "right" }}
        minWidth={150}
        onClose={() => setRemoveAnchor(null)}
        open={Boolean(removeAnchor)}
        transformOrigin={{ vertical: "top", horizontal: "right" }}
      >
        {folder && (folder.smb || folder.nfs.length > 0) ? (
          <AppMenuItem
            onClick={() => {
              setRemoveAnchor(null);
              onFolderAction("stop", folder);
            }}
          >
            Stop sharing
          </AppMenuItem>
        ) : null}
        {folder ? (
          <AppMenuItem
            onClick={() => {
              setRemoveAnchor(null);
              onFolderAction("delete", folder);
            }}
          >
            Delete folder and its data
          </AppMenuItem>
        ) : null}
        {!folder && group.samba ? (
          <AppMenuItem
            onClick={() => {
              setRemoveAnchor(null);
              onDeleteSamba(group.samba!);
            }}
          >
            Remove SMB
          </AppMenuItem>
        ) : null}
        {!folder && group.nfs ? (
          <AppMenuItem
            onClick={() => {
              setRemoveAnchor(null);
              onDeleteNFS(group.nfs!);
            }}
          >
            Remove NFS
          </AppMenuItem>
        ) : null}
      </AppMenu>
    </div>
  );
};

function renderExpandedContent(
  group: ShareGroup,
  setEditingShare: (share: ShareGroup) => void,
  setDeletingSamba: (share: SambaShare | null) => void,
  setDeletingNFS: (share: NFSExport | null) => void,
  onFolderAction: (action: FolderAction, folder: SharedFolder) => void,
): ReactNode {
  const folder = group.folder;
  return (
    <div style={{ display: "flex", gap: GAP_XL, flexWrap: "wrap" }}>
      <div style={{ flex: 1, minWidth: 280 }}>
        <AppTypography gutterBottom variant="subtitle2">
          <strong>Share Details:</strong>
        </AppTypography>
        <AppTypography variant="body2">
          <strong>Path:</strong> {group.path}
        </AppTypography>
        <AppTypography variant="body2">
          <strong>{folder ? "Access:" : "Comment:"}</strong>{" "}
          {group.comment || "-"}
        </AppTypography>
        <div style={{ marginTop: 10 }}>{renderProtocolSummary(group)}</div>
        <div style={{ marginTop: 12 }}>
          <AppButton
            onClick={() => setEditingShare(group)}
            size="small"
            variant="outlined"
          >
            Edit Share
          </AppButton>
          {folder && (folder.smb || folder.nfs.length > 0) ? (
            <AppButton
              onClick={() => onFolderAction("stop", folder)}
              size="small"
              style={{ marginInlineStart: 8 }}
            >
              Stop sharing
            </AppButton>
          ) : null}
          {folder ? (
            <AppButton
              color="error"
              onClick={() => onFolderAction("delete", folder)}
              size="small"
              style={{ marginInlineStart: 8 }}
            >
              Delete folder and its data
            </AppButton>
          ) : null}
        </div>
      </div>

      {group.samba ? (
        <div style={{ flex: 1, minWidth: 280 }}>
          <AppTypography gutterBottom variant="subtitle2">
            <strong>SMB</strong>
          </AppTypography>
          <div className="expand-panel__chips">
            {Object.entries(group.samba.properties).map(([key, value]) => (
              <Chip
                key={key}
                label={`${key} = ${value}`}
                size="small"
                variant="soft"
              />
            ))}
          </div>
          {!folder && (
            <AppButton
              color="error"
              onClick={() => setDeletingSamba(group.samba)}
              size="small"
              style={{ marginTop: 12 }}
            >
              Remove SMB
            </AppButton>
          )}
        </div>
      ) : null}

      {group.nfs ? (
        <div style={{ flex: 1, minWidth: 280 }}>
          <AppTypography gutterBottom variant="subtitle2">
            <strong>NFS</strong>
          </AppTypography>
          {group.nfs.clients.length > 0 ? (
            group.nfs.clients.map((client, index) => (
              <div key={index}>
                <AppTypography variant="body2">
                  <strong>{client.host}</strong>
                </AppTypography>
                <div className="expand-panel__chips">
                  {client.options?.length ? (
                    client.options.map((option) => (
                      <Chip
                        key={`${client.host}-${option}`}
                        label={option}
                        size="small"
                        variant="soft"
                      />
                    ))
                  ) : (
                    <AppTypography color="text.secondary" variant="body2">
                      (default options)
                    </AppTypography>
                  )}
                </div>
              </div>
            ))
          ) : (
            <AppTypography color="text.secondary" variant="body2">
              No NFS access rules configured
            </AppTypography>
          )}
          {!folder && (
            <AppButton
              color="error"
              onClick={() => setDeletingNFS(group.nfs)}
              size="small"
              style={{ marginTop: 12 }}
            >
              Remove NFS
            </AppButton>
          )}
        </div>
      ) : null}
    </div>
  );
}

const SharesPage = () => {
  const [viewMode, setViewMode] = useViewMode("shares");
  const [shareDialogOpen, setShareDialogOpen] = useState(false);
  const [editingShare, setEditingShare] = useState<ShareGroup | null>(null);
  const [deletingNFS, setDeletingNFS] = useState<NFSExport | null>(null);
  const [deleteNFSDialogOpen, setDeleteNFSDialogOpen] = useState(false);
  const [deletingSamba, setDeletingSamba] = useState<SambaShare | null>(null);
  const [deleteSambaDialogOpen, setDeleteSambaDialogOpen] = useState(false);

  // The dialog of a shared folder: creating one (folder null), or changing,
  // unsharing or deleting one. It is mounted while it is set, so each use
  // starts with fresh fields.
  const [folderDialog, setFolderDialog] = useState<{
    action: FolderAction;
    folder: SharedFolder | null;
  } | null>(null);
  const [folderDialogOpen, setFolderDialogOpen] = useState(false);
  const [addAnchor, setAddAnchor] = useState<HTMLElement | null>(null);
  // Mounted while it is open or closing, so it opens on what the server has.
  const [smbSettings, setSMBSettings] = useState<"open" | "closing" | null>(
    null,
  );
  const { privileged } = useAuth();
  const { isEnabled: btrfsEnabled } = useCapability("btrfsAvailable");
  const foldersAllowed = privileged && btrfsEnabled;

  // Stable, as the table's expanded rows are rendered through it.
  const openFolderDialog = useCallback(
    (action: FolderAction, folder: SharedFolder | null) => {
      setFolderDialog({ action, folder });
      setFolderDialogOpen(true);
    },
    [],
  );
  const openEditDialog = useCallback(
    (share: ShareGroup) => {
      if (share.folder) {
        openFolderDialog("edit", share.folder);
        return;
      }
      setEditingShare(share);
      setShareDialogOpen(true);
    },
    [openFolderDialog],
  );
  const openDeleteNFSDialog = useCallback((share: NFSExport | null) => {
    if (!share) return;
    setDeletingNFS(share);
    setDeleteNFSDialogOpen(true);
  }, []);
  const openDeleteSambaDialog = useCallback((share: SambaShare | null) => {
    if (!share) return;
    setDeletingSamba(share);
    setDeleteSambaDialogOpen(true);
  }, []);

  const { data: nfsShares, refetch: refetchNFS } = useSuspenseQuery({
    ...linuxio.shares.list_nfs_shares,
    refetchInterval: 10000,
  });
  const { data: sambaShares, refetch: refetchSamba } = useSuspenseQuery({
    ...linuxio.shares.list_samba_shares,
    refetchInterval: 10000,
  });

  // The record of shared folders is root's; without it they show as the
  // plain shares they are published as.
  const { data: folders } = useQuery({
    ...linuxio.shares.list_folders,
    enabled: foldersAllowed,
    refetchInterval: 10000,
  });

  const shareGroups = useMemo(
    () =>
      buildShareGroups(
        Array.isArray(sambaShares) ? sambaShares : [],
        Array.isArray(nfsShares) ? nfsShares : [],
        folders ?? [],
      ),
    [folders, nfsShares, sambaShares],
  );
  const sharesSurface = useReorderableSurface({
    getId: getShareGroupId,
    items: shareGroups,
    surface: "shares",
  });
  const sharesTableDnd = useReorderableTableDnd<ShareGroup, ShareGroup>({
    handleAriaLabel: "Reorder share",
    surface: sharesSurface,
  });
  const renderShareExpandedContent = useCallback<
    NonNullable<AppVirtualTableProps<ShareGroup>["renderExpandedContent"]>
  >(
    ({ original: group }) =>
      renderExpandedContent(
        group,
        openEditDialog,
        openDeleteSambaDialog,
        openDeleteNFSDialog,
        openFolderDialog,
      ),
    [
      openDeleteNFSDialog,
      openDeleteSambaDialog,
      openEditDialog,
      openFolderDialog,
    ],
  );

  const sharesActions = (
    <HeaderActions
      options={
        privileged ? (
          <AppActionIconButton
            ariaLabel="SMB settings"
            icon="mdi:cog-outline"
            iconSize={20}
            label="SMB settings"
            onClick={() => setSMBSettings("open")}
          />
        ) : undefined
      }
      create={
        <AppActionIconButton
          ariaLabel="Add Share"
          icon="mdi:plus"
          iconSize={20}
          label="Add Share"
          onClick={(event) =>
            foldersAllowed
              ? setAddAnchor(event.currentTarget)
              : setShareDialogOpen(true)
          }
        />
      }
      view={
        <ViewModeToggle
          alternateMode="table"
          onViewModeChange={setViewMode}
          viewMode={viewMode}
        />
      }
    />
  );

  const content = (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: GAP_LG,
        height: "100%",
        minHeight: 0,
      }}
    >
      {viewMode === "card" ? (
        shareGroups.length > 0 ? (
          <ReorderableCardGrid
            fillAvailable
            getId={getShareGroupId}
            renderItem={(group) => (
              <FolderShareCard
                actions={
                  <FolderShareCardActions
                    group={group}
                    onDeleteNFS={openDeleteNFSDialog}
                    onDeleteSamba={openDeleteSambaDialog}
                    onEditShare={openEditDialog}
                    onFolderAction={openFolderDialog}
                  />
                }
                comment={group.comment}
                name={group.name}
                path={group.path}
                protocolSummary={renderProtocolSummary(group)}
              />
            )}
            size={CARD_GRID_SIZE_DENSE}
            surface={sharesSurface}
          />
        ) : (
          <div style={{ textAlign: "center", paddingBlock: 24 }}>
            <AppTypography color="text.secondary" variant="body2">
              No shares configured. Add a folder share to get started.
            </AppTypography>
          </div>
        )
      ) : (
        <AppVirtualTable
          ariaLabel="Folder shares"
          columns={tableColumns}
          data={sharesSurface.items}
          dnd={sharesTableDnd}
          emptyMessage="No shares configured. Add a folder share to get started."
          fillAvailable
          getRowId={getShareGroupId}
          persistExpandedKey="folder-shares"
          renderExpandedContent={renderShareExpandedContent}
        />
      )}
    </div>
  );

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: GAP_LG,
        height: "100%",
        minHeight: 0,
      }}
    >
      <RoutedTabActions>{sharesActions}</RoutedTabActions>
      {content}

      <AppMenu
        anchorEl={addAnchor}
        anchorOrigin={{ vertical: "bottom", horizontal: "right" }}
        minWidth={220}
        onClose={() => setAddAnchor(null)}
        open={Boolean(addAnchor)}
        transformOrigin={{ vertical: "top", horizontal: "right" }}
      >
        <AppMenuItem
          onClick={() => {
            setAddAnchor(null);
            openFolderDialog("edit", null);
          }}
        >
          Create shared folder
        </AppMenuItem>
        <AppMenuItem
          onClick={() => {
            setAddAnchor(null);
            setShareDialogOpen(true);
          }}
        >
          Share an existing folder
        </AppMenuItem>
      </AppMenu>
      {folderDialog?.action === "edit" && (
        <SharedFolderDialog
          folder={folderDialog.folder}
          onClose={() => setFolderDialogOpen(false)}
          onExited={() => setFolderDialog(null)}
          open={folderDialogOpen}
        />
      )}
      {folderDialog?.action === "stop" && folderDialog.folder && (
        <StopSharingDialog
          folder={folderDialog.folder}
          onClose={() => setFolderDialogOpen(false)}
          onExited={() => setFolderDialog(null)}
          open={folderDialogOpen}
        />
      )}
      {smbSettings && (
        <SMBSettingsDialog
          onClose={() => setSMBSettings("closing")}
          onExited={() => setSMBSettings(null)}
          open={smbSettings === "open"}
        />
      )}
      {folderDialog?.action === "delete" && folderDialog.folder && (
        <DeleteSharedFolderDialog
          folder={folderDialog.folder}
          onClose={() => setFolderDialogOpen(false)}
          onExited={() => setFolderDialog(null)}
          open={folderDialogOpen}
        />
      )}

      <FolderShareDialog
        group={editingShare}
        key={editingShare?.id ?? "new-share"}
        onClose={() => setShareDialogOpen(false)}
        onExited={() => setEditingShare(null)}
        onSuccess={() => {
          void refetchSamba();
          void refetchNFS();
        }}
        open={shareDialogOpen}
      />
      <DeleteSambaShareDialog
        onClose={() => setDeleteSambaDialogOpen(false)}
        onExited={() => setDeletingSamba(null)}
        onSuccess={() => refetchSamba()}
        open={deleteSambaDialogOpen}
        share={deletingSamba}
      />
      <DeleteNFSShareDialog
        onClose={() => setDeleteNFSDialogOpen(false)}
        onExited={() => setDeletingNFS(null)}
        onSuccess={() => refetchNFS()}
        open={deleteNFSDialogOpen}
        share={deletingNFS}
      />
    </div>
  );
};

export default SharesPage;
