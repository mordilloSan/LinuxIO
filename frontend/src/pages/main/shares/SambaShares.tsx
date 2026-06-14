import { Icon } from "@iconify/react";
import { useQueryClient } from "@tanstack/react-query";
import React, { useCallback, useEffect, useRef, useState } from "react";

import { linuxio, type SambaShare } from "@/api";
import SambaShareCard from "@/components/cards/SambaShareCard";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import PageLoader from "@/components/loaders/PageLoader";
import AppVirtualDataTable from "@/components/tables/AppVirtualDataTable";
import type { AppVirtualDataTableColumnDef } from "@/components/tables/AppVirtualDataTable";
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
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import PathPickerField from "@/components/ui/PathPickerField";
import { useScopedToast } from "@/hooks/useScopedToast";
import { getMutationErrorMessage } from "@/utils/mutations";

interface SambaSharesProps {
  onCreateHandler?: (handler: () => void) => void;
  viewMode?: "table" | "card";
}

// ============================================================================
// Samba access options model
// ============================================================================

interface AccessOptions {
  browseable: boolean;
  guestOk: boolean;
  readOnly: boolean;
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
          endAdornment={
            <Icon
              icon={open ? "mdi:chevron-up" : "mdi:chevron-down"}
              style={{ opacity: 0.5 }}
              width={18}
            />
          }
          fullWidth
          label="Access Options"
          onClick={handleOpen}
          size="small"
          style={{ cursor: "pointer" }}
          value={accessOptsSummary(opts)}
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
          {accessOptionLabels.map(({ key, label }) => (
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
// Create Samba Share Dialog
// ============================================================================

interface CreateDialogProps {
  onClose: () => void;
  onSuccess: () => void;
  open: boolean;
}

export const CreateSambaShareDialog: React.FC<CreateDialogProps> = ({
  open,
  onClose,
  onSuccess,
}) => {
  const toast = useScopedToast({ href: "/shares", label: "Open shares" });
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
    createShare({ name: name.trim(), properties: props });
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
    <GeneralDialog fullWidth maxWidth="sm" onClose={handleClose} open={open}>
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
            fullWidth
            label="Share Name"
            onChange={(e) => setName(e.target.value)}
            placeholder="e.g., shared_data"
            size="small"
            value={name}
          />
          <PathPickerField onChange={setPath} value={path} />
          <AppTextField
            fullWidth
            label="Comment"
            onChange={(e) => setComment(e.target.value)}
            placeholder="Optional description"
            size="small"
            value={comment}
          />
          <AccessOptionsDropdown onChange={setAccessOpts} opts={accessOpts} />
          <AppTextField
            fullWidth
            helperText="Comma-separated users or @groups"
            label="Valid Users"
            onChange={(e) => setValidUsers(e.target.value)}
            placeholder="e.g., @staff, admin"
            size="small"
            value={validUsers}
          />
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
// Edit Samba Share Dialog
// ============================================================================

interface EditDialogProps {
  onClose: () => void;
  onSuccess: () => void;
  open: boolean;
  share: SambaShare | null;
}

export const EditSambaShareDialog: React.FC<EditDialogProps> = ({
  open,
  onClose,
  share,
  onSuccess,
}) => {
  const toast = useScopedToast({ href: "/shares", label: "Open shares" });
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
    updateShare({
      oldName: share.name,
      newName: share.name,
      properties: props,
    });
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
      fullWidth
      key={share?.name}
      maxWidth="sm"
      onClose={handleClose}
      open={open}
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
            disabled
            fullWidth
            label="Share Name"
            size="small"
            value={share?.name || ""}
          />
          <PathPickerField onChange={setPath} value={path} />
          <AppTextField
            fullWidth
            label="Comment"
            onChange={(e) => setComment(e.target.value)}
            placeholder="Optional description"
            size="small"
            value={comment}
          />
          <AccessOptionsDropdown onChange={setAccessOpts} opts={accessOpts} />
          <AppTextField
            fullWidth
            helperText="Comma-separated users or @groups"
            label="Valid Users"
            onChange={(e) => setValidUsers(e.target.value)}
            placeholder="e.g., @staff, admin"
            size="small"
            value={validUsers}
          />
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
// Delete Samba Share Dialog
// ============================================================================

interface DeleteDialogProps {
  onClose: () => void;
  onSuccess: () => void;
  open: boolean;
  share: SambaShare | null;
}

export const DeleteSambaShareDialog: React.FC<DeleteDialogProps> = ({
  open,
  onClose,
  share,
  onSuccess,
}) => {
  const toast = useScopedToast({ href: "/shares", label: "Open shares" });
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
    deleteShare({ name: share.name });
  };

  return (
    <GeneralDialog fullWidth maxWidth="sm" onClose={onClose} open={open}>
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
    return <PageLoader />;
  }

  const sharesList = Array.isArray(shares) ? shares : [];

  const columns: AppVirtualDataTableColumnDef<(typeof sharesList)[number]>[] = [
    {
      accessorKey: "name",
      header: "Share Name",
      cell: ({ row }) => (
        <AppTypography fontWeight={700} variant="body2">
          {row.original.name}
        </AppTypography>
      ),
      meta: { align: "left" },
    },
    {
      id: "path",
      header: "Path",
      accessorFn: (share) => share.properties["path"],
      cell: ({ row }) => (
        <AppTypography style={{ fontFamily: "monospace" }} variant="body2">
          {row.original.properties["path"]}
        </AppTypography>
      ),
      meta: { align: "left" },
    },
    {
      id: "access",
      header: "Access",
      accessorFn: (share) =>
        [
          share.properties["read only"] === "yes" ? "read only" : "writable",
          share.properties["guest ok"] === "yes" ? "guest" : "",
        ]
          .filter(Boolean)
          .join(" "),
      cell: ({ row }) => (
        <div style={{ display: "flex", flexWrap: "wrap", gap: 3 }}>
          {row.original.properties["read only"] === "yes" ? (
            <Chip label="read only" size="small" variant="soft" />
          ) : (
            <Chip label="writable" size="small" variant="soft" />
          )}
          {row.original.properties["guest ok"] === "yes" && (
            <Chip label="guest" size="small" variant="soft" />
          )}
        </div>
      ),
      meta: {
        align: "left",
        hideBelow: "sm",
      },
    },
    {
      id: "actions",
      header: "",
      enableSorting: false,
      cell: ({ row }) => {
        const share = row.original;
        return (
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
        );
      },
      meta: {
        align: "right",
        width: "160px",
      },
    },
  ];

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        height: "100%",
        minHeight: 0,
      }}
    >
      {viewMode === "card" ? (
        sharesList.length > 0 ? (
          <AppGrid container spacing={2}>
            {sharesList.map((share) => (
              <AppGrid key={share.name} size={{ xs: 12, sm: 6, md: 4, lg: 3 }}>
                <SambaShareCard
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
              No Samba shares found. Click &quot;Add Share&quot; to create one.
            </AppTypography>
          </div>
        )
      ) : (
        <AppVirtualDataTable
          ariaLabel="Samba shares"
          columns={columns}
          data={sharesList}
          emptyMessage="No Samba shares found. Click 'Add Share' to create one."
          fillAvailable
          getRowId={(share) => share.name}
          renderExpandedContent={({ original: share }) => (
            <div className="expand-panel">
              {share.properties["comment"] && (
                <AppTypography gutterBottom variant="subtitle2">
                  <strong>Comment:</strong> {share.properties["comment"]}
                </AppTypography>
              )}
              <div>
                <AppTypography gutterBottom variant="subtitle2">
                  <strong>All Properties:</strong>
                </AppTypography>
                <div className="expand-panel__chips">
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
            </div>
          )}
        />
      )}

      <CreateSambaShareDialog
        onClose={() => setCreateOpen(false)}
        onSuccess={() => refetch()}
        open={createOpen}
      />
      <EditSambaShareDialog
        onClose={() => setEditOpen(false)}
        onSuccess={() => refetch()}
        open={editOpen}
        share={selected}
      />
      <DeleteSambaShareDialog
        onClose={() => setDeleteOpen(false)}
        onSuccess={() => refetch()}
        open={deleteOpen}
        share={selected}
      />
    </div>
  );
};

export default SambaShares;
