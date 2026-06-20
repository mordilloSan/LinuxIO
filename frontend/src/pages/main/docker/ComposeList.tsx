import { Icon } from "@iconify/react";
import { useQueryClient } from "@tanstack/react-query";
import React, { useCallback, useMemo, useState } from "react";

import ComposeStackCard from "../../../components/cards/ComposeStackCard";

import { jobSnapshotResult, linuxio } from "@/api";
import DockerIcon from "@/components/docker/DockerIcon";
import AppDataTable from "@/components/tables/AppDataTable";
import type { AppDataTableColumnDef } from "@/components/tables/AppDataTable";
import Chip from "@/components/ui/AppChip";
import AppGrid from "@/components/ui/AppGrid";
import AppIconButton from "@/components/ui/AppIconButton";
import AppSearchField from "@/components/ui/AppSearchField";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import {
  getComposeStatusColor,
  getContainerStatusColor,
} from "@/constants/statusColors";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useAppMediaQuery, useAppTheme } from "@/theme";
import type { ContainerInfo, ContainerPort } from "@/types/container";
import { getMutationErrorMessage } from "@/utils/mutations";

import "./compose-list.css";

const LogsDialog = React.lazy(() => import("./LogsDialog"));
const TerminalDialog = React.lazy(() => import("./TerminalDialog"));

const DOCKER_TOAST_META = { href: "/docker", label: "Open Docker" };

interface ComposeService {
  container_count: number;
  container_ids: string[];
  icon?: string;
  image: string;
  name: string;
  ports: string[];
  state: string;
  status: string;
  url?: string;
}
export interface ComposeProject {
  config_files: string[];
  containers: ContainerInfo[];
  icon?: string;
  name: string;
  services: Record<string, ComposeService>;
  status: string; // "running", "partial", "stopped"
  update_available: boolean;
  working_dir: string;
}
interface ComposeListProps {
  isLoading?: boolean;
  isPending?: boolean;
  onDelete: (project: ComposeProject) => void;
  onEdit?: (projectName: string, configPath: string) => void;
  onPreview?: (projectName: string, configPath: string) => void;
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

const ComposeList: React.FC<ComposeListProps> = ({
  projects,
  onStart,
  onStop,
  onRestart,
  onDelete,
  onEdit,
  onPreview,
  isLoading = false,
  isPending = false,
  viewMode = "table",
}) => {
  const [search, setSearch] = useState("");
  const [logsContainer, setLogsContainer] = useState<ContainerInfo | null>(
    null,
  );
  const [terminalContainer, setTerminalContainer] =
    useState<ContainerInfo | null>(null);
  const theme = useAppTheme();
  const queryClient = useQueryClient();
  const toast = useScopedToast(DOCKER_TOAST_META);
  const isSmallUp = useAppMediaQuery(theme.breakpoints.up("sm"));
  const filtered = useMemo(() => {
    const normalizedSearch = search.trim().toLowerCase();
    if (!normalizedSearch) return projects;

    return projects.filter((project) =>
      project.name.toLowerCase().includes(normalizedSearch),
    );
  }, [projects, search]);
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
  const { mutateAsync: startContainer } =
    linuxio.docker.start_container.useMutation();
  const { mutateAsync: stopContainer } =
    linuxio.docker.stop_container.useMutation();
  const { mutateAsync: restartContainer } =
    linuxio.docker.restart_container.useMutation();
  const { mutateAsync: removeContainer } =
    linuxio.docker.remove_container.useMutation();
  const { mutateAsync: updateContainer, isPending: isUpdatingContainer } =
    linuxio.docker.update_container.useMutation();

  const refreshContainerViews = useCallback(() => {
    queryClient.invalidateQueries({
      queryKey: linuxio.docker.list_containers.queryKey(),
    });
    queryClient.invalidateQueries({
      queryKey: linuxio.docker.list_compose_projects.queryKey(),
    });
    queryClient.invalidateQueries({
      queryKey: linuxio.docker.list_images.queryKey(),
    });
  }, [queryClient]);

  const handleStartContainer = useCallback(
    async (container: ContainerInfo) => {
      const name = getContainerName(container);
      try {
        await startContainer({ containerId: container.Id });
        toast.success(`Container ${name} started`);
        refreshContainerViews();
      } catch (error) {
        toast.error(getMutationErrorMessage(error, `Failed to start ${name}`));
      }
    },
    [refreshContainerViews, startContainer, toast],
  );

  const handleStopContainer = useCallback(
    async (container: ContainerInfo) => {
      const name = getContainerName(container);
      try {
        await stopContainer({ containerId: container.Id });
        toast.success(`Container ${name} stopped`);
        refreshContainerViews();
      } catch (error) {
        toast.error(getMutationErrorMessage(error, `Failed to stop ${name}`));
      }
    },
    [refreshContainerViews, stopContainer, toast],
  );

  const handleRestartContainer = useCallback(
    async (container: ContainerInfo) => {
      const name = getContainerName(container);
      try {
        await restartContainer({ containerId: container.Id });
        toast.success(`Container ${name} restarted`);
        refreshContainerViews();
      } catch (error) {
        toast.error(
          getMutationErrorMessage(error, `Failed to restart ${name}`),
        );
      }
    },
    [refreshContainerViews, restartContainer, toast],
  );

  const handleRemoveContainer = useCallback(
    async (container: ContainerInfo) => {
      const name = getContainerName(container);
      try {
        await removeContainer({ containerId: container.Id });
        toast.success(`Container ${name} removed`);
        refreshContainerViews();
      } catch (error) {
        toast.error(getMutationErrorMessage(error, `Failed to remove ${name}`));
      }
    },
    [refreshContainerViews, removeContainer, toast],
  );

  const handleUpdateContainer = useCallback(
    async (container: ContainerInfo) => {
      const name = getContainerName(container);
      try {
        const data = await updateContainer({ containerId: container.Id });
        const result = jobSnapshotResult<{ updated: boolean }>(data);
        toast.success(
          result.updated
            ? `Container ${name} updated`
            : `Container ${name} is already up to date`,
        );
        refreshContainerViews();
      } catch (error) {
        toast.error(getMutationErrorMessage(error, `Failed to update ${name}`));
      }
    },
    [refreshContainerViews, toast, updateContainer],
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
                <span
                  style={{
                    display: "inline-block",
                    width: 10,
                    height: 10,
                    borderRadius: "50%",
                    backgroundColor: statusColor,
                  }}
                />
              )}
            </div>
          );
        },
        meta: { width: isSmallUp ? "106px" : "40px" },
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
                copyText={project.name}
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
        meta: { align: "left" },
      },
      {
        id: "containers",
        header: "Containers",
        accessorFn: (project) => getTotalContainers(project),
        cell: ({ row }) => getTotalContainers(row.original),
        meta: {
          align: "center",
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
                <AppTooltip title="Edit">
                  <AppIconButton
                    disabled={isLoading}
                    onClick={() =>
                      onEdit(project.name, project.config_files[0])
                    }
                    size="small"
                  >
                    <Icon height={20} icon="mdi:pencil" width={20} />
                  </AppIconButton>
                </AppTooltip>
              )}
              {project.status === "running" || project.status === "partial" ? (
                <>
                  <AppTooltip title="Restart">
                    <AppIconButton
                      disabled={isLoading}
                      onClick={() => onRestart(project.name)}
                      size="small"
                    >
                      <Icon height={20} icon="mdi:restart" width={20} />
                    </AppIconButton>
                  </AppTooltip>
                  <AppTooltip title="Stop">
                    <AppIconButton
                      disabled={isLoading}
                      onClick={() => onStop(project.name)}
                      size="small"
                    >
                      <Icon height={20} icon="mdi:stop-circle" width={20} />
                    </AppIconButton>
                  </AppTooltip>
                </>
              ) : (
                <AppTooltip title="Start">
                  <AppIconButton
                    disabled={isLoading}
                    onClick={() => onStart(project.name)}
                    size="small"
                  >
                    <Icon height={20} icon="mdi:play" width={20} />
                  </AppIconButton>
                </AppTooltip>
              )}
              <AppTooltip title="Delete">
                <AppIconButton
                  disabled={isLoading}
                  onClick={() => onDelete(project)}
                  size="small"
                >
                  <Icon height={20} icon="mdi:delete" width={20} />
                </AppIconButton>
              </AppTooltip>
            </div>
          );
        },
        meta: {
          align: "right",
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
                  copyText={name}
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
      },
      {
        id: "service",
        header: "Service",
        cell: ({ row }) => {
          const serviceName = getContainerServiceName(row.original);

          return (
            <AppTypography
              copyText={serviceName}
              noWrap
              title={serviceName}
              toastMeta={DOCKER_TOAST_META}
              variant="body2"
            >
              {serviceName}
            </AppTypography>
          );
        },
        meta: { hideBelow: "md" },
      },
      {
        accessorKey: "Image",
        header: "Image",
        cell: ({ row }) => (
          <AppTypography
            copyText={row.original.Image}
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
        meta: { hideBelow: "sm" },
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
              copyText={ports.length > 0 ? portsText : undefined}
              noWrap
              title={portsText}
              toastMeta={DOCKER_TOAST_META}
              variant="body2"
            >
              {portsText}
            </AppTypography>
          );
        },
        meta: { hideBelow: "md" },
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
                <AppTooltip title="Start container">
                  <AppIconButton
                    disabled={isLoading}
                    onClick={() => void handleStartContainer(container)}
                    size="small"
                  >
                    <Icon height={18} icon="mdi:play" width={18} />
                  </AppIconButton>
                </AppTooltip>
              )}
              {container.State === "running" && (
                <AppTooltip title="Stop container">
                  <AppIconButton
                    disabled={isLoading}
                    onClick={() => void handleStopContainer(container)}
                    size="small"
                  >
                    <Icon height={18} icon="mdi:stop" width={18} />
                  </AppIconButton>
                </AppTooltip>
              )}
              <AppTooltip title="Restart container">
                <AppIconButton
                  disabled={isLoading}
                  onClick={() => void handleRestartContainer(container)}
                  size="small"
                >
                  <Icon height={18} icon="mdi:restart" width={18} />
                </AppIconButton>
              </AppTooltip>
              {container.updateAvailable && (
                <AppTooltip title="Update container">
                  <AppIconButton
                    disabled={isLoading || isUpdatingContainer}
                    onClick={() => void handleUpdateContainer(container)}
                    size="small"
                  >
                    <Icon height={18} icon="mdi:update" width={18} />
                  </AppIconButton>
                </AppTooltip>
              )}
              <AppTooltip title="View logs">
                <AppIconButton
                  disabled={isLoading}
                  onClick={() => setLogsContainer(container)}
                  size="small"
                >
                  <Icon
                    height={18}
                    icon="mdi:file-document-outline"
                    width={18}
                  />
                </AppIconButton>
              </AppTooltip>
              {container.State === "running" && (
                <AppTooltip title="Open terminal">
                  <AppIconButton
                    disabled={isLoading}
                    onClick={() => setTerminalContainer(container)}
                    size="small"
                  >
                    <Icon height={18} icon="mdi:console" width={18} />
                  </AppIconButton>
                </AppTooltip>
              )}
              {container.url && (
                <AppTooltip title="Open app">
                  <AppIconButton
                    disabled={isLoading}
                    onClick={() =>
                      window.open(container.url, "_blank", "noopener")
                    }
                    size="small"
                  >
                    <Icon height={18} icon="mdi:open-in-new" width={18} />
                  </AppIconButton>
                </AppTooltip>
              )}
              <AppTooltip title="Remove container">
                <AppIconButton
                  disabled={isLoading}
                  onClick={() => void handleRemoveContainer(container)}
                  size="small"
                >
                  <Icon height={18} icon="mdi:delete" width={18} />
                </AppIconButton>
              </AppTooltip>
            </div>
          );
        },
        meta: {
          align: "right",
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
        {isPending ? "Loading..." : `${filtered.length} shown`}
      </AppTypography>
    </div>
  );
  const containerDialogs = (
    <React.Suspense fallback={null}>
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
    </React.Suspense>
  );
  if (viewMode === "card") {
    const skeletonCount = 8;

    return (
      <div>
        {searchBar}
        {isPending ? (
          <AppGrid container spacing={2}>
            {Array.from({ length: skeletonCount }, (_, index) => (
              <AppGrid
                key={`compose-stack-skeleton-${index}`}
                size={{
                  xs: 12,
                  sm: 6,
                  md: 4,
                  lg: 2,
                }}
              >
                <ComposeStackCard isPending />
              </AppGrid>
            ))}
          </AppGrid>
        ) : filtered.length === 0 ? (
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
          <AppGrid container spacing={2}>
            {filtered.map((project) => (
              <AppGrid
                key={project.name}
                size={{
                  xs: 12,
                  sm: 6,
                  md: 4,
                  lg: 2,
                }}
              >
                <ComposeStackCard
                  isLoading={isLoading || isUpdatingContainer}
                  onDelete={onDelete}
                  onEdit={onEdit}
                  onPreview={onPreview}
                  onRestart={onRestart}
                  onStart={onStart}
                  onStop={onStop}
                  project={project}
                />
              </AppGrid>
            ))}
          </AppGrid>
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
export default React.memo(ComposeList);
