import { Icon } from "@iconify/react";
import { useQueryClient } from "@tanstack/react-query";
import React, { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";

import { linuxio, type SambaConnectedClient, type SambaShare } from "@/api";
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
import AppGrid from "@/components/ui/AppGrid";
import AppPopover from "@/components/ui/AppPopover";
import { AppTableCell } from "@/components/ui/AppTable";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import DirectoryTree from "@/components/ui/DirectoryTree";
import { getMutationErrorMessage } from "@/utils/mutations";

interface SambaSharesProps {
  onCreateHandler?: (handler: () => void) => void;
  viewMode?: "table" | "card";
}

// Common properties displayed as chips on cards
const displayProps = [
  "browseable",
  "read only",
  "guest ok",
  "writable",
] as const;

// ============================================================================
// Samba access options model
// ============================================================================

interface AccessOptions {
  browseable: boolean;
  readOnly: boolean;
  guestOk: boolean;
}

const defaultAccessOpts: AccessOptions = {
  browseable: true,
  readOnly: false,
  guestOk: false,
};

const accessOptionLabels: { key: keyof AccessOptions; label: string }[] = [
  { key: "browseable", label: "Browseable" },
  { key: "readOnly", label: "Read Only" },
  { key: "guestOk", label: "Guest Access" },
];

function accessOptsSummary(o: AccessOptions): string {
  const parts: string[] = [];
  parts.push(o.browseable ? "browseable" : "not browseable");
  parts.push(o.readOnly ? "read only" : "writable");
  if (o.guestOk) parts.push("guest ok");
  return parts.join(", ");
}

function buildProperties(
  path: string,
  comment: string,
  opts: AccessOptions,
  validUsers: string,
): Record<string, string> {
  const props: Record<string, string> = { path };
  if (comment) props["comment"] = comment;
  props["browseable"] = opts.browseable ? "yes" : "no";
  props["read only"] = opts.readOnly ? "yes" : "no";
  props["guest ok"] = opts.guestOk ? "yes" : "no";
  if (validUsers.trim()) props["valid users"] = validUsers.trim();
  return props;
}

function propsToAccessOpts(
  p: Record<string, string> | undefined,
): AccessOptions {
  if (!p) return { ...defaultAccessOpts };
  return {
    browseable: p["browseable"] !== "no",
    readOnly: p["read only"] === "yes",
    guestOk: p["guest ok"] === "yes",
  };
}

// ============================================================================
// Access options dropdown — same dot-toggle pattern as NFS
// ============================================================================

const AccessOptionsDropdown: React.FC<{
  opts: AccessOptions;
  onChange: (next: AccessOptions) => void;
}> = ({ opts, onChange }) => {
  const anchorRef = useRef<HTMLDivElement>(null);
  const [anchorEl, setAnchorEl] = useState<HTMLDivElement | null>(null);
  const [open, setOpen] = useState(false);

  const handleOpen = () => {
    setAnchorEl(anchorRef.current);
    setOpen(true);
  };

  const toggle = (key: keyof AccessOptions) =>
    onChange({ ...opts, [key]: !opts[key] });

  return (
    <>
      <div ref={anchorRef}>
        <AppTextField
          label="Access Options"
          value={accessOptsSummary(opts)}
          size="small"
          fullWidth
          onClick={handleOpen}
          style={{ cursor: "pointer" }}
          endAdornment={
            <Icon
              icon={open ? "mdi:chevron-up" : "mdi:chevron-down"}
              width={18}
              style={{ opacity: 0.5 }}
            />
          }
        />
      </div>
      <AppPopover
        open={open}
        onClose={() => setOpen(false)}
        anchorEl={anchorEl}
        anchorOrigin={{ vertical: "bottom", horizontal: "left" }}
        transformOrigin={{ vertical: "top", horizontal: "left" }}
        matchAnchorWidth
      >
        <div style={{ padding: "6px 0" }}>
          {accessOptionLabels.map(({ key, label }) => (
            <button
              key={key}
              type="button"
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
// Path picker — input that opens a directory tree popover
// ============================================================================

const PathPicker: React.FC<{
  value: string;
  onChange: (path: string) => void;
  label?: string;
}> = ({ value, onChange, label = "Directory Path" }) => {
  const anchorRef = useRef<HTMLDivElement>(null);
  const [anchorEl, setAnchorEl] = useState<HTMLDivElement | null>(null);
  const [open, setOpen] = useState(false);

  const handleOpen = () => {
    setAnchorEl(anchorRef.current);
    setOpen(true);
  };

  return (
    <>
      <div ref={anchorRef}>
        <AppTextField
          label={label}
          value={value}
          size="small"
          fullWidth
          shrinkLabel
          onClick={handleOpen}
          style={{ cursor: "pointer" }}
          placeholder="Click to select a folder"
          endAdornment={
            <Icon
              icon={open ? "mdi:chevron-up" : "mdi:chevron-down"}
              width={18}
              style={{ opacity: 0.5 }}
            />
          }
        />
      </div>
      <AppPopover
        open={open}
        onClose={() => setOpen(false)}
        anchorEl={anchorEl}
        anchorOrigin={{ vertical: "bottom", horizontal: "left" }}
        transformOrigin={{ vertical: "top", horizontal: "left" }}
        matchAnchorWidth
      >
        <DirectoryTree selectedPath={value} onSelect={onChange} />
      </AppPopover>
    </>
  );
};

// ============================================================================
// Create Samba Share Dialog
// ============================================================================

interface CreateDialogProps {
  open: boolean;
  onClose: () => void;
  onSuccess: () => void;
}

const CreateSambaShareDialog: React.FC<CreateDialogProps> = ({
  open,
  onClose,
  onSuccess,
}) => {
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [path, setPath] = useState("");
  const [comment, setComment] = useState("");
  const [accessOpts, setAccessOpts] = useState<AccessOptions>({
    ...defaultAccessOpts,
  });
  const [validUsers, setValidUsers] = useState("");
  const [validationError, setValidationError] = useState<string | null>(null);

  const { mutate: createShare, isPending } =
    linuxio.shares.create_samba_share.useMutation({
      onSuccess: () => {
        toast.success(`Samba share "${name}" created`);
        queryClient.invalidateQueries({
          queryKey: linuxio.shares.list_samba_shares.queryKey(),
        });
        onSuccess();
        handleClose();
      },
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to create Samba share"),
        );
      },
    });

  const handleCreate = () => {
    if (!name.trim()) {
      setValidationError("Share name is required");
      return;
    }
    if (!path.trim()) {
      setValidationError("Share path is required");
      return;
    }
    setValidationError(null);
    const props = buildProperties(path, comment, accessOpts, validUsers);
    createShare([name.trim(), props]);
  };

  const handleClose = () => {
    setName("");
    setPath("");
    setComment("");
    setAccessOpts({ ...defaultAccessOpts });
    setValidUsers("");
    setValidationError(null);
    onClose();
  };

  return (
    <GeneralDialog open={open} onClose={handleClose} maxWidth="sm" fullWidth>
      <AppDialogTitle>Create Samba Share</AppDialogTitle>
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
            label="Share Name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="e.g., shared_data"
            fullWidth
            size="small"
          />
          <PathPicker value={path} onChange={setPath} />
          <AppTextField
            label="Comment"
            value={comment}
            onChange={(e) => setComment(e.target.value)}
            placeholder="Optional description"
            fullWidth
            size="small"
          />
          <AccessOptionsDropdown opts={accessOpts} onChange={setAccessOpts} />
          <AppTextField
            label="Valid Users"
            value={validUsers}
            onChange={(e) => setValidUsers(e.target.value)}
            placeholder="e.g., @staff, admin"
            helperText="Comma-separated users or @groups"
            fullWidth
            size="small"
          />
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
// Edit Samba Share Dialog
// ============================================================================

interface EditDialogProps {
  open: boolean;
  onClose: () => void;
  share: SambaShare | null;
  onSuccess: () => void;
}

const EditSambaShareDialog: React.FC<EditDialogProps> = ({
  open,
  onClose,
  share,
  onSuccess,
}) => {
  const queryClient = useQueryClient();
  const p = share?.properties;
  const [path, setPath] = useState(() => p?.["path"] || "");
  const [comment, setComment] = useState(() => p?.["comment"] || "");
  const [accessOpts, setAccessOpts] = useState<AccessOptions>(() =>
    propsToAccessOpts(p),
  );
  const [validUsers, setValidUsers] = useState(() => p?.["valid users"] || "");

  const { mutate: updateShare, isPending } =
    linuxio.shares.update_samba_share.useMutation({
      onSuccess: () => {
        toast.success(`Samba share "${share?.name}" updated`);
        queryClient.invalidateQueries({
          queryKey: linuxio.shares.list_samba_shares.queryKey(),
        });
        onSuccess();
        handleClose();
      },
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to update Samba share"),
        );
      },
    });

  const handleSave = () => {
    if (!share || !path.trim()) return;
    const props = buildProperties(path, comment, accessOpts, validUsers);
    updateShare([share.name, props]);
  };

  const handleClose = () => {
    setPath("");
    setComment("");
    setAccessOpts({ ...defaultAccessOpts });
    setValidUsers("");
    onClose();
  };

  return (
    <GeneralDialog
      key={share?.name}
      open={open}
      onClose={handleClose}
      maxWidth="sm"
      fullWidth
    >
      <AppDialogTitle>Edit Samba Share</AppDialogTitle>
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
            label="Share Name"
            value={share?.name || ""}
            disabled
            fullWidth
            size="small"
          />
          <PathPicker value={path} onChange={setPath} />
          <AppTextField
            label="Comment"
            value={comment}
            onChange={(e) => setComment(e.target.value)}
            placeholder="Optional description"
            fullWidth
            size="small"
          />
          <AccessOptionsDropdown opts={accessOpts} onChange={setAccessOpts} />
          <AppTextField
            label="Valid Users"
            value={validUsers}
            onChange={(e) => setValidUsers(e.target.value)}
            placeholder="e.g., @staff, admin"
            helperText="Comma-separated users or @groups"
            fullWidth
            size="small"
          />
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
// Delete Samba Share Dialog
// ============================================================================

interface DeleteDialogProps {
  open: boolean;
  onClose: () => void;
  share: SambaShare | null;
  onSuccess: () => void;
}

const DeleteSambaShareDialog: React.FC<DeleteDialogProps> = ({
  open,
  onClose,
  share,
  onSuccess,
}) => {
  const queryClient = useQueryClient();
  const { mutate: deleteShare, isPending } =
    linuxio.shares.delete_samba_share.useMutation({
      onSuccess: () => {
        toast.success(`Removed Samba share "${share?.name}"`);
        queryClient.invalidateQueries({
          queryKey: linuxio.shares.list_samba_shares.queryKey(),
        });
        onSuccess();
        onClose();
      },
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to remove Samba share"),
        );
      },
    });

  const handleDelete = () => {
    if (!share) return;
    deleteShare([share.name]);
  };

  return (
    <GeneralDialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <AppDialogTitle>Remove Samba Share</AppDialogTitle>
      <AppDialogContent>
        <AppDialogContentText>
          Are you sure you want to remove this Samba share?
        </AppDialogContentText>
        {share && (
          <div style={{ marginTop: 8, marginBottom: 8 }}>
            <AppTypography variant="body2">
              <strong>Name:</strong> {share.name}
            </AppTypography>
            <AppTypography variant="body2">
              <strong>Path:</strong> {share.properties["path"]}
            </AppTypography>
          </div>
        )}
        <AppAlert severity="warning">
          This will remove the share from smb.conf and reload Samba.
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
// Samba Shares Component
// ============================================================================

const SambaShares: React.FC<SambaSharesProps> = ({
  onCreateHandler,
  viewMode = "table",
}) => {
  const [createOpen, setCreateOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [selected, setSelected] = useState<SambaShare | null>(null);

  const {
    data: shares = [],
    isPending: loading,
    refetch,
  } = linuxio.shares.list_samba_shares.useQuery({
    refetchInterval: 10000,
  });

  const { data: sambaClients = [] } =
    linuxio.shares.list_samba_clients.useQuery({
      refetchInterval: 10000,
    });

  const clientsByShare = (
    (sambaClients ?? []) as SambaConnectedClient[]
  ).reduce<Record<string, SambaConnectedClient[]>>((acc, client) => {
    (acc[client.share] ??= []).push(client);
    return acc;
  }, {});

  const handleCreate = useCallback(() => {
    setCreateOpen(true);
  }, []);

  useEffect(() => {
    if (onCreateHandler) {
      onCreateHandler(handleCreate);
    }
  }, [onCreateHandler, handleCreate]);

  const handleEdit = (share: SambaShare) => {
    setSelected(share);
    setEditOpen(true);
  };

  const handleDelete = (share: SambaShare) => {
    setSelected(share);
    setDeleteOpen(true);
  };

  if (loading) {
    return <ComponentLoader />;
  }

  const sharesList = Array.isArray(shares) ? shares : [];

  const columns: UnifiedTableColumn[] = [
    { field: "name", headerName: "Share Name", align: "left" },
    { field: "path", headerName: "Path", align: "left" },
    {
      field: "access",
      headerName: "Access",
      align: "left",
      className: "app-table-hide-below-sm",
    },
    {
      field: "connected",
      headerName: "Connected",
      align: "center",
      width: "100px",
      className: "app-table-hide-below-sm",
    },
    { field: "actions", headerName: "", align: "right", width: "160px" },
  ];

  return (
    <div>
      {viewMode === "card" ? (
        sharesList.length > 0 ? (
          <AppGrid container spacing={2}>
            {sharesList.map((share) => (
              <AppGrid key={share.name} size={{ xs: 12, sm: 6, md: 4, lg: 3 }}>
                <FrostedCard style={{ padding: 8 }}>
                  <AppTypography
                    variant="body2"
                    fontWeight={700}
                    style={{ marginBottom: 2 }}
                  >
                    {share.name}
                  </AppTypography>
                  <AppTypography
                    variant="body2"
                    style={{ marginBottom: 4, fontFamily: "monospace" }}
                  >
                    {share.properties["path"]}
                  </AppTypography>
                  {share.properties["comment"] && (
                    <AppTypography
                      variant="caption"
                      color="text.secondary"
                      style={{ marginBottom: 4, display: "block" }}
                    >
                      {share.properties["comment"]}
                    </AppTypography>
                  )}

                  <div
                    style={{
                      display: "flex",
                      flexWrap: "wrap",
                      gap: 3,
                      marginBottom: 8,
                    }}
                  >
                    {displayProps.map((prop) =>
                      share.properties[prop] ? (
                        <Chip
                          key={prop}
                          label={`${prop}: ${share.properties[prop]}`}
                          size="small"
                          variant="soft"
                        />
                      ) : null,
                    )}
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
              No Samba shares found. Click &quot;Add Share&quot; to create one.
            </AppTypography>
          </div>
        )
      ) : (
        <UnifiedCollapsibleTable
          data={sharesList}
          columns={columns}
          getRowKey={(share) => share.name}
          renderMainRow={(share) => (
            <>
              <AppTableCell>
                <AppTypography variant="body2" fontWeight={700}>
                  {share.name}
                </AppTypography>
              </AppTableCell>
              <AppTableCell>
                <AppTypography
                  variant="body2"
                  style={{ fontFamily: "monospace" }}
                >
                  {share.properties["path"]}
                </AppTypography>
              </AppTableCell>
              <AppTableCell className="app-table-hide-below-sm">
                <div style={{ display: "flex", flexWrap: "wrap", gap: 3 }}>
                  {share.properties["read only"] === "yes" ? (
                    <Chip label="read only" size="small" variant="soft" />
                  ) : (
                    <Chip label="writable" size="small" variant="soft" />
                  )}
                  {share.properties["guest ok"] === "yes" && (
                    <Chip label="guest" size="small" variant="soft" />
                  )}
                </div>
              </AppTableCell>
              <AppTableCell
                className="app-table-hide-below-sm"
                style={{ textAlign: "center" }}
              >
                {(clientsByShare[share.name] ?? []).length > 0 ? (
                  <Chip
                    label={`${(clientsByShare[share.name] ?? []).length}`}
                    size="small"
                    variant="soft"
                    color="success"
                  />
                ) : (
                  <AppTypography variant="body2" color="text.secondary">
                    0
                  </AppTypography>
                )}
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
          renderExpandedContent={(share) => {
            const connected = clientsByShare[share.name] ?? [];
            return (
              <div style={{ display: "flex", gap: 24 }}>
                <div style={{ flex: 1 }}>
                  {share.properties["comment"] && (
                    <AppTypography variant="subtitle2" gutterBottom>
                      <strong>Comment:</strong> {share.properties["comment"]}
                    </AppTypography>
                  )}
                  <AppTypography variant="subtitle2" gutterBottom>
                    <strong>All Properties:</strong>
                  </AppTypography>
                  <div
                    style={{
                      display: "flex",
                      flexWrap: "wrap",
                      gap: 4,
                    }}
                  >
                    {Object.entries(share.properties)
                      .filter(([key]) => key !== "path")
                      .map(([key, value]) => (
                        <Chip
                          key={key}
                          label={`${key} = ${value}`}
                          size="small"
                          variant="soft"
                        />
                      ))}
                  </div>
                </div>
                <div style={{ flex: 1 }}>
                  <AppTypography variant="subtitle2" gutterBottom>
                    <strong>Connected Clients:</strong>
                  </AppTypography>
                  {connected.length > 0 ? (
                    <div
                      style={{
                        display: "flex",
                        flexWrap: "wrap",
                        gap: 4,
                      }}
                    >
                      {connected.map((c, i) => (
                        <Chip
                          key={i}
                          label={`${c.username}@${c.ip}`}
                          size="small"
                          variant="soft"
                          color="success"
                        />
                      ))}
                    </div>
                  ) : (
                    <AppTypography variant="body2" color="text.secondary">
                      No clients connected
                    </AppTypography>
                  )}
                </div>
              </div>
            );
          }}
          emptyMessage="No Samba shares found. Click 'Add Share' to create one."
        />
      )}

      <CreateSambaShareDialog
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        onSuccess={() => refetch()}
      />
      <EditSambaShareDialog
        open={editOpen}
        onClose={() => setEditOpen(false)}
        share={selected}
        onSuccess={() => refetch()}
      />
      <DeleteSambaShareDialog
        open={deleteOpen}
        onClose={() => setDeleteOpen(false)}
        share={selected}
        onSuccess={() => refetch()}
      />
    </div>
  );
};

export default SambaShares;
