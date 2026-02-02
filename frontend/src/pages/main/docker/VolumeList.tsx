import DeleteIcon from "@mui/icons-material/Delete";
import {
  Box,
  TableCell,
  TextField,
  Chip,
  Typography,
  Checkbox,
  Button,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  DialogContentText,
} from "@mui/material";
import { useQueryClient } from "@tanstack/react-query";
import React, { useCallback, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";

import linuxio from "@/api/react-query";
import UnifiedCollapsibleTable, {
  UnifiedTableColumn,
} from "@/components/tables/UnifiedCollapsibleTable";
import {
  responsiveTextStyles,
  longTextStyles,
  wrappableChipStyles,
} from "@/theme/tableStyles";
import { getMutationErrorMessage } from "@/utils/mutations";

interface VolumeListProps {
  onMountCreateHandler?: (handler: () => void) => void;
}

interface DeleteVolumeDialogProps {
  open: boolean;
  onClose: () => void;
  volumeNames: string[];
  onSuccess: () => void;
}

const DeleteVolumeDialog: React.FC<DeleteVolumeDialogProps> = ({
  open,
  onClose,
  volumeNames,
  onSuccess,
}) => {
  const queryClient = useQueryClient();

  const { mutateAsync: deleteVolume, isPending: isDeleting } =
    linuxio.docker.delete_volume.useMutation({
      onError: (error: Error) => {
        toast.error(
          getMutationErrorMessage(error, "Failed to delete volume(s)"),
        );
      },
    });

  const handleDelete = async () => {
    // Delete volumes sequentially
    for (const name of volumeNames) {
      await deleteVolume([name]);
    }
    const successMessage =
      volumeNames.length === 1
        ? `Volume "${volumeNames[0]}" deleted successfully`
        : `${volumeNames.length} volumes deleted successfully`;
    toast.success(successMessage);
    queryClient.invalidateQueries({
      queryKey: ["linuxio", "docker", "list_volumes"],
    });
    onSuccess();
    handleClose();
  };

  const handleClose = () => {
    onClose();
  };

  return (
    <Dialog open={open} onClose={handleClose} maxWidth="sm" fullWidth>
      <DialogTitle>
        Delete Volume{volumeNames.length > 1 ? "s" : ""}
      </DialogTitle>
      <DialogContent>
        <DialogContentText>
          Are you sure you want to delete the following volume
          {volumeNames.length > 1 ? "s" : ""}?
        </DialogContentText>
        <Box sx={{ mt: 2, mb: 1 }}>
          {volumeNames.map((name) => (
            <Chip key={name} label={name} size="small" sx={{ mr: 1, mb: 1 }} />
          ))}
        </Box>
        <DialogContentText sx={{ mt: 2, color: "warning.main" }}>
          This action cannot be undone. Volumes in use by containers cannot be
          deleted.
        </DialogContentText>
      </DialogContent>
      <DialogActions>
        <Button onClick={handleClose} disabled={isDeleting}>
          Cancel
        </Button>
        <Button
          onClick={handleDelete}
          variant="contained"
          color="error"
          disabled={isDeleting}
        >
          {isDeleting ? "Deleting..." : "Delete"}
        </Button>
      </DialogActions>
    </Dialog>
  );
};

const VolumeList: React.FC<VolumeListProps> = ({ onMountCreateHandler }) => {
  const { data: volumes = [] } = linuxio.docker.list_volumes.useQuery({
    refetchInterval: 10000,
  });

  const [search, setSearch] = useState("");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);

  // Ensure volumes is an array (handle null/undefined from API)
  const volumesList = Array.isArray(volumes) ? volumes : [];

  // Create volume handler
  const handleCreateVolume = useCallback(() => {
    // TODO: Open volume creation dialog
    console.log("Create volume clicked");
  }, []);

  // Mount handler to parent
  useEffect(() => {
    if (onMountCreateHandler) {
      onMountCreateHandler(handleCreateVolume);
    }
  }, [onMountCreateHandler, handleCreateVolume]);

  const filtered = volumesList.filter(
    (vol) =>
      vol.Name.toLowerCase().includes(search.toLowerCase()) ||
      vol.Driver.toLowerCase().includes(search.toLowerCase()) ||
      vol.Mountpoint?.toLowerCase().includes(search.toLowerCase()),
  );

  // Compute effective selection - only include items that are in the filtered list
  const effectiveSelected = useMemo(() => {
    const filteredNames = new Set(filtered.map((v) => v.Name));
    const result = new Set<string>();
    selected.forEach((name) => {
      if (filteredNames.has(name)) {
        result.add(name);
      }
    });
    return result;
  }, [selected, filtered]);

  const handleSelectAll = (checked: boolean) => {
    if (checked) {
      setSelected(new Set(filtered.map((v) => v.Name)));
    } else {
      setSelected(new Set());
    }
  };

  const handleSelectOne = (name: string, checked: boolean) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (checked) {
        next.add(name);
      } else {
        next.delete(name);
      }
      return next;
    });
  };

  const handleDeleteSuccess = () => {
    setSelected(new Set());
  };

  const selectedVolumes = filtered.filter((v) => effectiveSelected.has(v.Name));
  const allSelected =
    filtered.length > 0 && effectiveSelected.size === filtered.length;
  const someSelected =
    effectiveSelected.size > 0 && effectiveSelected.size < filtered.length;

  const columns: UnifiedTableColumn[] = [
    { field: "name", headerName: "Volume Name", align: "left" },
    {
      field: "driver",
      headerName: "Driver",
      align: "left",
      width: "120px",
      sx: { display: { xs: "none", sm: "table-cell" } },
    },
    {
      field: "mountpoint",
      headerName: "Mountpoint",
      align: "left",
      sx: { display: { xs: "none", md: "table-cell" } },
    },
    {
      field: "scope",
      headerName: "Scope",
      align: "left",
      width: "100px",
      sx: { display: { xs: "none", sm: "table-cell" } },
    },
  ];

  return (
    <Box>
      <Box mb={2} display="flex" alignItems="center" gap={2} flexWrap="wrap">
        <TextField
          variant="outlined"
          size="small"
          placeholder="Search volumes…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          sx={{
            width: 320,
            "@media (max-width: 600px)": {
              width: "100%",
            },
          }}
        />
        <Box fontWeight="bold">{filtered.length} shown</Box>
        {effectiveSelected.size > 0 && (
          <Button
            variant="contained"
            color="error"
            size="small"
            startIcon={<DeleteIcon />}
            onClick={() => setDeleteDialogOpen(true)}
          >
            Delete ({effectiveSelected.size})
          </Button>
        )}
      </Box>
      <UnifiedCollapsibleTable
        data={filtered}
        columns={columns}
        getRowKey={(volume) => volume.Name}
        renderFirstCell={(volume) => (
          <Checkbox
            size="small"
            checked={effectiveSelected.has(volume.Name)}
            onChange={(e) => handleSelectOne(volume.Name, e.target.checked)}
            onClick={(e) => e.stopPropagation()}
          />
        )}
        renderHeaderFirstCell={() => (
          <Checkbox
            size="small"
            checked={allSelected}
            indeterminate={someSelected}
            onChange={(e) => handleSelectAll(e.target.checked)}
          />
        )}
        renderMainRow={(volume) => (
          <>
            <TableCell>
              <Typography
                variant="body2"
                fontWeight="medium"
                sx={responsiveTextStyles}
              >
                {volume.Name}
              </Typography>
            </TableCell>
            <TableCell sx={{ display: { xs: "none", sm: "table-cell" } }}>
              <Chip
                label={volume.Driver}
                size="small"
                sx={{ fontSize: "0.75rem" }}
              />
            </TableCell>
            <TableCell sx={{ display: { xs: "none", md: "table-cell" } }}>
              <Typography
                variant="body2"
                sx={{
                  fontFamily: "monospace",
                  fontSize: "0.85rem",
                  ...longTextStyles,
                }}
              >
                {volume.Mountpoint || "-"}
              </Typography>
            </TableCell>
            <TableCell sx={{ display: { xs: "none", sm: "table-cell" } }}>
              <Typography variant="body2" sx={responsiveTextStyles}>
                {volume.Scope || "local"}
              </Typography>
            </TableCell>
          </>
        )}
        renderExpandedContent={(volume) => (
          <>
            <Typography variant="subtitle2" gutterBottom>
              <b>Full Mountpoint:</b>
            </Typography>
            <Typography
              variant="body2"
              sx={{
                fontFamily: "monospace",
                fontSize: "0.85rem",
                mb: 2,
                ...longTextStyles,
              }}
            >
              {volume.Mountpoint || "-"}
            </Typography>

            {volume.CreatedAt && (
              <>
                <Typography variant="subtitle2" gutterBottom>
                  <b>Created:</b>
                </Typography>
                <Typography variant="body2" sx={{ mb: 2, fontSize: "0.85rem" }}>
                  {new Date(volume.CreatedAt).toLocaleString()}
                </Typography>
              </>
            )}

            <Typography variant="subtitle2" gutterBottom>
              <b>Labels:</b>
            </Typography>
            <Box sx={{ mb: 2, display: "flex", flexWrap: "wrap" }}>
              {volume.Labels && Object.keys(volume.Labels).length > 0 ? (
                Object.entries(volume.Labels).map(([key, val]) => (
                  <Chip
                    key={key}
                    label={`${key}: ${val}`}
                    size="small"
                    sx={{ mr: 1, mb: 1, ...wrappableChipStyles }}
                  />
                ))
              ) : (
                <Typography variant="body2" color="text.secondary">
                  (no labels)
                </Typography>
              )}
            </Box>

            <Typography variant="subtitle2" gutterBottom>
              <b>Options:</b>
            </Typography>
            <Box>
              {volume.Options && Object.keys(volume.Options).length > 0 ? (
                Object.entries(volume.Options).map(([key, val]) => (
                  <Chip
                    key={key}
                    label={`${key}: ${val}`}
                    size="small"
                    sx={{ mr: 1, mb: 1, ...wrappableChipStyles }}
                  />
                ))
              ) : (
                <Typography variant="body2" color="text.secondary">
                  (no options)
                </Typography>
              )}
            </Box>
          </>
        )}
        emptyMessage="No volumes found."
      />

      <DeleteVolumeDialog
        open={deleteDialogOpen}
        onClose={() => setDeleteDialogOpen(false)}
        volumeNames={selectedVolumes.map((v) => v.Name)}
        onSuccess={handleDeleteSuccess}
      />
    </Box>
  );
};

export default VolumeList;
