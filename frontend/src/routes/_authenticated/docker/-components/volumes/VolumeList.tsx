import { Icon } from "@iconify/react";
import { useSuspenseQuery } from "@tanstack/react-query";
import { getRouteApi, useNavigate } from "@tanstack/react-router";
import { useCallback, useMemo, useState } from "react";

import { linuxio } from "@/api";
import ResourceDetailsLayout from "@/components/cards/ResourceDetailsLayout";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import ReorderableCardGrid from "@/components/reorder/ReorderableCardGrid";
import { useReorderableSurface } from "@/components/reorder/useReorderableSurface";
import { RoutedTabSearch } from "@/components/tabbar";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import { useReorderableTableDnd } from "@/components/tables/useReorderableTableDnd";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppHeaderSearch from "@/components/ui/AppHeaderSearch";
import AppMenu, { AppMenuItem } from "@/components/ui/AppMenu";
import AppTypography from "@/components/ui/AppTypography";
import { useRegisterCreateHandler } from "@/hooks/useRegisterCreateHandler";
import { useBackgroundTaskActions } from "@/tasks/useBackgroundTaskActions";
import { CARD_GRID_SIZE_STANDARD } from "@/theme/constants";
import { ensureTrailingSlash } from "@/utils/path";

import { CreateVolumeDialog } from "./CreateVolumeDialog";
import { VolumeDetailsContent } from "./VolumeDetailsContent";
import { formatReferenceCount, formatVolumeSize } from "./volumeFormat";
import { useFocusedResourceParam } from "../../-hooks/useFocusedResourceParam";
import VolumeCard from "../cards/VolumeCard";
import BatchDeleteDialog from "../shared/BatchDeleteDialog";

const dockerRouteApi = getRouteApi("/_authenticated/docker/volumes");

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
  const navigateApp = useNavigate();
  const { startDownload } = useBackgroundTaskActions();
  const searchParams = dockerRouteApi.useSearch();
  const focusedVolumeName = searchParams.volume;
  const { data: rawVolumes } = useSuspenseQuery({
    ...linuxio.docker.list_volumes,
    ...{
      refetchInterval: 60000,
    },
  });
  const [search, setSearch] = useState("");
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [backupDialogOpen, setBackupDialogOpen] = useState(false);
  const [menuAnchor, setMenuAnchor] = useState<HTMLElement | null>(null);

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
    setCreateDialogOpen(true);
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
  const { mutateAsync: deleteVolume } =
    linuxio.docker.delete_volume.useMutation();
  const handleDeleteSuccess = () => {
    updateFocusedVolume(null);
  };
  const handleVolumeRowClick = useCallback(
    ({ original: volume }: { original: { Name: string } }) =>
      updateFocusedVolume(volume.Name),
    [updateFocusedVolume],
  );
  const runningVolumeContainers =
    focusedVolume?.Containers?.filter(
      (container) => container.State === "running",
    ) ?? [];
  const browseFocusedVolume = () => {
    if (!focusedVolume?.MountpointAccessible) return;
    void navigateApp({
      to: "/filebrowser/$",
      params: { _splat: focusedVolume.Mountpoint.replace(/^\/+/, "") },
    });
  };
  const chooseVolumeAction = (action: "backup" | "delete") => {
    setMenuAnchor(null);
    if (action === "backup") setBackupDialogOpen(true);
    else setDeleteDialogOpen(true);
  };

  // Stable column defs — see docs/table-row-gestures.md: a rebuilt array
  // remounts every cell subtree on the press that arms the reorder hold.
  const columns = useMemo<
    AppVirtualTableColumnDef<(typeof filtered)[number]>[]
  >(
    () => [
      {
        accessorKey: "Name",
        header: "Volume / local path",
        cell: ({ row }) => (
          <div style={{ minWidth: 0 }}>
            <AppTypography fontWeight={500} noWrap variant="body2">
              {row.original.Name}
            </AppTypography>
            <AppTypography
              color="text.secondary"
              style={{ overflowWrap: "anywhere" }}
              variant="body2"
            >
              {row.original.Mountpoint || "Local path unavailable"}
            </AppTypography>
          </div>
        ),
        meta: { align: "left" },
      },
      {
        accessorKey: "Driver",
        header: "Driver",
        cell: ({ row }) => (
          <AppTypography color="text.secondary" variant="body2">
            {row.original.Driver}
          </AppTypography>
        ),
        meta: {
          align: "left",
          hideBelow: "md",
          width: "100px",
        },
      },
      {
        accessorKey: "Scope",
        header: "Scope",
        cell: ({ row }) => (
          <AppTypography color="text.secondary" variant="body2">
            {row.original.Scope || "local"}
          </AppTypography>
        ),
        meta: {
          align: "left",
          hideBelow: "md",
          width: "100px",
        },
      },
      {
        id: "size",
        header: "Size",
        cell: ({ row }) => (
          <AppTypography
            style={{ fontVariantNumeric: "tabular-nums" }}
            variant="body2"
          >
            {formatVolumeSize(row.original.UsageData?.Size)}
          </AppTypography>
        ),
        meta: {
          align: "right",
          width: "100px",
        },
      },
      {
        id: "references",
        header: "References",
        cell: ({ row }) => (
          <AppTypography
            style={{ fontVariantNumeric: "tabular-nums" }}
            variant="body2"
          >
            {formatReferenceCount(row.original.UsageData?.RefCount)}
          </AppTypography>
        ),
        meta: {
          align: "right",
          width: "100px",
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
        <ResourceDetailsLayout
          onClose={() => updateFocusedVolume(null)}
          resourceLabel="volume"
          subtitle={`${focusedVolume.Driver} · ${focusedVolume.Scope || "local"}`}
          summary={
            <VolumeCard
              actions={
                <div
                  style={{
                    alignItems: "center",
                    display: "flex",
                    gap: "var(--app-space-2)",
                  }}
                >
                  {focusedVolume.MountpointAccessible && (
                    <AppActionIconButton
                      ariaLabel={`Browse volume ${focusedVolume.Name} in Navigator`}
                      icon="mdi:folder-open-outline"
                      iconSize={18}
                      label="Browse in Navigator"
                      onClick={browseFocusedVolume}
                    />
                  )}
                  {focusedVolume.MountpointAccessible ? (
                    <AppActionIconButton
                      ariaLabel={`Actions for volume ${focusedVolume.Name}`}
                      icon="mdi:dots-vertical"
                      iconSize={20}
                      onClick={(event) => setMenuAnchor(event.currentTarget)}
                      tooltip={false}
                    />
                  ) : (
                    <AppActionIconButton
                      ariaLabel={`Delete volume ${focusedVolume.Name}`}
                      color="var(--app-palette-error-main)"
                      icon="mdi:delete"
                      iconSize={18}
                      label="Delete volume"
                      onClick={() => setDeleteDialogOpen(true)}
                    />
                  )}
                </div>
              }
              selected
              volume={focusedVolume}
            />
          }
          title={focusedVolume.Name}
        >
          <VolumeDetailsContent volume={focusedVolume} />
        </ResourceDetailsLayout>
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

      <CreateVolumeDialog
        drivers={[
          "local",
          ...new Set(
            volumesList
              .map((volume) => volume.Driver)
              .filter(
                (volumeDriver) =>
                  volumeDriver !== "" && volumeDriver !== "local",
              ),
          ),
        ]}
        existingNames={volumesList.map((volume) => volume.Name)}
        onClose={() => setCreateDialogOpen(false)}
        open={createDialogOpen}
      />

      <AppMenu
        anchorEl={menuAnchor}
        ariaLabel={
          focusedVolume ? `Actions for volume ${focusedVolume.Name}` : undefined
        }
        minWidth={190}
        onClose={() => setMenuAnchor(null)}
        open={Boolean(menuAnchor && focusedVolume)}
      >
        <AppMenuItem
          onClick={() => chooseVolumeAction("backup")}
          startAdornment={<Icon icon="mdi:download" width={18} />}
        >
          Download backup
        </AppMenuItem>
        <AppMenuItem
          danger
          onClick={() => chooseVolumeAction("delete")}
          startAdornment={<Icon icon="mdi:delete" width={18} />}
        >
          Delete volume
        </AppMenuItem>
      </AppMenu>

      <GeneralDialog
        aria-label={
          focusedVolume
            ? `Download backup of ${focusedVolume.Name}?`
            : "Download volume backup?"
        }
        fullWidth
        maxWidth="sm"
        onClose={() => setBackupDialogOpen(false)}
        open={backupDialogOpen && Boolean(focusedVolume)}
      >
        <AppDialogTitle>
          Download backup of {focusedVolume?.Name}?
        </AppDialogTitle>
        <AppDialogContent>
          <AppDialogContentText>
            LinuxIO will create a ZIP archive and download it through the
            browser.
          </AppDialogContentText>
          {runningVolumeContainers.length > 0 ? (
            <>
              <AppDialogContentText
                style={{
                  color: "var(--app-palette-warning-main)",
                  marginTop: "var(--app-space-8)",
                }}
              >
                These running containers may be writing to the volume. The
                archive can contain inconsistent data.
              </AppDialogContentText>
              <div className="app-dialog-fields">
                {runningVolumeContainers.map((container) => (
                  <Chip
                    key={container.Id}
                    label={`${container.Name} (${container.State})`}
                    size="small"
                    variant="soft"
                  />
                ))}
              </div>
            </>
          ) : (
            <AppDialogContentText style={{ marginTop: "var(--app-space-8)" }}>
              No attached containers are running.
            </AppDialogContentText>
          )}
        </AppDialogContent>
        <AppDialogActions>
          <AppButton onClick={() => setBackupDialogOpen(false)}>
            Cancel
          </AppButton>
          <AppButton
            onClick={() => {
              setBackupDialogOpen(false);
              if (focusedVolume) {
                void startDownload([
                  ensureTrailingSlash(focusedVolume.Mountpoint),
                ]);
              }
            }}
            variant="contained"
          >
            Download backup
          </AppButton>
        </AppDialogActions>
      </GeneralDialog>

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
