import { useSuspenseQuery } from "@tanstack/react-query";
import { getRouteApi } from "@tanstack/react-router";
import { useCallback, useMemo, useState } from "react";

import { type DockerVolume, linuxio, useCallMutation } from "@/api";
import VolumeCard from "@/components/cards/VolumeCard";
import BatchDeleteDialog from "@/components/docker/BatchDeleteDialog";
import DockerResourceDetailsLayout from "@/components/docker/DockerResourceDetailsLayout";
import ReorderableCardGrid from "@/components/reorder/ReorderableCardGrid";
import { RoutedTabSearch } from "@/components/tabbar";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import Chip from "@/components/ui/AppChip";
import AppHeaderSearch from "@/components/ui/AppHeaderSearch";
import AppTypography from "@/components/ui/AppTypography";
import { useFocusedResourceParam } from "@/hooks/useFocusedResourceParam";
import { useRegisterCreateHandler } from "@/hooks/useRegisterCreateHandler";
import { useReorderableSurface } from "@/hooks/useReorderableSurface";
import { useReorderableTableDnd } from "@/hooks/useReorderableTableDnd";
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

const VolumeDetailsContent = ({ volume }: { volume: DockerVolume }) => (
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

interface VolumeListProps {
  onMountCreateHandler?: (handler: () => void) => void;
  viewMode?: "table" | "card";
}
const getVolumeId = (volume: { Name: string }) => volume.Name;

const VolumeList = ({
  onMountCreateHandler,
  viewMode = "table",
}: VolumeListProps) => {
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
  const updateFocusedVolume = useCallback(
    (name: string | null) => {
      void navigate({
        to: "/docker/volumes",
        search: (previous) => ({ ...previous, volume: name ?? undefined }),
      });
    },
    [navigate],
  );
  const focusedVolume = useFocusedResourceParam({
    focusedId: focusedVolumeName,
    getId: getVolumeId,
    items: volumesList,
    onClear: () => updateFocusedVolume(null),
  });

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

  // Configless: this is a batch flow — the dialog owns aggregation and toasts.
  const { mutateAsync: deleteVolume } = useCallMutation(
    linuxio.docker.delete_volume,
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
    AppVirtualTableColumnDef<(typeof filtered)[number]>[]
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
          <Chip label={row.original.Driver} size="xsmall" variant="soft" />
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
                  color="var(--app-palette-error-main)"
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
          <VolumeDetailsContent volume={focusedVolume} />
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
              paddingTop: "var(--app-space-16)",
              paddingBottom: "var(--app-space-16)",
            }}
          >
            <AppTypography color="text.secondary" variant="body2">
              No volumes found.
            </AppTypography>
          </div>
        )
      ) : (
        <AppVirtualTable
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

      <BatchDeleteDialog
        items={
          focusedVolume
            ? [{ key: focusedVolume.Name, label: focusedVolume.Name }]
            : []
        }
        noun="volume"
        onClose={() => setDeleteDialogOpen(false)}
        onDeleteOne={(item) => deleteVolume({ name: item.key })}
        onSuccess={handleDeleteSuccess}
        open={deleteDialogOpen}
        warning="Volumes in use by containers cannot be deleted."
      />
    </div>
  );
};
export default VolumeList;
