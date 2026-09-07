import { Icon } from "@iconify/react";
import { useSuspenseQuery } from "@tanstack/react-query";
import { getRouteApi, useNavigate } from "@tanstack/react-router";
import { useCallback, useMemo, useState, type SyntheticEvent } from "react";

import { type DockerVolume, linuxio, useCallMutation } from "@/api";
import { DetailRow } from "@/components/cards/UnitInfoPanelCard";
import VolumeCard from "@/components/cards/VolumeCard";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import BatchDeleteDialog from "@/components/docker/BatchDeleteDialog";
import DockerResourceDetailsLayout from "@/components/docker/DockerResourceDetailsLayout";
import ReorderableCardGrid from "@/components/reorder/ReorderableCardGrid";
import { RoutedTabSearch } from "@/components/tabbar";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
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
import AppSelect from "@/components/ui/AppSelect";
import AppTextField from "@/components/ui/AppTextField";
import AppTypography from "@/components/ui/AppTypography";
import { useBackgroundTaskActions } from "@/hooks/backgroundTasks/useBackgroundTaskActions";
import { useFocusedResourceParam } from "@/hooks/useFocusedResourceParam";
import { useRegisterCreateHandler } from "@/hooks/useRegisterCreateHandler";
import { useReorderableSurface } from "@/hooks/useReorderableSurface";
import { useReorderableTableDnd } from "@/hooks/useReorderableTableDnd";
import { useScopedToast } from "@/hooks/useScopedToast";
import { CARD_GRID_SIZE_STANDARD } from "@/theme/constants";
import {
  longTextStyles,
  responsiveTextStyles,
  wrappableChipStyle,
  wrappableChipLabelStyle,
} from "@/theme/tableStyles";
import { parseDockerKeyValueLines } from "@/utils/dockerKeyValues";
import { formatFileSize } from "@/utils/formaters";
import { ensureTrailingSlash } from "@/utils/path";

const dockerRouteApi = getRouteApi("/_authenticated/docker/volumes");
const DOCKER_TOAST_META = { label: "Open Docker", to: "/docker" } as const;
const DOCKER_RESOURCE_NAME = /^[a-zA-Z0-9][a-zA-Z0-9_.-]*$/;
const CUSTOM_VOLUME_DRIVER = "__custom__";

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

const CreateVolumeDialog = ({
  drivers,
  existingNames,
  onClose,
  open,
}: {
  drivers: string[];
  existingNames: string[];
  onClose: () => void;
  open: boolean;
}) => {
  const [name, setName] = useState("");
  const [driver, setDriver] = useState("local");
  const [customDriver, setCustomDriver] = useState("");
  const [labelsText, setLabelsText] = useState("");
  const [formError, setFormError] = useState<string>();
  const toast = useScopedToast(DOCKER_TOAST_META);
  const parsedLabels = parseDockerKeyValueLines(labelsText);
  const trimmedName = name.trim();
  const selectedDriver =
    driver === CUSTOM_VOLUME_DRIVER ? customDriver.trim() : driver;
  const nameTaken = existingNames.includes(trimmedName);
  const nameInvalid =
    trimmedName !== "" && !DOCKER_RESOURCE_NAME.test(trimmedName);
  const { mutate: createVolume, isPending } = useCallMutation(
    linuxio.docker.create_volume,
    {
      success: () => {
        toast.success(`Volume "${trimmedName}" created`);
        closeDialog();
      },
      error: "Failed to create volume",
      toast: DOCKER_TOAST_META,
    },
  );

  const closeDialog = () => {
    setName("");
    setDriver("local");
    setCustomDriver("");
    setLabelsText("");
    setFormError(undefined);
    onClose();
  };
  const handleSubmit = (event: SyntheticEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!trimmedName || !selectedDriver) {
      setFormError("Name and driver are required.");
      return;
    }
    if (nameTaken) {
      setFormError("Choose a volume name that is not already in use.");
      return;
    }
    if (nameInvalid) {
      setFormError(
        "Name must start with an alphanumeric character and use only letters, numbers, underscores, periods, or hyphens.",
      );
      return;
    }
    if (parsedLabels.error) {
      setFormError(parsedLabels.error);
      return;
    }
    setFormError(undefined);
    createVolume({
      name: trimmedName,
      driver: selectedDriver,
      labels:
        Object.keys(parsedLabels.values).length > 0
          ? parsedLabels.values
          : undefined,
    });
  };

  return (
    <GeneralDialog
      aria-label="Create volume"
      fullWidth
      maxWidth="xs"
      onClose={isPending ? undefined : closeDialog}
      open={open}
    >
      <form onSubmit={handleSubmit}>
        <AppDialogTitle>Create volume</AppDialogTitle>
        <AppDialogContent>
          <div
            style={{
              display: "flex",
              flexDirection: "column",
              gap: "var(--app-space-12)",
              marginTop: "var(--app-space-8)",
            }}
          >
            <AppTextField
              autoFocus
              disabled={isPending}
              error={nameTaken || nameInvalid}
              fullWidth
              helperText={
                nameTaken
                  ? "This volume name already exists."
                  : nameInvalid
                    ? "Use letters, numbers, underscores, periods, or hyphens."
                    : undefined
              }
              label="Name"
              name="volume-name"
              onChange={(event) => setName(event.target.value)}
              required
              value={name}
            />
            <AppSelect
              disabled={isPending}
              fullWidth
              label="Driver"
              onChange={(event) => setDriver(event.target.value)}
              value={driver}
            >
              {drivers.map((volumeDriver) => (
                <option key={volumeDriver} value={volumeDriver}>
                  {volumeDriver}
                </option>
              ))}
              <option value={CUSTOM_VOLUME_DRIVER}>Custom…</option>
            </AppSelect>
            {driver === CUSTOM_VOLUME_DRIVER && (
              <AppTextField
                autoFocus
                disabled={isPending}
                fullWidth
                label="Custom driver"
                name="volume-custom-driver"
                onChange={(event) => setCustomDriver(event.target.value)}
                required
                value={customDriver}
              />
            )}
            <AppTextField
              disabled={isPending}
              error={Boolean(parsedLabels.error)}
              fullWidth
              helperText={
                parsedLabels.error ?? "Optional, one key=value label per line."
              }
              label="Labels"
              multiline
              name="volume-labels"
              onChange={(event) => setLabelsText(event.target.value)}
              rows={4}
              value={labelsText}
            />
            {formError && (
              <AppTypography color="error" role="alert" variant="body2">
                {formError}
              </AppTypography>
            )}
          </div>
        </AppDialogContent>
        <AppDialogActions>
          <AppButton disabled={isPending} onClick={closeDialog} type="button">
            Cancel
          </AppButton>
          <AppButton disabled={isPending} type="submit" variant="contained">
            {isPending ? "Creating…" : "Create volume"}
          </AppButton>
        </AppDialogActions>
      </form>
    </GeneralDialog>
  );
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
        <b>Containers:</b>
      </AppTypography>
      {volume.Containers?.length ? (
        <div>
          {volume.Containers.map((container, index) => (
            <DetailRow
              key={container.Id}
              label={container.Name}
              noBorder={index === 0}
              split
            >
              <Chip
                color={container.State === "running" ? "success" : "default"}
                label={container.State || "unknown"}
                size="xsmall"
                variant="soft"
              />
            </DetailRow>
          ))}
        </div>
      ) : (
        <AppTypography color="text.secondary" variant="body2">
          No containers use this volume.
        </AppTypography>
      )}
    </div>
    {!volume.MountpointAccessible && (
      <AppTypography color="text.secondary" variant="body2">
        This volume does not expose an accessible host mountpoint, so browsing
        and backups are unavailable.
      </AppTypography>
    )}
    <div>
      <AppTypography gutterBottom variant="subtitle2">
        <b>Labels:</b>
      </AppTypography>
      <div
        style={{
          alignItems: "flex-start",
          display: "flex",
          flexDirection: "column",
          gap: "var(--app-space-4)",
        }}
      >
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
  const navigateApp = useNavigate();
  const { startDownload } = useBackgroundTaskActions();
  const searchParams = dockerRouteApi.useSearch();
  const focusedVolumeName = searchParams.volume;
  const { data: rawVolumes } = useSuspenseQuery({
    ...linuxio.docker.list_volumes,
    ...{
      refetchInterval: 10000,
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
              <div
                style={{
                  display: "flex",
                  flexDirection: "column",
                  gap: "var(--app-space-4)",
                  marginTop: "var(--app-space-8)",
                }}
              >
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
