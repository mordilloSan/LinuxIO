import { Icon } from "@iconify/react";
import { useQueryClient } from "@tanstack/react-query";
import React, { useCallback, useMemo, useState } from "react";

import { linuxio } from "@/api";
import VolumeCard from "@/components/cards/VolumeCard";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppDataTable from "@/components/tables/AppDataTable";
import type { AppDataTableColumnDef } from "@/components/tables/AppDataTable";
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
import AppTypography from "@/components/ui/AppTypography";
import { useRegisterCreateHandler } from "@/hooks/useRegisterCreateHandler";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useAppTheme } from "@/theme";
import {
  longTextStyles,
  responsiveTextStyles,
  wrappableChipStyle,
  wrappableChipLabelStyle,
} from "@/theme/tableStyles";
import { getMutationErrorMessage } from "@/utils/mutations";
interface VolumeListProps {
  onMountCreateHandler?: (handler: () => void) => void;
  viewMode?: "table" | "card";
}
interface DeleteVolumeDialogProps {
  onClose: () => void;
  onSuccess: () => void;
  open: boolean;
  volumeNames: string[];
}
const DeleteVolumeDialog: React.FC<DeleteVolumeDialogProps> = ({
  open,
  onClose,
  volumeNames,
  onSuccess,
}) => {
  const queryClient = useQueryClient();
  const theme = useAppTheme();
  const toast = useScopedToast({ href: "/docker", label: "Open Docker" });
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
      await deleteVolume({ name });
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
    <GeneralDialog fullWidth maxWidth="sm" onClose={handleClose} open={open}>
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
              style={{
                marginRight: 4,
                marginBottom: 4,
              }}
              variant="soft"
            />
          ))}
        </div>
        <AppDialogContentText
          style={{
            marginTop: 8,
            color: "var(--app-palette-warning-main)",
          }}
        >
          This action cannot be undone. Volumes in use by containers cannot be
          deleted.
        </AppDialogContentText>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={isDeleting} onClick={handleClose}>
          Cancel
        </AppButton>
        <AppButton
          color="error"
          disabled={isDeleting}
          onClick={handleDelete}
          variant="contained"
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

  useRegisterCreateHandler(onMountCreateHandler, handleCreateVolume);
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
  const columns: AppDataTableColumnDef<(typeof filtered)[number]>[] = [
    {
      id: "select",
      header: () => (
        <AppCheckbox
          checked={allSelected}
          indeterminate={someSelected}
          onChange={(e) => handleSelectAll(e.target.checked)}
          size="small"
        />
      ),
      enableSorting: false,
      cell: ({ row }) => (
        <AppCheckbox
          checked={effectiveSelected.has(row.original.Name)}
          onChange={(e) => handleSelectOne(row.original.Name, e.target.checked)}
          onClick={(e) => e.stopPropagation()}
          size="small"
        />
      ),
      meta: {
        align: "center",
        className: "app-vdt__cell--select",
        getCellRenderKey: (row) => {
          const volume = row as (typeof filtered)[number];
          return [volume.Name, effectiveSelected.has(volume.Name)];
        },
        width: "40px",
      },
    },
    {
      accessorKey: "Name",
      header: "Volume Name",
      cell: ({ row }) => (
        <AppTypography
          fontWeight={500}
          style={responsiveTextStyles}
          variant="body2"
        >
          {row.original.Name}
        </AppTypography>
      ),
      meta: { align: "left" },
    },
    {
      accessorKey: "Driver",
      header: "Driver",
      cell: ({ row }) => (
        <Chip
          label={row.original.Driver}
          size="small"
          style={{ fontSize: "0.75rem" }}
          variant="soft"
        />
      ),
      meta: {
        align: "left",
        hideBelow: "sm",
        width: "120px",
      },
    },
    {
      accessorKey: "Mountpoint",
      header: "Mountpoint",
      cell: ({ row }) => (
        <AppTypography
          style={{
            fontFamily: "monospace",
            fontSize: "0.85rem",
            ...longTextStyles,
          }}
          variant="body2"
        >
          {row.original.Mountpoint || "-"}
        </AppTypography>
      ),
      meta: {
        align: "left",
        hideBelow: "md",
      },
    },
    {
      accessorKey: "Scope",
      header: "Scope",
      cell: ({ row }) => (
        <AppTypography style={responsiveTextStyles} variant="body2">
          {row.original.Scope || "local"}
        </AppTypography>
      ),
      meta: {
        align: "left",
        hideBelow: "sm",
        width: "100px",
      },
    },
  ];
  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        height: "100%",
        minHeight: 0,
      }}
    >
      <div
        style={{
          display: "flex",
          flexShrink: 0,
          alignItems: "center",
          gap: theme.spacing(2),
          flexWrap: "wrap",
          marginBottom: theme.spacing(2),
        }}
      >
        <AppSearchField
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Search volumes…"
          style={{ width: 320 }}
          value={search}
        />
        <AppTypography fontWeight={700}>{filtered.length} shown</AppTypography>
        {effectiveSelected.size > 0 && (
          <AppButton
            color="error"
            onClick={() => setDeleteDialogOpen(true)}
            size="small"
            startIcon={<Icon height={20} icon="mdi:delete" width={20} />}
            variant="contained"
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
                  onSelect={(checked) => handleSelectOne(volume.Name, checked)}
                  selected={effectiveSelected.has(volume.Name)}
                  volume={volume}
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
            <AppTypography color="text.secondary" variant="body2">
              No volumes found.
            </AppTypography>
          </div>
        )
      ) : (
        <AppDataTable
          ariaLabel="Docker volumes"
          columns={columns}
          data={filtered}
          emptyMessage="No volumes found."
          fillAvailable
          getRowId={(volume) => volume.Name}
          renderExpandedContent={({ original: volume }) => (
            <div className="expand-panel">
              <div>
                <AppTypography gutterBottom variant="subtitle2">
                  <b>Full Mountpoint:</b>
                </AppTypography>
                <AppTypography
                  className="expand-panel__mono"
                  style={longTextStyles}
                  variant="body2"
                >
                  {volume.Mountpoint || "-"}
                </AppTypography>
              </div>

              {volume.CreatedAt && (
                <div>
                  <AppTypography gutterBottom variant="subtitle2">
                    <b>Created:</b>
                  </AppTypography>
                  <AppTypography className="expand-panel__mono" variant="body2">
                    {new Date(volume.CreatedAt).toLocaleString()}
                  </AppTypography>
                </div>
              )}

              <div>
                <AppTypography gutterBottom variant="subtitle2">
                  <b>Labels:</b>
                </AppTypography>
                <div className="expand-panel__chips">
                  {volume.Labels && Object.keys(volume.Labels).length > 0 ? (
                    Object.entries(volume.Labels).map(([key, val]) => (
                      <Chip
                        key={key}
                        label={`${key}: ${val}`}
                        size="small"
                        style={wrappableChipStyle}
                        labelStyle={wrappableChipLabelStyle}
                        variant="soft"
                      />
                    ))
                  ) : (
                    <AppTypography color="text.secondary" variant="body2">
                      (no labels)
                    </AppTypography>
                  )}
                </div>
              </div>

              <div>
                <AppTypography gutterBottom variant="subtitle2">
                  <b>Options:</b>
                </AppTypography>
                <div className="expand-panel__chips">
                  {volume.Options && Object.keys(volume.Options).length > 0 ? (
                    Object.entries(volume.Options).map(([key, val]) => (
                      <Chip
                        key={key}
                        label={`${key}: ${val}`}
                        size="small"
                        style={wrappableChipStyle}
                        labelStyle={wrappableChipLabelStyle}
                        variant="soft"
                      />
                    ))
                  ) : (
                    <AppTypography color="text.secondary" variant="body2">
                      (no options)
                    </AppTypography>
                  )}
                </div>
              </div>
            </div>
          )}
        />
      )}

      <DeleteVolumeDialog
        onClose={() => setDeleteDialogOpen(false)}
        onSuccess={handleDeleteSuccess}
        open={deleteDialogOpen}
        volumeNames={selectedVolumes.map((v) => v.Name)}
      />
    </div>
  );
};
export default VolumeList;
