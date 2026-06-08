import { Icon } from "@iconify/react";
import React, { useCallback, useMemo, useState } from "react";

import ComposeStackCard from "../../../components/cards/ComposeStackCard";

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
import { useAppMediaQuery, useAppTheme } from "@/theme";
import type { ContainerInfo, ContainerPort } from "@/types/container";
import { isLinuxIOManagedComposeProject } from "@/utils/dockerManaged";

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
  auto_update?: boolean;
  config_files: string[];
  containers: ContainerInfo[];
  icon?: string;
  name: string;
  services: Record<string, ComposeService>;
  status: string; // "running", "partial", "stopped"
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
  const theme = useAppTheme();
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
                sx={{
                  display: {
                    xs: "none",
                    sm: "inline-flex",
                  },
                  textTransform: "capitalize",
                  fontSize: "0.68rem",
                  "& .MuiChip-label": {
                    px: 3,
                  },
                }}
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
              <AppTypography fontWeight={700} variant="body2">
                {project.name}
              </AppTypography>
            </div>
          </AppTableCell>
          <AppTableCell align="center" className="app-table-hide-below-sm">
            {getTotalContainers(project)}
          </AppTableCell>
          <AppTableCell className="app-table-hide-below-sm">
            <AppTooltip title={project.config_files.join(", ") || "Unknown"}>
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
                  noWrap
                  style={{
                    maxWidth: 200,
                  }}
                  variant="body2"
                >
                  {project.config_files[0]?.split("/").pop() ||
                    "docker-compose.yml"}
                </AppTypography>
              </div>
            </AppTooltip>
          </AppTableCell>
          <AppTableCell className="app-table-hide-below-lg">
            <AppTooltip title={project.working_dir || "Unknown"}>
              <AppTypography
                noWrap
                style={{
                  maxWidth: 600,
                  fontSize: "0.85rem",
                  color: "var(--mui-palette-text-secondary)",
                }}
                variant="body2"
              >
                {project.working_dir || "-"}
              </AppTypography>
            </AppTooltip>
          </AppTableCell>
          <AppTableCell align="right">
            <div
              style={{
                display: "flex",
                justifyContent: "flex-end",
                gap: isSmallUp ? theme.spacing(0.5) : 0,
              }}
            >
              {isLinuxIOManagedComposeProject(project.name) ? (
                <AppTooltip arrow title="View compose file">
                  <Chip
                    label="Managed by LinuxIO"
                    onClick={
                      onPreview && project.config_files.length > 0
                        ? () => onPreview(project.name, project.config_files[0])
                        : undefined
                    }
                    size="small"
                    sx={{
                      fontSize: "0.68rem",
                      opacity: 0.7,
                      cursor:
                        onPreview && project.config_files.length > 0
                          ? "pointer"
                          : "default",
                      "&:hover": {
                        opacity: 1,
                      },
                    }}
                    variant="soft"
                  />
                </AppTooltip>
              ) : (
                <>
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
                  {project.status === "running" ||
                  project.status === "partial" ? (
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
                      <AppTooltip title="Delete">
                        <AppIconButton
                          disabled={isLoading}
                          onClick={() => onDelete(project)}
                          size="small"
                        >
                          <Icon height={20} icon="mdi:delete" width={20} />
                        </AppIconButton>
                      </AppTooltip>
                    </>
                  ) : (
                    <>
                      <AppTooltip title="Start">
                        <AppIconButton
                          disabled={isLoading}
                          onClick={() => onStart(project.name)}
                          size="small"
                        >
                          <Icon height={20} icon="mdi:play" width={20} />
                        </AppIconButton>
                      </AppTooltip>
                      <AppTooltip title="Delete">
                        <AppIconButton
                          disabled={isLoading}
                          onClick={() => onDelete(project)}
                          size="small"
                        >
                          <Icon height={20} icon="mdi:delete" width={20} />
                        </AppIconButton>
                      </AppTooltip>
                    </>
                  )}
                </>
              )}
            </div>
          </AppTableCell>
        </>
      );
    },
    [
      isLoading,
      isSmallUp,
      onDelete,
      onEdit,
      onPreview,
      onRestart,
      onStart,
      onStop,
      theme,
    ],
  );

  // Render expanded content
  const renderExpandedContent = useCallback(
    (project: ComposeProject) => {
      const containers = containersByProject.get(project.name) ?? [];
      return (
        <>
          <AppTable>
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
              </AppTableRow>
            </AppTableHead>
            <AppTableBody>
              {containers.map((container) => {
                const name = getContainerName(container);
                const serviceName = getContainerServiceName(container);
                const ports = getDedupedContainerPorts(container);
                const displayState = getContainerDisplayState(container);
                return (
                  <AppTableRow key={container.Id}>
                    <AppTableCell>
                      <div
                        style={{
                          display: "flex",
                          alignItems: "center",
                          gap: theme.spacing(1),
                        }}
                      >
                        <DockerIcon
                          alt={name}
                          identifier={container.icon}
                          size={20}
                        />
                        {name}
                      </div>
                    </AppTableCell>
                    <AppTableCell className="app-table-hide-below-md">
                      {serviceName}
                    </AppTableCell>
                    <AppTableCell className="app-table-hide-below-sm">
                      <AppTypography
                        noWrap
                        style={{
                          maxWidth: 260,
                        }}
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
                        sx={{
                          textTransform: "capitalize",
                        }}
                        variant="soft"
                      />
                    </AppTableCell>
                    <AppTableCell className="app-table-hide-below-md">
                      {ports.length > 0
                        ? ports.map(formatContainerPort).join(", ")
                        : "-"}
                    </AppTableCell>
                  </AppTableRow>
                );
              })}
              {containers.length === 0 && (
                <AppTableRow>
                  <AppTableCell>
                    <AppTypography color="text.secondary" variant="body2">
                      No containers found for this stack.
                    </AppTypography>
                  </AppTableCell>
                </AppTableRow>
              )}
            </AppTableBody>
          </AppTable>
          <div
            style={{
              marginTop: theme.spacing(2),
            }}
          >
            <AppTypography
              color="text.secondary"
              style={{
                wordBreak: "break-word",
                overflowWrap: "break-word",
              }}
              variant="body2"
            >
              <b>Working Directory:</b> {project.working_dir || "-"}
            </AppTypography>
            <AppTypography
              color="text.secondary"
              style={{
                wordBreak: "break-word",
                overflowWrap: "break-word",
              }}
              variant="body2"
            >
              <b>Config Files:</b> {project.config_files.join(", ") || "-"}
            </AppTypography>
          </div>
        </>
      );
    },
    [containersByProject, theme],
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
                  isLoading={isLoading}
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
    </div>
  );
};
export default ComposeList;
