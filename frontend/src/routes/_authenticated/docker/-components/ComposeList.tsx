import { Icon } from "@iconify/react";
import { getRouteApi } from "@tanstack/react-router";
import { lazy, memo, Suspense, useCallback, useMemo, useState } from "react";

import type { ComposeProject, ContainerInfo, ContainerPort } from "@/api";
import ComposeStackCard from "@/components/cards/ComposeStackCard";
import ContainerActions from "@/components/docker/ContainerActions";
import DockerIcon from "@/components/docker/DockerIcon";
import DockerResourceDetailsLayout from "@/components/docker/DockerResourceDetailsLayout";
import ReorderableCardGrid from "@/components/reorder/ReorderableCardGrid";
import { RoutedTabSearch } from "@/components/tabbar";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import Chip from "@/components/ui/AppChip";
import AppHeaderSearch from "@/components/ui/AppHeaderSearch";
import AppTypography from "@/components/ui/AppTypography";
import StatusDot from "@/components/ui/StatusDot";
import {
  getComposeStatusColor,
  getContainerStatusColor,
} from "@/constants/statusColors";
import { useFocusedResourceParam } from "@/hooks/useFocusedResourceParam";
import { useReorderableSurface } from "@/hooks/useReorderableSurface";
import { useReorderableTableDnd } from "@/hooks/useReorderableTableDnd";
import { useAppMediaQuery } from "@/theme";
import { up } from "@/theme/breakpoints";
import { CARD_GRID_SIZE_DENSE } from "@/theme/constants";
import {
  getContainerDisplayState,
  getContainerName,
  getDedupedPorts,
} from "@/utils/dockerContainer";

import "./compose-list.css";

const LogsDialog = lazy(() => import("@/components/docker/LogsDialog"));
const TerminalDialog = lazy(() => import("@/components/docker/TerminalDialog"));

const DOCKER_TOAST_META = { label: "Open Docker", to: "/docker" } as const;
const dockerRouteApi = getRouteApi("/_authenticated/docker/compose");

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

const getContainerServiceName = (container: ContainerInfo) =>
  container.Labels?.["com.docker.compose.service"] || "-";

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

// Hoisted so AppVirtualTable's row comparator (which compares
// `getRowAttributes` by reference) sees a stable function instead of a new
// closure on every renderStackContainers call.
const getComposeContainerRowAttributes = () => ({
  className: "compose-container-row",
});

interface ComposeContainerActionsProps {
  container: ContainerInfo;
  disabled: boolean;
  onOpenLogs: (container: ContainerInfo) => void;
  onOpenTerminal: (container: ContainerInfo) => void;
}

const ComposeContainerActions = memo(function ComposeContainerActions({
  container,
  disabled,
  onOpenLogs,
  onOpenTerminal,
}: ComposeContainerActionsProps) {
  const name = getContainerName(container);

  return (
    <div
      aria-label={`Actions for ${name}`}
      className="compose-container-actions"
      role="group"
    >
      <ContainerActions
        actionPending={disabled}
        container={container}
        name={name}
        onOpenLogs={() => onOpenLogs(container)}
        onOpenTerminal={() => onOpenTerminal(container)}
      />
    </div>
  );
});

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
  const navigate = dockerRouteApi.useNavigate();
  const searchParams = dockerRouteApi.useSearch();
  const focusedProjectName =
    typeof searchParams.stack === "string" ? searchParams.stack : undefined;
  const [search, setSearch] = useState("");
  const [logsContainer, setLogsContainer] = useState<ContainerInfo | null>(
    null,
  );
  const [logsDialogOpen, setLogsDialogOpen] = useState(false);
  const [terminalContainer, setTerminalContainer] =
    useState<ContainerInfo | null>(null);
  const [terminalDialogOpen, setTerminalDialogOpen] = useState(false);
  const isSmallUp = useAppMediaQuery(up("sm"));
  const surface = useReorderableSurface({
    getId: getComposeProjectId,
    items: projects,
    surface: "docker.stacks",
  });
  const updateFocusedProject = useCallback(
    (projectName: string | null) => {
      void navigate({
        to: "/docker/compose",
        search: (previous) => ({
          ...previous,
          stack: projectName ?? undefined,
        }),
      });
    },
    [navigate],
  );
  const tableDnd = useReorderableTableDnd<ComposeProject, ComposeProject>({
    handleAriaLabel: "Reorder stack",
    surface,
  });
  const orderedProjects = surface.items;
  const focusedProject = useFocusedResourceParam({
    focusedId: focusedProjectName,
    getId: getComposeProjectId,
    items: orderedProjects,
    onClear: () => updateFocusedProject(null),
  });
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

  const columns = useMemo<AppVirtualTableColumnDef<ComposeProject>[]>(
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
                  size="xsmall"
                  style={{
                    textTransform: "capitalize",
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
                gap: "var(--app-space-6)",
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
                  size="xsmall"
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
                gap: isSmallUp ? "var(--app-space-2)" : 0,
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
                    icon="mdi:stop"
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
    [isLoading, isSmallUp, onDelete, onEdit, onRestart, onStart, onStop],
  );

  const expandedContainerColumns = useMemo<
    AppVirtualTableColumnDef<ContainerInfo>[]
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
                    size="xsmall"
                    style={{ marginTop: 2 }}
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
          const ports = getDedupedPorts(row.original);
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
              getDedupedPorts(container)
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
            <ComposeContainerActions
              container={container}
              disabled={isLoading}
              onOpenLogs={(container) => {
                setLogsContainer(container);
                setLogsDialogOpen(true);
              }}
              onOpenTerminal={(container) => {
                setTerminalContainer(container);
                setTerminalDialogOpen(true);
              }}
            />
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
    [isLoading],
  );

  const renderStackContainers = useCallback(
    (project: ComposeProject) => {
      const containers = containersByProject.get(project.name) ?? [];

      return (
        <AppVirtualTable
          ariaLabel={`Containers in ${project.name}`}
          className="compose-expanded-table"
          columns={expandedContainerColumns}
          data={containers}
          density="compact"
          emptyMessage="No containers found for this stack."
          enableSorting={false}
          fillAvailable={false}
          getRowAttributes={getComposeContainerRowAttributes}
          getRowId={(container) => container.Id}
          maxHeight={260}
          variant="embedded"
        />
      );
    },
    [containersByProject, expandedContainerColumns],
  );
  const searchControl = (
    <RoutedTabSearch active={search !== ""}>
      <AppHeaderSearch
        clearOnDocumentEscape
        onChange={setSearch}
        placeholder="Search stacks…"
        value={search}
      />
    </RoutedTabSearch>
  );
  const containerDialogs = (
    <Suspense fallback={null}>
      {logsContainer && (
        <LogsDialog
          containerId={logsContainer.Id}
          containerName={getContainerName(logsContainer)}
          onClose={() => setLogsDialogOpen(false)}
          onExited={() => setLogsContainer(null)}
          open={logsDialogOpen}
        />
      )}
      {terminalContainer && (
        <TerminalDialog
          containerId={terminalContainer.Id}
          containerName={getContainerName(terminalContainer)}
          onClose={() => setTerminalDialogOpen(false)}
          onExited={() => setTerminalContainer(null)}
          open={terminalDialogOpen}
        />
      )}
    </Suspense>
  );
  if (focusedProject) {
    return (
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          height: "100%",
          minHeight: 0,
        }}
      >
        <DockerResourceDetailsLayout
          onClose={() => updateFocusedProject(null)}
          resourceLabel="stack"
          subtitle={`${focusedProject.status} · ${getTotalContainers(focusedProject)} containers`}
          summary={
            <ComposeStackCard
              isLoading={isLoading}
              onDelete={onDelete}
              onEdit={onEdit}
              onRestart={onRestart}
              onStart={onStart}
              onStop={onStop}
              project={focusedProject}
              selected
            />
          }
          title={focusedProject.name}
        >
          {renderStackContainers(focusedProject)}
        </DockerResourceDetailsLayout>
        {containerDialogs}
      </div>
    );
  }
  if (viewMode === "card") {
    return (
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          height: "100%",
          minHeight: 0,
        }}
      >
        {searchControl}
        {filtered.length === 0 ? (
          <div
            style={{
              textAlign: "center",
              paddingTop: "var(--app-space-16)",
              paddingBottom: "var(--app-space-16)",
            }}
          >
            <AppTypography color="text.secondary" variant="body2">
              No compose stacks found. Start containers with docker compose to
              see them here.
            </AppTypography>
          </div>
        ) : (
          <ReorderableCardGrid
            fillAvailable
            getId={getComposeProjectId}
            items={filtered}
            renderItem={(project) => (
              <ComposeStackCard
                isLoading={isLoading}
                onDelete={onDelete}
                onEdit={onEdit}
                onRestart={onRestart}
                onStart={onStart}
                onStop={onStop}
                onOpen={
                  surface.editMode
                    ? undefined
                    : () => updateFocusedProject(project.name)
                }
                project={project}
              />
            )}
            size={CARD_GRID_SIZE_DENSE}
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
      {searchControl}
      <AppVirtualTable
        ariaLabel="Docker compose stacks"
        columns={columns}
        data={filtered}
        dnd={tableDnd}
        emptyMessage="No compose stacks found. Start containers with docker compose to see them here."
        fillAvailable
        getRowId={(project) => project.name}
        getRowCanExpand={(row) =>
          (containersByProject.get(row.original.name)?.length ?? 0) > 0
        }
        onRowClick={
          surface.editMode
            ? undefined
            : ({ original: project }) => updateFocusedProject(project.name)
        }
        persistExpandedKey="compose-stacks"
        renderExpandedContent={({ original: project }) =>
          renderStackContainers(project)
        }
      />
      {containerDialogs}
    </div>
  );
};
export default memo(ComposeList);
