import { Icon } from "@iconify/react";
import React, { useRef, useState } from "react";

import { DeleteNFSShareDialog } from "./NFSShares";
import { DeleteSambaShareDialog } from "./SambaShares";
import NFSMounts from "../storage/NFSMounts";

import {
  linuxio,
  type NFSClient,
  type NFSExport,
  type SambaShare,
} from "@/api";
import FolderShareCard from "@/components/cards/FolderShareCard";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import PageLoader from "@/components/loaders/PageLoader";
import TabContainer from "@/components/tabbar/TabContainer";
import UnifiedCollapsibleTable, {
  UnifiedTableColumn,
} from "@/components/tables/UnifiedCollapsibleTable";
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
import AppGrid from "@/components/ui/AppGrid";
import AppIconButton from "@/components/ui/AppIconButton";
import AppMenu, { AppMenuItem } from "@/components/ui/AppMenu";
import AppPopover from "@/components/ui/AppPopover";
import { AppTableCell } from "@/components/ui/AppTable";
import AppTextField from "@/components/ui/AppTextField";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import PathPickerField from "@/components/ui/PathPickerField";
import { useCapability } from "@/hooks/useCapabilities";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useViewMode } from "@/hooks/useViewMode";
import { getMutationErrorMessage } from "@/utils/mutations";

type ShareGroup = {
  id: string;
  name: string;
  path: string;
  comment: string;
  samba: SambaShare | null;
  nfs: NFSExport | null;
};

interface ClientOptions {
  allSquash: boolean;
  crossmnt: boolean;
  insecure: boolean;
  noRootSquash: boolean;
  noSubtreeCheck: boolean;
  rw: boolean;
  sync: boolean;
}

interface CreateFolderShareDialogProps {
  onClose: () => void;
  onSuccess: () => void;
  open: boolean;
}

interface EditFolderShareDialogProps extends CreateFolderShareDialogProps {
  group: ShareGroup | null;
}

const defaultNFSOptions: ClientOptions = {
  rw: true,
  sync: true,
  noSubtreeCheck: true,
  noRootSquash: false,
  allSquash: false,
  insecure: false,
  crossmnt: false,
};

const nfsOptionLabels: { key: keyof ClientOptions; label: string }[] = [
  { key: "rw", label: "Read / Write" },
  { key: "sync", label: "Sync" },
  { key: "noSubtreeCheck", label: "No Subtree Check" },
  { key: "noRootSquash", label: "No Root Squash" },
  { key: "allSquash", label: "All Squash" },
  { key: "insecure", label: "Insecure" },
  { key: "crossmnt", label: "Crossmnt" },
];

const tableColumns: UnifiedTableColumn[] = [
  { field: "name", headerName: "Name", align: "left" },
  { field: "comment", headerName: "Comment", align: "left" },
  { field: "smb", headerName: "SMB", align: "left", width: "110px" },
  { field: "nfs", headerName: "NFS", align: "left", width: "110px" },
  { field: "path", headerName: "Path", align: "left" },
];

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
    };

    if (!existing.name) {
      existing.name = inferShareName(path);
    }
    existing.nfs = nfs;
    groups.set(path, existing);
  }

  return Array.from(groups.values()).sort((left, right) =>
    left.name.localeCompare(right.name),
  );
}

function nfsOptionsToStrings(options: ClientOptions): string[] {
  const values: string[] = [];
  values.push(options.rw ? "rw" : "ro");
  values.push(options.sync ? "sync" : "async");
  values.push(options.noSubtreeCheck ? "no_subtree_check" : "subtree_check");
  if (options.allSquash) {
    values.push("all_squash");
  } else {
    values.push(options.noRootSquash ? "no_root_squash" : "root_squash");
  }
  if (options.insecure) {
    values.push("insecure");
  }
  if (options.crossmnt) {
    values.push("crossmnt");
  }
  return values;
}

function nfsOptionsSummary(options: ClientOptions): string {
  return nfsOptionsToStrings(options).join(", ");
}

function nfsOptionsFromStrings(options: string[] = []): ClientOptions {
  const set = new Set(options);
  return {
    rw: !set.has("ro"),
    sync: !set.has("async"),
    noSubtreeCheck: !set.has("subtree_check"),
    noRootSquash: set.has("no_root_squash"),
    allSquash: set.has("all_squash"),
    insecure: set.has("insecure"),
    crossmnt: set.has("crossmnt"),
  };
}

function parseNFSClients(value: string, options: ClientOptions): NFSClient[] {
  return value
    .split(",")
    .map((client) => client.trim())
    .filter(Boolean)
    .map((host) => ({
      host,
      options: nfsOptionsToStrings(options),
    }));
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

function renderProtocolSummary(group: ShareGroup): React.ReactNode {
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
    </div>
  );
}

const NFSOptionsDropdown: React.FC<{
  options: ClientOptions;
  onChange: (next: ClientOptions) => void;
}> = ({ options, onChange }) => {
  const anchorRef = useRef<HTMLDivElement>(null);
  const [anchorEl, setAnchorEl] = useState<HTMLDivElement | null>(null);
  const [open, setOpen] = useState(false);

  const toggle = (key: keyof ClientOptions) =>
    onChange({ ...options, [key]: !options[key] });

  return (
    <>
      <div ref={anchorRef} style={{ flex: 1, minWidth: 220 }}>
        <AppTextField
          endAdornment={
            <Icon
              icon={open ? "mdi:chevron-up" : "mdi:chevron-down"}
              style={{ opacity: 0.5 }}
              width={18}
            />
          }
          fullWidth
          label="Options"
          onClick={() => {
            setAnchorEl(anchorRef.current);
            setOpen(true);
          }}
          size="small"
          style={{ cursor: "pointer" }}
          value={nfsOptionsSummary(options)}
        />
      </div>
      <AppPopover
        anchorEl={anchorEl}
        anchorOrigin={{ vertical: "bottom", horizontal: "left" }}
        matchAnchorWidth
        onClose={() => setOpen(false)}
        open={open}
        transformOrigin={{ vertical: "top", horizontal: "left" }}
      >
        <div style={{ padding: "6px 0" }}>
          {nfsOptionLabels.map(({ key, label }) => (
            <button
              key={key}
              onClick={() => toggle(key)}
              style={{
                display: "flex",
                alignItems: "center",
                gap: 10,
                width: "100%",
                padding: "7px 14px",
                background: "none",
                border: "none",
                cursor: "pointer",
                fontSize: "0.85rem",
                color: "inherit",
                textAlign: "left",
              }}
              type="button"
            >
              <span
                style={{
                  width: 10,
                  height: 10,
                  borderRadius: "50%",
                  backgroundColor: options[key] ? "#00E676" : "#9e9e9e",
                  flexShrink: 0,
                  transition: "background-color 150ms ease",
                }}
              />
              <span>{label}</span>
            </button>
          ))}
        </div>
      </AppPopover>
    </>
  );
};

const CreateFolderShareDialog: React.FC<CreateFolderShareDialogProps> = ({
  open,
  onClose,
  onSuccess,
}) => {
  const toast = useScopedToast({ href: "/shares", label: "Open shares" });
  const { reason: nfsReason, status: nfsStatus } =
    useCapability("nfsServerAvailable");
  const nfsUnavailable = nfsStatus === "unavailable";
  const [path, setPath] = useState("");
  const [sambaEnabled, setSambaEnabled] = useState(true);
  const [nfsEnabled, setNFSEnabled] = useState(false);
  const [sambaName, setSambaName] = useState("");
  const [comment, setComment] = useState("");
  const [sambaPublic, setSambaPublic] = useState(false);
  const [nfsClients, setNFSClients] = useState("*");
  const [nfsOptions, setNFSOptions] = useState<ClientOptions>({
    ...defaultNFSOptions,
  });
  const [validationError, setValidationError] = useState<string | null>(null);

  const sambaCreate = linuxio.shares.create_samba_share.useMutation();
  const nfsCreate = linuxio.shares.create_nfs_share.useMutation();

  const isPending = sambaCreate.isPending || nfsCreate.isPending;
  const resolvedName = sambaName.trim() || inferShareName(path);

  const handleClose = () => {
    setPath("");
    setSambaEnabled(true);
    setNFSEnabled(false);
    setSambaName("");
    setComment("");
    setSambaPublic(false);
    setNFSClients("*");
    setNFSOptions({ ...defaultNFSOptions });
    setValidationError(null);
    onClose();
  };

  const handleCreate = async () => {
    const normalizedPath = normalizeSharePath(path.trim());
    const parsedNFSClients = parseNFSClients(nfsClients, nfsOptions);

    if (!normalizedPath) {
      setValidationError("Folder path is required");
      return;
    }
    if (!sambaEnabled && !nfsEnabled) {
      setValidationError("Enable SMB and/or NFS for this folder share");
      return;
    }
    if (sambaEnabled && !resolvedName) {
      setValidationError("Share name is required when SMB is enabled");
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

    let createdAny = false;

    try {
      if (sambaEnabled) {
        const sambaProperties: Record<string, string> = {
          path: normalizedPath,
          browseable: "yes",
          "read only": "no",
          "guest ok": sambaPublic ? "yes" : "no",
        };
        if (comment.trim()) {
          sambaProperties["comment"] = comment.trim();
        }

        await sambaCreate.mutateAsync({
          name: resolvedName,
          properties: sambaProperties,
        });
        createdAny = true;
      }

      if (nfsEnabled) {
        await nfsCreate.mutateAsync({
          path: normalizedPath,
          clients: parsedNFSClients,
        });
        createdAny = true;
      }

      toast.success(`Folder share created for ${normalizedPath}`);
      onSuccess();
      handleClose();
    } catch (error) {
      const message = getMutationErrorMessage(
        error as Error,
        "Failed to create folder share",
      );

      if (createdAny) {
        toast.error(
          `${message}. Some protocols may already have been created.`,
        );
        onSuccess();
        handleClose();
        return;
      }

      setValidationError(message);
    }
  };

  return (
    <GeneralDialog fullWidth maxWidth="sm" onClose={handleClose} open={open}>
      <AppDialogTitle>Add Folder Share</AppDialogTitle>
      <AppDialogContent>
        <div
          style={{
            display: "flex",
            flexDirection: "column",
            gap: 10,
            marginTop: 8,
          }}
        >
          <PathPickerField
            label="Folder Path"
            onChange={setPath}
            value={path}
          />

          <div
            style={{
              display: "flex",
              flexDirection: "column",
              gap: 6,
              padding: 10,
              borderRadius: 10,
              background: "rgba(255,255,255,0.03)",
            }}
          >
            <AppFormControlLabel
              control={
                <AppCheckbox
                  checked={sambaEnabled}
                  onChange={(event) => setSambaEnabled(event.target.checked)}
                />
              }
              label="Enable SMB"
            />
            {sambaEnabled ? (
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

          <div
            style={{
              display: "flex",
              flexDirection: "column",
              gap: 6,
              padding: 10,
              borderRadius: 10,
              background: "rgba(255,255,255,0.03)",
            }}
          >
            <AppFormControlLabel
              control={
                <AppCheckbox
                  checked={nfsEnabled}
                  onChange={(event) => setNFSEnabled(event.target.checked)}
                />
              }
              disabled={nfsUnavailable}
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
          onClick={handleCreate}
          variant="contained"
        >
          {isPending ? "Creating..." : "Create Share"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

const EditFolderShareDialog: React.FC<EditFolderShareDialogProps> = ({
  open,
  onClose,
  onSuccess,
  group,
}) => {
  const toast = useScopedToast({ href: "/shares", label: "Open shares" });
  const { reason: nfsReason, status: nfsStatus } =
    useCapability("nfsServerAvailable");
  const nfsUnavailable = nfsStatus === "unavailable";
  const [sambaEnabled, setSambaEnabled] = useState(Boolean(group?.samba));
  const [nfsEnabled, setNFSEnabled] = useState(Boolean(group?.nfs));
  const [sambaName, setSambaName] = useState(
    group?.samba?.name ?? inferShareName(group?.path ?? ""),
  );
  const [comment, setComment] = useState(
    group?.samba?.properties["comment"] ?? group?.comment ?? "",
  );
  const [sambaPublic, setSambaPublic] = useState(
    group?.samba?.properties["guest ok"] === "yes",
  );
  const [nfsClients, setNFSClients] = useState(
    group?.nfs?.clients.map((client) => client.host).join(", ") || "*",
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

  if (!group) {
    return null;
  }

  const handleSave = async () => {
    const resolvedName = sambaName.trim() || inferShareName(group.path);
    const parsedNFSClients = parseNFSClients(nfsClients, nfsOptions);

    if (!sambaEnabled && !nfsEnabled) {
      setValidationError("Enable SMB and/or NFS for this folder share");
      return;
    }
    if (sambaEnabled && !resolvedName) {
      setValidationError("Share name is required when SMB is enabled");
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

    let changedAny = false;

    try {
      if (sambaEnabled) {
        const sambaProperties = buildFolderSambaProperties(
          group.path,
          comment,
          sambaPublic,
          group.samba?.properties,
        );

        if (group.samba) {
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
      } else if (group.samba) {
        await sambaDelete.mutateAsync({ name: group.samba.name });
        changedAny = true;
      }

      if (nfsEnabled) {
        if (group.nfs) {
          await nfsUpdate.mutateAsync({
            path: group.path,
            clients: parsedNFSClients,
          });
        } else {
          await nfsCreate.mutateAsync({
            path: group.path,
            clients: parsedNFSClients,
          });
        }
        changedAny = true;
      } else if (group.nfs) {
        await nfsDelete.mutateAsync({ path: group.path });
        changedAny = true;
      }

      toast.success(`Folder share updated for ${group.path}`);
      onSuccess();
      onClose();
    } catch (error) {
      const message = getMutationErrorMessage(
        error as Error,
        "Failed to update folder share",
      );

      if (changedAny) {
        toast.error(`${message}. Some changes may already have been applied.`);
        onSuccess();
        onClose();
        return;
      }

      setValidationError(message);
    }
  };

  return (
    <GeneralDialog fullWidth maxWidth="sm" onClose={onClose} open={open}>
      <AppDialogTitle>Edit Folder Share</AppDialogTitle>
      <AppDialogContent>
        <div
          style={{
            display: "flex",
            flexDirection: "column",
            gap: 10,
            marginTop: 8,
          }}
        >
          <AppTextField
            disabled
            fullWidth
            label="Folder Path"
            shrinkLabel
            size="small"
            value={group.path}
          />

          <div
            style={{
              display: "flex",
              flexDirection: "column",
              gap: 6,
              padding: 10,
              borderRadius: 10,
              background: "rgba(255,255,255,0.03)",
            }}
          >
            <AppFormControlLabel
              control={
                <AppCheckbox
                  checked={sambaEnabled}
                  onChange={(event) => setSambaEnabled(event.target.checked)}
                />
              }
              label="Enable SMB"
            />
            {sambaEnabled ? (
              <>
                <AppTextField
                  className="app-text-field--compact-copy"
                  fullWidth
                  label="Share Name"
                  onChange={(event) => setSambaName(event.target.value)}
                  placeholder={inferShareName(group.path)}
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

          <div
            style={{
              display: "flex",
              flexDirection: "column",
              gap: 6,
              padding: 10,
              borderRadius: 10,
              background: "rgba(255,255,255,0.03)",
            }}
          >
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
        <AppButton disabled={isPending} onClick={onClose}>
          Cancel
        </AppButton>
        <AppButton
          disabled={isPending}
          onClick={handleSave}
          variant="contained"
        >
          {isPending ? "Saving..." : "Save Share"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

const FolderShareCardActions: React.FC<{
  group: ShareGroup;
  onEditShare: (group: ShareGroup) => void;
  onDeleteSamba: (share: SambaShare) => void;
  onDeleteNFS: (share: NFSExport) => void;
}> = ({ group, onEditShare, onDeleteSamba, onDeleteNFS }) => {
  const [removeAnchor, setRemoveAnchor] = useState<HTMLButtonElement | null>(
    null,
  );

  return (
    <div style={{ display: "flex", gap: 2 }}>
      <AppTooltip title="Edit Share">
        <AppIconButton
          color="primary"
          onClick={() => onEditShare(group)}
          size="small"
        >
          <Icon icon="mdi:pencil-outline" width={18} />
        </AppIconButton>
      </AppTooltip>
      <AppTooltip title="Remove Share">
        <AppIconButton
          color="error"
          onClick={(event) => setRemoveAnchor(event.currentTarget)}
          size="small"
        >
          <Icon icon="mdi:trash-can-outline" width={18} />
        </AppIconButton>
      </AppTooltip>

      <AppMenu
        anchorEl={removeAnchor}
        anchorOrigin={{ vertical: "bottom", horizontal: "right" }}
        minWidth={150}
        onClose={() => setRemoveAnchor(null)}
        open={Boolean(removeAnchor)}
        transformOrigin={{ vertical: "top", horizontal: "right" }}
      >
        {group.samba ? (
          <AppMenuItem
            onClick={() => {
              setRemoveAnchor(null);
              onDeleteSamba(group.samba!);
            }}
          >
            Remove SMB
          </AppMenuItem>
        ) : null}
        {group.nfs ? (
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
  setEditingShare: (share: ShareGroup | null) => void,
  setDeletingSamba: (share: SambaShare | null) => void,
  setDeletingNFS: (share: NFSExport | null) => void,
): React.ReactNode {
  return (
    <div style={{ display: "flex", gap: 24, flexWrap: "wrap" }}>
      <div style={{ flex: 1, minWidth: 280 }}>
        <AppTypography gutterBottom variant="subtitle2">
          <strong>Share Details:</strong>
        </AppTypography>
        <AppTypography variant="body2">
          <strong>Path:</strong> {group.path}
        </AppTypography>
        <AppTypography variant="body2">
          <strong>Comment:</strong> {group.comment || "-"}
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
          <AppButton
            color="error"
            onClick={() => setDeletingSamba(group.samba)}
            size="small"
            style={{ marginTop: 12 }}
          >
            Remove SMB
          </AppButton>
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
          <AppButton
            color="error"
            onClick={() => setDeletingNFS(group.nfs)}
            size="small"
            style={{ marginTop: 12 }}
          >
            Remove NFS
          </AppButton>
        </div>
      ) : null}
    </div>
  );
}

const SharesPage: React.FC = () => {
  const { reason: nfsReason, status: nfsStatus } =
    useCapability("nfsClientAvailable");
  const nfsUnavailable = nfsStatus === "unavailable";
  const [viewMode, setViewMode] = useViewMode("shares", "table");
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [editingShare, setEditingShare] = useState<ShareGroup | null>(null);
  const [deletingNFS, setDeletingNFS] = useState<NFSExport | null>(null);
  const [deletingSamba, setDeletingSamba] = useState<SambaShare | null>(null);
  const [mountNFSHandler, setMountNFSHandler] = useState<(() => void) | null>(
    null,
  );
  const [nfsView, setNfsView] = useViewMode("shares.mounts", "table");

  const {
    data: nfsShares = [],
    isPending: loadingNFS,
    refetch: refetchNFS,
  } = linuxio.shares.list_nfs_shares.useQuery({
    refetchInterval: 10000,
  });
  const {
    data: sambaShares = [],
    isPending: loadingSamba,
    refetch: refetchSamba,
  } = linuxio.shares.list_samba_shares.useQuery({
    refetchInterval: 10000,
  });

  if (loadingNFS || loadingSamba) {
    return <PageLoader />;
  }

  const shareGroups = buildShareGroups(
    Array.isArray(sambaShares) ? sambaShares : [],
    Array.isArray(nfsShares) ? nfsShares : [],
  );

  const sharesActions = (
    <div
      style={{
        display: "flex",
        gap: 8,
        alignItems: "center",
        flexWrap: "wrap",
      }}
    >
      <AppTooltip
        title={
          viewMode === "table" ? "Switch to card view" : "Switch to table view"
        }
      >
        <AppIconButton
          onClick={() => setViewMode(viewMode === "table" ? "card" : "table")}
          size="small"
        >
          {viewMode === "table" ? (
            <Icon height={20} icon="mdi:card-multiple" width={20} />
          ) : (
            <Icon height={20} icon="mdi:table" width={20} />
          )}
        </AppIconButton>
      </AppTooltip>
      <AppButton
        onClick={() => setCreateDialogOpen(true)}
        size="small"
        startIcon={<Icon height={20} icon="mdi:plus" width={20} />}
        variant="contained"
      >
        Add Share
      </AppButton>
    </div>
  );

  const sharesContent = (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      {viewMode === "card" ? (
        shareGroups.length > 0 ? (
          <AppGrid container spacing={2}>
            {shareGroups.map((group) => (
              <AppGrid key={group.id} size={{ xs: 12, sm: 6, md: 4, lg: 2 }}>
                <FolderShareCard
                  actions={
                    <FolderShareCardActions
                      group={group}
                      onDeleteNFS={(share) => setDeletingNFS(share)}
                      onDeleteSamba={(share) => setDeletingSamba(share)}
                      onEditShare={(shareGroup) => setEditingShare(shareGroup)}
                    />
                  }
                  comment={group.comment}
                  name={group.name}
                  path={group.path}
                  protocolSummary={renderProtocolSummary(group)}
                />
              </AppGrid>
            ))}
          </AppGrid>
        ) : (
          <div style={{ textAlign: "center", paddingBlock: 24 }}>
            <AppTypography color="text.secondary" variant="body2">
              No shares configured. Add a folder share to get started.
            </AppTypography>
          </div>
        )
      ) : (
        <UnifiedCollapsibleTable
          columns={tableColumns}
          data={shareGroups}
          emptyMessage="No shares configured. Add a folder share to get started."
          getRowKey={(group) => group.id}
          renderExpandedContent={(group) =>
            renderExpandedContent(
              group,
              setEditingShare,
              setDeletingSamba,
              setDeletingNFS,
            )
          }
          renderMainRow={(group) => (
            <>
              <AppTableCell>
                <AppTypography fontWeight={700} variant="body2">
                  {group.name}
                </AppTypography>
              </AppTableCell>
              <AppTableCell>
                <AppTypography color="text.secondary" variant="body2">
                  {group.comment || "-"}
                </AppTypography>
              </AppTableCell>
              <AppTableCell>
                <AppTypography variant="body2">
                  {getSambaAccessLabel(group.samba)}
                </AppTypography>
              </AppTableCell>
              <AppTableCell>
                <AppTypography variant="body2">
                  {getNFSAccessLabel(group.nfs)}
                </AppTypography>
              </AppTableCell>
              <AppTableCell>
                <AppTypography
                  style={{ fontFamily: "monospace" }}
                  variant="body2"
                >
                  {group.path}
                </AppTypography>
              </AppTableCell>
            </>
          )}
        />
      )}
    </div>
  );

  const mountsContent = (
    <NFSMounts
      onMountCreateHandler={(handler) => setMountNFSHandler(() => handler)}
      viewMode={nfsView}
    />
  );

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      <TabContainer
        defaultTab="shares"
        tabs={[
          {
            value: "shares",
            label: "Shares",
            component: sharesContent,
            rightContent: sharesActions,
          },
          {
            value: "mounts",
            label: "Mounts",
            component: mountsContent,
            rightContent: (
              <>
                <AppTooltip
                  title={
                    nfsView === "table"
                      ? "Switch to card view"
                      : "Switch to table view"
                  }
                >
                  <AppIconButton
                    onClick={() =>
                      setNfsView(nfsView === "table" ? "card" : "table")
                    }
                    size="small"
                  >
                    {nfsView === "table" ? (
                      <Icon height={20} icon="mdi:card-multiple" width={20} />
                    ) : (
                      <Icon height={20} icon="mdi:table" width={20} />
                    )}
                  </AppIconButton>
                </AppTooltip>
                {mountNFSHandler && (
                  <AppTooltip title={nfsUnavailable ? nfsReason : "Mount NFS"}>
                    <span>
                      <AppButton
                        disabled={nfsUnavailable}
                        onClick={mountNFSHandler}
                        size="small"
                        startIcon={
                          <Icon height={20} icon="mdi:plus" width={20} />
                        }
                        variant="contained"
                      >
                        Mount NFS
                      </AppButton>
                    </span>
                  </AppTooltip>
                )}
              </>
            ),
          },
        ]}
        urlParam="sharesTab"
      />

      <CreateFolderShareDialog
        onClose={() => setCreateDialogOpen(false)}
        onSuccess={() => {
          refetchSamba();
          refetchNFS();
        }}
        open={createDialogOpen}
      />
      <EditFolderShareDialog
        group={editingShare}
        key={editingShare?.id ?? "no-share"}
        onClose={() => setEditingShare(null)}
        onSuccess={() => {
          refetchSamba();
          refetchNFS();
        }}
        open={editingShare !== null}
      />
      <DeleteSambaShareDialog
        onClose={() => setDeletingSamba(null)}
        onSuccess={() => refetchSamba()}
        open={deletingSamba !== null}
        share={deletingSamba}
      />
      <DeleteNFSShareDialog
        onClose={() => setDeletingNFS(null)}
        onSuccess={() => refetchNFS()}
        open={deletingNFS !== null}
        share={deletingNFS}
      />
    </div>
  );
};

export default SharesPage;
