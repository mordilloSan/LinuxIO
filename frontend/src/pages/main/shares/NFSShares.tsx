import { Icon } from "@iconify/react";
import { useQueryClient } from "@tanstack/react-query";
import React, { useCallback, useEffect, useRef, useState } from "react";

import { linuxio, type NFSClient, type NFSExport } from "@/api";
import NFSShareCard from "@/components/cards/NFSShareCard";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import PageLoader from "@/components/loaders/PageLoader";
import UnifiedCollapsibleTable, {
  UnifiedTableColumn,
} from "@/components/tables/UnifiedCollapsibleTable";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppGrid from "@/components/ui/AppGrid";
import AppIconButton from "@/components/ui/AppIconButton";
import AppPopover from "@/components/ui/AppPopover";
import { AppTableCell } from "@/components/ui/AppTable";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import DirectoryTree from "@/components/ui/DirectoryTree";
import { useCapability } from "@/hooks/useCapabilities";
import { useScopedToast } from "@/hooks/useScopedToast";
import { getMutationErrorMessage } from "@/utils/mutations";

interface NFSSharesProps {
  onCreateHandler?: (handler: () => void) => void;
  viewMode?: "table" | "card";
}

// ============================================================================
// NFS option model — each toggle represents a mutually exclusive pair
// ============================================================================

interface ClientOptions {
  allSquash: boolean; // all_squash — overrides root squash when on
  crossmnt: boolean; // crossmnt
  insecure: boolean; // insecure (true) vs secure (default)
  noRootSquash: boolean; // no_root_squash (true) vs root_squash
  noSubtreeCheck: boolean; // no_subtree_check (true) vs subtree_check
  rw: boolean; // rw (true) vs ro (false)
  sync: boolean; // sync (true) vs async (false)
}

interface ClientRow {
  host: string;
  opts: ClientOptions;
}

const defaultOpts: ClientOptions = {
  rw: true,
  sync: true,
  noSubtreeCheck: true,
  noRootSquash: false,
  allSquash: false,
  insecure: false,
  crossmnt: false,
};

const optionLabels: { key: keyof ClientOptions; label: string }[] = [
  { key: "rw", label: "Read / Write" },
  { key: "sync", label: "Sync" },
  { key: "noSubtreeCheck", label: "No Subtree Check" },
  { key: "noRootSquash", label: "No Root Squash" },
  { key: "allSquash", label: "All Squash" },
  { key: "insecure", label: "Insecure" },
  { key: "crossmnt", label: "Crossmnt" },
];

function optsToStrings(o: ClientOptions): string[] {
  const out: string[] = [];
  out.push(o.rw ? "rw" : "ro");
  out.push(o.sync ? "sync" : "async");
  out.push(o.noSubtreeCheck ? "no_subtree_check" : "subtree_check");
  if (o.allSquash) {
    out.push("all_squash");
  } else {
    out.push(o.noRootSquash ? "no_root_squash" : "root_squash");
  }
  if (o.insecure) out.push("insecure");
  if (o.crossmnt) out.push("crossmnt");
  return out;
}

function optsSummary(o: ClientOptions): string {
  return optsToStrings(o).join(", ");
}

function stringsToOpts(options: string[]): ClientOptions {
  const set = new Set(options);
  return {
    rw: !set.has("ro"),
    sync: !set.has("async"),
    noSubtreeCheck: set.has("no_subtree_check"),
    noRootSquash: set.has("no_root_squash"),
    allSquash: set.has("all_squash"),
    insecure: set.has("insecure"),
    crossmnt: set.has("crossmnt"),
  };
}

function rowsToNFSClients(rows: ClientRow[]): NFSClient[] {
  return rows
    .filter((r) => r.host.trim())
    .map((r) => ({ host: r.host.trim(), options: optsToStrings(r.opts) }));
}

function nfsClientsToRows(clients: NFSClient[]): ClientRow[] {
  if (!clients || clients.length === 0)
    return [{ host: "*", opts: { ...defaultOpts } }];
  return clients.map((c) => ({
    host: c.host,
    opts: stringsToOpts(c.options ?? []),
  }));
}

// ============================================================================
// Options dropdown — read-only input that opens a popover with dot toggles
// ============================================================================

const OptionsDropdown: React.FC<{
  opts: ClientOptions;
  onChange: (next: ClientOptions) => void;
}> = ({ opts, onChange }) => {
  const anchorRef = useRef<HTMLDivElement>(null);
  const [anchorEl, setAnchorEl] = useState<HTMLDivElement | null>(null);
  const [open, setOpen] = useState(false);

  const handleOpen = () => {
    setAnchorEl(anchorRef.current);
    setOpen(true);
  };

  const toggle = (key: keyof ClientOptions) =>
    onChange({ ...opts, [key]: !opts[key] });

  return (
    <>
      <div ref={anchorRef} style={{ flex: 1 }}>
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
          onClick={handleOpen}
          size="small"
          style={{ cursor: "pointer" }}
          value={optsSummary(opts)}
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
          {optionLabels.map(({ key, label }) => (
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
                  backgroundColor: opts[key] ? "#00E676" : "#9e9e9e",
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

// ============================================================================
// Client row — host input + options dropdown + remove button
// ============================================================================

const ClientRowEditor: React.FC<{
  client: ClientRow;
  index: number;
  canRemove: boolean;
  onChange: (index: number, next: ClientRow) => void;
  onRemove: (index: number) => void;
}> = ({ client, index, canRemove, onChange, onRemove }) => (
  <div style={{ display: "flex", gap: 8, alignItems: "flex-start" }}>
    <AppTextField
      label="Host"
      onChange={(e) => onChange(index, { ...client, host: e.target.value })}
      placeholder="e.g., 192.168.1.0/24 or *"
      size="small"
      style={{ flex: 1 }}
      value={client.host}
    />
    <OptionsDropdown
      onChange={(next) => onChange(index, { ...client, opts: next })}
      opts={client.opts}
    />
    {canRemove && (
      <AppIconButton onClick={() => onRemove(index)} size="small">
        &times;
      </AppIconButton>
    )}
  </div>
);

// ============================================================================
// Path picker — input that opens a directory tree popover
// ============================================================================

const PathPicker: React.FC<{
  value: string;
  onChange: (path: string) => void;
}> = ({ value, onChange }) => {
  const anchorRef = useRef<HTMLDivElement>(null);
  const [anchorEl, setAnchorEl] = useState<HTMLDivElement | null>(null);
  const [open, setOpen] = useState(false);

  const handleOpen = () => {
    setAnchorEl(anchorRef.current);
    setOpen(true);
  };

  const handleSelect = (path: string) => {
    onChange(path);
  };

  return (
    <>
      <div ref={anchorRef}>
        <AppTextField
          endAdornment={
            <Icon
              icon={open ? "mdi:chevron-up" : "mdi:chevron-down"}
              style={{ opacity: 0.5 }}
              width={18}
            />
          }
          fullWidth
          label="Export Path"
          onClick={handleOpen}
          placeholder="Click to select a folder"
          shrinkLabel
          size="small"
          style={{ cursor: "pointer" }}
          value={value}
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
        <DirectoryTree onSelect={handleSelect} selectedPath={value} />
      </AppPopover>
    </>
  );
};

// ============================================================================
// Create NFS Share Dialog
// ============================================================================

interface CreateDialogProps {
  onClose: () => void;
  onSuccess: () => void;
  open: boolean;
}

export const CreateNFSShareDialog: React.FC<CreateDialogProps> = ({
  open,
  onClose,
  onSuccess,
}) => {
  const toast = useScopedToast({ href: "/shares", label: "Open shares" });
  const queryClient = useQueryClient();
  const [path, setPath] = useState("");
  const [clients, setClients] = useState<ClientRow[]>([
    { host: "*", opts: { ...defaultOpts } },
  ]);
  const [validationError, setValidationError] = useState<string | null>(null);

  const { mutate: createShare, isPending } =
    linuxio.shares.create_nfs_share.useMutation({
      onSuccess: () => {
        toast.success(`NFS export created for ${path}`);
        queryClient.invalidateQueries({
          queryKey: linuxio.shares.list_nfs_shares.queryKey(),
        });
        onSuccess();
        handleClose();
      },
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to create NFS export"),
        );
      },
    });

  const handleCreate = () => {
    if (!path) {
      setValidationError("Export path is required");
      return;
    }
    const parsed = rowsToNFSClients(clients);
    if (parsed.length === 0) {
      setValidationError("At least one client is required");
      return;
    }
    setValidationError(null);
    createShare([path, parsed]);
  };

  const handleClose = () => {
    setPath("");
    setClients([{ host: "*", opts: { ...defaultOpts } }]);
    setValidationError(null);
    onClose();
  };

  const handleClientChange = (i: number, next: ClientRow) =>
    setClients((prev) => prev.map((c, idx) => (idx === i ? next : c)));

  const handleClientRemove = (i: number) =>
    setClients((prev) => prev.filter((_, idx) => idx !== i));

  return (
    <GeneralDialog fullWidth maxWidth="sm" onClose={handleClose} open={open}>
      <AppDialogTitle>Create NFS Export</AppDialogTitle>
      <AppDialogContent>
        <div
          style={{
            display: "flex",
            flexDirection: "column",
            gap: 8,
            marginTop: 8,
          }}
        >
          <PathPicker onChange={setPath} value={path} />
          <AppTypography style={{ marginTop: 4 }} variant="subtitle2">
            Client Access Rules
          </AppTypography>
          {clients.map((client, i) => (
            <ClientRowEditor
              canRemove={clients.length > 1}
              client={client}
              index={i}
              key={i}
              onChange={handleClientChange}
              onRemove={handleClientRemove}
            />
          ))}
          <AppButton
            onClick={() =>
              setClients((prev) => [
                ...prev,
                { host: "", opts: { ...defaultOpts } },
              ])
            }
            size="small"
            style={{ alignSelf: "flex-start" }}
            variant="outlined"
          >
            Add Client
          </AppButton>
          {validationError && (
            <AppAlert severity="error">{validationError}</AppAlert>
          )}
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
          {isPending ? "Creating..." : "Create"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

// ============================================================================
// Edit NFS Share Dialog
// ============================================================================

interface EditDialogProps {
  onClose: () => void;
  onSuccess: () => void;
  open: boolean;
  share: NFSExport | null;
}

export const EditNFSShareDialog: React.FC<EditDialogProps> = ({
  open,
  onClose,
  share,
  onSuccess,
}) => {
  const toast = useScopedToast({ href: "/shares", label: "Open shares" });
  const queryClient = useQueryClient();
  const [clients, setClients] = useState<ClientRow[]>(() =>
    share
      ? nfsClientsToRows(share.clients)
      : [{ host: "*", opts: { ...defaultOpts } }],
  );

  const { mutate: updateShare, isPending } =
    linuxio.shares.update_nfs_share.useMutation({
      onSuccess: () => {
        toast.success(`NFS export updated for ${share?.path}`);
        queryClient.invalidateQueries({
          queryKey: linuxio.shares.list_nfs_shares.queryKey(),
        });
        onSuccess();
        handleClose();
      },
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to update NFS export"),
        );
      },
    });

  const handleSave = () => {
    if (!share) return;
    const parsed = rowsToNFSClients(clients);
    if (parsed.length === 0) return;
    updateShare([share.path, parsed]);
  };

  const handleClose = () => {
    setClients([{ host: "*", opts: { ...defaultOpts } }]);
    onClose();
  };

  const handleClientChange = (i: number, next: ClientRow) =>
    setClients((prev) => prev.map((c, idx) => (idx === i ? next : c)));

  const handleClientRemove = (i: number) =>
    setClients((prev) => prev.filter((_, idx) => idx !== i));

  return (
    <GeneralDialog
      fullWidth
      key={share?.path}
      maxWidth="sm"
      onClose={handleClose}
      open={open}
    >
      <AppDialogTitle>Edit NFS Export</AppDialogTitle>
      <AppDialogContent>
        <div
          style={{
            display: "flex",
            flexDirection: "column",
            gap: 8,
            marginTop: 8,
          }}
        >
          <AppTextField
            disabled
            fullWidth
            label="Export Path"
            size="small"
            value={share?.path || ""}
          />
          <AppTypography style={{ marginTop: 4 }} variant="subtitle2">
            Client Access Rules
          </AppTypography>
          {clients.map((client, i) => (
            <ClientRowEditor
              canRemove={clients.length > 1}
              client={client}
              index={i}
              key={i}
              onChange={handleClientChange}
              onRemove={handleClientRemove}
            />
          ))}
          <AppButton
            onClick={() =>
              setClients((prev) => [
                ...prev,
                { host: "", opts: { ...defaultOpts } },
              ])
            }
            size="small"
            style={{ alignSelf: "flex-start" }}
            variant="outlined"
          >
            Add Client
          </AppButton>
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
          {isPending ? "Saving..." : "Save"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

// ============================================================================
// Delete NFS Share Dialog
// ============================================================================

interface DeleteDialogProps {
  onClose: () => void;
  onSuccess: () => void;
  open: boolean;
  share: NFSExport | null;
}

export const DeleteNFSShareDialog: React.FC<DeleteDialogProps> = ({
  open,
  onClose,
  share,
  onSuccess,
}) => {
  const toast = useScopedToast({ href: "/shares", label: "Open shares" });
  const queryClient = useQueryClient();
  const { mutate: deleteShare, isPending } =
    linuxio.shares.delete_nfs_share.useMutation({
      onSuccess: () => {
        toast.success(`Removed NFS export for ${share?.path}`);
        queryClient.invalidateQueries({
          queryKey: linuxio.shares.list_nfs_shares.queryKey(),
        });
        onSuccess();
        onClose();
      },
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to remove NFS export"),
        );
      },
    });

  const handleDelete = () => {
    if (!share) return;
    deleteShare([share.path]);
  };

  return (
    <GeneralDialog fullWidth maxWidth="sm" onClose={onClose} open={open}>
      <AppDialogTitle>Remove NFS Export</AppDialogTitle>
      <AppDialogContent>
        <AppDialogContentText>
          Are you sure you want to remove this NFS export?
        </AppDialogContentText>
        {share && (
          <div style={{ marginTop: 8, marginBottom: 8 }}>
            <AppTypography variant="body2">
              <strong>Path:</strong> {share.path}
            </AppTypography>
            <AppTypography variant="body2">
              <strong>Clients:</strong>{" "}
              {share.clients.map((c) => c.host).join(", ")}
            </AppTypography>
          </div>
        )}
        <AppAlert severity="warning">
          This will remove the export from /etc/exports and re-export.
        </AppAlert>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={isPending} onClick={onClose}>
          Cancel
        </AppButton>
        <AppButton
          color="error"
          disabled={isPending}
          onClick={handleDelete}
          variant="contained"
        >
          {isPending ? "Removing..." : "Remove"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

// ============================================================================
// NFS Shares Component
// ============================================================================

const NFSShares: React.FC<NFSSharesProps> = ({
  onCreateHandler,
  viewMode = "table",
}) => {
  const toast = useScopedToast({ href: "/shares", label: "Open shares" });
  const { reason: nfsReason, status: nfsStatus } =
    useCapability("nfsServerAvailable");
  const nfsUnavailable = nfsStatus === "unavailable";
  const [createOpen, setCreateOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [selected, setSelected] = useState<NFSExport | null>(null);

  const {
    data: shares = [],
    isPending: loading,
    refetch,
  } = linuxio.shares.list_nfs_shares.useQuery({
    refetchInterval: 10000,
  });

  const handleCreate = useCallback(() => {
    if (nfsUnavailable) {
      toast.error(nfsReason);
      return;
    }
    setCreateOpen(true);
  }, [nfsUnavailable, nfsReason, toast]);

  useEffect(() => {
    if (onCreateHandler) {
      onCreateHandler(handleCreate);
    }
  }, [onCreateHandler, handleCreate]);

  const handleEdit = (share: NFSExport) => {
    setSelected(share);
    setEditOpen(true);
  };

  const handleDelete = (share: NFSExport) => {
    setSelected(share);
    setDeleteOpen(true);
  };

  if (loading) {
    return <PageLoader />;
  }

  const sharesList = Array.isArray(shares) ? shares : [];

  const columns: UnifiedTableColumn[] = [
    { field: "path", headerName: "Export Path", align: "left" },
    { field: "clients", headerName: "Clients", align: "left" },
    {
      field: "status",
      headerName: "Status",
      align: "center",
      width: "100px",
    },
    { field: "actions", headerName: "", align: "right", width: "160px" },
  ];

  return (
    <div>
      {nfsUnavailable ? (
        <AppAlert severity="warning">{nfsReason}</AppAlert>
      ) : null}

      {viewMode === "card" ? (
        sharesList.length > 0 ? (
          <AppGrid container spacing={2}>
            {sharesList.map((share) => (
              <AppGrid key={share.path} size={{ xs: 12, sm: 6, md: 4, lg: 3 }}>
                <NFSShareCard
                  onEdit={() => handleEdit(share)}
                  onRemove={() => handleDelete(share)}
                  share={share}
                />
              </AppGrid>
            ))}
          </AppGrid>
        ) : (
          <div style={{ textAlign: "center", paddingBlock: 16 }}>
            <AppTypography color="text.secondary" variant="body2">
              No NFS exports found. Click &quot;Add Export&quot; to create one.
            </AppTypography>
          </div>
        )
      ) : (
        <UnifiedCollapsibleTable
          columns={columns}
          data={sharesList}
          emptyMessage="No NFS exports found. Click 'Add Export' to create one."
          getRowKey={(share) => share.path}
          renderExpandedContent={(share) => (
            <div>
              <AppTypography gutterBottom variant="subtitle2">
                <strong>Client Access Rules:</strong>
              </AppTypography>
              {share.clients.map((client: NFSClient, i: number) => (
                <div key={i} style={{ marginBottom: 4 }}>
                  <AppTypography variant="body2">
                    <strong>{client.host}</strong>
                  </AppTypography>
                  <div
                    style={{
                      display: "flex",
                      flexWrap: "wrap",
                      gap: 3,
                      marginTop: 2,
                    }}
                  >
                    {client.options?.length > 0 ? (
                      client.options.map((opt: string, j: number) => (
                        <Chip key={j} label={opt} size="small" variant="soft" />
                      ))
                    ) : (
                      <AppTypography color="text.secondary" variant="body2">
                        (default options)
                      </AppTypography>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}
          renderMainRow={(share) => (
            <>
              <AppTableCell>
                <AppTypography
                  style={{ fontFamily: "monospace" }}
                  variant="body2"
                >
                  {share.path}
                </AppTypography>
              </AppTableCell>
              <AppTableCell>
                <div style={{ display: "flex", flexWrap: "wrap", gap: 3 }}>
                  {share.clients.map((c, i) => (
                    <Chip key={i} label={c.host} size="small" variant="soft" />
                  ))}
                </div>
              </AppTableCell>
              <AppTableCell style={{ textAlign: "center" }}>
                <Chip
                  color={share.active ? "success" : "default"}
                  label={share.active ? "Active" : "Inactive"}
                  size="small"
                  variant="soft"
                />
              </AppTableCell>
              <AppTableCell>
                <div style={{ display: "flex", gap: 4 }}>
                  <AppButton
                    onClick={(e) => {
                      e.stopPropagation();
                      handleEdit(share);
                    }}
                    size="small"
                    variant="outlined"
                  >
                    Edit
                  </AppButton>
                  <AppButton
                    color="error"
                    onClick={(e) => {
                      e.stopPropagation();
                      handleDelete(share);
                    }}
                    size="small"
                  >
                    Remove
                  </AppButton>
                </div>
              </AppTableCell>
            </>
          )}
        />
      )}

      <CreateNFSShareDialog
        onClose={() => setCreateOpen(false)}
        onSuccess={() => refetch()}
        open={createOpen}
      />
      <EditNFSShareDialog
        onClose={() => setEditOpen(false)}
        onSuccess={() => refetch()}
        open={editOpen}
        share={selected}
      />
      <DeleteNFSShareDialog
        onClose={() => setDeleteOpen(false)}
        onSuccess={() => refetch()}
        open={deleteOpen}
        share={selected}
      />
    </div>
  );
};

export default NFSShares;
