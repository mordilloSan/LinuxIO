import { Icon } from "@iconify/react";
import { lazy, memo, Suspense, useCallback, useMemo, useState } from "react";

import {
  linuxio,
  type ComposeProject,
  type ContainerInfo,
  type ContainerPort,
  useCallMutation,
} from "@/api";
import ComposeStackCard from "@/components/cards/ComposeStackCard";
import DockerIcon from "@/components/docker/DockerIcon";
import ReorderableCardGrid from "@/components/reorder/ReorderableCardGrid";
import AppDataTable from "@/components/tables/AppDataTable";
import type { AppDataTableColumnDef } from "@/components/tables/AppDataTable";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import Chip from "@/components/ui/AppChip";
import AppSearchField from "@/components/ui/AppSearchField";
import AppTypography from "@/components/ui/AppTypography";
import StatusDot from "@/components/ui/StatusDot";
import {
  getComposeStatusColor,
  getContainerStatusColor,
} from "@/constants/statusColors";
import { useReorderableSurface } from "@/hooks/useReorderableSurface";
import { useReorderableTableDnd } from "@/hooks/useReorderableTableDnd";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useAppMediaQuery, useAppTheme } from "@/theme";
import { getMutationErrorMessage } from "@/utils/mutations";

import "./compose-list.css";

const LogsDialog = lazy(() => import("@/components/docker/LogsDialog"));
const TerminalDialog = lazy(() => import("@/components/docker/TerminalDialog"));

const DOCKER_TOAST_META = { label: "Open Docker", to: "/docker" } as const;
interface ComposeListProps {
  isLoading?: boolean;
  onDelete: (project: ComposeProject) => void;
  onEdit?: (projectName: string, configPath: string) => void;
  onRestart: (projectName: string) => void;
  onStart: (projectName: string) => void;
  onStop: (projectName: string) => void;
  projects: ComposeProject[];
  viewMode?: "table" | "card";
}

const getContainerName = (container: ContainerInfo) =>
  container.Names?.[0]?.replace(/^\//, "") || container.Id.slice(0, 12);

const getContainerServiceName = (container: ContainerInfo) =>
  container.Labels?.["com.docker.compose.service"] || "-";

const getContainerDisplayState = (container: ContainerInfo) => {
  const status = container.Status.toLowerCase();
  if (status.includes("unhealthy")) return "Unhealthy";
  if (status.includes("healthy")) return "Healthy";
  if (container.State === "running") return "Running";
  if (container.State === "exited") return "Stopped";
  if (container.State === "dead") return "Dead";
  return container.State || "Unknown";
};

const getDedupedContainerPorts = (container: ContainerInfo) => {
  const seen = new Set<string>();
  return (container.Ports ?? [])
    .filter((port) => {
      const key = port.PublicPort
        ? `${port.PrivatePort}/${port.Type}:${port.PublicPort}`
        : `${port.PrivatePort}/${port.Type}`;
      if (seen.has(key)) return false;
      seen.add(key);
      return true;
    })
    .sort(
      (a, b) => a.PrivatePort - b.PrivatePort || a.Type.localeCompare(b.Type),
    );
};

const formatContainerPort = (port: ContainerPort) =>
  port.PublicPort
    ? `${port.PublicPort}:${port.PrivatePort}/${port.Type}`
    : `${port.PrivatePort}/${port.Type}`;

const getTotalContainers = (project: ComposeProject) => {
  if (project.containers?.length) return project.containers.length;
  return Object.values(project.services).reduce(
    (acc, service) => acc + service.container_count,
    0,
  );
};

const getComposeProjectId = (project: ComposeProject) => project.name;

const ComposeList = ({
  projects,
  onStart,
  onStop,
  onRestart,
  onDelete,
  onEdit,
  isLoading = false,
  viewMode = "table",
}: ComposeListProps) => {
  const [search, setSearch] = useState("");
  const [logsContainer, setLogsContainer] = useState<ContainerInfo | null>(
    null,
  );
  const [terminalContainer, setTerminalContainer] =
    useState<ContainerInfo | null>(null);
  const theme = useAppTheme();
  const toast = useScopedToast(DOCKER_TOAST_META);
  const isSmallUp = useAppMediaQuery(theme.breakpoints.up("sm"));
  const surface = useReorderableSurface({
    getId: getComposeProjectId,
    items: projects,
    surface: "docker.stacks",
  });
  const tableDnd = useReorderableTableDnd<ComposeProject, ComposeProject>({
    handleAriaLabel: "Reorder stack",
    surface,
  });
  const orderedProjects = surface.items;
  const filtered = useMemo(() => {
    const normalizedSearch = search.trim().toLowerCase();
    if (!normalizedSearch) return orderedProjects;

    return orderedProjects.filter((project) =>
      project.name.toLowerCase().includes(normalizedSearch),
    );
  }, [orderedProjects, search]);
  const containersByProject = useMemo(() => {
    return new Map(
      projects.map((project) => [
        project.name,
        [...(project.containers ?? [])].sort((a, b) =>
          getContainerName(a).localeCompare(getContainerName(b)),
        ),
      ]),
    );
  }, [projects]);
  const { mutateAsync: startContainer } = useCallMutation(
    linuxio.docker.start_container,
  );
  const { mutateAsync: stopContainer } = useCallMutation(
    linuxio.docker.stop_container,
  );
  const { mutateAsync: restartContainer } = useCallMutation(
    linuxio.docker.restart_container,
  );
  const { mutateAsync: removeContainer } = useCallMutation(
    linuxio.docker.remove_container,
  );
  const { mutateAsync: updateContainer, isPending: isUpdatingContainer } =
    useCallMutation(linuxio.docker.update_container);

  const handleStartContainer = useCallback(
    async (container: ContainerInfo) => {
      const name = getContainerName(container);
      try {
        await startContainer({ containerId: container.Id });
        toast.success(`Container ${name} started`);
      } catch (error) {
        toast.error(getMutationErrorMessage(error, `Failed to start ${name}`));
      }
    },
    [startContainer, toast],
  );

  const handleStopContainer = useCallback(
    async (container: ContainerInfo) => {
      const name = getContainerName(container);
      try {
        await stopContainer({ containerId: container.Id });
        toast.success(`Container ${name} stopped`);
      } catch (error) {
        toast.error(getMutationErrorMessage(error, `Failed to stop ${name}`));
      }
    },
    [stopContainer, toast],
  );

  const handleRestartContainer = useCallback(
    async (container: ContainerInfo) => {
      const name = getContainerName(container);
      try {
        await restartContainer({ containerId: container.Id });
        toast.success(`Container ${name} restarted`);
      } catch (error) {
        toast.error(
          getMutationErrorMessage(error, `Failed to restart ${name}`),
        );
      }
    },
    [restartContainer, toast],
  );

  const handleRemoveContainer = useCallback(
    async (container: ContainerInfo) => {
      const name = getContainerName(container);
      try {
        await removeContainer({ containerId: container.Id });
        toast.success(`Container ${name} removed`);
      } catch (error) {
        toast.error(getMutationErrorMessage(error, `Failed to remove ${name}`));
      }
    },
    [removeContainer, toast],
  );

  const handleUpdateContainer = useCallback(
    async (container: ContainerInfo) => {
      const name = getContainerName(container);
      try {
        const result = await updateContainer({ containerId: container.Id });
        toast.success(
          result.updated
            ? `Container ${name} updated`
            : `Container ${name} is already up to date`,
        );
      } catch (error) {
        toast.error(getMutationErrorMessage(error, `Failed to update ${name}`));
      }
    },
    [toast, updateContainer],
  );

  const columns = useMemo<AppDataTableColumnDef<ComposeProject>[]>(
    () => [
      {
        id: "status",
        header: "",
        enableSorting: false,
        cell: ({ row }) => {
          const project = row.original;
          const statusColor = getComposeStatusColor(project.status);
          return (
            <div
              style={{
                display: "flex",
                alignItems: "center",
                justifyContent: isSmallUp ? "flex-start" : "center",
              }}
            >
              {isSmallUp ? (
                <Chip
                  color={statusColor}
                  label={project.status}
                  labelStyle={{ paddingInline: 12 }}
                  size="small"
                  style={{
                    textTransform: "capitalize",
                    fontSize: "0.68rem",
                  }}
                  variant="soft"
                />
              ) : (
                <StatusDot color={statusColor} size={10} />
              )}
            </div>
          );
        },
        meta: {
          getCellRenderKey: (row) => {
            const project = row as ComposeProject;
            return [project.name, project.status];
          },
          width: isSmallUp ? "106px" : "40px",
        },
      },
      {
        accessorKey: "name",
        header: "Stack",
        cell: ({ row }) => {
          const project = row.original;
          return (
            <div
              style={{
                display: "flex",
                alignItems: "center",
                gap: theme.spacing(1.5),
              }}
            >
              <DockerIcon
                alt={project.name}
                identifier={project.icon}
                size={28}
              />
              <AppTypography
                fontWeight={700}
                noWrap
                title={project.name}
                toastMeta={DOCKER_TOAST_META}
                variant="body2"
              >
                {project.name}
              </AppTypography>
              {project.update_available && (
                <Chip
                  color="warning"
                  label="Update"
                  size="small"
                  style={{ fontSize: "0.68rem" }}
                  variant="soft"
                />
              )}
            </div>
          );
        },
        meta: {
          align: "left",
          getCellRenderKey: (row) => {
            const project = row as ComposeProject;
            return [project.name, project.icon, project.update_available];
          },
        },
      },
      {
        id: "containers",
        header: "Containers",
        accessorFn: (project) => getTotalContainers(project),
        cell: ({ row }) => getTotalContainers(row.original),
        meta: {
          align: "center",
          getCellRenderKey: (row) => {
            const project = row as ComposeProject;
            return [project.name, getTotalContainers(project)];
          },
          hideBelow: "sm",
          width: "100px",
        },
      },
      {
        id: "config",
        header: "Config Files",
        accessorFn: (project) => project.config_files.join(", "),
        cell: ({ row }) => {
          const project = row.original;
          const configText = project.config_files.join(", ") || "Unknown";
          return (
            <div
              style={{
                display: "flex",
                alignItems: "center",
              }}
            >
              <Icon
                height={20}
                icon="mdi:folder-open"
                style={{
                  marginRight: 4,
                  opacity: 0.7,
                }}
                width={20}
              />
              <AppTypography
                copyText={configText}
                noWrap
                style={{
                  maxWidth: 200,
                }}
                title={configText}
                toastMeta={DOCKER_TOAST_META}
                variant="body2"
              >
                {project.config_files[0]?.split("/").pop() ||
                  "docker-compose.yml"}
              </AppTypography>
            </div>
          );
        },
        meta: {
          align: "left",
          getCellRenderKey: (row) => {
            const project = row as ComposeProject;
            return [project.name, project.config_files.join("\u0000")];
          },
          hideBelow: "sm",
        },
      },
      {
        accessorKey: "working_dir",
        header: "Location",
        cell: ({ row }) => {
          const location = row.original.working_dir || "Unknown";
          return (
            <AppTypography
              copyText={location}
              noWrap
              style={{
                maxWidth: 600,
                fontSize: "0.85rem",
                color: "var(--app-palette-text-secondary)",
              }}
              title={location}
              toastMeta={DOCKER_TOAST_META}
              variant="body2"
            >
              {row.original.working_dir || "-"}
            </AppTypography>
          );
        },
        meta: {
          align: "left",
          getCellRenderKey: (row) => {
            const project = row as ComposeProject;
            return [project.name, project.working_dir];
          },
          hideBelow: "lg",
        },
      },
      {
        id: "actions",
        header: "Actions",
        enableSorting: false,
        cell: ({ row }) => {
          const project = row.original;
          return (
            <div
              style={{
                display: "flex",
                justifyContent: "flex-end",
                gap: isSmallUp ? theme.spacing(0.5) : 0,
              }}
            >
              {onEdit && project.config_files.length > 0 && (
                <AppActionIconButton
                  disabled={isLoading}
                  icon="mdi:pencil"
                  iconSize={20}
                  label="Edit"
                  onClick={() => onEdit(project.name, project.config_files[0])}
                />
              )}
              {project.status === "running" || project.status === "partial" ? (
                <>
                  <AppActionIconButton
                    disabled={isLoading}
                    icon="mdi:restart"
                    iconSize={20}
                    label="Restart"
                    onClick={() => onRestart(project.name)}
                  />
                  <AppActionIconButton
                    disabled={isLoading}
                    icon="mdi:stop-circle"
                    iconSize={20}
                    label="Stop"
                    onClick={() => onStop(project.name)}
                  />
                </>
              ) : (
                <AppActionIconButton
                  disabled={isLoading}
                  icon="mdi:play"
                  iconSize={20}
                  label="Start"
                  onClick={() => onStart(project.name)}
                />
              )}
              <AppActionIconButton
                disabled={isLoading}
                icon="mdi:delete"
                iconSize={20}
                label="Delete"
                onClick={() => onDelete(project)}
              />
            </div>
          );
        },
        meta: {
          align: "right",
          getCellRenderKey: (row) => {
            const project = row as ComposeProject;
            return [
              project.name,
              project.status,
              project.config_files.join("\u0000"),
              project.working_dir,
            ];
          },
          width: "200px",
        },
      },
    ],
    [isLoading, isSmallUp, onDelete, onEdit, onRestart, onStart, onStop, theme],
  );

  const expandedContainerColumns = useMemo<
    AppDataTableColumnDef<ContainerInfo>[]
  >(
    () => [
      {
        id: "name",
        header: "Container Name",
        cell: ({ row }) => {
          const container = row.original;
          const name = getContainerName(container);

          return (
            <div className="compose-container-name">
              <DockerIcon alt={name} identifier={container.icon} size={24} />
              <div className="compose-container-name-text">
                <AppTypography
                  fontWeight={700}
                  noWrap
                  title={name}
                  toastMeta={DOCKER_TOAST_META}
                  variant="body2"
                >
                  {name}
                </AppTypography>
                <AppTypography
                  className="compose-container-id"
                  color="text.secondary"
                  copyText={container.Id}
                  noWrap
                  title={container.Id}
                  toastMeta={DOCKER_TOAST_META}
                  tooltipOnlyWhenTruncated={false}
                  variant="caption"
                >
                  {container.Id.slice(0, 12)}
                </AppTypography>
                {container.updateAvailable && (
                  <Chip
                    color="warning"
                    label="Update"
                    size="small"
                    style={{ fontSize: "0.68rem", marginTop: 2 }}
                    variant="soft"
                  />
                )}
              </div>
            </div>
          );
        },
        meta: {
          getCellRenderKey: (row) => {
            const container = row as ContainerInfo;
            return [
              container.Id,
              getContainerName(container),
              container.icon,
              container.updateAvailable,
            ];
          },
        },
      },
      {
        id: "service",
        header: "Service",
        cell: ({ row }) => {
          const serviceName = getContainerServiceName(row.original);

          return (
            <AppTypography
              noWrap
              title={serviceName}
              toastMeta={DOCKER_TOAST_META}
              variant="body2"
            >
              {serviceName}
            </AppTypography>
          );
        },
        meta: {
          getCellRenderKey: (row) => {
            const container = row as ContainerInfo;
            return [container.Id, getContainerServiceName(container)];
          },
          hideBelow: "md",
        },
      },
      {
        accessorKey: "Image",
        header: "Image",
        cell: ({ row }) => (
          <AppTypography
            noWrap
            style={{
              maxWidth: 260,
            }}
            title={row.original.Image}
            toastMeta={DOCKER_TOAST_META}
            variant="body2"
          >
            {row.original.Image}
          </AppTypography>
        ),
        meta: {
          getCellRenderKey: (row) => {
            const container = row as ContainerInfo;
            return [container.Id, container.Image];
          },
          hideBelow: "sm",
        },
      },
      {
        id: "state",
        header: "State",
        cell: ({ row }) => {
          const displayState = getContainerDisplayState(row.original);

          return (
            <Chip
              color={getContainerStatusColor(displayState)}
              label={displayState}
              size="small"
              style={{
                textTransform: "capitalize",
              }}
              variant="soft"
            />
          );
        },
        meta: {
          getCellRenderKey: (row) => {
            const container = row as ContainerInfo;
            return [
              container.Id,
              container.State,
              getContainerDisplayState(container),
            ];
          },
        },
      },
      {
        id: "ports",
        header: "Ports",
        cell: ({ row }) => {
          const ports = getDedupedContainerPorts(row.original);
          const portsText =
            ports.length > 0 ? ports.map(formatContainerPort).join(", ") : "-";

          return (
            <AppTypography
              noWrap
              title={portsText}
              toastMeta={DOCKER_TOAST_META}
              variant="body2"
            >
              {portsText}
            </AppTypography>
          );
        },
        meta: {
          getCellRenderKey: (row) => {
            const container = row as ContainerInfo;
            return [
              container.Id,
              getDedupedContainerPorts(container)
                .map(formatContainerPort)
                .join("\u0000"),
            ];
          },
          hideBelow: "md",
        },
      },
      {
        id: "actions",
        header: "Actions",
        enableSorting: false,
        cell: ({ row }) => {
          const container = row.original;

          return (
            <div className="compose-container-actions">
              {container.State !== "running" && (
                <AppActionIconButton
                  disabled={isLoading}
                  icon="mdi:play"
                  iconSize={18}
                  label="Start container"
                  onClick={() => void handleStartContainer(container)}
                />
              )}
              {container.State === "running" && (
                <AppActionIconButton
                  disabled={isLoading}
                  icon="mdi:stop"
                  iconSize={18}
                  label="Stop container"
                  onClick={() => void handleStopContainer(container)}
                />
              )}
              <AppActionIconButton
                disabled={isLoading}
                icon="mdi:restart"
                iconSize={18}
                label="Restart container"
                onClick={() => void handleRestartContainer(container)}
              />
              {container.updateAvailable && (
                <AppActionIconButton
                  disabled={isLoading || isUpdatingContainer}
                  icon="mdi:update"
                  iconSize={18}
                  label="Update container"
                  onClick={() => void handleUpdateContainer(container)}
                />
              )}
              <AppActionIconButton
                disabled={isLoading}
                icon="mdi:file-document-outline"
                iconSize={18}
                label="View logs"
                onClick={() => setLogsContainer(container)}
              />
              {container.State === "running" && (
                <AppActionIconButton
                  disabled={isLoading}
                  icon="mdi:console"
                  iconSize={18}
                  label="Open terminal"
                  onClick={() => setTerminalContainer(container)}
                />
              )}
              {container.url && (
                <AppActionIconButton
                  disabled={isLoading}
                  icon="mdi:open-in-new"
                  iconSize={18}
                  label="Open app"
                  onClick={() =>
                    window.open(container.url, "_blank", "noopener")
                  }
                />
              )}
              <AppActionIconButton
                disabled={isLoading}
                icon="mdi:delete"
                iconSize={18}
                label="Remove container"
                onClick={() => void handleRemoveContainer(container)}
              />
            </div>
          );
        },
        meta: {
          align: "right",
          getCellRenderKey: (row) => {
            const container = row as ContainerInfo;
            return [
              container.Id,
              container.Names.join("\u0000"),
              container.State,
              container.updateAvailable,
              container.url,
            ];
          },
          width: "180px",
        },
      },
    ],
    [
      handleRemoveContainer,
      handleRestartContainer,
      handleStartContainer,
      handleStopContainer,
      handleUpdateContainer,
      isLoading,
      isUpdatingContainer,
    ],
  );

  const renderExpandedContent = useCallback(
    (project: ComposeProject) => {
      const containers = containersByProject.get(project.name) ?? [];

      return (
        <AppDataTable
          ariaLabel={`Containers in ${project.name}`}
          className="compose-expanded-table"
          columns={expandedContainerColumns}
          data={containers}
          density="compact"
          emptyMessage="No containers found for this stack."
          enableSorting={false}
          getRowAttributes={() => ({ className: "compose-container-row" })}
          getRowId={(container) => container.Id}
          maxHeight={260}
          variant="embedded"
        />
      );
    },
    [containersByProject, expandedContainerColumns],
  );
  const searchBar = (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        flexWrap: "nowrap",
        gap: isSmallUp ? theme.spacing(2) : theme.spacing(1),
        marginBottom: theme.spacing(2),
      }}
    >
      <AppSearchField
        onChange={(e) => setSearch(e.target.value)}
        placeholder="Search stacks…"
        style={{
          flex: isSmallUp ? "0 0 320px" : "1 1 auto",
          minWidth: 0,
          width: isSmallUp ? 320 : undefined,
        }}
        value={search}
      />
      <AppTypography
        fontWeight={700}
        style={{
          flexShrink: 0,
          whiteSpace: "nowrap",
        }}
      >
        {filtered.length} shown
      </AppTypography>
    </div>
  );
  const containerDialogs = (
    <Suspense fallback={null}>
      {logsContainer && (
        <LogsDialog
          containerId={logsContainer.Id}
          containerName={getContainerName(logsContainer)}
          onClose={() => setLogsContainer(null)}
          open={!!logsContainer}
        />
      )}
      {terminalContainer && (
        <TerminalDialog
          containerId={terminalContainer.Id}
          containerName={getContainerName(terminalContainer)}
          onClose={() => setTerminalContainer(null)}
          open={!!terminalContainer}
        />
      )}
    </Suspense>
  );
  if (viewMode === "card") {
    return (
      <div>
        {searchBar}
        {filtered.length === 0 ? (
          <div
            style={{
              textAlign: "center",
              paddingTop: theme.spacing(4),
              paddingBottom: theme.spacing(4),
            }}
          >
            <AppTypography color="text.secondary" variant="body2">
              No compose stacks found. Start containers with docker compose to
              see them here.
            </AppTypography>
          </div>
        ) : (
          <ReorderableCardGrid
            getId={getComposeProjectId}
            items={filtered}
            renderItem={(project) => (
              <ComposeStackCard
                isLoading={isLoading || isUpdatingContainer}
                onDelete={onDelete}
                onEdit={onEdit}
                onRestart={onRestart}
                onStart={onStart}
                onStop={onStop}
                project={project}
              />
            )}
            size={{ xs: 12, sm: 6, md: 4, lg: 2 }}
            surface={surface}
          />
        )}
        {containerDialogs}
      </div>
    );
  }
  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        height: "100%",
        minHeight: 0,
      }}
    >
      {searchBar}
      <AppDataTable
        ariaLabel="Docker compose stacks"
        columns={columns}
        data={filtered}
        dnd={tableDnd}
        emptyMessage="No compose stacks found. Start containers with docker compose to see them here."
        getRowId={(project) => project.name}
        renderExpandedContent={({ original: project }) =>
          renderExpandedContent(project)
        }
        style={{
          flex: "1 1 0",
          minHeight: 0,
        }}
      />
      {containerDialogs}
    </div>
  );
};
export default memo(ComposeList);
