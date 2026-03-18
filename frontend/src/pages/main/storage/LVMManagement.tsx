import { Icon } from "@iconify/react";
import {
  Accordion,
  AccordionDetails,
  AccordionSummary,
  FormControl,
  InputLabel,
  MenuItem,
  Select,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  TextField,
} from "@mui/material";
import { useTheme } from "@mui/material/styles";
import { useQueryClient } from "@tanstack/react-query";
import React, { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";

import {
  linuxio,
  type LogicalVolume,
  type PhysicalVolume,
  type VolumeGroup,
} from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppAlert from "@/components/ui/AppAlert";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppIconButton from "@/components/ui/AppIconButton";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { formatFileSize } from "@/utils/formaters";
import { getMutationErrorMessage } from "@/utils/mutations";
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
  const theme = useTheme();
  const queryClient = useQueryClient();
  const [vgName, setVgName] = useState("");
  const [lvName, setLvName] = useState("");
  const [size, setSize] = useState("");
  const [validationError, setValidationError] = useState<string | null>(null);
  const { mutate: createLV, isPending: isCreating } =
    linuxio.storage.create_lv.useMutation({
      onSuccess: () => {
        toast.success(`Logical volume ${lvName} created successfully`);
        queryClient.invalidateQueries({
          queryKey: linuxio.storage.list_lvs.queryKey(),
        });
        queryClient.invalidateQueries({
          queryKey: linuxio.storage.list_vgs.queryKey(),
        });
        onSuccess();
        handleClose();
      },
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to create logical volume"),
        );
      },
    });
  const handleCreate = () => {
    if (!vgName || !lvName || !size) {
      setValidationError("All fields are required");
      return;
    }
    setValidationError(null);
    createLV([vgName, lvName, size]);
  };
  const handleClose = () => {
    setVgName("");
    setLvName("");
    setSize("");
    setValidationError(null);
    onClose();
  };
  const selectedVG = volumeGroups.find((vg) => vg.name === vgName);
  return (
    <GeneralDialog open={open} onClose={handleClose} maxWidth="sm" fullWidth>
      <AppDialogTitle>Create Logical Volume</AppDialogTitle>
      <AppDialogContent>
        <div
          style={{
            display: "flex",
            flexDirection: "column",
            gap: theme.spacing(2),
            marginTop: theme.spacing(1),
          }}
        >
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
            <AppTypography variant="body2" color="text.secondary">
              Available space: {formatFileSize(selectedVG.free)}
            </AppTypography>
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
          {validationError && <AppAlert severity="error">{validationError}</AppAlert>}
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton onClick={handleClose} disabled={isCreating}>
          Cancel
        </AppButton>
        <AppButton
          onClick={handleCreate}
          variant="contained"
          disabled={isCreating}
        >
          {isCreating ? "Creating..." : "Create"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};
const ResizeLVDialog: React.FC<ResizeLVDialogProps> = ({
  open,
  onClose,
  lv,
  onSuccess,
}) => {
  const theme = useTheme();
  const queryClient = useQueryClient();
  // Pre-fill with current size in GB
  const [newSize, setNewSize] = useState(() => {
    if (lv) {
      const sizeGB = Math.round(lv.size / (1024 * 1024 * 1024));
      return `${sizeGB}G`;
    }
    return "";
  });
  const [validationError, setValidationError] = useState<string | null>(null);
  const { mutate: resizeLV, isPending: isResizing } =
    linuxio.storage.resize_lv.useMutation({
      onSuccess: () => {
        toast.success(`Logical volume ${lv?.name} resized successfully`);
        queryClient.invalidateQueries({
          queryKey: linuxio.storage.list_lvs.queryKey(),
        });
        queryClient.invalidateQueries({
          queryKey: linuxio.storage.list_vgs.queryKey(),
        });
        onSuccess();
        handleClose();
      },
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to resize logical volume"),
        );
      },
    });
  const handleResize = () => {
    if (!lv || !newSize) {
      setValidationError("Size is required");
      return;
    }
    setValidationError(null);
    resizeLV([lv.vgName, lv.name, newSize]);
  };
  const handleClose = () => {
    setNewSize("");
    setValidationError(null);
    onClose();
  };
  return (
    <GeneralDialog
      key={lv?.path}
      open={open}
      onClose={handleClose}
      maxWidth="sm"
      fullWidth
    >
      <AppDialogTitle>Resize Logical Volume</AppDialogTitle>
      <AppDialogContent>
        <div
          style={{
            display: "flex",
            flexDirection: "column",
            gap: theme.spacing(2),
            marginTop: theme.spacing(1),
          }}
        >
          {lv && (
            <>
              <AppTypography variant="body2">
                <strong>Volume:</strong> {lv.path}
              </AppTypography>
              <AppTypography variant="body2">
                <strong>Current Size:</strong> {formatFileSize(lv.size)}
              </AppTypography>
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
          {validationError && <AppAlert severity="error">{validationError}</AppAlert>}
        </div>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton onClick={handleClose} disabled={isResizing}>
          Cancel
        </AppButton>
        <AppButton
          onClick={handleResize}
          variant="contained"
          disabled={isResizing}
        >
          {isResizing ? "Resizing..." : "Resize"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};
const DeleteLVDialog: React.FC<DeleteLVDialogProps> = ({
  open,
  onClose,
  lv,
  onSuccess,
}) => {
  const queryClient = useQueryClient();
  const { mutate: deleteLV, isPending: isDeleting } =
    linuxio.storage.delete_lv.useMutation({
      onSuccess: () => {
        toast.success(`Logical volume ${lv?.name} deleted successfully`);
        queryClient.invalidateQueries({
          queryKey: linuxio.storage.list_lvs.queryKey(),
        });
        queryClient.invalidateQueries({
          queryKey: linuxio.storage.list_vgs.queryKey(),
        });
        onSuccess();
        handleClose();
      },
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to delete logical volume"),
        );
      },
    });
  const handleDelete = () => {
    if (!lv) return;
    deleteLV([lv.vgName, lv.name]);
  };
  const handleClose = () => {
    onClose();
  };
  return (
    <GeneralDialog open={open} onClose={handleClose} maxWidth="sm" fullWidth>
      <AppDialogTitle>Delete Logical Volume</AppDialogTitle>
      <AppDialogContent>
        <AppDialogContentText>
          Are you sure you want to delete the logical volume{" "}
          <strong>{lv?.name}</strong>?
        </AppDialogContentText>
        {lv?.mountpoint && (
          <AppAlert
            severity="warning"
            style={{
              marginTop: 8,
            }}
          >
            This volume is currently mounted at <strong>{lv.mountpoint}</strong>
            . Please unmount it first.
          </AppAlert>
        )}
        <AppDialogContentText
          style={{
            marginTop: 8,
            color: "var(--mui-palette-error-main)",
          }}
        >
          This action cannot be undone. All data on this volume will be lost.
        </AppDialogContentText>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton onClick={handleClose} disabled={isDeleting}>
          Cancel
        </AppButton>
        <AppButton
          onClick={handleDelete}
          variant="contained"
          color="error"
          disabled={isDeleting || !!lv?.mountpoint}
        >
          {isDeleting ? "Deleting..." : "Delete"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};
const PVTable: React.FC<{
  data: PhysicalVolume[];
}> = ({ data }) => (
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
              <AppTypography color="text.secondary" align="center">
                No physical volumes found
              </AppTypography>
            </TableCell>
          </TableRow>
        ) : (
          data.map((pv) => (
            <TableRow key={pv.name}>
              <TableCell>
                <AppTypography
                  variant="body2"
                  style={{
                    fontFamily: "monospace",
                  }}
                >
                  {pv.name}
                </AppTypography>
              </TableCell>
              <TableCell>{pv.vgName || "-"}</TableCell>
              <TableCell>{formatFileSize(pv.size)}</TableCell>
              <TableCell>{formatFileSize(pv.free)}</TableCell>
              <TableCell>
                <Chip label={pv.format} size="small" variant="soft" />
              </TableCell>
            </TableRow>
          ))
        )}
      </TableBody>
    </Table>
  </TableContainer>
);
const VGTable: React.FC<{
  data: VolumeGroup[];
}> = ({ data }) => (
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
              <AppTypography color="text.secondary" align="center">
                No volume groups found
              </AppTypography>
            </TableCell>
          </TableRow>
        ) : (
          data.map((vg) => (
            <TableRow key={vg.name}>
              <TableCell>
                <AppTypography variant="body2" fontWeight={600}>
                  {vg.name}
                </AppTypography>
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
              <AppTypography color="text.secondary" align="center">
                No logical volumes found
              </AppTypography>
            </TableCell>
          </TableRow>
        ) : (
          data.map((lv) => (
            <TableRow key={lv.path}>
              <TableCell>
                <AppTypography variant="body2" fontWeight={600}>
                  {lv.name}
                </AppTypography>
                <AppTypography
                  variant="caption"
                  color="text.secondary"
                  style={{
                    fontFamily: "monospace",
                  }}
                >
                  {lv.path}
                </AppTypography>
              </TableCell>
              <TableCell>{lv.vgName}</TableCell>
              <TableCell>{formatFileSize(lv.size)}</TableCell>
              <TableCell>
                {lv.mountpoint ? (
                  <AppTypography
                    variant="body2"
                    style={{
                      fontFamily: "monospace",
                    }}
                  >
                    {lv.mountpoint}
                  </AppTypography>
                ) : (
                  <Chip label="Not mounted" size="small" variant="soft" />
                )}
              </TableCell>
              <TableCell>
                {lv.mountpoint ? (
                  <div
                    style={{
                      width: 100,
                    }}
                  >
                    <AppLinearProgress
                      variant="determinate"
                      value={lv.usedPct}
                      style={{
                        height: 6,
                        borderRadius: 3,
                        marginBottom: 2,
                      }}
                      color={
                        lv.usedPct > 90
                          ? "error"
                          : lv.usedPct > 70
                            ? "warning"
                            : "primary"
                      }
                    />
                    <AppTypography variant="caption">
                      {lv.usedPct.toFixed(1)}%
                    </AppTypography>
                  </div>
                ) : (
                  "-"
                )}
              </TableCell>
              <TableCell align="right">
                <AppTooltip title="Resize">
                  <AppIconButton size="small" onClick={() => onResize(lv)}>
                    <Icon icon="mdi:pencil" width={20} height={20} />
                  </AppIconButton>
                </AppTooltip>
                <AppTooltip title="Delete">
                  <AppIconButton
                    size="small"
                    color="error"
                    onClick={() => onDelete(lv)}
                  >
                    <Icon icon="mdi:delete" width={20} height={20} />
                  </AppIconButton>
                </AppTooltip>
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
  } = linuxio.storage.list_pvs.useQuery({
    refetchInterval: 10000,
  });
  const {
    data: vgs = [],
    isPending: vgsLoading,
    refetch: refetchVGs,
  } = linuxio.storage.list_vgs.useQuery({
    refetchInterval: 10000,
  });
  const {
    data: lvs = [],
    isPending: lvsLoading,
    refetch: refetchLVs,
  } = linuxio.storage.list_lvs.useQuery({
    refetchInterval: 10000,
  });
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
    <>
      <Accordion
        expanded={expanded === "lvs"}
        onChange={handleAccordionChange("lvs")}
      >
        <AccordionSummary
          expandIcon={<Icon icon="mdi:chevron-down" width={24} height={24} />}
        >
          <AppTypography fontWeight={600}>
            Logical Volumes ({lvsList.length})
          </AppTypography>
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
        <AccordionSummary
          expandIcon={<Icon icon="mdi:chevron-down" width={24} height={24} />}
        >
          <AppTypography fontWeight={600}>
            Volume Groups ({vgsList.length})
          </AppTypography>
        </AccordionSummary>
        <AccordionDetails>
          <VGTable data={vgsList} />
        </AccordionDetails>
      </Accordion>

      <Accordion
        expanded={expanded === "pvs"}
        onChange={handleAccordionChange("pvs")}
      >
        <AccordionSummary
          expandIcon={<Icon icon="mdi:chevron-down" width={24} height={24} />}
        >
          <AppTypography fontWeight={600}>
            Physical Volumes ({pvsList.length})
          </AppTypography>
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
    </>
  );
};
export default LVMManagement;
