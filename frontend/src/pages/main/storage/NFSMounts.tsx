import {
  Autocomplete,
  FormControlLabel,
  Grid,
  Switch,
  TextField,
} from "@mui/material";
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
import FrostedCard from "@/components/cards/RootCard";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import UnifiedCollapsibleTable, {
  UnifiedTableColumn,
} from "@/components/tables/UnifiedCollapsibleTable";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import { AppTableCell } from "@/components/ui/AppTable";
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
interface UnmountDialogProps {
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
          <TextField
            label="NFS Server"
            value={server}
            onChange={(e) => setServer(e.target.value)}
            placeholder="e.g., 192.168.1.100 or nas.local"
            fullWidth
            size="small"
          />
          <Autocomplete
            freeSolo
            options={exports}
            value={exportPath}
            onInputChange={(_, value) => setExportPath(value)}
            loading={loadingExports}
            renderInput={(params) => (
              <TextField
                {...params}
                label="Path on Server"
                placeholder="e.g., /shared/data"
                size="small"
                slotProps={{
                  input: {
                    ...params.InputProps,
                    endAdornment: (
                      <>
                        {loadingExports ? (
                          <AppCircularProgress size={20} />
                        ) : null}
                        {params.InputProps.endAdornment}
                      </>
                    ),
                  },
                }}
              />
            )}
          />
          <TextField
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
          <FormControlLabel
            control={
              <Switch
                checked={mountAtBoot}
                onChange={(e) => setMountAtBoot(e.target.checked)}
              />
            }
            label="Mount at boot (add to /etc/fstab)"
          />
          <FormControlLabel
            control={
              <Switch
                checked={readOnly}
                onChange={(e) => setReadOnly(e.target.checked)}
              />
            }
            label="Mount read-only"
          />
          <TextField
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
const UnmountDialog: React.FC<UnmountDialogProps> = ({
  open,
  onClose,
  mount,
  onSuccess,
}) => {
  const queryClient = useQueryClient();
  const [removeFstab, setRemoveFstab] = useState(false);
  const { mutate: unmountNFS, isPending: isUnmounting } =
    linuxio.storage.unmount_nfs.useMutation({
      onSuccess: (result) => {
        if (result.warning) {
          toast.warning(result.warning);
        } else {
          toast.success(`Unmounted ${mount?.mountpoint}`);
        }
        queryClient.invalidateQueries({
          queryKey: linuxio.storage.list_nfs_mounts.queryKey(),
        });
        onSuccess();
        handleClose();
      },
      onError: (error: Error) => {
        toast.error(getMutationErrorMessage(error, "Failed to unmount"));
      },
    });
  const handleUnmount = () => {
    if (!mount) return;
    unmountNFS([mount.mountpoint, removeFstab ? "true" : "false"]);
  };
  const handleClose = () => {
    setRemoveFstab(false);
    onClose();
  };
  return (
    <GeneralDialog open={open} onClose={handleClose} maxWidth="sm" fullWidth>
      <AppDialogTitle>Unmount NFS Share</AppDialogTitle>
      <AppDialogContent>
        <AppDialogContentText>
          Are you sure you want to unmount the NFS share?
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
        <FormControlLabel
          control={
            <Switch
              checked={removeFstab}
              onChange={(e) => setRemoveFstab(e.target.checked)}
            />
          }
          label="Also remove from /etc/fstab"
        />
      </AppDialogContent>
      <AppDialogActions>
        <AppButton onClick={handleClose} disabled={isUnmounting}>
          Cancel
        </AppButton>
        <AppButton
          onClick={handleUnmount}
          variant="contained"
          color="error"
          disabled={isUnmounting}
        >
          {isUnmounting ? "Unmounting..." : "Unmount"}
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
          <TextField
            label="Server Address"
            value={server}
            slotProps={{
              input: {
                readOnly: true,
              },
            }}
            fullWidth
            size="small"
          />
          <TextField
            label="Path on Server"
            value={exportPath}
            slotProps={{
              input: {
                readOnly: true,
              },
            }}
            fullWidth
            size="small"
          />
          {mount && (
            <TextField
              label="Local Mountpoint"
              value={mount.mountpoint}
              slotProps={{
                input: {
                  readOnly: true,
                },
              }}
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
          <FormControlLabel
            control={
              <Switch
                checked={mountAtBoot}
                onChange={(e) => setMountAtBoot(e.target.checked)}
              />
            }
            label="Mount at boot (add to /etc/fstab)"
          />
          <FormControlLabel
            control={
              <Switch
                checked={readOnly}
                onChange={(e) => setReadOnly(e.target.checked)}
              />
            }
            label="Mount read-only"
          />
          <TextField
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
  const [search, setSearch] = useState("");
  const [mountDialogOpen, setMountDialogOpen] = useState(false);
  const [unmountDialogOpen, setUnmountDialogOpen] = useState(false);
  const [editDialogOpen, setEditDialogOpen] = useState(false);
  const [selectedMount, setSelectedMount] = useState<NFSMount | null>(null);
  const {
    data: mounts = [],
    isPending: loading,
    refetch,
  } = linuxio.storage.list_nfs_mounts.useQuery({
    refetchInterval: 10000,
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
    setSelectedMount(mount);
    setUnmountDialogOpen(true);
  };
  const handleEdit = (mount: NFSMount) => {
    setSelectedMount(mount);
    setEditDialogOpen(true);
  };
  if (loading) {
    return <ComponentLoader />;
  }
  const mountsList = Array.isArray(mounts) ? mounts : [];
  const filtered = mountsList.filter(
    (m) =>
      m.source.toLowerCase().includes(search.toLowerCase()) ||
      m.mountpoint.toLowerCase().includes(search.toLowerCase()),
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
      <div
        style={{
          marginBottom: 8,
          display: "flex",
          alignItems: "center",
          gap: 8,
          flexWrap: "wrap",
        }}
      >
        <TextField
          variant="outlined"
          size="small"
          placeholder="Search NFS mounts..."
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          sx={{
            width: 320,
            "@media (max-width: 600px)": {
              width: "100%",
            },
          }}
        />
        <AppTypography fontWeight={700}>{filtered.length} mounts</AppTypography>
      </div>

      {viewMode === "card" ? (
        filtered.length > 0 ? (
          <Grid container spacing={2}>
            {filtered.map((mount) => (
              <Grid
                key={mount.mountpoint}
                size={{
                  xs: 12,
                  sm: 6,
                  md: 4,
                  lg: 3,
                }}
              >
                <FrostedCard
                  style={{
                    padding: 8,
                  }}
                >
                  <AppTypography
                    variant="body2"
                    fontWeight={700}
                    style={{
                      marginBottom: 2,
                      fontFamily: "monospace",
                    }}
                  >
                    {mount.source}
                  </AppTypography>
                  <AppTypography
                    variant="body2"
                    style={{
                      marginBottom: 4,
                      fontFamily: "monospace",
                    }}
                  >
                    {mount.mountpoint}
                  </AppTypography>

                  <div
                    style={{
                      width: "100%",
                      marginBottom: 4,
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

                  <div
                    style={{
                      display: "flex",
                      flexWrap: "wrap",
                      gap: 3,
                      marginBottom: 4,
                    }}
                  >
                    <Chip label={mount.fsType} size="small" variant="soft" />
                    {mount.options?.slice(0, 2).map((opt, i) => (
                      <Chip
                        key={`${mount.mountpoint}-${i}`}
                        label={opt}
                        size="small"
                        variant="soft"
                      />
                    ))}
                  </div>

                  <div
                    style={{
                      display: "flex",
                      gap: 4,
                    }}
                  >
                    <AppButton
                      size="small"
                      variant="outlined"
                      onClick={() => handleEdit(mount)}
                    >
                      Edit
                    </AppButton>
                    <AppButton
                      size="small"
                      color="error"
                      onClick={() => handleUnmount(mount)}
                    >
                      Unmount
                    </AppButton>
                  </div>
                </FrostedCard>
              </Grid>
            ))}
          </Grid>
        ) : (
          <div
            style={{
              textAlign: "center",
              paddingBlock: 16,
            }}
          >
            <AppTypography variant="body2" color="text.secondary">
              No NFS mounts found. Click Mount NFS to add one.
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
              <AppTableCell className="app-table-hide-below-sm">
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
                    {formatFileSize(mount.used)} / {formatFileSize(mount.size)}
                  </AppTypography>
                </div>
              </AppTableCell>
              <AppTableCell>
                <div
                  style={{
                    display: "flex",
                    gap: 4,
                  }}
                >
                  <AppButton
                    size="small"
                    variant="outlined"
                    onClick={(e) => {
                      e.stopPropagation();
                      handleEdit(mount);
                    }}
                  >
                    Edit
                  </AppButton>
                  <AppButton
                    size="small"
                    color="error"
                    onClick={(e) => {
                      e.stopPropagation();
                      handleUnmount(mount);
                    }}
                  >
                    Unmount
                  </AppButton>
                </div>
              </AppTableCell>
            </>
          )}
          renderExpandedContent={(mount) => (
            <div>
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
              <AppTypography variant="subtitle2" gutterBottom>
                <strong>Storage:</strong> {formatFileSize(mount.used)} used of{" "}
                {formatFileSize(mount.size)} ({mount.usedPct.toFixed(1)}% used,{" "}
                {formatFileSize(mount.free)} free)
              </AppTypography>
            </div>
          )}
          emptyMessage="No NFS mounts found. Click 'Mount NFS' to add one."
        />
      )}

      <MountNFSDialog
        open={mountDialogOpen}
        onClose={() => setMountDialogOpen(false)}
        onSuccess={() => refetch()}
      />

      <UnmountDialog
        open={unmountDialogOpen}
        onClose={() => setUnmountDialogOpen(false)}
        mount={selectedMount}
        onSuccess={() => refetch()}
      />

      <EditNFSDialog
        open={editDialogOpen}
        onClose={() => setEditDialogOpen(false)}
        mount={selectedMount}
        onSuccess={() => refetch()}
      />
    </div>
  );
};
export default NFSMounts;
