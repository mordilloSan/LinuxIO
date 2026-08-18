import { useSuspenseQuery } from "@tanstack/react-query";
import { getRouteApi } from "@tanstack/react-router";
import { useCallback, useEffect, useMemo, useState } from "react";

import { linuxio, useCallMutation } from "@/api";
import VolumeCard from "@/components/cards/VolumeCard";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import DockerResourceDetailsLayout from "@/components/docker/DockerResourceDetailsLayout";
import ReorderableCardGrid from "@/components/reorder/ReorderableCardGrid";
import { RoutedTabSearch } from "@/components/tabbar";
import AppVirtualDataTable from "@/components/tables/AppVirtualDataTable";
import type { AppVirtualDataTableColumnDef } from "@/components/tables/AppVirtualDataTable";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
  OVERLAY_ROOT_SELECTOR,
} from "@/components/ui/AppDialog";
import AppHeaderSearch from "@/components/ui/AppHeaderSearch";
import AppTypography from "@/components/ui/AppTypography";
import { useRegisterCreateHandler } from "@/hooks/useRegisterCreateHandler";
import { useReorderableSurface } from "@/hooks/useReorderableSurface";
import { useReorderableTableDnd } from "@/hooks/useReorderableTableDnd";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useAppTheme } from "@/theme";
import { CARD_GRID_SIZE_STANDARD } from "@/theme/constants";
import {
  longTextStyles,
  responsiveTextStyles,
  wrappableChipStyle,
  wrappableChipLabelStyle,
} from "@/theme/tableStyles";
import { formatFileSize } from "@/utils/formaters";

const dockerRouteApi = getRouteApi("/_authenticated/docker/volumes");

const formatVolumeSize = (size?: number) => {
  if (size === undefined || size < 0) return "Unavailable";
  return formatFileSize(size);
};

const formatReferenceCount = (count?: number) => {
  if (count === undefined || count < 0) return "Unavailable";
  return count.toLocaleString();
};

const formatDockerValue = (value: unknown) => {
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }
  if (value === null) return "null";
  try {
    return JSON.stringify(value) ?? "Unavailable";
  } catch {
    return "Unavailable";
  }
};
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
const DeleteVolumeDialog = ({
  open,
  onClose,
  volumeNames,
  onSuccess,
}: DeleteVolumeDialogProps) => {
  const theme = useAppTheme();
  const toast = useScopedToast({ label: "Open Docker", to: "/docker" });
  // Configless: this is a batch flow — the caller owns aggregation and toasts.
  const { mutateAsync: deleteVolume, isPending: isDeleting } = useCallMutation(
    linuxio.docker.delete_volume,
  );
  const handleDelete = async () => {
    // Delete volumes sequentially
    const failures: string[] = [];
    for (const name of volumeNames) {
      try {
        await deleteVolume({ name });
      } catch {
        failures.push(name);
      }
    }
    if (failures.length > 0) {
      toast.error(
        `Failed to delete ${failures.length} of ${volumeNames.length} volume${volumeNames.length === 1 ? "" : "s"}`,
      );
    } else {
      const successMessage =
        volumeNames.length === 1
          ? `Volume "${volumeNames[0]}" deleted successfully`
          : `${volumeNames.length} volumes deleted successfully`;
      toast.success(successMessage);
    }
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
const getVolumeId = (volume: { Name: string }) => volume.Name;

const VolumeList = ({
  onMountCreateHandler,
  viewMode = "table",
}: VolumeListProps) => {
  const theme = useAppTheme();
  const navigate = dockerRouteApi.useNavigate();
  const searchParams = dockerRouteApi.useSearch();
  const focusedVolumeName = searchParams.volume;
  const { data: rawVolumes } = useSuspenseQuery({
    ...linuxio.docker.list_volumes,
    ...{
      refetchInterval: 10000,
    },
  });
  const [search, setSearch] = useState("");
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);

  // Ensure volumes is an array (handle null/undefined from API)
  const volumesList = rawVolumes;
  const focusedVolume = useMemo(
    () =>
      volumesList.find((volume) => volume.Name === focusedVolumeName) ?? null,
    [focusedVolumeName, volumesList],
  );
  const updateFocusedVolume = useCallback(
    (name: string | null) => {
      void navigate({
        to: "/docker/volumes",
        search: (previous) => ({ ...previous, volume: name ?? undefined }),
      });
    },
    [navigate],
  );
  useEffect(() => {
    if (focusedVolumeName && !focusedVolume) updateFocusedVolume(null);
  }, [focusedVolume, focusedVolumeName, updateFocusedVolume]);
  useEffect(() => {
    if (!focusedVolume) return;
    const handleKeyDown = (event: KeyboardEvent) => {
      if (
        (event.key !== "Escape" && event.key !== "Esc") ||
        event.defaultPrevented ||
        document.querySelector(OVERLAY_ROOT_SELECTOR)
      ) {
        return;
      }
      updateFocusedVolume(null);
      event.preventDefault();
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [focusedVolume, updateFocusedVolume]);

  // Create volume handler
  const handleCreateVolume = useCallback(() => {
    // TODO: Open volume creation dialog
    console.log("Create volume clicked");
  }, []);

  useRegisterCreateHandler(onMountCreateHandler, handleCreateVolume);
  const surface = useReorderableSurface({
    getId: getVolumeId,
    items: volumesList,
    surface: "docker.volumes",
  });
  const tableDnd = useReorderableTableDnd<
    (typeof volumesList)[number],
    (typeof volumesList)[number]
  >({ handleAriaLabel: "Reorder volume", surface });
  const filtered = surface.items.filter(
    (vol) =>
      vol.Name.toLowerCase().includes(search.toLowerCase()) ||
      vol.Driver.toLowerCase().includes(search.toLowerCase()) ||
      vol.Mountpoint?.toLowerCase().includes(search.toLowerCase()),
  );

  const handleDeleteSuccess = () => {
    updateFocusedVolume(null);
  };
  const handleVolumeRowClick = useCallback(
    ({ original: volume }: { original: { Name: string } }) =>
      updateFocusedVolume(volume.Name),
    [updateFocusedVolume],
  );

  // Stable column defs — see docs/table-row-gestures.md: a rebuilt array
  // remounts every cell subtree on the press that arms the reorder hold.
  const columns = useMemo<
    AppVirtualDataTableColumnDef<(typeof filtered)[number]>[]
  >(
    () => [
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
              fontFamily: "var(--app-font-mono)",
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
      {
        id: "size",
        header: "Size",
        cell: ({ row }) => (
          <AppTypography style={responsiveTextStyles} variant="body2">
            {formatVolumeSize(row.original.UsageData?.Size)}
          </AppTypography>
        ),
        meta: {
          align: "right",
          hideBelow: "lg",
          width: "120px",
        },
      },
      {
        id: "references",
        header: "References",
        cell: ({ row }) => (
          <AppTypography style={responsiveTextStyles} variant="body2">
            {formatReferenceCount(row.original.UsageData?.RefCount)}
          </AppTypography>
        ),
        meta: {
          align: "right",
          hideBelow: "lg",
          width: "120px",
        },
      },
    ],
    [],
  );
  // Shared by the focused details layout; table expansion retains its existing
  // markup below so its inline expansion behavior remains unchanged.
  const renderExpandedContent = (volume: (typeof volumesList)[number]) => (
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
          <b>Usage:</b>
        </AppTypography>
        <div className="expand-panel__chips">
          <Chip
            label={`Size: ${formatVolumeSize(volume.UsageData?.Size)}`}
            size="small"
            variant="soft"
          />
          <Chip
            label={`References: ${formatReferenceCount(volume.UsageData?.RefCount)}`}
            size="small"
            variant="soft"
          />
        </div>
      </div>
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
      <div>
        <AppTypography gutterBottom variant="subtitle2">
          <b>Driver Status:</b>
        </AppTypography>
        <div className="expand-panel__chips">
          {volume.Status && Object.keys(volume.Status).length > 0 ? (
            Object.entries(volume.Status).map(([key, value]) => (
              <Chip
                key={key}
                label={`${key}: ${formatDockerValue(value)}`}
                size="small"
                style={wrappableChipStyle}
                labelStyle={wrappableChipLabelStyle}
                variant="soft"
              />
            ))
          ) : (
            <AppTypography color="text.secondary" variant="body2">
              (no driver status)
            </AppTypography>
          )}
        </div>
      </div>
      {volume.ClusterVolume && (
        <div>
          <AppTypography gutterBottom variant="subtitle2">
            <b>Cluster Volume:</b>
          </AppTypography>
          <div className="expand-panel__chips">
            {Object.entries(volume.ClusterVolume).map(([key, value]) => (
              <Chip
                key={key}
                label={`${key}: ${formatDockerValue(value)}`}
                size="small"
                style={wrappableChipStyle}
                labelStyle={wrappableChipLabelStyle}
                variant="soft"
              />
            ))}
          </div>
        </div>
      )}
    </div>
  );

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        height: "100%",
        minHeight: 0,
      }}
    >
      {!focusedVolume && (
        <RoutedTabSearch active={search !== ""}>
          <AppHeaderSearch
            clearOnDocumentEscape
            onChange={setSearch}
            placeholder="Search volumes…"
            value={search}
          />
        </RoutedTabSearch>
      )}
      {focusedVolume ? (
        <DockerResourceDetailsLayout
          onClose={() => updateFocusedVolume(null)}
          resourceLabel="volume"
          subtitle={`${focusedVolume.Driver} · ${focusedVolume.Scope || "local"}`}
          summary={
            <VolumeCard
              actions={
                <AppActionIconButton
                  ariaLabel={`Delete volume ${focusedVolume.Name}`}
                  color={theme.palette.error.main}
                  icon="mdi:delete"
                  iconSize={18}
                  label="Delete volume"
                  onClick={() => setDeleteDialogOpen(true)}
                />
              }
              selected
              volume={focusedVolume}
            />
          }
          title={focusedVolume.Name}
        >
          {renderExpandedContent(focusedVolume)}
        </DockerResourceDetailsLayout>
      ) : viewMode === "card" ? (
        filtered.length > 0 ? (
          <ReorderableCardGrid
            fillAvailable
            getId={getVolumeId}
            items={filtered}
            renderItem={(volume) => (
              <VolumeCard
                onOpen={
                  surface.editMode
                    ? undefined
                    : () => updateFocusedVolume(volume.Name)
                }
                volume={volume}
              />
            )}
            size={CARD_GRID_SIZE_STANDARD}
            surface={surface}
          />
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
        <AppVirtualDataTable
          ariaLabel="Docker volumes"
          columns={columns}
          data={filtered}
          dnd={tableDnd}
          emptyMessage="No volumes found."
          fillAvailable
          getRowId={getVolumeId}
          onRowClick={surface.editMode ? undefined : handleVolumeRowClick}
          selectedRowId={focusedVolumeName}
        />
      )}

      <DeleteVolumeDialog
        onClose={() => setDeleteDialogOpen(false)}
        onSuccess={handleDeleteSuccess}
        open={deleteDialogOpen}
        volumeNames={focusedVolume ? [focusedVolume.Name] : []}
      />
    </div>
  );
};
export default VolumeList;
