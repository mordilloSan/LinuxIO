import { Icon } from "@iconify/react";
import {
  Grid,
  TableCell,
  TextField,
  Checkbox,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  DialogContentText,
  useTheme,
} from "@mui/material";
import { useQueryClient } from "@tanstack/react-query";
import React, { useCallback, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";

import { linuxio } from "@/api";
import FrostedCard from "@/components/cards/RootCard";
import UnifiedCollapsibleTable, {
  UnifiedTableColumn,
} from "@/components/tables/UnifiedCollapsibleTable";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";
import {
  responsiveTextStyles,
  longTextStyles,
  wrappableChipStyles,
} from "@/theme/tableStyles";
import { getMutationErrorMessage } from "@/utils/mutations";
interface VolumeListProps {
  onMountCreateHandler?: (handler: () => void) => void;
  viewMode?: "table" | "card";
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
  const theme = useTheme();
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
      queryKey: linuxio.docker.list_volumes.queryKey(),
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
        <div
          style={{
            display: "flex",
            flexWrap: "wrap",
            marginTop: theme.spacing(2),
            marginBottom: theme.spacing(1),
          }}
        >
          {volumeNames.map((name) => (
            <Chip
              key={name}
              label={name}
              size="small"
              variant="soft"
              sx={{
                mr: 1,
                mb: 1,
              }}
            />
          ))}
        </div>
        <DialogContentText
          sx={{
            mt: 2,
            color: "warning.main",
          }}
        >
          This action cannot be undone. Volumes in use by containers cannot be
          deleted.
        </DialogContentText>
      </DialogContent>
      <DialogActions>
        <AppButton onClick={handleClose} disabled={isDeleting}>
          Cancel
        </AppButton>
        <AppButton
          onClick={handleDelete}
          variant="contained"
          color="error"
          disabled={isDeleting}
        >
          {isDeleting ? "Deleting..." : "Delete"}
        </AppButton>
      </DialogActions>
    </Dialog>
  );
};
const VolumeList: React.FC<VolumeListProps> = ({
  onMountCreateHandler,
  viewMode = "table",
}) => {
  const theme = useTheme();
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
    {
      field: "name",
      headerName: "Volume Name",
      align: "left",
    },
    {
      field: "driver",
      headerName: "Driver",
      align: "left",
      width: "120px",
      sx: {
        display: {
          xs: "none",
          sm: "table-cell",
        },
      },
    },
    {
      field: "mountpoint",
      headerName: "Mountpoint",
      align: "left",
      sx: {
        display: {
          xs: "none",
          md: "table-cell",
        },
      },
    },
    {
      field: "scope",
      headerName: "Scope",
      align: "left",
      width: "100px",
      sx: {
        display: {
          xs: "none",
          sm: "table-cell",
        },
      },
    },
  ];
  return (
    <div>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: theme.spacing(2),
          flexWrap: "wrap",
          marginBottom: theme.spacing(2),
        }}
      >
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
        <AppTypography fontWeight={700}>{filtered.length} shown</AppTypography>
        {effectiveSelected.size > 0 && (
          <AppButton
            variant="contained"
            color="error"
            size="small"
            startIcon={<Icon icon="mdi:delete" width={20} height={20} />}
            onClick={() => setDeleteDialogOpen(true)}
          >
            Delete ({effectiveSelected.size})
          </AppButton>
        )}
      </div>
      {viewMode === "card" ? (
        filtered.length > 0 ? (
          <Grid container spacing={2}>
            {filtered.map((volume) => (
              <Grid
                key={volume.Name}
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
                  <div
                    style={{
                      display: "flex",
                      alignItems: "center",
                      justifyContent: "space-between",
                      gap: theme.spacing(1),
                      marginBottom: theme.spacing(1),
                    }}
                  >
                    <div
                      style={{
                        display: "flex",
                        alignItems: "center",
                        gap: theme.spacing(1),
                      }}
                    >
                      <Checkbox
                        size="small"
                        checked={effectiveSelected.has(volume.Name)}
                        onChange={(e) =>
                          handleSelectOne(volume.Name, e.target.checked)
                        }
                      />
                      <AppTypography variant="body2" fontWeight={700} noWrap>
                        {volume.Name}
                      </AppTypography>
                    </div>
                    <Chip
                      label={volume.Driver}
                      size="small"
                      variant="soft"
                      sx={{
                        fontSize: "0.75rem",
                      }}
                    />
                  </div>

                  <AppTypography
                    variant="body2"
                    style={{
                      marginBottom: 4,
                      fontFamily: "monospace",
                      fontSize: "0.8rem",
                      ...longTextStyles,
                    }}
                  >
                    {volume.Mountpoint || "-"}
                  </AppTypography>

                  <div
                    style={{
                      display: "flex",
                      flexWrap: "wrap",
                      gap: theme.spacing(0.75),
                    }}
                  >
                    <Chip
                      label={`Scope: ${volume.Scope || "local"}`}
                      size="small"
                      variant="soft"
                    />
                    {volume.CreatedAt && (
                      <Chip
                        label={new Date(volume.CreatedAt).toLocaleDateString()}
                        size="small"
                        variant="soft"
                      />
                    )}
                  </div>
                </FrostedCard>
              </Grid>
            ))}
          </Grid>
        ) : (
          <div
            style={{
              textAlign: "center",
              paddingTop: theme.spacing(4),
              paddingBottom: theme.spacing(4),
            }}
          >
            <AppTypography variant="body2" color="text.secondary">
              No volumes found.
            </AppTypography>
          </div>
        )
      ) : (
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
                <AppTypography
                  variant="body2"
                  fontWeight={500}
                  style={responsiveTextStyles}
                >
                  {volume.Name}
                </AppTypography>
              </TableCell>
              <TableCell
                sx={{
                  display: {
                    xs: "none",
                    sm: "table-cell",
                  },
                }}
              >
                <Chip
                  label={volume.Driver}
                  size="small"
                  variant="soft"
                  sx={{
                    fontSize: "0.75rem",
                  }}
                />
              </TableCell>
              <TableCell
                sx={{
                  display: {
                    xs: "none",
                    md: "table-cell",
                  },
                }}
              >
                <AppTypography
                  variant="body2"
                  style={{
                    fontFamily: "monospace",
                    fontSize: "0.85rem",
                    ...longTextStyles,
                  }}
                >
                  {volume.Mountpoint || "-"}
                </AppTypography>
              </TableCell>
              <TableCell
                sx={{
                  display: {
                    xs: "none",
                    sm: "table-cell",
                  },
                }}
              >
                <AppTypography variant="body2" style={responsiveTextStyles}>
                  {volume.Scope || "local"}
                </AppTypography>
              </TableCell>
            </>
          )}
          renderExpandedContent={(volume) => (
            <>
              <AppTypography variant="subtitle2" gutterBottom>
                <b>Full Mountpoint:</b>
              </AppTypography>
              <AppTypography
                variant="body2"
                style={{
                  fontFamily: "monospace",
                  fontSize: "0.85rem",
                  marginBottom: 8,
                  ...longTextStyles,
                }}
              >
                {volume.Mountpoint || "-"}
              </AppTypography>

              {volume.CreatedAt && (
                <>
                  <AppTypography variant="subtitle2" gutterBottom>
                    <b>Created:</b>
                  </AppTypography>
                  <AppTypography
                    variant="body2"
                    style={{
                      marginBottom: 8,
                      fontSize: "0.85rem",
                    }}
                  >
                    {new Date(volume.CreatedAt).toLocaleString()}
                  </AppTypography>
                </>
              )}

              <AppTypography variant="subtitle2" gutterBottom>
                <b>Labels:</b>
              </AppTypography>
              <div
                style={{
                  display: "flex",
                  flexWrap: "wrap",
                  marginBottom: theme.spacing(2),
                }}
              >
                {volume.Labels && Object.keys(volume.Labels).length > 0 ? (
                  Object.entries(volume.Labels).map(([key, val]) => (
                    <Chip
                      key={key}
                      label={`${key}: ${val}`}
                      size="small"
                      variant="soft"
                      sx={{
                        mr: 1,
                        mb: 1,
                        ...wrappableChipStyles,
                      }}
                    />
                  ))
                ) : (
                  <AppTypography variant="body2" color="text.secondary">
                    (no labels)
                  </AppTypography>
                )}
              </div>

              <AppTypography variant="subtitle2" gutterBottom>
                <b>Options:</b>
              </AppTypography>
              <div>
                {volume.Options && Object.keys(volume.Options).length > 0 ? (
                  Object.entries(volume.Options).map(([key, val]) => (
                    <Chip
                      key={key}
                      label={`${key}: ${val}`}
                      size="small"
                      variant="soft"
                      sx={{
                        mr: 1,
                        mb: 1,
                        ...wrappableChipStyles,
                      }}
                    />
                  ))
                ) : (
                  <AppTypography variant="body2" color="text.secondary">
                    (no options)
                  </AppTypography>
                )}
              </div>
            </>
          )}
          emptyMessage="No volumes found."
        />
      )}

      <DeleteVolumeDialog
        open={deleteDialogOpen}
        onClose={() => setDeleteDialogOpen(false)}
        volumeNames={selectedVolumes.map((v) => v.Name)}
        onSuccess={handleDeleteSuccess}
      />
    </div>
  );
};
export default VolumeList;
