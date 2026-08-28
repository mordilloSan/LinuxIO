import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { useCallback, useMemo, useState } from "react";

import { CACHE_TTL_MS, linuxio, type CIFSMount, useCallMutation } from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppAlert from "@/components/ui/AppAlert";
import AppAutocomplete from "@/components/ui/AppAutocomplete";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTextField from "@/components/ui/AppTextField";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { getUsageColor } from "@/constants/statusColors";
import { useCapability } from "@/hooks/useCapabilities";
import { useDebouncedValue } from "@/hooks/useDebouncedValue";
import { useRegisterCreateHandler } from "@/hooks/useRegisterCreateHandler";
import { useReorderableSurface } from "@/hooks/useReorderableSurface";
import { useReorderableTableDnd } from "@/hooks/useReorderableTableDnd";
import { useScopedToast } from "@/hooks/useScopedToast";
import { formatFileSize } from "@/utils/formaters";

const STORAGE_TOAST_META = {
  label: "Open storage",
  to: "/storage",
} as const;

interface CIFSMountsProps {
  onMountCreateHandler?: (handler: () => void) => void;
}

interface MountCIFSDialogProps {
  open: boolean;
  onClose: () => void;
}

interface RemoveCIFSDialogProps {
  mount: CIFSMount | null;
  open: boolean;
  onClose: () => void;
}

interface EditCIFSDialogProps {
  mount: CIFSMount | null;
  open: boolean;
  onClose: () => void;
}

function getStatusLabel(mount: CIFSMount): string {
  return mount.mounted ? "Mounted" : "Configured";
}

function getAuthLabel(mount: CIFSMount): string {
  return mount.username ? `User: ${mount.username}` : "Guest";
}

const MountCIFSDialog = ({ open, onClose }: MountCIFSDialogProps) => {
  const [server, setServer] = useState("");
  const [share, setShare] = useState("");
  const [mountpoint, setMountpoint] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [domain, setDomain] = useState("");
  const [readOnly, setReadOnly] = useState(false);
  const [customOptions, setCustomOptions] = useState("");
  const [validationError, setValidationError] = useState<string | null>(null);
  // Debounced server whose shares are being browsed; "" disables the query.
  // Too-short input clears on the next tick instead of waiting out the delay.
  const serverToBrowse = server.length >= 3 ? server : "";
  const browseServer = useDebouncedValue(
    serverToBrowse,
    serverToBrowse ? 500 : 0,
  );

  const { mutate: mountCIFS, isPending: isMounting } = useCallMutation(
    linuxio.storage.mount_cifs,
    {
      success: `SMB share mounted at ${mountpoint}`,
      warning: (result) => result.warning,
      error: "Failed to mount SMB share",
      toast: STORAGE_TOAST_META,
      options: { onSuccess: () => handleClose() },
    },
  );

  // Browsing is best-effort; on error the field falls back to free text.
  const sharesQuery = useQuery({
    ...linuxio.storage.list_cifs_shares({ server: browseServer }),
    enabled: browseServer !== "",
    staleTime: CACHE_TTL_MS.THIRTY_SECONDS,
  });
  const shares = sharesQuery.data ?? [];
  const loadingShares = browseServer !== "" && sharesQuery.isLoading;

  const buildOptions = () => {
    const opts: string[] = [readOnly ? "ro" : "rw"];
    if (customOptions.trim()) {
      opts.push(
        ...customOptions
          .split(",")
          .map((o) => o.trim())
          .filter(Boolean),
      );
    }
    return opts;
  };

  const handleMount = () => {
    if (!server || !share || !mountpoint) {
      setValidationError("Server, share, and mountpoint are required");
      return;
    }
    setValidationError(null);
    mountCIFS({
      server,
      share,
      mountpoint,
      username,
      password,
      domain,
      options: buildOptions(),
    });
  };

  const handleClose = () => {
    setServer("");
    setShare("");
    setMountpoint("");
    setUsername("");
    setPassword("");
    setDomain("");
    setReadOnly(false);
    setCustomOptions("");
    setValidationError(null);
    onClose();
  };

  return (
    <GeneralDialog fullWidth maxWidth="sm" onClose={handleClose} open={open}>
      <AppDialogTitle>Mount SMB Share</AppDialogTitle>
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
            fullWidth
            label="SMB Server"
            onChange={(e) => setServer(e.target.value)}
            placeholder="e.g., 192.168.1.100 or nas.local"
            size="small"
            value={server}
          />
          <AppAutocomplete
            endAdornment={
              loadingShares ? <AppCircularProgress size={20} /> : null
            }
            freeSolo
            fullWidth
            label="Share"
            loading={loadingShares}
            onChange={setShare}
            onInputChange={setShare}
            options={shares}
            placeholder="e.g., media"
            size="small"
            value={share}
          />
          <AppTextField
            fullWidth
            label="Local Mountpoint"
            onChange={(e) => setMountpoint(e.target.value)}
            placeholder="e.g., /mnt/smb/media"
            size="small"
            value={mountpoint}
          />
          <AppTypography style={{ marginTop: 4 }} variant="subtitle2">
            Credentials
          </AppTypography>
          <AppTextField
            fullWidth
            helperText="Leave blank to mount as guest (anonymous)"
            label="Username"
            onChange={(e) => setUsername(e.target.value)}
            placeholder="e.g., miguel"
            size="small"
            value={username}
          />
          <AppTextField
            autoComplete="new-password"
            fullWidth
            label="Password"
            onChange={(e) => setPassword(e.target.value)}
            size="small"
            type="password"
            value={password}
          />
          <AppTextField
            fullWidth
            label="Domain / Workgroup (optional)"
            onChange={(e) => setDomain(e.target.value)}
            placeholder="e.g., WORKGROUP"
            size="small"
            value={domain}
          />
          <AppTypography style={{ marginTop: 4 }} variant="subtitle2">
            Mount Options
          </AppTypography>
          <AppFormControlLabel
            control={
              <AppSwitch
                checked={readOnly}
                onChange={(e) => setReadOnly(e.target.checked)}
              />
            }
            label="Mount read-only"
          />
          <AppTextField
            fullWidth
            helperText="Additional comma-separated mount options (credentials are set above)"
            label="Custom Mount Options"
            onChange={(e) => setCustomOptions(e.target.value)}
            placeholder="e.g., vers=3.0,uid=1000,iocharset=utf8"
            size="small"
            value={customOptions}
          />
          <AppAlert severity="info">
            The mount is saved to /etc/fstab and re-mounts automatically at
            boot.
          </AppAlert>
          {validationError && (
            <AppAlert severity="error">{validationError}</AppAlert>
          )}
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={isMounting} onClick={handleClose}>
          Cancel
        </AppButton>
        <AppButton
          disabled={isMounting}
          onClick={handleMount}
          variant="contained"
        >
          {isMounting ? "Mounting..." : "Mount"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

const RemoveCIFSDialog = ({ open, onClose, mount }: RemoveCIFSDialogProps) => {
  const { mutate: removeEntry, isPending: isRemoving } = useCallMutation(
    linuxio.storage.unmount_cifs,
    {
      success: `Removed ${mount?.mountpoint}`,
      warning: (result) => result.warning,
      error: "Failed to remove entry",
      toast: STORAGE_TOAST_META,
      options: { onSuccess: () => onClose() },
    },
  );

  const handleRemove = () => {
    if (!mount) {
      return;
    }
    removeEntry({ mountpoint: mount.mountpoint, removeFstab: "true" });
  };

  return (
    <GeneralDialog fullWidth maxWidth="xs" onClose={onClose} open={open}>
      <AppDialogTitle>Remove SMB Mount</AppDialogTitle>
      <AppDialogContent>
        <AppDialogContentText>
          {mount?.mounted
            ? "This will unmount the share, remove it from /etc/fstab, and delete its stored credentials."
            : "This will remove the share from /etc/fstab and delete its stored credentials."}
        </AppDialogContentText>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={isRemoving} onClick={onClose}>
          Cancel
        </AppButton>
        <AppButton
          color="error"
          disabled={isRemoving}
          onClick={handleRemove}
          variant="contained"
        >
          {isRemoving ? "Removing..." : "Remove"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};

// EditCIFSDialog is a thin shell; the form is keyed by mountpoint so its state
// initializes once per mount (no set-state-during-render).
const EditCIFSDialog = ({ open, onClose, mount }: EditCIFSDialogProps) => (
  <GeneralDialog fullWidth maxWidth="sm" onClose={onClose} open={open}>
    <AppDialogTitle>Edit SMB Mount Options</AppDialogTitle>
    {open && mount ? (
      <EditCIFSForm key={mount.mountpoint} mount={mount} onClose={onClose} />
    ) : null}
  </GeneralDialog>
);

const EditCIFSForm = ({
  mount,
  onClose,
}: {
  mount: CIFSMount;
  onClose: () => void;
}) => {
  const [readOnly, setReadOnly] = useState(() =>
    (mount.options ?? []).includes("ro"),
  );
  const [customOptions, setCustomOptions] = useState(() =>
    (mount.options ?? []).filter((o) => o !== "ro" && o !== "rw").join(","),
  );

  const { mutate: remountCIFS, isPending: isSaving } = useCallMutation(
    linuxio.storage.remount_cifs,
    {
      success: "SMB mount options updated",
      warning: (result) => result.warning,
      error: "Failed to update mount options",
      toast: STORAGE_TOAST_META,
      options: { onSuccess: () => onClose() },
    },
  );

  const handleSave = () => {
    const opts: string[] = [readOnly ? "ro" : "rw"];
    if (customOptions.trim()) {
      opts.push(
        ...customOptions
          .split(",")
          .map((o) => o.trim())
          .filter(Boolean),
      );
    }
    remountCIFS({
      mountpoint: mount.mountpoint,
      options: opts,
      updateFstab: "true",
    });
  };

  return (
    <>
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
            disabled
            fullWidth
            label="Share"
            size="small"
            value={mount.source}
          />
          <AppTextField
            disabled
            fullWidth
            label="Mountpoint"
            size="small"
            value={mount.mountpoint}
          />
          <AppFormControlLabel
            control={
              <AppSwitch
                checked={readOnly}
                onChange={(e) => setReadOnly(e.target.checked)}
              />
            }
            label="Mount read-only"
          />
          <AppTextField
            fullWidth
            helperText="Additional comma-separated mount options"
            label="Custom Mount Options"
            onChange={(e) => setCustomOptions(e.target.value)}
            size="small"
            value={customOptions}
          />
          <AppAlert severity="info">
            Credentials are reused from the existing entry and are not
            re-collected here.
          </AppAlert>
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={isSaving} onClick={onClose}>
          Cancel
        </AppButton>
        <AppButton disabled={isSaving} onClick={handleSave} variant="contained">
          {isSaving ? "Saving..." : "Save"}
        </AppButton>
      </AppDialogActions>
    </>
  );
};

const getCIFSMountId = (mount: CIFSMount) => mount.mountpoint;
type PendingMountAction = "mount" | "unmount";

const CIFSMounts = ({ onMountCreateHandler }: CIFSMountsProps) => {
  const toast = useScopedToast(STORAGE_TOAST_META);
  const { reason: cifsReason, status: cifsStatus } = useCapability(
    "sambaClientAvailable",
  );
  const cifsUnavailable = cifsStatus === "unavailable";

  const [mountDialogOpen, setMountDialogOpen] = useState(false);
  const [editDialogOpen, setEditDialogOpen] = useState(false);
  const [removeDialogOpen, setRemoveDialogOpen] = useState(false);
  const [selectedMount, setSelectedMount] = useState<CIFSMount | null>(null);
  const [pendingActionByMountpoint, setPendingActionByMountpoint] = useState<
    ReadonlyMap<string, PendingMountAction>
  >(() => new Map());

  const { data: mounts } = useSuspenseQuery({
    ...linuxio.storage.list_cifs_mounts,
    refetchInterval: 10000,
  });

  const { mutateAsync: mountExisting } = useCallMutation(
    linuxio.storage.mount_cifs,
    {
      success: "SMB entry mounted",
      warning: (result) => result.warning,
      error: "Failed to mount SMB entry",
      toast: STORAGE_TOAST_META,
    },
  );

  const { mutateAsync: unmountEntry } = useCallMutation(
    linuxio.storage.unmount_cifs,
    {
      // The message needs `variables`, so the toast stays in a callback; the
      // warning affordance still owns the warning case.
      success: (result, variables) => {
        if (!result.warning) {
          toast.success(`Unmounted ${variables.mountpoint}`);
        }
      },
      warning: (result) => result.warning,
      error: "Failed to unmount",
      toast: STORAGE_TOAST_META,
    },
  );

  const handleCreate = useCallback(() => {
    if (cifsUnavailable) {
      toast.error(cifsReason);
      return;
    }
    setMountDialogOpen(true);
  }, [cifsUnavailable, cifsReason, toast]);
  useRegisterCreateHandler(onMountCreateHandler, handleCreate);

  const runMountAction = useCallback(
    (
      mountpoint: string,
      action: PendingMountAction,
      run: () => Promise<unknown>,
    ) => {
      if (pendingActionByMountpoint.has(mountpoint)) return;

      setPendingActionByMountpoint((current) =>
        new Map(current).set(mountpoint, action),
      );
      void run()
        .catch(() => undefined)
        .finally(() => {
          setPendingActionByMountpoint((current) => {
            if (current.get(mountpoint) !== action) return current;
            const next = new Map(current);
            next.delete(mountpoint);
            return next;
          });
        });
    },
    [pendingActionByMountpoint],
  );

  // Re-activate an inactive fstab entry — the backend mounts it from fstab
  // using the stored credentials, so no password is needed.
  const handleMountExisting = useCallback(
    (mount: CIFSMount) => {
      if (cifsUnavailable) {
        toast.error(cifsReason);
        return;
      }
      runMountAction(mount.mountpoint, "mount", () =>
        mountExisting({
          server: mount.server,
          share: mount.share,
          mountpoint: mount.mountpoint,
          username: "",
          password: "",
          domain: "",
          options: [],
        }),
      );
    },
    [cifsReason, cifsUnavailable, mountExisting, runMountAction, toast],
  );

  const handleUnmount = useCallback(
    (mount: CIFSMount) => {
      runMountAction(mount.mountpoint, "unmount", () =>
        unmountEntry({
          mountpoint: mount.mountpoint,
          removeFstab: "false",
        }),
      );
    },
    [runMountAction, unmountEntry],
  );

  const mountsList = Array.isArray(mounts) ? mounts : [];
  const surface = useReorderableSurface({
    getId: getCIFSMountId,
    items: mountsList,
    surface: "shares.mounts.cifs",
  });
  const tableDnd = useReorderableTableDnd<CIFSMount, CIFSMount>({
    handleAriaLabel: "Reorder SMB mount",
    surface,
  });

  const columns = useMemo<AppVirtualTableColumnDef<CIFSMount>[]>(
    () => [
      {
        accessorKey: "source",
        header: "SMB Share",
        cell: ({ row }) => (
          <AppTypography
            style={{ fontFamily: "var(--app-font-mono)" }}
            variant="body2"
          >
            {row.original.source}
          </AppTypography>
        ),
        meta: {
          align: "left",
          getCellRenderKey: (row) => {
            const mount = row as CIFSMount;
            return [mount.mountpoint, mount.source];
          },
        },
      },
      {
        accessorKey: "mountpoint",
        header: "Mount Point",
        cell: ({ row }) => (
          <AppTypography
            style={{ fontFamily: "var(--app-font-mono)" }}
            variant="body2"
          >
            {row.original.mountpoint}
          </AppTypography>
        ),
        meta: {
          align: "left",
          getCellRenderKey: (row) => (row as CIFSMount).mountpoint,
        },
      },
      {
        id: "auth",
        header: "Auth",
        accessorFn: (mount) => getAuthLabel(mount),
        cell: ({ row }) => (
          <Chip
            label={getAuthLabel(row.original)}
            size="small"
            variant="soft"
          />
        ),
        meta: {
          align: "left",
          getCellRenderKey: (row) => {
            const mount = row as CIFSMount;
            return [mount.mountpoint, getAuthLabel(mount)];
          },
          width: "140px",
        },
      },
      {
        id: "status",
        header: "Status",
        accessorFn: (mount) => getStatusLabel(mount),
        cell: ({ row }) => (
          <Chip
            label={getStatusLabel(row.original)}
            size="small"
            variant="soft"
          />
        ),
        meta: {
          align: "left",
          getCellRenderKey: (row) => {
            const mount = row as CIFSMount;
            return [mount.mountpoint, getStatusLabel(mount)];
          },
          width: "120px",
        },
      },
      {
        accessorKey: "usedPct",
        header: "Usage",
        cell: ({ row }) => {
          const mount = row.original;
          return mount.mounted ? (
            <div style={{ width: "100%" }}>
              <AppLinearProgress
                color={getUsageColor(mount.usedPct)}
                style={{ height: 6, borderRadius: 3, marginBottom: 2 }}
                value={mount.usedPct}
                variant="determinate"
              />
              <AppTypography color="text.secondary" variant="caption">
                {formatFileSize(mount.used)} / {formatFileSize(mount.size)}
              </AppTypography>
            </div>
          ) : (
            <AppTypography color="text.secondary" variant="caption">
              Not mounted
            </AppTypography>
          );
        },
        meta: {
          align: "left",
          getCellRenderKey: (row) => {
            const mount = row as CIFSMount;
            return [
              mount.mountpoint,
              mount.mounted,
              mount.usedPct,
              mount.used,
              mount.size,
            ];
          },
          hideBelow: "sm",
          width: "200px",
        },
      },
      {
        id: "actions",
        header: "Actions",
        enableSorting: false,
        cell: ({ row }) => {
          const mount = row.original;
          const pendingAction = pendingActionByMountpoint.get(mount.mountpoint);
          const isPending = Boolean(pendingAction);
          return (
            <div
              aria-busy={isPending}
              aria-label={`Actions for ${mount.mountpoint}`}
              role="group"
              style={{ display: "flex", gap: 4, justifyContent: "flex-end" }}
              onClick={(e) => e.stopPropagation()}
            >
              {!mount.mounted && (
                <AppTooltip
                  title={
                    cifsUnavailable
                      ? cifsReason
                      : pendingAction === "mount"
                        ? "Mounting..."
                        : "Mount"
                  }
                >
                  <span>
                    <AppActionIconButton
                      ariaLabel={
                        pendingAction === "mount"
                          ? `Mounting ${mount.mountpoint}`
                          : "Mount"
                      }
                      disabled={cifsUnavailable || isPending}
                      icon="mdi:play"
                      loading={isPending}
                      onClick={() => handleMountExisting(mount)}
                    />
                  </span>
                </AppTooltip>
              )}
              {mount.mounted && (
                <AppTooltip
                  title={
                    pendingAction === "unmount" ? "Unmounting..." : "Unmount"
                  }
                >
                  <span>
                    <AppActionIconButton
                      ariaLabel={
                        pendingAction === "unmount"
                          ? `Unmounting ${mount.mountpoint}`
                          : "Unmount"
                      }
                      disabled={isPending}
                      icon="mdi:eject"
                      loading={isPending}
                      onClick={() => handleUnmount(mount)}
                    />
                  </span>
                </AppTooltip>
              )}
              <AppTooltip title="Edit options">
                <span>
                  <AppActionIconButton
                    ariaLabel="Edit options"
                    disabled={isPending}
                    icon="mdi:pencil"
                    onClick={() => {
                      setSelectedMount(mount);
                      setEditDialogOpen(true);
                    }}
                  />
                </span>
              </AppTooltip>
              <AppTooltip title="Remove">
                <span>
                  <AppActionIconButton
                    ariaLabel="Remove"
                    disabled={isPending}
                    icon="mdi:delete"
                    onClick={() => {
                      setSelectedMount(mount);
                      setRemoveDialogOpen(true);
                    }}
                  />
                </span>
              </AppTooltip>
            </div>
          );
        },
        meta: {
          align: "right",
          getCellRenderKey: (row) => {
            const mount = row as CIFSMount;
            return [
              mount.mountpoint,
              mount.mounted,
              cifsUnavailable,
              cifsReason,
              pendingActionByMountpoint.get(mount.mountpoint),
            ];
          },
          width: "180px",
        },
      },
    ],
    [
      cifsReason,
      cifsUnavailable,
      handleMountExisting,
      handleUnmount,
      pendingActionByMountpoint,
    ],
  );

  return (
    <div style={{ display: "flex", flexDirection: "column", minHeight: 0 }}>
      {cifsUnavailable ? (
        <AppAlert severity="warning">{cifsReason}</AppAlert>
      ) : null}

      <AppVirtualTable
        ariaLabel="SMB mounts"
        columns={columns}
        data={surface.items}
        dnd={tableDnd}
        emptyMessage="No SMB entries found. Click 'Mount SMB' to add one."
        fillAvailable={false}
        getRowId={(mount) => mount.mountpoint}
        maxHeight={400}
        persistExpandedKey="cifs-mounts"
        renderExpandedContent={({ original: mount }) => (
          <div className="expand-panel">
            <AppTypography gutterBottom variant="subtitle2">
              <strong>Status:</strong> {getStatusLabel(mount)} /{" "}
              {getAuthLabel(mount)}
            </AppTypography>
            <div>
              <AppTypography gutterBottom variant="subtitle2">
                <strong>Options:</strong>
              </AppTypography>
              <div className="expand-panel__chips">
                {mount.options && mount.options.length > 0 ? (
                  mount.options.map((opt, i) => (
                    <Chip key={i} label={opt} size="small" variant="soft" />
                  ))
                ) : (
                  <AppTypography color="text.secondary" variant="body2">
                    (no options)
                  </AppTypography>
                )}
              </div>
            </div>
            <AppTypography gutterBottom variant="subtitle2">
              <strong>Filesystem Type:</strong> {mount.fsType || "cifs"}
            </AppTypography>
            {mount.mounted ? (
              <AppTypography gutterBottom variant="subtitle2">
                <strong>Storage:</strong> {formatFileSize(mount.used)} used of{" "}
                {formatFileSize(mount.size)} ({mount.usedPct.toFixed(1)}% used,{" "}
                {formatFileSize(mount.free)} free)
              </AppTypography>
            ) : (
              <AppTypography gutterBottom variant="subtitle2">
                <strong>Storage:</strong> Not currently mounted
              </AppTypography>
            )}
          </div>
        )}
      />

      <MountCIFSDialog
        onClose={() => setMountDialogOpen(false)}
        open={mountDialogOpen}
      />

      <EditCIFSDialog
        mount={selectedMount}
        onClose={() => setEditDialogOpen(false)}
        open={editDialogOpen}
      />

      <RemoveCIFSDialog
        mount={selectedMount}
        onClose={() => setRemoveDialogOpen(false)}
        open={removeDialogOpen}
      />
    </div>
  );
};

export default CIFSMounts;
