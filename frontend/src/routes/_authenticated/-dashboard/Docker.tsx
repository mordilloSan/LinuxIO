import { Icon } from "@iconify/react";
import { useSuspenseQueries, useSuspenseQuery } from "@tanstack/react-query";
import { lazy, Suspense, useCallback, useState, type MouseEvent } from "react";

import { type ContainerInfo, linuxio, useCallMutation } from "@/api";
import DashboardCard from "@/components/cards/DashboardCard";
import DockerIcon from "@/components/docker/DockerIcon";
import AppDivider from "@/components/ui/AppDivider";
import AppMenu, { AppMenuItem } from "@/components/ui/AppMenu";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import StatusDot from "@/components/ui/StatusDot";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useAppMediaQuery, useAppTheme } from "@/theme";
import { getMutationErrorMessage } from "@/utils/mutations";

import DashboardStatRows from "./DashboardStatRows";

const LogsDialog = lazy(() => import("@/components/docker/LogsDialog"));
const TerminalDialog = lazy(() => import("@/components/docker/TerminalDialog"));

const CONTAINERS_REFETCH_MS = 5000;
const COLLECTIONS_REFETCH_MS = 30_000;

const cleanName = (name: string) => name.replace(/^\//, "");
const getStatusLabel = (status: string, state: string): string => {
  const health = status.match(/\((\w+)\)/)?.[1];
  if (health === "healthy" || health === "unhealthy") return health;
  return state;
};
const getCollectionCount = (items: unknown[] | null | undefined) =>
  items?.length ?? 0;

const selectContainerCounts = (containers: ContainerInfo[]) => ({
  running: containers.filter((c) => c.State === "running").length,
  total: containers.length,
});

interface ContainerSummary {
  icon?: string;
  id: string;
  name: string;
  state: string;
  statusLabel: string;
}

const selectContainerSummaries = (
  containers: ContainerInfo[],
): ContainerSummary[] =>
  [...containers]
    .sort((a, b) => {
      if (a.State === "running" && b.State !== "running") return -1;
      if (a.State !== "running" && b.State === "running") return 1;
      return 0;
    })
    .map((c) => ({
      icon: c.icon,
      id: c.Id,
      name: cleanName(c.Names[0] ?? c.Id.slice(0, 12)),
      state: c.State,
      statusLabel: getStatusLabel(c.Status, c.State),
    }));

const DockerStats = () => {
  const [
    { data: counts },
    { data: imagesCount },
    { data: networksCount },
    { data: volumesCount },
  ] = useSuspenseQueries({
    queries: [
      {
        ...linuxio.docker.list_containers,
        refetchInterval: CONTAINERS_REFETCH_MS,
        select: selectContainerCounts,
      },
      {
        ...linuxio.docker.list_images,
        refetchInterval: COLLECTIONS_REFETCH_MS,
        select: getCollectionCount,
      },
      {
        ...linuxio.docker.list_networks,
        refetchInterval: COLLECTIONS_REFETCH_MS,
        select: getCollectionCount,
      },
      {
        ...linuxio.docker.list_volumes,
        refetchInterval: COLLECTIONS_REFETCH_MS,
        select: getCollectionCount,
      },
    ],
  });

  return (
    <DashboardStatRows
      rows={[
        {
          label: "Containers",
          value: `${counts.running}/${counts.total}`,
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
      ]}
    />
  );
};

const DockerContainers = () => {
  const theme = useAppTheme();
  const toast = useScopedToast({ label: "Open Docker", to: "/docker" });
  const isSmallUp = useAppMediaQuery(theme.breakpoints.up("sm"));
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
          return theme.palette.text.disabled;
      }
    },
    [theme],
  );
  const { mutate: startContainer } = useCallMutation(
    linuxio.docker.start_container,
  );
  const { mutate: stopContainer } = useCallMutation(
    linuxio.docker.stop_container,
  );
  const { mutate: restartContainer } = useCallMutation(
    linuxio.docker.restart_container,
  );
  const { mutate: removeContainer } = useCallMutation(
    linuxio.docker.remove_container,
  );
  const handleContextMenu = useCallback(
    (e: MouseEvent<HTMLElement>, id: string, name: string, state: string) => {
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
      const request = { containerId: id };
      const callbacks = {
        onSuccess: () => {
          toast.success(
            `Container ${name} ${action === "remove" ? "removed" : `${action}ed`}`,
          );
        },
        onError: (e: Error) => {
          toast.error(
            getMutationErrorMessage(e, `Failed to ${action} container`),
          );
        },
      };
      if (action === "start") startContainer(request, callbacks);
      else if (action === "stop") stopContainer(request, callbacks);
      else if (action === "restart") restartContainer(request, callbacks);
      else removeContainer(request, callbacks);
      handleMenuClose();
    },
    [
      menuContainer,
      startContainer,
      stopContainer,
      restartContainer,
      removeContainer,
      handleMenuClose,
      toast,
    ],
  );
  const { data: containers } = useSuspenseQuery({
    ...linuxio.docker.list_containers,
    refetchInterval: CONTAINERS_REFETCH_MS,
    select: selectContainerSummaries,
  });

  return (
    <>
      <div
        className="custom-scrollbar"
        style={{
          display: "grid",
          gridTemplateColumns: isSmallUp
            ? "repeat(4, 36px)"
            : "repeat(3, 36px)",
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
        {containers.map((c) => {
          const statusColor = resolveStateColor(c.statusLabel);
          return (
            <AppTooltip
              arrow
              key={c.id}
              placement="top"
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
                    {c.name}
                  </AppTypography>
                  <AppTypography
                    component="span"
                    style={{
                      color: statusColor,
                    }}
                  >
                    {c.statusLabel}
                  </AppTypography>
                </div>
              }
            >
              <div
                onContextMenu={(e) =>
                  handleContextMenu(e, c.id, c.name, c.state)
                }
                style={{
                  position: "relative",
                  width: 36,
                  height: 36,
                  cursor: "context-menu",
                }}
              >
                <DockerIcon alt={c.name} identifier={c.icon} size={36} />
                <StatusDot
                  color={resolveStateColor(c.state)}
                  size={8}
                  style={{
                    position: "absolute",
                    bottom: 0,
                    right: 0,
                    border: `1.5px solid ${theme.palette.background.paper}`,
                  }}
                />
              </div>
            </AppTooltip>
          );
        })}
        <AppMenu
          anchorEl={menuAnchor}
          autoFocus={false}
          minWidth={140}
          onClose={handleMenuClose}
          open={Boolean(menuAnchor)}
        >
          {menuContainer?.state !== "running" && (
            <AppMenuItem
              onClick={() => handleAction("start")}
              startAdornment={<Icon icon="mdi:play" width={18} />}
            >
              Start
            </AppMenuItem>
          )}
          {menuContainer?.state === "running" && (
            <AppMenuItem
              onClick={() => handleAction("stop")}
              startAdornment={<Icon icon="mdi:stop" width={18} />}
            >
              Stop
            </AppMenuItem>
          )}
          <AppMenuItem
            onClick={() => handleAction("restart")}
            startAdornment={<Icon icon="mdi:restart" width={18} />}
          >
            Restart
          </AppMenuItem>
          {menuContainer?.state !== "running" && (
            <AppMenuItem
              onClick={() => handleAction("remove")}
              startAdornment={<Icon icon="mdi:delete-outline" width={18} />}
            >
              Remove
            </AppMenuItem>
          )}
          <AppDivider />
          <AppMenuItem
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
            startAdornment={<Icon icon="mdi:text-box-outline" width={18} />}
          >
            Logs
          </AppMenuItem>
          <AppMenuItem
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
            startAdornment={<Icon icon="mdi:console" width={18} />}
          >
            Terminal
          </AppMenuItem>
        </AppMenu>
      </div>
      {dialogContainer && (
        <Suspense fallback={null}>
          {hasLoadedLogsDialog && (
            <LogsDialog
              containerId={dialogContainer.id}
              containerName={dialogContainer.name}
              onClose={() => setLogsOpen(false)}
              open={logsOpen}
            />
          )}
          {hasLoadedTerminalDialog && (
            <TerminalDialog
              containerId={dialogContainer.id}
              containerName={dialogContainer.name}
              onClose={() => setTerminalOpen(false)}
              open={terminalOpen}
            />
          )}
        </Suspense>
      )}
    </>
  );
};

const DockerInfo = () => (
  <DashboardCard
    avatarIcon="mdi:docker"
    contentLayout="auto"
    stats={<DockerStats />}
    stats2={<DockerContainers />}
    title="Docker"
  />
);

export default DockerInfo;
