import DeleteIcon from "@mui/icons-material/Delete";
import EditIcon from "@mui/icons-material/Edit";
import ExpandMoreIcon from "@mui/icons-material/ExpandMore";
import {
  Accordion,
  AccordionDetails,
  AccordionSummary,
  Alert,
  Box,
  Button,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  FormControl,
  IconButton,
  InputLabel,
  LinearProgress,
  MenuItem,
  Select,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  TextField,
  Tooltip,
  Typography,
} from "@mui/material";
import React, { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";

import type {
  LogicalVolume,
  PhysicalVolume,
  VolumeGroup,
} from "@/api/linuxio-types";
import linuxio from "@/api/react-query";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import { formatFileSize } from "@/utils/formaters";

interface LVMManagementProps {
  onMountCreateHandler?: (handler: () => void) => void;
}

interface CreateLVDialogProps {
  open: boolean;
  onClose: () => void;
  volumeGroups: VolumeGroup[];
  onSuccess: () => void;
}

interface ResizeLVDialogProps {
  open: boolean;
  onClose: () => void;
  lv: LogicalVolume | null;
  onSuccess: () => void;
}

interface DeleteLVDialogProps {
  open: boolean;
  onClose: () => void;
  lv: LogicalVolume | null;
  onSuccess: () => void;
}

const CreateLVDialog: React.FC<CreateLVDialogProps> = ({
  open,
  onClose,
  volumeGroups,
  onSuccess,
}) => {
  const [vgName, setVgName] = useState("");
  const [lvName, setLvName] = useState("");
  const [size, setSize] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [isCreating, setIsCreating] = useState(false);

  const createMutation = linuxio.storage.create_lv.useMutation();

  const handleCreate = async () => {
    if (!vgName || !lvName || !size) {
      setError("All fields are required");
      return;
    }

    setError(null);
    setIsCreating(true);

    try {
      await createMutation.mutateAsync([vgName, lvName, size]);
      toast.success(`Logical volume ${lvName} created successfully`);
      onSuccess();
      handleClose();
    } catch (err: any) {
      setError(err?.message || "Failed to create logical volume");
    } finally {
      setIsCreating(false);
    }
  };

  const handleClose = () => {
    setVgName("");
    setLvName("");
    setSize("");
    setError(null);
    onClose();
  };

  const selectedVG = volumeGroups.find((vg) => vg.name === vgName);

  return (
    <Dialog open={open} onClose={handleClose} maxWidth="sm" fullWidth>
      <DialogTitle>Create Logical Volume</DialogTitle>
      <DialogContent>
        <Box sx={{ display: "flex", flexDirection: "column", gap: 2, mt: 1 }}>
          <FormControl fullWidth>
            <InputLabel>Volume Group</InputLabel>
            <Select
              value={vgName}
              label="Volume Group"
              onChange={(e) => setVgName(e.target.value)}
            >
              {volumeGroups.map((vg) => (
                <MenuItem key={vg.name} value={vg.name}>
                  {vg.name} ({formatFileSize(vg.free)} free)
                </MenuItem>
              ))}
            </Select>
          </FormControl>
          {selectedVG && (
            <Typography variant="body2" color="text.secondary">
              Available space: {formatFileSize(selectedVG.free)}
            </Typography>
          )}
          <TextField
            label="Logical Volume Name"
            value={lvName}
            onChange={(e) => setLvName(e.target.value)}
            placeholder="e.g., data, backup"
            fullWidth
          />
          <TextField
            label="Size"
            value={size}
            onChange={(e) => setSize(e.target.value)}
            placeholder="e.g., 10G, 500M"
            helperText="Use K, M, G, T suffix for size units"
            fullWidth
          />
          {error && <Alert severity="error">{error}</Alert>}
        </Box>
      </DialogContent>
      <DialogActions>
        <Button onClick={handleClose} disabled={isCreating}>
          Cancel
        </Button>
        <Button
          onClick={handleCreate}
          variant="contained"
          disabled={isCreating}
        >
          {isCreating ? "Creating..." : "Create"}
        </Button>
      </DialogActions>
    </Dialog>
  );
};

const ResizeLVDialog: React.FC<ResizeLVDialogProps> = ({
  open,
  onClose,
  lv,
  onSuccess,
}) => {
  const [newSize, setNewSize] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [isResizing, setIsResizing] = useState(false);

  const resizeMutation = linuxio.storage.resize_lv.useMutation();

  useEffect(() => {
    if (lv) {
      // Pre-fill with current size in GB
      const sizeGB = Math.round(lv.size / (1024 * 1024 * 1024));
      setNewSize(`${sizeGB}G`);
    }
  }, [lv]);

  const handleResize = async () => {
    if (!lv || !newSize) {
      setError("Size is required");
      return;
    }

    setError(null);
    setIsResizing(true);

    try {
      await resizeMutation.mutateAsync([lv.vgName, lv.name, newSize]);
      toast.success(`Logical volume ${lv.name} resized successfully`);
      onSuccess();
      handleClose();
    } catch (err: any) {
      setError(err?.message || "Failed to resize logical volume");
    } finally {
      setIsResizing(false);
    }
  };

  const handleClose = () => {
    setNewSize("");
    setError(null);
    onClose();
  };

  return (
    <Dialog open={open} onClose={handleClose} maxWidth="sm" fullWidth>
      <DialogTitle>Resize Logical Volume</DialogTitle>
      <DialogContent>
        <Box sx={{ display: "flex", flexDirection: "column", gap: 2, mt: 1 }}>
          {lv && (
            <>
              <Typography variant="body2">
                <strong>Volume:</strong> {lv.path}
              </Typography>
              <Typography variant="body2">
                <strong>Current Size:</strong> {formatFileSize(lv.size)}
              </Typography>
            </>
          )}
          <TextField
            label="New Size"
            value={newSize}
            onChange={(e) => setNewSize(e.target.value)}
            placeholder="e.g., 20G, 1T"
            helperText="Use K, M, G, T suffix for size units"
            fullWidth
          />
          {error && <Alert severity="error">{error}</Alert>}
        </Box>
      </DialogContent>
      <DialogActions>
        <Button onClick={handleClose} disabled={isResizing}>
          Cancel
        </Button>
        <Button
          onClick={handleResize}
          variant="contained"
          disabled={isResizing}
        >
          {isResizing ? "Resizing..." : "Resize"}
        </Button>
      </DialogActions>
    </Dialog>
  );
};

const DeleteLVDialog: React.FC<DeleteLVDialogProps> = ({
  open,
  onClose,
  lv,
  onSuccess,
}) => {
  const [error, setError] = useState<string | null>(null);
  const [isDeleting, setIsDeleting] = useState(false);

  const deleteMutation = linuxio.storage.delete_lv.useMutation();

  const handleDelete = async () => {
    if (!lv) return;

    setError(null);
    setIsDeleting(true);

    try {
      await deleteMutation.mutateAsync([lv.vgName, lv.name]);
      toast.success(`Logical volume ${lv.name} deleted successfully`);
      onSuccess();
      handleClose();
    } catch (err: any) {
      setError(err?.message || "Failed to delete logical volume");
    } finally {
      setIsDeleting(false);
    }
  };

  const handleClose = () => {
    setError(null);
    onClose();
  };

  return (
    <Dialog open={open} onClose={handleClose} maxWidth="sm" fullWidth>
      <DialogTitle>Delete Logical Volume</DialogTitle>
      <DialogContent>
        <DialogContentText>
          Are you sure you want to delete the logical volume{" "}
          <strong>{lv?.name}</strong>?
        </DialogContentText>
        {lv?.mountpoint && (
          <Alert severity="warning" sx={{ mt: 2 }}>
            This volume is currently mounted at <strong>{lv.mountpoint}</strong>
            . Please unmount it first.
          </Alert>
        )}
        <DialogContentText sx={{ mt: 2, color: "error.main" }}>
          This action cannot be undone. All data on this volume will be lost.
        </DialogContentText>
        {error && (
          <Alert severity="error" sx={{ mt: 2 }}>
            {error}
          </Alert>
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={handleClose} disabled={isDeleting}>
          Cancel
        </Button>
        <Button
          onClick={handleDelete}
          variant="contained"
          color="error"
          disabled={isDeleting || !!lv?.mountpoint}
        >
          {isDeleting ? "Deleting..." : "Delete"}
        </Button>
      </DialogActions>
    </Dialog>
  );
};

const PVTable: React.FC<{ data: PhysicalVolume[] }> = ({ data }) => (
  <TableContainer>
    <Table size="small">
      <TableHead>
        <TableRow>
          <TableCell>Name</TableCell>
          <TableCell>Volume Group</TableCell>
          <TableCell>Size</TableCell>
          <TableCell>Free</TableCell>
          <TableCell>Format</TableCell>
        </TableRow>
      </TableHead>
      <TableBody>
        {data.length === 0 ? (
          <TableRow>
            <TableCell colSpan={5}>
              <Typography color="text.secondary" align="center">
                No physical volumes found
              </Typography>
            </TableCell>
          </TableRow>
        ) : (
          data.map((pv) => (
            <TableRow key={pv.name}>
              <TableCell>
                <Typography variant="body2" fontFamily="monospace">
                  {pv.name}
                </Typography>
              </TableCell>
              <TableCell>{pv.vgName || "-"}</TableCell>
              <TableCell>{formatFileSize(pv.size)}</TableCell>
              <TableCell>{formatFileSize(pv.free)}</TableCell>
              <TableCell>
                <Chip label={pv.format} size="small" />
              </TableCell>
            </TableRow>
          ))
        )}
      </TableBody>
    </Table>
  </TableContainer>
);

const VGTable: React.FC<{ data: VolumeGroup[] }> = ({ data }) => (
  <TableContainer>
    <Table size="small">
      <TableHead>
        <TableRow>
          <TableCell>Name</TableCell>
          <TableCell>Size</TableCell>
          <TableCell>Free</TableCell>
          <TableCell>PVs</TableCell>
          <TableCell>LVs</TableCell>
        </TableRow>
      </TableHead>
      <TableBody>
        {data.length === 0 ? (
          <TableRow>
            <TableCell colSpan={5}>
              <Typography color="text.secondary" align="center">
                No volume groups found
              </Typography>
            </TableCell>
          </TableRow>
        ) : (
          data.map((vg) => (
            <TableRow key={vg.name}>
              <TableCell>
                <Typography variant="body2" fontWeight={600}>
                  {vg.name}
                </Typography>
              </TableCell>
              <TableCell>{formatFileSize(vg.size)}</TableCell>
              <TableCell>{formatFileSize(vg.free)}</TableCell>
              <TableCell>{vg.pvCount}</TableCell>
              <TableCell>{vg.lvCount}</TableCell>
            </TableRow>
          ))
        )}
      </TableBody>
    </Table>
  </TableContainer>
);

interface LVTableProps {
  data: LogicalVolume[];
  onResize: (lv: LogicalVolume) => void;
  onDelete: (lv: LogicalVolume) => void;
}

const LVTable: React.FC<LVTableProps> = ({ data, onResize, onDelete }) => (
  <TableContainer>
    <Table size="small">
      <TableHead>
        <TableRow>
          <TableCell>Name</TableCell>
          <TableCell>Volume Group</TableCell>
          <TableCell>Size</TableCell>
          <TableCell>Mountpoint</TableCell>
          <TableCell>Usage</TableCell>
          <TableCell align="right">Actions</TableCell>
        </TableRow>
      </TableHead>
      <TableBody>
        {data.length === 0 ? (
          <TableRow>
            <TableCell colSpan={6}>
              <Typography color="text.secondary" align="center">
                No logical volumes found
              </Typography>
            </TableCell>
          </TableRow>
        ) : (
          data.map((lv) => (
            <TableRow key={lv.path}>
              <TableCell>
                <Typography variant="body2" fontWeight={600}>
                  {lv.name}
                </Typography>
                <Typography
                  variant="caption"
                  color="text.secondary"
                  fontFamily="monospace"
                >
                  {lv.path}
                </Typography>
              </TableCell>
              <TableCell>{lv.vgName}</TableCell>
              <TableCell>{formatFileSize(lv.size)}</TableCell>
              <TableCell>
                {lv.mountpoint ? (
                  <Typography variant="body2" fontFamily="monospace">
                    {lv.mountpoint}
                  </Typography>
                ) : (
                  <Chip label="Not mounted" size="small" variant="outlined" />
                )}
              </TableCell>
              <TableCell>
                {lv.mountpoint ? (
                  <Box sx={{ width: 100 }}>
                    <LinearProgress
                      variant="determinate"
                      value={lv.usedPct}
                      sx={{ height: 6, borderRadius: 3, mb: 0.5 }}
                      color={
                        lv.usedPct > 90
                          ? "error"
                          : lv.usedPct > 70
                            ? "warning"
                            : "primary"
                      }
                    />
                    <Typography variant="caption">
                      {lv.usedPct.toFixed(1)}%
                    </Typography>
                  </Box>
                ) : (
                  "-"
                )}
              </TableCell>
              <TableCell align="right">
                <Tooltip title="Resize">
                  <IconButton size="small" onClick={() => onResize(lv)}>
                    <EditIcon fontSize="small" />
                  </IconButton>
                </Tooltip>
                <Tooltip title="Delete">
                  <IconButton
                    size="small"
                    color="error"
                    onClick={() => onDelete(lv)}
                  >
                    <DeleteIcon fontSize="small" />
                  </IconButton>
                </Tooltip>
              </TableCell>
            </TableRow>
          ))
        )}
      </TableBody>
    </Table>
  </TableContainer>
);

const LVMManagement: React.FC<LVMManagementProps> = ({
  onMountCreateHandler,
}) => {
  const [expanded, setExpanded] = useState<string | false>("lvs");
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [resizeDialogOpen, setResizeDialogOpen] = useState(false);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [selectedLV, setSelectedLV] = useState<LogicalVolume | null>(null);

  const {
    data: pvs = [],
    isPending: pvsLoading,
    refetch: refetchPVs,
  } = linuxio.storage.list_pvs.useQuery({ refetchInterval: 10000 });

  const {
    data: vgs = [],
    isPending: vgsLoading,
    refetch: refetchVGs,
  } = linuxio.storage.list_vgs.useQuery({ refetchInterval: 10000 });

  const {
    data: lvs = [],
    isPending: lvsLoading,
    refetch: refetchLVs,
  } = linuxio.storage.list_lvs.useQuery({ refetchInterval: 10000 });

  const handleCreateLV = useCallback(() => {
    setCreateDialogOpen(true);
  }, []);

  useEffect(() => {
    if (onMountCreateHandler) {
      onMountCreateHandler(handleCreateLV);
    }
  }, [onMountCreateHandler, handleCreateLV]);

  const handleAccordionChange =
    (panel: string) => (_: React.SyntheticEvent, isExpanded: boolean) => {
      setExpanded(isExpanded ? panel : false);
    };

  const handleRefreshAll = () => {
    refetchPVs();
    refetchVGs();
    refetchLVs();
  };

  const handleResize = (lv: LogicalVolume) => {
    setSelectedLV(lv);
    setResizeDialogOpen(true);
  };

  const handleDelete = (lv: LogicalVolume) => {
    setSelectedLV(lv);
    setDeleteDialogOpen(true);
  };

  if (pvsLoading || vgsLoading || lvsLoading) {
    return <ComponentLoader />;
  }

  const pvsList = Array.isArray(pvs) ? pvs : [];
  const vgsList = Array.isArray(vgs) ? vgs : [];
  const lvsList = Array.isArray(lvs) ? lvs : [];

  return (
    <Box>
      <Accordion
        expanded={expanded === "lvs"}
        onChange={handleAccordionChange("lvs")}
      >
        <AccordionSummary expandIcon={<ExpandMoreIcon />}>
          <Typography fontWeight={600}>
            Logical Volumes ({lvsList.length})
          </Typography>
        </AccordionSummary>
        <AccordionDetails>
          <LVTable
            data={lvsList}
            onResize={handleResize}
            onDelete={handleDelete}
          />
        </AccordionDetails>
      </Accordion>

      <Accordion
        expanded={expanded === "vgs"}
        onChange={handleAccordionChange("vgs")}
      >
        <AccordionSummary expandIcon={<ExpandMoreIcon />}>
          <Typography fontWeight={600}>
            Volume Groups ({vgsList.length})
          </Typography>
        </AccordionSummary>
        <AccordionDetails>
          <VGTable data={vgsList} />
        </AccordionDetails>
      </Accordion>

      <Accordion
        expanded={expanded === "pvs"}
        onChange={handleAccordionChange("pvs")}
      >
        <AccordionSummary expandIcon={<ExpandMoreIcon />}>
          <Typography fontWeight={600}>
            Physical Volumes ({pvsList.length})
          </Typography>
        </AccordionSummary>
        <AccordionDetails>
          <PVTable data={pvsList} />
        </AccordionDetails>
      </Accordion>

      <CreateLVDialog
        open={createDialogOpen}
        onClose={() => setCreateDialogOpen(false)}
        volumeGroups={vgsList}
        onSuccess={handleRefreshAll}
      />

      <ResizeLVDialog
        open={resizeDialogOpen}
        onClose={() => setResizeDialogOpen(false)}
        lv={selectedLV}
        onSuccess={handleRefreshAll}
      />

      <DeleteLVDialog
        open={deleteDialogOpen}
        onClose={() => setDeleteDialogOpen(false)}
        lv={selectedLV}
        onSuccess={handleRefreshAll}
      />
    </Box>
  );
};

export default LVMManagement;
