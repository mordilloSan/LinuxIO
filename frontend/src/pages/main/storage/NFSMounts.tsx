import {
  Alert,
  Autocomplete,
  Box,
  Button,
  Chip,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  FormControlLabel,
  LinearProgress,
  Switch,
  TableCell,
  TextField,
  Typography,
} from "@mui/material";
import React, {
  useCallback,
  useEffect,
  useEffectEvent,
  useRef,
  useState,
} from "react";
import { toast } from "sonner";

import type { NFSMount } from "@/api/linuxio-types";
import linuxio from "@/api/react-query";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import UnifiedCollapsibleTable, {
  UnifiedTableColumn,
} from "@/components/tables/UnifiedCollapsibleTable";
import { formatFileSize } from "@/utils/formaters";

interface NFSMountsProps {
  onMountCreateHandler?: (handler: () => void) => void;
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

  const mountMutation = linuxio.storage.mount_nfs.useMutation();
  const exportsMutation = linuxio.storage.list_nfs_exports.useMutation();

  const fetchExports = useEffectEvent(async (serverAddress: string) => {
    setLoadingExports(true);
    try {
      const result = await exportsMutation.mutateAsync([serverAddress]);
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

  const handleMount = async () => {
    if (!server || !exportPath || !mountpoint) {
      setValidationError("Server, export path, and mountpoint are required");
      return;
    }

    setValidationError(null);

    try {
      const result = await mountMutation.mutateAsync([
        server,
        exportPath,
        mountpoint,
        buildOptionsString(),
        mountAtBoot ? "true" : "false",
      ]);

      if (result.warning) {
        toast.warning(result.warning);
      } else {
        toast.success(`NFS share mounted at ${mountpoint}`);
      }
      onSuccess();
      handleClose();
    } catch (err: any) {
      toast.error(err?.message || "Failed to mount NFS share");
    }
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
    mountMutation.reset();
    onClose();
  };

  return (
    <Dialog open={open} onClose={handleClose} maxWidth="sm" fullWidth>
      <DialogTitle>Mount NFS Share</DialogTitle>
      <DialogContent>
        <Box sx={{ display: "flex", flexDirection: "column", gap: 2, mt: 1 }}>
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
                        {loadingExports ? <CircularProgress size={20} /> : null}
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
          <Typography variant="subtitle2" sx={{ mt: 1 }}>
            Mount Options
          </Typography>
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
          {validationError && <Alert severity="error">{validationError}</Alert>}
          {mountMutation.error && (
            <Alert severity="error">{mountMutation.error.message}</Alert>
          )}
        </Box>
      </DialogContent>
      <DialogActions>
        <Button onClick={handleClose} disabled={mountMutation.isPending}>
          Cancel
        </Button>
        <Button
          onClick={handleMount}
          variant="contained"
          disabled={mountMutation.isPending}
        >
          {mountMutation.isPending ? "Mounting..." : "Mount"}
        </Button>
      </DialogActions>
    </Dialog>
  );
};

const UnmountDialog: React.FC<UnmountDialogProps> = ({
  open,
  onClose,
  mount,
  onSuccess,
}) => {
  const [removeFstab, setRemoveFstab] = useState(false);

  const unmountMutation = linuxio.storage.unmount_nfs.useMutation();

  const handleUnmount = async () => {
    if (!mount) return;

    try {
      const result = await unmountMutation.mutateAsync([
        mount.mountpoint,
        removeFstab ? "true" : "false",
      ]);

      if (result.warning) {
        toast.warning(result.warning);
      } else {
        toast.success(`Unmounted ${mount.mountpoint}`);
      }
      onSuccess();
      handleClose();
    } catch (err: any) {
      toast.error(err?.message || "Failed to unmount");
    }
  };

  const handleClose = () => {
    setRemoveFstab(false);
    unmountMutation.reset();
    onClose();
  };

  return (
    <Dialog open={open} onClose={handleClose} maxWidth="sm" fullWidth>
      <DialogTitle>Unmount NFS Share</DialogTitle>
      <DialogContent>
        <DialogContentText>
          Are you sure you want to unmount the NFS share?
        </DialogContentText>
        {mount && (
          <Box sx={{ mt: 2, mb: 2 }}>
            <Typography variant="body2">
              <strong>Source:</strong> {mount.source}
            </Typography>
            <Typography variant="body2">
              <strong>Mountpoint:</strong> {mount.mountpoint}
            </Typography>
          </Box>
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
        {unmountMutation.error && (
          <Alert severity="error" sx={{ mt: 2 }}>
            {unmountMutation.error.message}
          </Alert>
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={handleClose} disabled={unmountMutation.isPending}>
          Cancel
        </Button>
        <Button
          onClick={handleUnmount}
          variant="contained"
          color="error"
          disabled={unmountMutation.isPending}
        >
          {unmountMutation.isPending ? "Unmounting..." : "Unmount"}
        </Button>
      </DialogActions>
    </Dialog>
  );
};

const EditNFSDialog: React.FC<EditNFSDialogProps> = ({
  open,
  onClose,
  mount,
  onSuccess,
}) => {
  const [readOnly, setReadOnly] = useState(false);
  const [mountAtBoot, setMountAtBoot] = useState(false);
  const [customOptions, setCustomOptions] = useState("");

  const remountMutation = linuxio.storage.remount_nfs.useMutation();

  // Use server and exportPath directly from mount data
  const server = mount?.server || "";
  const exportPath = mount?.exportPath || "";

  useEffect(() => {
    if (mount) {
      const opts = mount.options || [];
      setReadOnly(opts.includes("ro"));
      // Use the inFstab field from backend to determine if mount is persistent
      setMountAtBoot(mount.inFstab);
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
      setCustomOptions(custom.join(","));
    }
  }, [mount]);

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

  const handleSave = async () => {
    if (!mount) return;

    try {
      const result = await remountMutation.mutateAsync([
        mount.mountpoint,
        buildOptionsString(),
        mountAtBoot ? "true" : "false",
      ]);

      if (result.warning) {
        toast.warning(result.warning);
      } else {
        toast.success(`NFS mount options updated`);
      }
      onSuccess();
      handleClose();
    } catch (err: any) {
      toast.error(err?.message || "Failed to update mount options");
    }
  };

  const handleClose = () => {
    setReadOnly(false);
    setMountAtBoot(false);
    setCustomOptions("");
    remountMutation.reset();
    onClose();
  };

  return (
    <Dialog open={open} onClose={handleClose} maxWidth="sm" fullWidth>
      <DialogTitle>Edit NFS Mount Options</DialogTitle>
      <DialogContent>
        <Box sx={{ display: "flex", flexDirection: "column", gap: 2, mt: 1 }}>
          <TextField
            label="Server Address"
            value={server}
            slotProps={{ input: { readOnly: true } }}
            fullWidth
            size="small"
          />
          <TextField
            label="Path on Server"
            value={exportPath}
            slotProps={{ input: { readOnly: true } }}
            fullWidth
            size="small"
          />
          {mount && (
            <TextField
              label="Local Mountpoint"
              value={mount.mountpoint}
              slotProps={{ input: { readOnly: true } }}
              fullWidth
              size="small"
            />
          )}
          <Typography variant="subtitle2" sx={{ mt: 1 }}>
            Mount Options
          </Typography>
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
          {remountMutation.error && (
            <Alert severity="error">{remountMutation.error.message}</Alert>
          )}
        </Box>
      </DialogContent>
      <DialogActions>
        <Button onClick={handleClose} disabled={remountMutation.isPending}>
          Cancel
        </Button>
        <Button
          onClick={handleSave}
          variant="contained"
          disabled={remountMutation.isPending}
        >
          {remountMutation.isPending ? "Saving..." : "Save"}
        </Button>
      </DialogActions>
    </Dialog>
  );
};

const NFSMounts: React.FC<NFSMountsProps> = ({ onMountCreateHandler }) => {
  const [search, setSearch] = useState("");
  const [mountDialogOpen, setMountDialogOpen] = useState(false);
  const [unmountDialogOpen, setUnmountDialogOpen] = useState(false);
  const [editDialogOpen, setEditDialogOpen] = useState(false);
  const [selectedMount, setSelectedMount] = useState<NFSMount | null>(null);

  const {
    data: mounts = [],
    isPending: loading,
    refetch,
  } = linuxio.storage.list_nfs_mounts.useQuery({ refetchInterval: 10000 });

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
    { field: "source", headerName: "NFS Share", align: "left" },
    { field: "mountpoint", headerName: "Mount Point", align: "left" },
    {
      field: "usage",
      headerName: "Usage",
      align: "left",
      width: "200px",
      sx: { display: { xs: "none", sm: "table-cell" } },
    },
    { field: "actions", headerName: "", align: "right", width: "160px" },
  ];

  return (
    <Box>
      <Box mb={2} display="flex" alignItems="center" gap={2} flexWrap="wrap">
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
        <Typography fontWeight="bold">{filtered.length} mounts</Typography>
      </Box>

      <UnifiedCollapsibleTable
        data={filtered}
        columns={columns}
        getRowKey={(mount) => mount.mountpoint}
        renderMainRow={(mount) => (
          <>
            <TableCell>
              <Typography variant="body2" fontFamily="monospace">
                {mount.source}
              </Typography>
            </TableCell>
            <TableCell>
              <Typography variant="body2" fontFamily="monospace">
                {mount.mountpoint}
              </Typography>
            </TableCell>
            <TableCell sx={{ display: { xs: "none", sm: "table-cell" } }}>
              <Box sx={{ width: "100%" }}>
                <LinearProgress
                  variant="determinate"
                  value={mount.usedPct}
                  sx={{ height: 6, borderRadius: 3, mb: 0.5 }}
                  color={
                    mount.usedPct > 90
                      ? "error"
                      : mount.usedPct > 70
                        ? "warning"
                        : "primary"
                  }
                />
                <Typography variant="caption" color="text.secondary">
                  {formatFileSize(mount.used)} / {formatFileSize(mount.size)}
                </Typography>
              </Box>
            </TableCell>
            <TableCell>
              <Box sx={{ display: "flex", gap: 1 }}>
                <Button
                  size="small"
                  variant="outlined"
                  onClick={(e) => {
                    e.stopPropagation();
                    handleEdit(mount);
                  }}
                >
                  Edit
                </Button>
                <Button
                  size="small"
                  color="error"
                  onClick={(e) => {
                    e.stopPropagation();
                    handleUnmount(mount);
                  }}
                >
                  Unmount
                </Button>
              </Box>
            </TableCell>
          </>
        )}
        renderExpandedContent={(mount) => (
          <Box>
            <Typography variant="subtitle2" gutterBottom>
              <strong>Options:</strong>
            </Typography>
            <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1, mb: 2 }}>
              {mount.options && mount.options.length > 0 ? (
                mount.options.map((opt, i) => (
                  <Chip key={i} label={opt} size="small" />
                ))
              ) : (
                <Typography variant="body2" color="text.secondary">
                  (no options)
                </Typography>
              )}
            </Box>
            <Typography variant="subtitle2" gutterBottom>
              <strong>Filesystem Type:</strong> {mount.fsType}
            </Typography>
            <Typography variant="subtitle2" gutterBottom>
              <strong>Storage:</strong> {formatFileSize(mount.used)} used of{" "}
              {formatFileSize(mount.size)} ({mount.usedPct.toFixed(1)}% used,{" "}
              {formatFileSize(mount.free)} free)
            </Typography>
          </Box>
        )}
        emptyMessage="No NFS mounts found. Click 'Mount NFS' to add one."
      />

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
    </Box>
  );
};

export default NFSMounts;
