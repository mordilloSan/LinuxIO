import { useQueryClient } from "@tanstack/react-query";
import React, { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";

import { linuxio, type NFSExport, type NFSClient } from "@/api";
import FrostedCard from "@/components/cards/RootCard";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import ComponentLoader from "@/components/loaders/ComponentLoader";
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
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppGrid from "@/components/ui/AppGrid";
import AppIconButton from "@/components/ui/AppIconButton";
import AppSwitch from "@/components/ui/AppSwitch";
import { AppTableCell } from "@/components/ui/AppTable";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import { getMutationErrorMessage } from "@/utils/mutations";

interface NFSSharesProps {
  onCreateHandler?: (handler: () => void) => void;
  viewMode?: "table" | "card";
}

// ============================================================================
// NFS option model — each toggle represents a mutually exclusive pair
// ============================================================================

interface ClientOptions {
  rw: boolean; // rw (true) vs ro (false)
  sync: boolean; // sync (true) vs async (false)
  noSubtreeCheck: boolean; // no_subtree_check (true) vs subtree_check
  noRootSquash: boolean; // no_root_squash (true) vs root_squash
  allSquash: boolean; // all_squash — overrides root squash when on
  insecure: boolean; // insecure (true) vs secure (default)
  crossmnt: boolean; // crossmnt
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
// Shared per-client option switches
// ============================================================================

const ClientOptionsPanel: React.FC<{
  opts: ClientOptions;
  onChange: (next: ClientOptions) => void;
}> = ({ opts, onChange }) => {
  const set = (key: keyof ClientOptions, val: boolean) =>
    onChange({ ...opts, [key]: val });

  return (
    <div
      style={{
        display: "flex",
        flexWrap: "wrap",
        gap: 4,
        paddingLeft: 4,
      }}
    >
      <AppFormControlLabel
        control={
          <AppSwitch
            checked={opts.rw}
            onChange={(e) => set("rw", e.target.checked)}
          />
        }
        label="Read/Write"
      />
      <AppFormControlLabel
        control={
          <AppSwitch
            checked={opts.sync}
            onChange={(e) => set("sync", e.target.checked)}
          />
        }
        label="Sync"
      />
      <AppFormControlLabel
        control={
          <AppSwitch
            checked={opts.noSubtreeCheck}
            onChange={(e) => set("noSubtreeCheck", e.target.checked)}
          />
        }
        label="No Subtree Check"
      />
      <AppFormControlLabel
        control={
          <AppSwitch
            checked={opts.noRootSquash}
            onChange={(e) => set("noRootSquash", e.target.checked)}
          />
        }
        label="No Root Squash"
      />
      <AppFormControlLabel
        control={
          <AppSwitch
            checked={opts.allSquash}
            onChange={(e) => set("allSquash", e.target.checked)}
          />
        }
        label="All Squash"
      />
      <AppFormControlLabel
        control={
          <AppSwitch
            checked={opts.insecure}
            onChange={(e) => set("insecure", e.target.checked)}
          />
        }
        label="Insecure"
      />
      <AppFormControlLabel
        control={
          <AppSwitch
            checked={opts.crossmnt}
            onChange={(e) => set("crossmnt", e.target.checked)}
          />
        }
        label="Crossmnt"
      />
    </div>
  );
};

// ============================================================================
// Create NFS Share Dialog
// ============================================================================

interface CreateDialogProps {
  open: boolean;
  onClose: () => void;
  onSuccess: () => void;
}

const CreateNFSShareDialog: React.FC<CreateDialogProps> = ({
  open,
  onClose,
  onSuccess,
}) => {
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

  return (
    <GeneralDialog open={open} onClose={handleClose} maxWidth="sm" fullWidth>
      <AppDialogTitle>Create NFS Export</AppDialogTitle>
      <AppDialogContent>
        <div
          style={{
            display: "flex",
            flexDirection: "column",
            gap: 8,
            marginTop: 4,
          }}
        >
          <AppTextField
            label="Export Path"
            value={path}
            onChange={(e) => setPath(e.target.value)}
            placeholder="e.g., /srv/nfs/data"
            fullWidth
            size="small"
          />
          <AppTypography variant="subtitle2" style={{ marginTop: 4 }}>
            Client Access Rules
          </AppTypography>
          {clients.map((client, i) => (
            <FrostedCard key={i} style={{ padding: 8 }}>
              <div
                style={{
                  display: "flex",
                  gap: 8,
                  alignItems: "center",
                  marginBottom: 4,
                }}
              >
                <AppTextField
                  label="Host"
                  value={client.host}
                  onChange={(e) =>
                    setClients((prev) =>
                      prev.map((c, idx) =>
                        idx === i ? { ...c, host: e.target.value } : c,
                      ),
                    )
                  }
                  placeholder="e.g., 192.168.1.0/24 or *"
                  size="small"
                  style={{ flex: 1 }}
                />
                {clients.length > 1 && (
                  <AppIconButton
                    size="small"
                    onClick={() =>
                      setClients((prev) => prev.filter((_, idx) => idx !== i))
                    }
                  >
                    &times;
                  </AppIconButton>
                )}
              </div>
              <ClientOptionsPanel
                opts={client.opts}
                onChange={(next) =>
                  setClients((prev) =>
                    prev.map((c, idx) =>
                      idx === i ? { ...c, opts: next } : c,
                    ),
                  )
                }
              />
            </FrostedCard>
          ))}
          <AppButton
            size="small"
            variant="outlined"
            onClick={() =>
              setClients((prev) => [
                ...prev,
                { host: "", opts: { ...defaultOpts } },
              ])
            }
            style={{ alignSelf: "flex-start" }}
          >
            Add Client
          </AppButton>
          {validationError && (
            <AppAlert severity="error">{validationError}</AppAlert>
          )}
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton onClick={handleClose} disabled={isPending}>
          Cancel
        </AppButton>
        <AppButton
          onClick={handleCreate}
          variant="contained"
          disabled={isPending}
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
  open: boolean;
  onClose: () => void;
  share: NFSExport | null;
  onSuccess: () => void;
}

const EditNFSShareDialog: React.FC<EditDialogProps> = ({
  open,
  onClose,
  share,
  onSuccess,
}) => {
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

  return (
    <GeneralDialog
      key={share?.path}
      open={open}
      onClose={handleClose}
      maxWidth="sm"
      fullWidth
    >
      <AppDialogTitle>Edit NFS Export</AppDialogTitle>
      <AppDialogContent>
        <div
          style={{
            display: "flex",
            flexDirection: "column",
            gap: 8,
            marginTop: 4,
          }}
        >
          <AppTextField
            label="Export Path"
            value={share?.path || ""}
            disabled
            fullWidth
            size="small"
          />
          <AppTypography variant="subtitle2" style={{ marginTop: 4 }}>
            Client Access Rules
          </AppTypography>
          {clients.map((client, i) => (
            <FrostedCard key={i} style={{ padding: 8 }}>
              <div
                style={{
                  display: "flex",
                  gap: 8,
                  alignItems: "center",
                  marginBottom: 4,
                }}
              >
                <AppTextField
                  label="Host"
                  value={client.host}
                  onChange={(e) =>
                    setClients((prev) =>
                      prev.map((c, idx) =>
                        idx === i ? { ...c, host: e.target.value } : c,
                      ),
                    )
                  }
                  placeholder="e.g., 192.168.1.0/24 or *"
                  size="small"
                  style={{ flex: 1 }}
                />
                {clients.length > 1 && (
                  <AppIconButton
                    size="small"
                    onClick={() =>
                      setClients((prev) => prev.filter((_, idx) => idx !== i))
                    }
                  >
                    &times;
                  </AppIconButton>
                )}
              </div>
              <ClientOptionsPanel
                opts={client.opts}
                onChange={(next) =>
                  setClients((prev) =>
                    prev.map((c, idx) =>
                      idx === i ? { ...c, opts: next } : c,
                    ),
                  )
                }
              />
            </FrostedCard>
          ))}
          <AppButton
            size="small"
            variant="outlined"
            onClick={() =>
              setClients((prev) => [
                ...prev,
                { host: "", opts: { ...defaultOpts } },
              ])
            }
            style={{ alignSelf: "flex-start" }}
          >
            Add Client
          </AppButton>
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton onClick={handleClose} disabled={isPending}>
          Cancel
        </AppButton>
        <AppButton
          onClick={handleSave}
          variant="contained"
          disabled={isPending}
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
  open: boolean;
  onClose: () => void;
  share: NFSExport | null;
  onSuccess: () => void;
}

const DeleteNFSShareDialog: React.FC<DeleteDialogProps> = ({
  open,
  onClose,
  share,
  onSuccess,
}) => {
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
    <GeneralDialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
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
        <AppButton onClick={onClose} disabled={isPending}>
          Cancel
        </AppButton>
        <AppButton
          onClick={handleDelete}
          variant="contained"
          color="error"
          disabled={isPending}
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
    setCreateOpen(true);
  }, []);

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
    return <ComponentLoader />;
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
      {viewMode === "card" ? (
        sharesList.length > 0 ? (
          <AppGrid container spacing={2}>
            {sharesList.map((share) => (
              <AppGrid key={share.path} size={{ xs: 12, sm: 6, md: 4, lg: 3 }}>
                <FrostedCard style={{ padding: 8 }}>
                  <div
                    style={{
                      display: "flex",
                      alignItems: "center",
                      gap: 6,
                      marginBottom: 4,
                    }}
                  >
                    <span
                      style={{
                        width: 8,
                        height: 8,
                        borderRadius: "50%",
                        backgroundColor: share.active ? "#00E676" : "#9e9e9e",
                        flexShrink: 0,
                      }}
                    />
                    <AppTypography
                      variant="body2"
                      fontWeight={700}
                      style={{ fontFamily: "monospace" }}
                    >
                      {share.path}
                    </AppTypography>
                  </div>

                  <div
                    style={{
                      display: "flex",
                      flexWrap: "wrap",
                      gap: 3,
                      marginBottom: 8,
                    }}
                  >
                    {share.clients.map((client, i) => (
                      <Chip
                        key={i}
                        label={
                          client.options?.length > 0
                            ? `${client.host}(${client.options.slice(0, 2).join(",")}${client.options.length > 2 ? "..." : ""})`
                            : client.host
                        }
                        size="small"
                        variant="soft"
                      />
                    ))}
                  </div>

                  <div style={{ display: "flex", gap: 4 }}>
                    <AppButton
                      size="small"
                      variant="outlined"
                      onClick={() => handleEdit(share)}
                    >
                      Edit
                    </AppButton>
                    <AppButton
                      size="small"
                      color="error"
                      onClick={() => handleDelete(share)}
                    >
                      Remove
                    </AppButton>
                  </div>
                </FrostedCard>
              </AppGrid>
            ))}
          </AppGrid>
        ) : (
          <div style={{ textAlign: "center", paddingBlock: 16 }}>
            <AppTypography variant="body2" color="text.secondary">
              No NFS exports found. Click &quot;Add Export&quot; to create one.
            </AppTypography>
          </div>
        )
      ) : (
        <UnifiedCollapsibleTable
          data={sharesList}
          columns={columns}
          getRowKey={(share) => share.path}
          renderMainRow={(share) => (
            <>
              <AppTableCell>
                <AppTypography
                  variant="body2"
                  style={{ fontFamily: "monospace" }}
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
                  label={share.active ? "Active" : "Inactive"}
                  size="small"
                  variant="soft"
                  color={share.active ? "success" : "default"}
                />
              </AppTableCell>
              <AppTableCell>
                <div style={{ display: "flex", gap: 4 }}>
                  <AppButton
                    size="small"
                    variant="outlined"
                    onClick={(e) => {
                      e.stopPropagation();
                      handleEdit(share);
                    }}
                  >
                    Edit
                  </AppButton>
                  <AppButton
                    size="small"
                    color="error"
                    onClick={(e) => {
                      e.stopPropagation();
                      handleDelete(share);
                    }}
                  >
                    Remove
                  </AppButton>
                </div>
              </AppTableCell>
            </>
          )}
          renderExpandedContent={(share) => (
            <div>
              <AppTypography variant="subtitle2" gutterBottom>
                <strong>Client Access Rules:</strong>
              </AppTypography>
              {share.clients.map((client, i) => (
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
                      client.options.map((opt, j) => (
                        <Chip key={j} label={opt} size="small" variant="soft" />
                      ))
                    ) : (
                      <AppTypography variant="body2" color="text.secondary">
                        (default options)
                      </AppTypography>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}
          emptyMessage="No NFS exports found. Click 'Add Export' to create one."
        />
      )}

      <CreateNFSShareDialog
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        onSuccess={() => refetch()}
      />
      <EditNFSShareDialog
        open={editOpen}
        onClose={() => setEditOpen(false)}
        share={selected}
        onSuccess={() => refetch()}
      />
      <DeleteNFSShareDialog
        open={deleteOpen}
        onClose={() => setDeleteOpen(false)}
        share={selected}
        onSuccess={() => refetch()}
      />
    </div>
  );
};

export default NFSShares;
