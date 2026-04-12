import { Icon } from "@iconify/react";
import { useQueryClient } from "@tanstack/react-query";
import React, {
  useCallback,
  useEffect,
  useEffectEvent,
  useRef,
  useState,
} from "react";
import { toast } from "sonner";

import { linuxio, CACHE_TTL_MS, type NFSMount } from "@/api";
import NFSMountCard from "@/components/cards/NFSMountCard";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import PageLoader from "@/components/loaders/PageLoader";
import UnifiedCollapsibleTable, {
  UnifiedTableColumn,
} from "@/components/tables/UnifiedCollapsibleTable";
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
import AppGrid from "@/components/ui/AppGrid";
import AppIconButton from "@/components/ui/AppIconButton";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppSwitch from "@/components/ui/AppSwitch";
import { AppTableCell } from "@/components/ui/AppTable";
import AppTextField from "@/components/ui/AppTextField";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { formatFileSize } from "@/utils/formaters";
import { getMutationErrorMessage } from "@/utils/mutations";
interface NFSMountsProps {
  onMountCreateHandler?: (handler: () => void) => void;
  viewMode?: "table" | "card";
}
interface MountNFSDialogProps {
  open: boolean;
  onClose: () => void;
  onSuccess: () => void;
}
interface RemoveDialogProps {
  open: boolean;
  onClose: () => void;
  mount: NFSMount | null;
  onSuccess: () => void;
}
interface EditNFSDialogProps {
  open: boolean;
  onClose: () => void;
  mount: NFSMount | null;
  onSuccess: () => void;
}

function getMountStatusLabel(mount: NFSMount): string {
  return mount.mounted ? "Mounted" : "Configured";
}

function getPersistenceLabel(mount: NFSMount): string {
  return mount.inFstab ? "Persistent" : "Temporary";
}

function buildMountOptionsFromEntry(mount: NFSMount): string {
  return (mount.options ?? []).join(",");
}

const MountEntryActions: React.FC<{
  mount: NFSMount;
  mountingMountpoint: string | null;
  onEdit: (mount: NFSMount) => void;
  onMount: (mount: NFSMount) => void;
  onUnmount: (mount: NFSMount) => void;
  onRemove: (mount: NFSMount) => void;
  stopPropagation?: boolean;
}> = ({
  mount,
  mountingMountpoint,
  onEdit,
  onMount,
  onUnmount,
  onRemove,
  stopPropagation = false,
}) => {
  const wrapClick =
    (handler: (mount: NFSMount) => void) =>
    (event: React.MouseEvent<HTMLButtonElement>) => {
      if (stopPropagation) {
        event.stopPropagation();
      }
      handler(mount);
    };

  const isMounting = mountingMountpoint === mount.mountpoint;
  const mountActionColor = mount.mounted
    ? "var(--color-success)"
    : "var(--color-text-secondary)";
  const mountActionLabel = mount.mounted ? "Unmount entry" : "Mount entry";

  return (
    <div
      style={{
        display: "flex",
        gap: 2,
        alignItems: "center",
        justifyContent: "flex-end",
        flexShrink: 0,
      }}
    >
      <AppTooltip title="Edit entry">
        <AppIconButton
          size="small"
          color="primary"
          aria-label="Edit entry"
          onClick={wrapClick(onEdit)}
        >
          <Icon icon="mdi:pencil-outline" width={18} />
        </AppIconButton>
      </AppTooltip>
      <AppTooltip title={isMounting ? "Mounting..." : mountActionLabel}>
        <span
          style={{
            display: "inline-flex",
          }}
        >
          <AppIconButton
            size="small"
            color="inherit"
            aria-label={mountActionLabel}
            disabled={isMounting}
            onClick={wrapClick(mount.mounted ? onUnmount : onMount)}
            style={{
              color: mountActionColor,
            }}
          >
            <Icon
              icon={mount.mounted ? "mdi:link-variant" : "mdi:link-variant-off"}
              width={18}
            />
          </AppIconButton>
        </span>
      </AppTooltip>
      <AppTooltip title="Remove entry">
        <AppIconButton
          size="small"
          color="error"
          aria-label="Remove entry"
          onClick={wrapClick(onRemove)}
        >
          <Icon icon="mdi:trash-can-outline" width={18} />
        </AppIconButton>
      </AppTooltip>
    </div>
  );
};

const MountNFSDialog: React.FC<MountNFSDialogProps> = ({
  open,
  onClose,
  onSuccess,
}) => {
  const queryClient = useQueryClient();
  const [server, setServer] = useState("");
  const [exportPath, setExportPath] = useState("");
  const [mountpoint, setMountpoint] = useState("");
  const [readOnly, setReadOnly] = useState(false);
  const [mountAtBoot, setMountAtBoot] = useState(false);
  const [customOptions, setCustomOptions] = useState("");
  const [validationError, setValidationError] = useState<string | null>(null);
  const [exports, setExports] = useState<string[]>([]);
  const [loadingExports, setLoadingExports] = useState(false);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const { mutate: mountNFS, isPending: isMounting } =
    linuxio.storage.mount_nfs.useMutation({
      onSuccess: (result) => {
        if (result.warning) {
          toast.warning(result.warning);
        } else {
          toast.success(`NFS share mounted at ${mountpoint}`);
        }
        queryClient.invalidateQueries({
          queryKey: linuxio.storage.list_nfs_mounts.queryKey(),
        });
        onSuccess();
        handleClose();
      },
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to mount NFS share"),
        );
      },
    });
  const fetchExports = useEffectEvent(async (serverAddress: string) => {
    setLoadingExports(true);
    try {
      const result = await queryClient.fetchQuery(
        linuxio.storage.list_nfs_exports.queryOptions(serverAddress, {
          staleTime: CACHE_TTL_MS.THIRTY_SECONDS,
        }),
      );
      setExports(result || []);
    } catch {
      setExports([]);
    } finally {
      setLoadingExports(false);
    }
  });

  // Fetch exports when server changes (debounced)
  useEffect(() => {
    if (debounceRef.current) {
      clearTimeout(debounceRef.current);
    }
    if (!server || server.length < 3) {
      setExports([]);
      return;
    }
    debounceRef.current = setTimeout(() => {
      fetchExports(server);
    }, 500);
    return () => {
      if (debounceRef.current) {
        clearTimeout(debounceRef.current);
      }
    };
  }, [server]);
  const buildOptionsString = () => {
    const opts: string[] = [];
    opts.push(readOnly ? "ro" : "rw");
    if (mountAtBoot) {
      opts.push("_netdev");
    }
    if (customOptions.trim()) {
      opts.push(
        ...customOptions
          .split(",")
          .map((o) => o.trim())
          .filter(Boolean),
      );
    }
    return opts.join(",");
  };
  const handleMount = () => {
    if (!server || !exportPath || !mountpoint) {
      setValidationError("Server, export path, and mountpoint are required");
      return;
    }
    setValidationError(null);
    mountNFS([
      server,
      exportPath,
      mountpoint,
      buildOptionsString(),
      mountAtBoot ? "true" : "false",
    ]);
  };
  const handleClose = () => {
    setServer("");
    setExportPath("");
    setMountpoint("");
    setReadOnly(false);
    setMountAtBoot(false);
    setCustomOptions("");
    setExports([]);
    setValidationError(null);
    onClose();
  };
  return (
    <GeneralDialog open={open} onClose={handleClose} maxWidth="sm" fullWidth>
      <AppDialogTitle>Mount NFS Share</AppDialogTitle>
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
            label="NFS Server"
            value={server}
            onChange={(e) => setServer(e.target.value)}
            placeholder="e.g., 192.168.1.100 or nas.local"
            fullWidth
            size="small"
          />
          <AppAutocomplete
            freeSolo
            options={exports}
            value={exportPath}
            onChange={setExportPath}
            onInputChange={setExportPath}
            loading={loadingExports}
            label="Path on Server"
            placeholder="e.g., /shared/data"
            size="small"
            fullWidth
            endAdornment={
              loadingExports ? <AppCircularProgress size={20} /> : null
            }
          />
          <AppTextField
            label="Local Mountpoint"
            value={mountpoint}
            onChange={(e) => setMountpoint(e.target.value)}
            placeholder="e.g., /mnt/nfs/data"
            fullWidth
            size="small"
          />
          <AppTypography
            variant="subtitle2"
            style={{
              marginTop: 4,
            }}
          >
            Mount Options
          </AppTypography>
          <AppFormControlLabel
            control={
              <AppSwitch
                checked={mountAtBoot}
                onChange={(e) => setMountAtBoot(e.target.checked)}
              />
            }
            label="Mount at boot (add to /etc/fstab)"
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
            label="Custom Mount Options"
            value={customOptions}
            onChange={(e) => setCustomOptions(e.target.value)}
            placeholder="e.g., soft,timeo=100,retrans=2"
            helperText="Additional comma-separated mount options"
            fullWidth
            size="small"
          />
          {validationError && (
            <AppAlert severity="error">{validationError}</AppAlert>
          )}
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton onClick={handleClose} disabled={isMounting}>
          Cancel
        </AppButton>
        <AppButton
          onClick={handleMount}
          variant="contained"
          disabled={isMounting}
        >
          {isMounting ? "Mounting..." : "Mount"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};
const RemoveDialog: React.FC<RemoveDialogProps> = ({
  open,
  onClose,
  mount,
  onSuccess,
}) => {
  const queryClient = useQueryClient();
  const { mutate: removeEntry, isPending: isRemoving } =
    linuxio.storage.unmount_nfs.useMutation({
      onSuccess: (result) => {
        if (mount?.mounted) {
          toast.success(
            mount.inFstab
              ? `Removed ${mount.mountpoint}`
              : `Unmounted ${mount.mountpoint}`,
          );
        } else {
          toast.success(`Removed saved entry for ${mount?.mountpoint}`);
        }
        if (result.warning) {
          toast.warning(result.warning);
        }
        queryClient.invalidateQueries({
          queryKey: linuxio.storage.list_nfs_mounts.queryKey(),
        });
        onSuccess();
        onClose();
      },
      onError: (error: Error) => {
        toast.error(getMutationErrorMessage(error, "Failed to remove entry"));
      },
    });

  const handleRemove = () => {
    if (!mount) return;
    removeEntry([mount.mountpoint, "true"]);
  };

  return (
    <GeneralDialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <AppDialogTitle>Remove NFS Entry</AppDialogTitle>
      <AppDialogContent>
        <AppDialogContentText>
          {mount?.mounted
            ? mount.inFstab
              ? "This will unmount the NFS entry and remove it from /etc/fstab."
              : "This will unmount the NFS entry and remove its saved LinuxIO entry."
            : mount?.inFstab
              ? "This will remove the saved NFS entry from /etc/fstab."
              : "This will remove the saved LinuxIO entry for this NFS mount."}
        </AppDialogContentText>
        {mount && (
          <div
            style={{
              marginTop: 8,
              marginBottom: 8,
            }}
          >
            <AppTypography variant="body2">
              <strong>Source:</strong> {mount.source}
            </AppTypography>
            <AppTypography variant="body2">
              <strong>Mountpoint:</strong> {mount.mountpoint}
            </AppTypography>
          </div>
        )}
      </AppDialogContent>
      <AppDialogActions>
        <AppButton onClick={onClose} disabled={isRemoving}>
          Cancel
        </AppButton>
        <AppButton
          onClick={handleRemove}
          variant="contained"
          color="error"
          disabled={isRemoving}
        >
          {isRemoving ? "Removing..." : "Remove"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};
const EditNFSDialog: React.FC<EditNFSDialogProps> = ({
  open,
  onClose,
  mount,
  onSuccess,
}) => {
  const queryClient = useQueryClient();
  // Use server and exportPath directly from mount data
  const server = mount?.server || "";
  const exportPath = mount?.exportPath || "";

  // Initialize state from mount prop
  const [readOnly, setReadOnly] = useState(() => {
    const opts = mount?.options || [];
    return opts.includes("ro");
  });
  const [mountAtBoot, setMountAtBoot] = useState(() => mount?.inFstab ?? false);
  const [customOptions, setCustomOptions] = useState(() => {
    if (mount) {
      const opts = mount.options || [];
      // Filter out known/default options to get user-defined custom ones
      const knownOptions = [
        // Read/write
        "ro",
        "rw",
        // Boot/network
        "_netdev",
        "defaults",
        "nofail",
        "auto",
        "noauto",
        // Access time (system defaults)
        "relatime",
        "noatime",
        "atime",
        "strictatime",
        "lazytime",
        // Common defaults
        "sync",
        "async",
        "exec",
        "noexec",
        "suid",
        "nosuid",
        "dev",
        "nodev",
        // NFS common
        "hard",
        "soft",
        "intr",
        "nointr",
      ];
      const custom = opts.filter((o) => !knownOptions.includes(o));
      return custom.join(",");
    }
    return "";
  });
  const { mutate: remountNFS, isPending: isRemounting } =
    linuxio.storage.remount_nfs.useMutation({
      onSuccess: (result) => {
        if (result.warning) {
          toast.warning(result.warning);
        } else {
          toast.success(`NFS mount options updated`);
        }
        queryClient.invalidateQueries({
          queryKey: linuxio.storage.list_nfs_mounts.queryKey(),
        });
        onSuccess();
        handleClose();
      },
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to update mount options"),
        );
      },
    });
  const buildOptionsString = () => {
    const opts: string[] = [];
    opts.push(readOnly ? "ro" : "rw");
    if (mountAtBoot) {
      opts.push("_netdev");
    }
    if (customOptions.trim()) {
      opts.push(
        ...customOptions
          .split(",")
          .map((o) => o.trim())
          .filter(Boolean),
      );
    }
    return opts.join(",");
  };
  const handleSave = () => {
    if (!mount) return;
    remountNFS([
      mount.mountpoint,
      buildOptionsString(),
      mountAtBoot ? "true" : "false",
    ]);
  };
  const handleClose = () => {
    setReadOnly(false);
    setMountAtBoot(false);
    setCustomOptions("");
    onClose();
  };
  return (
    <GeneralDialog
      key={mount?.mountpoint}
      open={open}
      onClose={handleClose}
      maxWidth="sm"
      fullWidth
    >
      <AppDialogTitle>Edit NFS Mount Options</AppDialogTitle>
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
            label="Server Address"
            value={server}
            disabled
            fullWidth
            size="small"
          />
          <AppTextField
            label="Path on Server"
            value={exportPath}
            disabled
            fullWidth
            size="small"
          />
          {mount && (
            <AppTextField
              label="Local Mountpoint"
              value={mount.mountpoint}
              disabled
              fullWidth
              size="small"
            />
          )}
          <AppTypography
            variant="subtitle2"
            style={{
              marginTop: 4,
            }}
          >
            Mount Options
          </AppTypography>
          <AppFormControlLabel
            control={
              <AppSwitch
                checked={mountAtBoot}
                onChange={(e) => setMountAtBoot(e.target.checked)}
              />
            }
            label="Mount at boot (add to /etc/fstab)"
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
            label="Custom Mount Options"
            value={customOptions}
            onChange={(e) => setCustomOptions(e.target.value)}
            placeholder="e.g., soft,timeo=100,retrans=2"
            helperText="Additional comma-separated mount options"
            fullWidth
            size="small"
          />
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton onClick={handleClose} disabled={isRemounting}>
          Cancel
        </AppButton>
        <AppButton
          onClick={handleSave}
          variant="contained"
          disabled={isRemounting}
        >
          {isRemounting ? "Saving..." : "Save"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};
const NFSMounts: React.FC<NFSMountsProps> = ({
  onMountCreateHandler,
  viewMode = "table",
}) => {
  const [search] = useState("");
  const [mountDialogOpen, setMountDialogOpen] = useState(false);
  const [removeDialogOpen, setRemoveDialogOpen] = useState(false);
  const [editDialogOpen, setEditDialogOpen] = useState(false);
  const [selectedMount, setSelectedMount] = useState<NFSMount | null>(null);
  const [mountingMountpoint, setMountingMountpoint] = useState<string | null>(
    null,
  );
  const {
    data: mounts = [],
    isPending: loading,
    refetch,
  } = linuxio.storage.list_nfs_mounts.useQuery({
    refetchInterval: 10000,
  });
  const { mutate: mountExistingEntry } = linuxio.storage.mount_nfs.useMutation({
    onSuccess: (result) => {
      if (result.warning) {
        toast.warning(result.warning);
      } else {
        toast.success("NFS entry mounted");
      }
      setMountingMountpoint(null);
      refetch();
    },
    onError: (error: Error) => {
      setMountingMountpoint(null);
      toast.error(getMutationErrorMessage(error, "Failed to mount NFS entry"));
    },
  });
  const { mutate: unmountEntry } = linuxio.storage.unmount_nfs.useMutation({
    onSuccess: (result, variables) => {
      if (result.warning) {
        toast.warning(result.warning);
      } else {
        toast.success(`Unmounted ${variables[0]}`);
      }
      refetch();
    },
    onError: (error: Error) => {
      toast.error(getMutationErrorMessage(error, "Failed to unmount"));
    },
  });
  const handleMountNFS = useCallback(() => {
    setMountDialogOpen(true);
  }, []);
  useEffect(() => {
    if (onMountCreateHandler) {
      onMountCreateHandler(handleMountNFS);
    }
  }, [onMountCreateHandler, handleMountNFS]);
  const handleUnmount = (mount: NFSMount) => {
    unmountEntry([mount.mountpoint, "false"]);
  };
  const handleEdit = (mount: NFSMount) => {
    setSelectedMount(mount);
    setEditDialogOpen(true);
  };
  const handleRemove = (mount: NFSMount) => {
    setSelectedMount(mount);
    setRemoveDialogOpen(true);
  };
  const handleMountExisting = (mount: NFSMount) => {
    if (!mount.server || !mount.exportPath) {
      toast.error("This NFS entry is missing its server or export path");
      return;
    }
    setMountingMountpoint(mount.mountpoint);
    mountExistingEntry([
      mount.server,
      mount.exportPath,
      mount.mountpoint,
      buildMountOptionsFromEntry(mount),
      mount.inFstab ? "true" : "false",
    ]);
  };
  if (loading) {
    return <PageLoader />;
  }
  const mountsList = Array.isArray(mounts) ? mounts : [];
  const filtered = mountsList.filter(
    (m) =>
      m.source.toLowerCase().includes(search.toLowerCase()) ||
      m.mountpoint.toLowerCase().includes(search.toLowerCase()) ||
      getMountStatusLabel(m).toLowerCase().includes(search.toLowerCase()),
  );
  const columns: UnifiedTableColumn[] = [
    {
      field: "source",
      headerName: "NFS Share",
      align: "left",
    },
    {
      field: "mountpoint",
      headerName: "Mount Point",
      align: "left",
    },
    {
      field: "status",
      headerName: "Status",
      align: "left",
      width: "160px",
    },
    {
      field: "usage",
      headerName: "Usage",
      align: "left",
      width: "200px",
      className: "app-table-hide-below-sm",
    },
    {
      field: "actions",
      headerName: "",
      align: "right",
      width: "160px",
    },
  ];
  return (
    <div>
      {viewMode === "card" ? (
        filtered.length > 0 ? (
          <AppGrid container spacing={2}>
            {filtered.map((mount) => (
              <AppGrid
                key={mount.mountpoint}
                size={{
                  xs: 12,
                  sm: 6,
                  md: 4,
                  lg: 3,
                }}
              >
                <NFSMountCard
                  mount={mount}
                  statusLabel={getMountStatusLabel(mount)}
                  persistenceLabel={getPersistenceLabel(mount)}
                  actions={
                    <MountEntryActions
                      mount={mount}
                      mountingMountpoint={mountingMountpoint}
                      onEdit={handleEdit}
                      onMount={handleMountExisting}
                      onUnmount={handleUnmount}
                      onRemove={handleRemove}
                    />
                  }
                />
              </AppGrid>
            ))}
          </AppGrid>
        ) : (
          <div
            style={{
              textAlign: "center",
              paddingBlock: 16,
            }}
          >
            <AppTypography variant="body2" color="text.secondary">
              No NFS entries found. Click Mount NFS to add one.
            </AppTypography>
          </div>
        )
      ) : (
        <UnifiedCollapsibleTable
          data={filtered}
          columns={columns}
          getRowKey={(mount) => mount.mountpoint}
          renderMainRow={(mount) => (
            <>
              <AppTableCell>
                <AppTypography
                  variant="body2"
                  style={{
                    fontFamily: "monospace",
                  }}
                >
                  {mount.source}
                </AppTypography>
              </AppTableCell>
              <AppTableCell>
                <AppTypography
                  variant="body2"
                  style={{
                    fontFamily: "monospace",
                  }}
                >
                  {mount.mountpoint}
                </AppTypography>
              </AppTableCell>
              <AppTableCell>
                <div style={{ display: "flex", gap: 4, flexWrap: "wrap" }}>
                  <Chip
                    label={getMountStatusLabel(mount)}
                    size="small"
                    variant="soft"
                  />
                  <Chip
                    label={getPersistenceLabel(mount)}
                    size="small"
                    variant="soft"
                  />
                </div>
              </AppTableCell>
              <AppTableCell className="app-table-hide-below-sm">
                {mount.mounted ? (
                  <div
                    style={{
                      width: "100%",
                    }}
                  >
                    <AppLinearProgress
                      variant="determinate"
                      value={mount.usedPct}
                      style={{
                        height: 6,
                        borderRadius: 3,
                        marginBottom: 2,
                      }}
                      color={
                        mount.usedPct > 90
                          ? "error"
                          : mount.usedPct > 70
                            ? "warning"
                            : "primary"
                      }
                    />
                    <AppTypography variant="caption" color="text.secondary">
                      {formatFileSize(mount.used)} /{" "}
                      {formatFileSize(mount.size)}
                    </AppTypography>
                  </div>
                ) : (
                  <AppTypography variant="caption" color="text.secondary">
                    Not mounted
                  </AppTypography>
                )}
              </AppTableCell>
              <AppTableCell>
                <MountEntryActions
                  mount={mount}
                  mountingMountpoint={mountingMountpoint}
                  onEdit={handleEdit}
                  onMount={handleMountExisting}
                  onUnmount={handleUnmount}
                  onRemove={handleRemove}
                  stopPropagation
                />
              </AppTableCell>
            </>
          )}
          renderExpandedContent={(mount) => (
            <div>
              <AppTypography variant="subtitle2" gutterBottom>
                <strong>Status:</strong> {getMountStatusLabel(mount)} /{" "}
                {getPersistenceLabel(mount)}
              </AppTypography>
              <AppTypography variant="subtitle2" gutterBottom>
                <strong>Options:</strong>
              </AppTypography>
              <div
                style={{
                  display: "flex",
                  flexWrap: "wrap",
                  gap: 4,
                  marginBottom: 8,
                }}
              >
                {mount.options && mount.options.length > 0 ? (
                  mount.options.map((opt, i) => (
                    <Chip key={i} label={opt} size="small" variant="soft" />
                  ))
                ) : (
                  <AppTypography variant="body2" color="text.secondary">
                    (no options)
                  </AppTypography>
                )}
              </div>
              <AppTypography variant="subtitle2" gutterBottom>
                <strong>Filesystem Type:</strong> {mount.fsType}
              </AppTypography>
              {mount.mounted ? (
                <AppTypography variant="subtitle2" gutterBottom>
                  <strong>Storage:</strong> {formatFileSize(mount.used)} used of{" "}
                  {formatFileSize(mount.size)} ({mount.usedPct.toFixed(1)}%
                  used, {formatFileSize(mount.free)} free)
                </AppTypography>
              ) : (
                <AppTypography variant="subtitle2" gutterBottom>
                  <strong>Storage:</strong> Not currently mounted
                </AppTypography>
              )}
            </div>
          )}
          emptyMessage="No NFS entries found. Click 'Mount NFS' to add one."
        />
      )}

      <MountNFSDialog
        open={mountDialogOpen}
        onClose={() => setMountDialogOpen(false)}
        onSuccess={() => refetch()}
      />

      <EditNFSDialog
        open={editDialogOpen}
        onClose={() => setEditDialogOpen(false)}
        mount={selectedMount}
        onSuccess={() => refetch()}
      />

      <RemoveDialog
        open={removeDialogOpen}
        onClose={() => setRemoveDialogOpen(false)}
        mount={selectedMount}
        onSuccess={() => refetch()}
      />
    </div>
  );
};
export default NFSMounts;
