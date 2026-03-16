import { Icon } from "@iconify/react";
import {
  Divider,
  ListItemIcon,
  Menu,
  MenuItem,
  useMediaQuery,
  useTheme,
} from "@mui/material";
import { useQueryClient } from "@tanstack/react-query";
import React, { Suspense, useCallback, useMemo, useState } from "react";
import { toast } from "sonner";

import { linuxio } from "@/api";
import DashboardCard from "@/components/cards/DashboardCard";
import DockerIcon from "@/components/docker/DockerIcon";
import ErrorMessage from "@/components/errors/Error";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { getMutationErrorMessage } from "@/utils/mutations";
const LogsDialog = React.lazy(() => import("@/pages/main/docker/LogsDialog"));
const TerminalDialog = React.lazy(
  () => import("@/pages/main/docker/TerminalDialog"),
);
const cleanName = (name: string) => name.replace(/^\//, "");
const getStatusLabel = (status: string, state: string): string => {
  const health = status.match(/\((\w+)\)/)?.[1];
  if (health === "healthy" || health === "unhealthy") return health;
  return state;
};
const getCollectionCount = <T,>(items: T[]) => items.length;
const DockerInfo: React.FC = () => {
  const theme = useTheme();
  const isSmallUp = useMediaQuery(theme.breakpoints.up("sm"));
  const queryClient = useQueryClient();
  const [menuAnchor, setMenuAnchor] = useState<null | HTMLElement>(null);
  const [menuContainer, setMenuContainer] = useState<{
    id: string;
    name: string;
    state: string;
  } | null>(null);
  const [logsOpen, setLogsOpen] = useState(false);
  const [terminalOpen, setTerminalOpen] = useState(false);
  const [hasLoadedLogsDialog, setHasLoadedLogsDialog] = useState(false);
  const [hasLoadedTerminalDialog, setHasLoadedTerminalDialog] = useState(false);
  const [dialogContainer, setDialogContainer] = useState<{
    id: string;
    name: string;
  } | null>(null);
  const resolveStateColor = useCallback(
    (state: string) => {
      switch (state) {
        case "running":
        case "healthy":
          return theme.palette.success.main;
        case "exited":
        case "unhealthy":
          return theme.palette.error.main;
        case "paused":
          return theme.palette.warning.main;
        case "restarting":
          return theme.palette.info.main;
        default:
          return theme.palette.grey[500];
      }
    },
    [theme],
  );
  const invalidateContainers = useCallback(
    () =>
      queryClient.invalidateQueries({
        queryKey: linuxio.docker.list_containers.queryKey(),
      }),
    [queryClient],
  );
  const { mutate: startContainer } =
    linuxio.docker.start_container.useMutation();
  const { mutate: stopContainer } = linuxio.docker.stop_container.useMutation();
  const { mutate: restartContainer } =
    linuxio.docker.restart_container.useMutation();
  const { mutate: removeContainer } =
    linuxio.docker.remove_container.useMutation();
  const handleContextMenu = useCallback(
    (
      e: React.MouseEvent<HTMLElement>,
      id: string,
      name: string,
      state: string,
    ) => {
      e.preventDefault();
      setMenuAnchor(e.currentTarget);
      setMenuContainer({
        id,
        name,
        state,
      });
    },
    [],
  );
  const handleMenuClose = useCallback(() => {
    setMenuAnchor(null);
    setMenuContainer(null);
  }, []);
  const handleAction = useCallback(
    (action: "start" | "stop" | "restart" | "remove") => {
      if (!menuContainer) return;
      const { id, name } = menuContainer;
      const args = [id];
      const callbacks = {
        onSuccess: () => {
          toast.success(
            `Container ${name} ${action === "remove" ? "removed" : `${action}ed`}`,
          );
          invalidateContainers();
        },
        onError: (e: Error) => {
          toast.error(
            getMutationErrorMessage(e, `Failed to ${action} container`),
          );
        },
      };
      if (action === "start") startContainer(args, callbacks);
      else if (action === "stop") stopContainer(args, callbacks);
      else if (action === "restart") restartContainer(args, callbacks);
      else removeContainer(args, callbacks);
      handleMenuClose();
    },
    [
      menuContainer,
      startContainer,
      stopContainer,
      restartContainer,
      removeContainer,
      handleMenuClose,
      invalidateContainers,
    ],
  );
  const {
    data: containers = [],
    isPending: isContainersLoading,
    isError: isContainersError,
  } = linuxio.docker.list_containers.useQuery({
    refetchInterval: 5000,
  });
  const { data: imagesCount = 0 } =
    linuxio.docker.list_images.useQueryWithSelect({
      refetchInterval: 30_000,
      select: getCollectionCount,
    });
  const { data: networksCount = 0 } =
    linuxio.docker.list_networks.useQueryWithSelect({
      refetchInterval: 30_000,
      select: getCollectionCount,
    });
  const { data: volumesCount = 0 } =
    linuxio.docker.list_volumes.useQueryWithSelect({
      refetchInterval: 30_000,
      select: getCollectionCount,
    });
  const runningCount = useMemo(
    () => containers.filter((c) => c.State === "running").length,
    [containers],
  );
  const sorted = useMemo(
    () =>
      [...containers].sort((a, b) => {
        if (a.State === "running" && b.State !== "running") return -1;
        if (a.State !== "running" && b.State === "running") return 1;
        return 0;
      }),
    [containers],
  );
  const stats = (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        alignSelf: "flex-start",
        width: "fit-content",
      }}
    >
      {[
        {
          label: "Containers",
          value: `${runningCount}/${containers.length}`,
        },
        {
          label: "Images",
          value: imagesCount,
        },
        {
          label: "Networks",
          value: networksCount,
        },
        {
          label: "Volumes",
          value: volumesCount,
        },
      ].map(({ label, value }, index, rows) => (
        <div
          key={label}
          style={{
            display: "flex",
            alignItems: "baseline",
            justifyContent: "flex-start",
            paddingTop: theme.spacing(0.5),
            paddingBottom: theme.spacing(0.5),
            borderBottom:
              index === rows.length - 1
                ? "none"
                : "1px solid var(--mui-palette-divider)",
            gap: theme.spacing(1),
          }}
        >
          <AppTypography
            variant="caption"
            color="text.secondary"
            style={{
              textTransform: "uppercase",
              letterSpacing: "0.06em",
              fontSize: "0.62rem",
              flexShrink: 0,
            }}
          >
            {label}
          </AppTypography>
          <AppTypography variant="body2" fontWeight={500} noWrap>
            {value}
          </AppTypography>
        </div>
      ))}
    </div>
  );
  const stats2 = isContainersError ? (
    <ErrorMessage />
  ) : isContainersLoading ? (
    <ComponentLoader />
  ) : (
    <div
      className="custom-scrollbar"
      style={{
        display: "grid",
        gridTemplateColumns: isSmallUp ? "repeat(4, 36px)" : "repeat(3, 36px)",
        columnGap: theme.spacing(4.5),
        rowGap: theme.spacing(5),
        justifyContent: "center",
        width: "fit-content",
        maxHeight: 110,
        overflowX: "hidden",
        overflowY: "auto",
        paddingRight: theme.spacing(0.5),
      }}
    >
      {sorted.map((c) => {
        const name = cleanName(c.Names[0] ?? c.Id.slice(0, 12));
        const statusColor = resolveStateColor(
          getStatusLabel(c.Status, c.State),
        );
        return (
          <AppTooltip
            key={c.Id}
            title={
              <div
                style={{
                  textAlign: "center",
                }}
              >
                <AppTypography
                  component="span"
                  style={{
                    fontSize: "0.8rem",
                  }}
                >
                  {name}
                </AppTypography>
                <AppTypography
                  component="span"
                  style={{
                    color: statusColor,
                  }}
                >
                  {getStatusLabel(c.Status, c.State)}
                </AppTypography>
              </div>
            }
            arrow
            placement="top"
          >
            <div
              onContextMenu={(e) => handleContextMenu(e, c.Id, name, c.State)}
              style={{
                position: "relative",
                width: 36,
                height: 36,
                cursor: "context-menu",
              }}
            >
              <DockerIcon identifier={c.icon} size={36} alt={name} />
              <div
                style={{
                  position: "absolute",
                  bottom: 0,
                  right: 0,
                  width: 8,
                  height: 8,
                  borderRadius: "50%",
                  backgroundColor: resolveStateColor(c.State),
                  border: `1.5px solid ${theme.palette.background.paper}`,
                }}
              />
            </div>
          </AppTooltip>
        );
      })}
      <Menu
        anchorEl={menuAnchor}
        open={Boolean(menuAnchor)}
        onClose={handleMenuClose}
        autoFocus={false}
        slotProps={{
          paper: {
            sx: {
              minWidth: 140,
            },
          },
        }}
      >
        {menuContainer?.state !== "running" && (
          <MenuItem onClick={() => handleAction("start")}>
            <ListItemIcon>
              <Icon icon="mdi:play" width={18} />
            </ListItemIcon>
            Start
          </MenuItem>
        )}
        {menuContainer?.state === "running" && (
          <MenuItem onClick={() => handleAction("stop")}>
            <ListItemIcon>
              <Icon icon="mdi:stop" width={18} />
            </ListItemIcon>
            Stop
          </MenuItem>
        )}
        <MenuItem onClick={() => handleAction("restart")}>
          <ListItemIcon>
            <Icon icon="mdi:restart" width={18} />
          </ListItemIcon>
          Restart
        </MenuItem>
        {menuContainer?.state !== "running" && (
          <MenuItem onClick={() => handleAction("remove")}>
            <ListItemIcon>
              <Icon icon="mdi:delete-outline" width={18} />
            </ListItemIcon>
            Remove
          </MenuItem>
        )}
        <Divider />
        <MenuItem
          onClick={() => {
            if (menuContainer) {
              setDialogContainer({
                id: menuContainer.id,
                name: menuContainer.name,
              });
              setHasLoadedLogsDialog(true);
              setLogsOpen(true);
            }
            handleMenuClose();
          }}
        >
          <ListItemIcon>
            <Icon icon="mdi:text-box-outline" width={18} />
          </ListItemIcon>
          Logs
        </MenuItem>
        <MenuItem
          onClick={() => {
            if (menuContainer) {
              setDialogContainer({
                id: menuContainer.id,
                name: menuContainer.name,
              });
              setHasLoadedTerminalDialog(true);
              setTerminalOpen(true);
            }
            handleMenuClose();
          }}
        >
          <ListItemIcon>
            <Icon icon="mdi:console" width={18} />
          </ListItemIcon>
          Terminal
        </MenuItem>
      </Menu>
    </div>
  );
  return (
    <>
      <DashboardCard
        title="Docker"
        avatarIcon="mdi:docker"
        stats={stats}
        stats2={stats2}
        contentLayout="auto"
      />
      {dialogContainer && (
        <Suspense fallback={null}>
          {hasLoadedLogsDialog && (
            <LogsDialog
              open={logsOpen}
              onClose={() => setLogsOpen(false)}
              containerId={dialogContainer.id}
              containerName={dialogContainer.name}
            />
          )}
          {hasLoadedTerminalDialog && (
            <TerminalDialog
              open={terminalOpen}
              onClose={() => setTerminalOpen(false)}
              containerId={dialogContainer.id}
              containerName={dialogContainer.name}
            />
          )}
        </Suspense>
      )}
    </>
  );
};
export default DockerInfo;
