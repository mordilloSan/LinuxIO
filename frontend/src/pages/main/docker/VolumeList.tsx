import { Icon } from "@iconify/react";
import { useQueryClient } from "@tanstack/react-query";
import React, { useCallback, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";

import { linuxio } from "@/api";
import VolumeCard from "@/components/cards/VolumeCard";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import UnifiedCollapsibleTable, {
  UnifiedTableColumn,
} from "@/components/tables/UnifiedCollapsibleTable";
import AppButton from "@/components/ui/AppButton";
import AppCheckbox from "@/components/ui/AppCheckbox";
import Chip from "@/components/ui/AppChip";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppGrid from "@/components/ui/AppGrid";
import AppSearchField from "@/components/ui/AppSearchField";
import { AppTableCell } from "@/components/ui/AppTable";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";
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
  const theme = useAppTheme();
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
    <GeneralDialog open={open} onClose={handleClose} maxWidth="sm" fullWidth>
      <AppDialogTitle>
        Delete Volume{volumeNames.length > 1 ? "s" : ""}
      </AppDialogTitle>
      <AppDialogContent>
        <AppDialogContentText>
          Are you sure you want to delete the following volume
          {volumeNames.length > 1 ? "s" : ""}?
        </AppDialogContentText>
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
              style={{
                marginRight: 4,
                marginBottom: 4,
              }}
            />
          ))}
        </div>
        <AppDialogContentText
          style={{
            marginTop: 8,
            color: "var(--mui-palette-warning-main)",
          }}
        >
          This action cannot be undone. Volumes in use by containers cannot be
          deleted.
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
          disabled={isDeleting}
        >
          {isDeleting ? "Deleting..." : "Delete"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};
const VolumeList: React.FC<VolumeListProps> = ({
  onMountCreateHandler,
  viewMode = "table",
}) => {
  const theme = useAppTheme();
  const { data: rawVolumes } = linuxio.docker.list_volumes.useQuery({
    refetchInterval: 10000,
  });
  const [search, setSearch] = useState("");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);

  // Ensure volumes is an array (handle null/undefined from API)
  const volumesList = Array.isArray(rawVolumes) ? rawVolumes : [];

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
      className: "app-table-hide-below-sm",
    },
    {
      field: "mountpoint",
      headerName: "Mountpoint",
      align: "left",
      className: "app-table-hide-below-md",
    },
    {
      field: "scope",
      headerName: "Scope",
      align: "left",
      width: "100px",
      className: "app-table-hide-below-sm",
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
        <AppSearchField
          placeholder="Search volumes…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          style={{ width: 320 }}
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
          <AppGrid container spacing={2}>
            {filtered.map((volume) => (
              <AppGrid
                key={volume.Name}
                size={{
                  xs: 12,
                  sm: 6,
                  md: 4,
                  lg: 3,
                }}
              >
                <VolumeCard
                  volume={volume}
                  selected={effectiveSelected.has(volume.Name)}
                  onSelect={(checked) => handleSelectOne(volume.Name, checked)}
                />
              </AppGrid>
            ))}
          </AppGrid>
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
            <AppCheckbox
              size="small"
              checked={effectiveSelected.has(volume.Name)}
              onChange={(e) => handleSelectOne(volume.Name, e.target.checked)}
              onClick={(e) => e.stopPropagation()}
            />
          )}
          renderHeaderFirstCell={() => (
            <AppCheckbox
              size="small"
              checked={allSelected}
              indeterminate={someSelected}
              onChange={(e) => handleSelectAll(e.target.checked)}
            />
          )}
          renderMainRow={(volume) => (
            <>
              <AppTableCell>
                <AppTypography
                  variant="body2"
                  fontWeight={500}
                  style={responsiveTextStyles}
                >
                  {volume.Name}
                </AppTypography>
              </AppTableCell>
              <AppTableCell className="app-table-hide-below-sm">
                <Chip
                  label={volume.Driver}
                  size="small"
                  variant="soft"
                  style={{
                    fontSize: "0.75rem",
                  }}
                />
              </AppTableCell>
              <AppTableCell className="app-table-hide-below-md">
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
              </AppTableCell>
              <AppTableCell className="app-table-hide-below-sm">
                <AppTypography variant="body2" style={responsiveTextStyles}>
                  {volume.Scope || "local"}
                </AppTypography>
              </AppTableCell>
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
