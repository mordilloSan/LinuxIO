import { Icon } from "@iconify/react";
import { useQueryClient } from "@tanstack/react-query";
import React, { useCallback, useMemo, useState } from "react";

import ComposeStackCard from "../../../components/cards/ComposeStackCard";

import { jobSnapshotResult, linuxio } from "@/api";
import DockerIcon from "@/components/docker/DockerIcon";
import type { UnifiedTableColumn } from "@/components/tables/UnifiedCollapsibleTable";
import UnifiedCollapsibleTable from "@/components/tables/UnifiedCollapsibleTable";
import Chip from "@/components/ui/AppChip";
import AppGrid from "@/components/ui/AppGrid";
import AppIconButton from "@/components/ui/AppIconButton";
import AppSearchField from "@/components/ui/AppSearchField";
import {
  AppTable,
  AppTableBody,
  AppTableCell,
  AppTableHead,
  AppTableRow,
} from "@/components/ui/AppTable";
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
  const filtered = projects.filter((p) =>
    p.name.toLowerCase().includes(search.toLowerCase()),
  );
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
  const getStatusColor = (status: string) => {
    return getComposeStatusColor(status);
  };
  const getTotalContainers = (project: ComposeProject) => {
    if (project.containers?.length) return project.containers.length;
    return Object.values(project.services).reduce(
      (acc, service) => acc + service.container_count,
      0,
    );
  };
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

  // Table columns configuration
  const columns: UnifiedTableColumn[] = [
    {
      field: "status",
      headerName: "",
      width: "40px",
    },
    {
      field: "name",
      headerName: "Stack",
    },
    {
      field: "containers",
      headerName: "Containers",
      width: "100px",
      align: "center",
      className: "app-table-hide-below-sm",
    },
    {
      field: "config",
      headerName: "Config Files",
      className: "app-table-hide-below-sm",
    },
    {
      field: "location",
      headerName: "Location",
      className: "app-table-hide-below-lg",
    },
    {
      field: "actions",
      headerName: "Actions",
      align: "center",
      width: "200px",
    },
  ];

  // Render main row content
  const renderMainRow = useCallback(
    (project: ComposeProject) => {
      const statusColor = getStatusColor(project.status);
      return (
        <>
          <AppTableCell>
            <div
              style={{
                display: "flex",
                alignItems: "center",
              }}
            >
              <span
                style={{
                  display: isSmallUp ? "none" : "inline-block",
                  width: 10,
                  height: 10,
                  borderRadius: "50%",
                  backgroundColor: statusColor,
                }}
              />
              <Chip
                color={statusColor}
                label={project.status}
                size="small"
                className="app-table-hide-below-sm"
                style={{
                  textTransform: "capitalize",
                  fontSize: "0.68rem",
                }}
                labelStyle={{ paddingInline: 12 }}
                variant="soft"
              />
            </div>
          </AppTableCell>
          <AppTableCell>
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
          </AppTableCell>
          <AppTableCell align="center" className="app-table-hide-below-sm">
            {getTotalContainers(project)}
          </AppTableCell>
          <AppTableCell className="app-table-hide-below-sm">
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
                copyText={project.config_files.join(", ") || "Unknown"}
                noWrap
                style={{
                  maxWidth: 200,
                }}
                title={project.config_files.join(", ") || "Unknown"}
                toastMeta={DOCKER_TOAST_META}
                variant="body2"
              >
                {project.config_files[0]?.split("/").pop() ||
                  "docker-compose.yml"}
              </AppTypography>
            </div>
          </AppTableCell>
          <AppTableCell className="app-table-hide-below-lg">
            <AppTypography
              copyText={project.working_dir || "Unknown"}
              noWrap
              style={{
                maxWidth: 600,
                fontSize: "0.85rem",
                color: "var(--app-palette-text-secondary)",
              }}
              title={project.working_dir || "Unknown"}
              toastMeta={DOCKER_TOAST_META}
              variant="body2"
            >
              {project.working_dir || "-"}
            </AppTypography>
          </AppTableCell>
          <AppTableCell align="right">
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
          </AppTableCell>
        </>
      );
    },
    [isLoading, isSmallUp, onDelete, onEdit, onRestart, onStart, onStop, theme],
  );

  // Render expanded content
  const renderExpandedContent = useCallback(
    (project: ComposeProject) => {
      const containers = containersByProject.get(project.name) ?? [];
      return (
        <AppTable className="compose-expanded-table">
          <AppTableHead>
            <AppTableRow>
              <AppTableCell>Container Name</AppTableCell>
              <AppTableCell className="app-table-hide-below-md">
                Service
              </AppTableCell>
              <AppTableCell className="app-table-hide-below-sm">
                Image
              </AppTableCell>
              <AppTableCell>State</AppTableCell>
              <AppTableCell className="app-table-hide-below-md">
                Ports
              </AppTableCell>
              <AppTableCell align="right">Actions</AppTableCell>
            </AppTableRow>
          </AppTableHead>
          <AppTableBody>
            {containers.map((container) => {
              const name = getContainerName(container);
              const serviceName = getContainerServiceName(container);
              const ports = getDedupedContainerPorts(container);
              const portsText =
                ports.length > 0
                  ? ports.map(formatContainerPort).join(", ")
                  : "-";
              const displayState = getContainerDisplayState(container);
              return (
                <AppTableRow
                  className="compose-container-row"
                  key={container.Id}
                >
                  <AppTableCell>
                    <div className="compose-container-name">
                      <DockerIcon
                        alt={name}
                        identifier={container.icon}
                        size={24}
                      />
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
                  </AppTableCell>
                  <AppTableCell className="app-table-hide-below-md">
                    <AppTypography
                      copyText={serviceName}
                      noWrap
                      title={serviceName}
                      toastMeta={DOCKER_TOAST_META}
                      variant="body2"
                    >
                      {serviceName}
                    </AppTypography>
                  </AppTableCell>
                  <AppTableCell className="app-table-hide-below-sm">
                    <AppTypography
                      copyText={container.Image}
                      noWrap
                      style={{
                        maxWidth: 260,
                      }}
                      title={container.Image}
                      toastMeta={DOCKER_TOAST_META}
                      variant="body2"
                    >
                      {container.Image}
                    </AppTypography>
                  </AppTableCell>
                  <AppTableCell>
                    <Chip
                      color={getContainerStatusColor(displayState)}
                      label={displayState}
                      size="small"
                      style={{
                        textTransform: "capitalize",
                      }}
                      variant="soft"
                    />
                  </AppTableCell>
                  <AppTableCell className="app-table-hide-below-md">
                    <AppTypography
                      copyText={ports.length > 0 ? portsText : undefined}
                      noWrap
                      title={portsText}
                      toastMeta={DOCKER_TOAST_META}
                      variant="body2"
                    >
                      {portsText}
                    </AppTypography>
                  </AppTableCell>
                  <AppTableCell align="right">
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
                            onClick={() =>
                              void handleUpdateContainer(container)
                            }
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
                            <Icon
                              height={18}
                              icon="mdi:open-in-new"
                              width={18}
                            />
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
                  </AppTableCell>
                </AppTableRow>
              );
            })}
            {containers.length === 0 && (
              <AppTableRow>
                <AppTableCell colSpan={6}>
                  <AppTypography color="text.secondary" variant="body2">
                    No containers found for this stack.
                  </AppTypography>
                </AppTableCell>
              </AppTableRow>
            )}
          </AppTableBody>
        </AppTable>
      );
    },
    [
      containersByProject,
      handleRemoveContainer,
      handleRestartContainer,
      handleStartContainer,
      handleStopContainer,
      handleUpdateContainer,
      isLoading,
      isUpdatingContainer,
    ],
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
    <div>
      {searchBar}
      <UnifiedCollapsibleTable
        columns={columns}
        data={filtered}
        emptyMessage="No compose stacks found. Start containers with docker compose to see them here."
        getRowKey={(project) => project.name}
        renderExpandedContent={renderExpandedContent}
        renderMainRow={renderMainRow}
      />
      {containerDialogs}
    </div>
  );
};
export default ComposeList;
