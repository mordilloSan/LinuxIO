import { useQueryClient } from "@tanstack/react-query";
import React, {
  useCallback,
  useEffect,
  useEffectEvent,
  useRef,
  useState,
} from "react";

import {
  CACHE_TTL_MS,
  jobSnapshotResult,
  linuxio,
  type CIFSMount,
} from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import PageLoader from "@/components/loaders/PageLoader";
import AppDataTable from "@/components/tables/AppDataTable";
import type { AppDataTableColumnDef } from "@/components/tables/AppDataTable";
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
import { useCapability } from "@/hooks/useCapabilities";
import { useRegisterCreateHandler } from "@/hooks/useRegisterCreateHandler";
import { useScopedToast } from "@/hooks/useScopedToast";
import { formatFileSize } from "@/utils/formaters";
import { getMutationErrorMessage } from "@/utils/mutations";

interface CIFSMountsProps {
  onMountCreateHandler?: (handler: () => void) => void;
}

interface MountCIFSDialogProps {
  open: boolean;
  onClose: () => void;
  onSuccess: () => void;
}

interface RemoveCIFSDialogProps {
  mount: CIFSMount | null;
  open: boolean;
  onClose: () => void;
  onSuccess: () => void;
}

interface EditCIFSDialogProps {
  mount: CIFSMount | null;
  open: boolean;
  onClose: () => void;
  onSuccess: () => void;
}

function getStatusLabel(mount: CIFSMount): string {
  return mount.mounted ? "Mounted" : "Configured";
}

function getAuthLabel(mount: CIFSMount): string {
  return mount.username ? `User: ${mount.username}` : "Guest";
}

const MountCIFSDialog: React.FC<MountCIFSDialogProps> = ({
  open,
  onClose,
  onSuccess,
}) => {
  const queryClient = useQueryClient();
  const toast = useScopedToast({ href: "/storage", label: "Open storage" });
  const [server, setServer] = useState("");
  const [share, setShare] = useState("");
  const [mountpoint, setMountpoint] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [domain, setDomain] = useState("");
  const [readOnly, setReadOnly] = useState(false);
  const [customOptions, setCustomOptions] = useState("");
  const [validationError, setValidationError] = useState<string | null>(null);
  const [shares, setShares] = useState<string[]>([]);
  const [loadingShares, setLoadingShares] = useState(false);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const { mutate: mountCIFS, isPending: isMounting } =
    linuxio.storage.mount_cifs.useMutation({
      onSuccess: (result) => {
        const mountResult = jobSnapshotResult(result);
        if (mountResult.warning) {
          toast.warning(mountResult.warning);
        } else {
          toast.success(`SMB share mounted at ${mountpoint}`);
        }
        queryClient.invalidateQueries({
          queryKey: linuxio.storage.list_cifs_mounts.queryKey(),
        });
        onSuccess();
        handleClose();
      },
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to mount SMB share"),
        );
      },
    });

  const fetchShares = useEffectEvent(async (serverAddress: string) => {
    setLoadingShares(true);
    try {
      const result = await queryClient.fetchQuery(
        linuxio.storage.list_cifs_shares.queryOptions(serverAddress, {
          staleTime: CACHE_TTL_MS.THIRTY_SECONDS,
        }),
      );
      setShares(result || []);
    } catch {
      setShares([]); // browsing is best-effort; fall back to free-text
    } finally {
      setLoadingShares(false);
    }
  });

  useEffect(() => {
    if (debounceRef.current) {
      clearTimeout(debounceRef.current);
    }
    debounceRef.current = setTimeout(
      () => {
        if (!server || server.length < 3) {
          setShares([]);
        } else {
          fetchShares(server);
        }
      },
      !server || server.length < 3 ? 0 : 500,
    );
    return () => {
      if (debounceRef.current) {
        clearTimeout(debounceRef.current);
      }
    };
  }, [server]);

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
    setShares([]);
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

const RemoveCIFSDialog: React.FC<RemoveCIFSDialogProps> = ({
  open,
  onClose,
  mount,
  onSuccess,
}) => {
  const toast = useScopedToast({ href: "/storage", label: "Open storage" });
  const { mutate: removeEntry, isPending: isRemoving } =
    linuxio.storage.unmount_cifs.useMutation({
      onSuccess: (result) => {
        const removeResult = jobSnapshotResult(result);
        toast.success(`Removed ${mount?.mountpoint}`);
        if (removeResult.warning) {
          toast.warning(removeResult.warning);
        }
        onSuccess();
        onClose();
      },
      onError: (error: Error) => {
        toast.error(getMutationErrorMessage(error, "Failed to remove entry"));
      },
    });

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
const EditCIFSDialog: React.FC<EditCIFSDialogProps> = ({
  open,
  onClose,
  mount,
  onSuccess,
}) => (
  <GeneralDialog fullWidth maxWidth="sm" onClose={onClose} open={open}>
    <AppDialogTitle>Edit SMB Mount Options</AppDialogTitle>
    {open && mount ? (
      <EditCIFSForm
        key={mount.mountpoint}
        mount={mount}
        onClose={onClose}
        onSuccess={onSuccess}
      />
    ) : null}
  </GeneralDialog>
);

const EditCIFSForm: React.FC<{
  mount: CIFSMount;
  onClose: () => void;
  onSuccess: () => void;
}> = ({ mount, onClose, onSuccess }) => {
  const toast = useScopedToast({ href: "/storage", label: "Open storage" });
  const [readOnly, setReadOnly] = useState(
    (mount.options ?? []).includes("ro"),
  );
  const [customOptions, setCustomOptions] = useState(
    (mount.options ?? []).filter((o) => o !== "ro" && o !== "rw").join(","),
  );

  const { mutate: remountCIFS, isPending: isSaving } =
    linuxio.storage.remount_cifs.useMutation({
      onSuccess: (result) => {
        const remountResult = jobSnapshotResult(result);
        if (remountResult.warning) {
          toast.warning(remountResult.warning);
        } else {
          toast.success("SMB mount options updated");
        }
        onSuccess();
        onClose();
      },
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to update mount options"),
        );
      },
    });

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

const CIFSMounts: React.FC<CIFSMountsProps> = ({ onMountCreateHandler }) => {
  const toast = useScopedToast({ href: "/storage", label: "Open storage" });
  const { reason: cifsReason, status: cifsStatus } = useCapability(
    "sambaClientAvailable",
  );
  const cifsUnavailable = cifsStatus === "unavailable";

  const [mountDialogOpen, setMountDialogOpen] = useState(false);
  const [editDialogOpen, setEditDialogOpen] = useState(false);
  const [removeDialogOpen, setRemoveDialogOpen] = useState(false);
  const [selectedMount, setSelectedMount] = useState<CIFSMount | null>(null);

  const {
    data: mounts = [],
    isPending: loading,
    refetch,
  } = linuxio.storage.list_cifs_mounts.useQuery({
    refetchInterval: 10000,
  });

  const { mutate: mountExisting } = linuxio.storage.mount_cifs.useMutation({
    onSuccess: (result) => {
      const mountResult = jobSnapshotResult(result);
      if (mountResult.warning) {
        toast.warning(mountResult.warning);
      } else {
        toast.success("SMB entry mounted");
      }
      refetch();
    },
    onError: (error: Error) => {
      toast.error(getMutationErrorMessage(error, "Failed to mount SMB entry"));
    },
  });

  const { mutate: unmountEntry } = linuxio.storage.unmount_cifs.useMutation({
    onSuccess: (result, variables) => {
      const unmountResult = jobSnapshotResult(result);
      if (unmountResult.warning) {
        toast.warning(unmountResult.warning);
      } else {
        toast.success(`Unmounted ${variables.mountpoint}`);
      }
      refetch();
    },
    onError: (error: Error) => {
      toast.error(getMutationErrorMessage(error, "Failed to unmount"));
    },
  });

  const handleCreate = useCallback(() => {
    if (cifsUnavailable) {
      toast.error(cifsReason);
      return;
    }
    setMountDialogOpen(true);
  }, [cifsUnavailable, cifsReason, toast]);
  useRegisterCreateHandler(onMountCreateHandler, handleCreate);

  // Re-activate an inactive fstab entry — the backend mounts it from fstab
  // using the stored credentials, so no password is needed.
  const handleMountExisting = (mount: CIFSMount) => {
    if (cifsUnavailable) {
      toast.error(cifsReason);
      return;
    }
    mountExisting({
      server: mount.server,
      share: mount.share,
      mountpoint: mount.mountpoint,
      username: "",
      password: "",
      domain: "",
      options: [],
    });
  };

  if (loading) {
    return <PageLoader />;
  }

  const mountsList = Array.isArray(mounts) ? mounts : [];

  const columns: AppDataTableColumnDef<CIFSMount>[] = [
    {
      accessorKey: "source",
      header: "SMB Share",
      cell: ({ row }) => (
        <AppTypography style={{ fontFamily: "monospace" }} variant="body2">
          {row.original.source}
        </AppTypography>
      ),
      meta: { align: "left" },
    },
    {
      accessorKey: "mountpoint",
      header: "Mount Point",
      cell: ({ row }) => (
        <AppTypography style={{ fontFamily: "monospace" }} variant="body2">
          {row.original.mountpoint}
        </AppTypography>
      ),
      meta: { align: "left" },
    },
    {
      id: "auth",
      header: "Auth",
      accessorFn: (mount) => getAuthLabel(mount),
      cell: ({ row }) => (
        <Chip label={getAuthLabel(row.original)} size="small" variant="soft" />
      ),
      meta: { align: "left", width: "140px" },
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
      meta: { align: "left", width: "120px" },
    },
    {
      accessorKey: "usedPct",
      header: "Usage",
      cell: ({ row }) => {
        const mount = row.original;
        return mount.mounted ? (
          <div style={{ width: "100%" }}>
            <AppLinearProgress
              color={
                mount.usedPct > 90
                  ? "error"
                  : mount.usedPct > 70
                    ? "warning"
                    : "primary"
              }
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
      meta: { align: "left", hideBelow: "sm", width: "200px" },
    },
    {
      id: "actions",
      header: "",
      enableSorting: false,
      cell: ({ row }) => {
        const mount = row.original;
        return (
          <div
            style={{ display: "flex", gap: 4, justifyContent: "flex-end" }}
            onClick={(e) => e.stopPropagation()}
          >
            {!mount.mounted && (
              <AppTooltip title={cifsUnavailable ? cifsReason : "Mount"}>
                <span>
                  <AppActionIconButton
                    ariaLabel="Mount"
                    disabled={cifsUnavailable}
                    icon="mdi:play"
                    onClick={() => handleMountExisting(mount)}
                  />
                </span>
              </AppTooltip>
            )}
            {mount.mounted && (
              <AppTooltip title="Unmount">
                <span>
                  <AppActionIconButton
                    ariaLabel="Unmount"
                    icon="mdi:eject"
                    onClick={() =>
                      unmountEntry({
                        mountpoint: mount.mountpoint,
                        removeFstab: "false",
                      })
                    }
                  />
                </span>
              </AppTooltip>
            )}
            <AppTooltip title="Edit options">
              <span>
                <AppActionIconButton
                  ariaLabel="Edit options"
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
      meta: { align: "right", width: "180px" },
    },
  ];

  return (
    <div style={{ display: "flex", flexDirection: "column", minHeight: 0 }}>
      {cifsUnavailable ? (
        <AppAlert severity="warning">{cifsReason}</AppAlert>
      ) : null}

      <AppDataTable
        ariaLabel="SMB mounts"
        columns={columns}
        data={mountsList}
        emptyMessage="No SMB entries found. Click 'Mount SMB' to add one."
        getRowId={(mount) => mount.mountpoint}
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
        onSuccess={() => refetch()}
        open={mountDialogOpen}
      />

      <EditCIFSDialog
        mount={selectedMount}
        onClose={() => setEditDialogOpen(false)}
        onSuccess={() => refetch()}
        open={editDialogOpen}
      />

      <RemoveCIFSDialog
        mount={selectedMount}
        onClose={() => setRemoveDialogOpen(false)}
        onSuccess={() => refetch()}
        open={removeDialogOpen}
      />
    </div>
  );
};

export default CIFSMounts;
