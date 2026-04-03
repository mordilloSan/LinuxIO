import { Icon } from "@iconify/react";
import React, { useCallback, useState } from "react";

import ComposeStackCard from "../../../components/cards/ComposeStackCard";

import DockerIcon from "@/components/docker/DockerIcon";
import UnifiedCollapsibleTable from "@/components/tables/UnifiedCollapsibleTable";
import type { UnifiedTableColumn } from "@/components/tables/UnifiedCollapsibleTable";
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
import { getComposeStatusColor } from "@/constants/statusColors";
import { useAppTheme, useAppMediaQuery } from "@/theme";
import { isLinuxIOManagedComposeProject } from "@/utils/dockerManaged";
interface ComposeService {
  name: string;
  image: string;
  icon?: string;
  url?: string;
  status: string;
  state: string;
  container_count: number;
  container_ids: string[];
  ports: string[];
}
export interface ComposeProject {
  name: string;
  icon?: string;
  status: string; // "running", "partial", "stopped"
  auto_update?: boolean;
  services: Record<string, ComposeService>;
  config_files: string[];
  working_dir: string;
}
interface ComposeListProps {
  projects: ComposeProject[];
  onStart: (projectName: string) => void;
  onStop: (projectName: string) => void;
  onRestart: (projectName: string) => void;
  onDelete: (project: ComposeProject) => void;
  onEdit?: (projectName: string, configPath: string) => void;
  onPreview?: (projectName: string, configPath: string) => void;
  isLoading?: boolean;
  isPending?: boolean;
  viewMode?: "table" | "card";
}
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
  const getStatusColor = (status: string) => {
    return getComposeStatusColor(status);
  };
  const getTotalContainers = (project: ComposeProject) => {
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
                label={project.status}
                size="small"
                color={statusColor}
                variant="soft"
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
                identifier={project.icon}
                size={28}
                alt={project.name}
              />
              <AppTypography variant="body2" fontWeight={700}>
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
                  icon="mdi:folder-open"
                  width={20}
                  height={20}
                  style={{
                    marginRight: 4,
                    opacity: 0.7,
                  }}
                />
                <AppTypography
                  variant="body2"
                  noWrap
                  style={{
                    maxWidth: 200,
                  }}
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
                variant="body2"
                noWrap
                style={{
                  maxWidth: 600,
                  fontSize: "0.85rem",
                  color: "var(--mui-palette-text-secondary)",
                }}
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
                <AppTooltip title="View compose file" arrow>
                  <Chip
                    label="Managed by LinuxIO"
                    size="small"
                    variant="soft"
                    onClick={
                      onPreview && project.config_files.length > 0
                        ? () => onPreview(project.name, project.config_files[0])
                        : undefined
                    }
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
                  />
                </AppTooltip>
              ) : (
                <>
                  {onEdit && project.config_files.length > 0 && (
                    <AppTooltip title="Edit">
                      <AppIconButton
                        size="small"
                        onClick={() =>
                          onEdit(project.name, project.config_files[0])
                        }
                        disabled={isLoading}
                      >
                        <Icon icon="mdi:pencil" width={20} height={20} />
                      </AppIconButton>
                    </AppTooltip>
                  )}
                  {project.status === "running" ||
                  project.status === "partial" ? (
                    <>
                      <AppTooltip title="Restart">
                        <AppIconButton
                          size="small"
                          onClick={() => onRestart(project.name)}
                          disabled={isLoading}
                        >
                          <Icon icon="mdi:restart" width={20} height={20} />
                        </AppIconButton>
                      </AppTooltip>
                      <AppTooltip title="Stop">
                        <AppIconButton
                          size="small"
                          onClick={() => onStop(project.name)}
                          disabled={isLoading}
                        >
                          <Icon icon="mdi:stop-circle" width={20} height={20} />
                        </AppIconButton>
                      </AppTooltip>
                      <AppTooltip title="Delete">
                        <AppIconButton
                          size="small"
                          onClick={() => onDelete(project)}
                          disabled={isLoading}
                        >
                          <Icon icon="mdi:delete" width={20} height={20} />
                        </AppIconButton>
                      </AppTooltip>
                    </>
                  ) : (
                    <>
                      <AppTooltip title="Start">
                        <AppIconButton
                          size="small"
                          onClick={() => onStart(project.name)}
                          disabled={isLoading}
                        >
                          <Icon icon="mdi:play" width={20} height={20} />
                        </AppIconButton>
                      </AppTooltip>
                      <AppTooltip title="Delete">
                        <AppIconButton
                          size="small"
                          onClick={() => onDelete(project)}
                          disabled={isLoading}
                        >
                          <Icon icon="mdi:delete" width={20} height={20} />
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
      return (
        <>
          <AppTable>
            <AppTableHead>
              <AppTableRow>
                <AppTableCell>Service Name</AppTableCell>
                <AppTableCell className="app-table-hide-below-sm">
                  Image
                </AppTableCell>
                <AppTableCell>State</AppTableCell>
                <AppTableCell className="app-table-hide-below-md">
                  Containers
                </AppTableCell>
                <AppTableCell className="app-table-hide-below-md">
                  Ports
                </AppTableCell>
              </AppTableRow>
            </AppTableHead>
            <AppTableBody>
              {Object.values(project.services).map((service) => (
                <AppTableRow key={service.name}>
                  <AppTableCell>
                    <div
                      style={{
                        display: "flex",
                        alignItems: "center",
                        gap: theme.spacing(1),
                      }}
                    >
                      <DockerIcon
                        identifier={service.icon}
                        size={20}
                        alt={service.name}
                      />
                      {service.name}
                    </div>
                  </AppTableCell>
                  <AppTableCell className="app-table-hide-below-sm">
                    <AppTypography
                      variant="body2"
                      noWrap
                      style={{
                        maxWidth: 200,
                      }}
                    >
                      {service.image}
                    </AppTypography>
                  </AppTableCell>
                  <AppTableCell>
                    <Chip
                      label={service.state}
                      size="small"
                      color={
                        service.state === "running" ? "success" : "default"
                      }
                      variant="soft"
                      sx={{
                        textTransform: "capitalize",
                      }}
                    />
                  </AppTableCell>
                  <AppTableCell className="app-table-hide-below-md">
                    {service.container_count}
                  </AppTableCell>
                  <AppTableCell className="app-table-hide-below-md">
                    {service.ports.length > 0 ? service.ports.join(", ") : "-"}
                  </AppTableCell>
                </AppTableRow>
              ))}
            </AppTableBody>
          </AppTable>
          <div
            style={{
              marginTop: theme.spacing(2),
            }}
          >
            <AppTypography
              variant="body2"
              color="text.secondary"
              style={{
                wordBreak: "break-word",
                overflowWrap: "break-word",
              }}
            >
              <b>Working Directory:</b> {project.working_dir || "-"}
            </AppTypography>
            <AppTypography
              variant="body2"
              color="text.secondary"
              style={{
                wordBreak: "break-word",
                overflowWrap: "break-word",
              }}
            >
              <b>Config Files:</b> {project.config_files.join(", ") || "-"}
            </AppTypography>
          </div>
        </>
      );
    },
    [theme],
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
        placeholder="Search stacks…"
        value={search}
        onChange={(e) => setSearch(e.target.value)}
        style={{
          flex: isSmallUp ? "0 0 320px" : "1 1 auto",
          minWidth: 0,
          width: isSmallUp ? 320 : undefined,
        }}
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
            <AppTypography variant="body2" color="text.secondary">
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
                  project={project}
                  onStart={onStart}
                  onStop={onStop}
                  onRestart={onRestart}
                  onDelete={onDelete}
                  onEdit={onEdit}
                  onPreview={onPreview}
                  isLoading={isLoading}
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
        data={filtered}
        columns={columns}
        getRowKey={(project) => project.name}
        renderMainRow={renderMainRow}
        renderExpandedContent={renderExpandedContent}
        emptyMessage="No compose stacks found. Start containers with docker compose to see them here."
      />
    </div>
  );
};
export default ComposeList;
