import { Icon } from "@iconify/react";
import {
  Box,
  Divider,
  ListItemIcon,
  Menu,
  MenuItem,
  Tooltip,
  Typography,
} from "@mui/material";
import { useQueryClient } from "@tanstack/react-query";
import React, { useCallback, useMemo, useState } from "react";
import { toast } from "sonner";

import { linuxio } from "@/api";
import GeneralCard from "@/components/cards/GeneralCard";
import DockerIcon from "@/components/docker/DockerIcon";
import ErrorMessage from "@/components/errors/Error";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import LogsDialog from "@/pages/main/docker/LogsDialog";
import TerminalDialog from "@/pages/main/docker/TerminalDialog";
import { getMutationErrorMessage } from "@/utils/mutations";

const stateColor: Record<string, string> = {
  running: "success.main",
  healthy: "success.main",
  exited: "error.main",
  unhealthy: "error.main",
  paused: "warning.main",
  restarting: "info.main",
};

const cleanName = (name: string) => name.replace(/^\//, "");

const getStatusLabel = (status: string, state: string): string => {
  const health = status.match(/\((\w+)\)/)?.[1];
  if (health === "healthy" || health === "unhealthy") return health;
  return state;
};

const DockerInfo: React.FC = () => {
  const queryClient = useQueryClient();
  const [menuAnchor, setMenuAnchor] = useState<null | HTMLElement>(null);
  const [menuContainer, setMenuContainer] = useState<{
    id: string;
    name: string;
    state: string;
  } | null>(null);
  const [logsOpen, setLogsOpen] = useState(false);
  const [terminalOpen, setTerminalOpen] = useState(false);
  const [dialogContainer, setDialogContainer] = useState<{
    id: string;
    name: string;
  } | null>(null);

  const invalidateContainers = useCallback(
    () =>
      queryClient.invalidateQueries({
        queryKey: linuxio.docker.list_containers.queryKey(),
      }),
    [queryClient],
  );

  const { mutate: startContainer } = linuxio.docker.start_container.useMutation(
    {
      onSuccess: () => {
        toast.success(`Container ${menuContainer?.name} started`);
        invalidateContainers();
      },
      onError: (e: Error) => {
        toast.error(getMutationErrorMessage(e, "Failed to start container"));
      },
    },
  );

  const { mutate: stopContainer } = linuxio.docker.stop_container.useMutation({
    onSuccess: () => {
      toast.success(`Container ${menuContainer?.name} stopped`);
      invalidateContainers();
    },
    onError: (e: Error) => {
      toast.error(getMutationErrorMessage(e, "Failed to stop container"));
    },
  });

  const { mutate: restartContainer } =
    linuxio.docker.restart_container.useMutation({
      onSuccess: () => {
        toast.success(`Container ${menuContainer?.name} restarted`);
        invalidateContainers();
      },
      onError: (e: Error) => {
        toast.error(getMutationErrorMessage(e, "Failed to restart container"));
      },
    });

  const { mutate: removeContainer } =
    linuxio.docker.remove_container.useMutation({
      onSuccess: () => {
        toast.success(`Container ${menuContainer?.name} removed`);
        invalidateContainers();
      },
      onError: (e: Error) => {
        toast.error(getMutationErrorMessage(e, "Failed to remove container"));
      },
    });

  const handleContextMenu = useCallback(
    (
      e: React.MouseEvent<HTMLElement>,
      id: string,
      name: string,
      state: string,
    ) => {
      e.preventDefault();
      setMenuAnchor(e.currentTarget);
      setMenuContainer({ id, name, state });
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
      const args = [menuContainer.id];
      if (action === "start") startContainer(args);
      else if (action === "stop") stopContainer(args);
      else if (action === "restart") restartContainer(args);
      else removeContainer(args);
      handleMenuClose();
    },
    [
      menuContainer,
      startContainer,
      stopContainer,
      restartContainer,
      removeContainer,
      handleMenuClose,
    ],
  );

  const {
    data: containers = [],
    isPending: isContainersLoading,
    isError: isContainersError,
  } = linuxio.docker.list_containers.useQuery({
    refetchInterval: 5000,
  });

  const { data: images = [] } = linuxio.docker.list_images.useQuery({
    refetchInterval: 30_000,
  });

  const { data: networks = [] } = linuxio.docker.list_networks.useQuery({
    refetchInterval: 30_000,
  });

  const { data: volumes = [] } = linuxio.docker.list_volumes.useQuery({
    refetchInterval: 30_000,
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
    <Box
      sx={{
        display: "flex",
        gap: 1,
        flexDirection: "column",
        alignSelf: "flex-start",
        mt: 4,
      }}
    >
      <Typography variant="body1">
        <strong>Containers:</strong> {runningCount}/{containers.length}
      </Typography>
      <Typography variant="body1">
        <strong>Images:</strong> {images.length}
      </Typography>
      <Typography variant="body1">
        <strong>Networks:</strong> {networks.length}
      </Typography>
      <Typography variant="body1">
        <strong>Volumes:</strong> {volumes.length}
      </Typography>
    </Box>
  );

  const stats2 = isContainersError ? (
    <ErrorMessage />
  ) : isContainersLoading ? (
    <ComponentLoader />
  ) : (
    <Box
      className="custom-scrollbar"
      sx={{
        display: "grid",
        gridTemplateColumns: {
          xs: "repeat(3, 36px)",
          sm: "repeat(4, 36px)",
        },
        gap: 2.5,
        justifyContent: "center",
        width: "100%",
        maxHeight: 90,
        overflowX: "hidden",
        overflowY: "auto",
        pr: 0.5,
        "&::-webkit-scrollbar-thumb": {
          backgroundColor: "transparent !important",
        },
        "&:hover::-webkit-scrollbar-thumb": {
          backgroundColor: "rgba(100, 100, 100, 0.2) !important",
        },
      }}
    >
      {sorted.map((c) => {
        const name = cleanName(c.Names[0] ?? c.Id.slice(0, 12));
        return (
          <Tooltip
            key={c.Id}
            title={
              <Box sx={{ textAlign: "center" }}>
                <Box component="span" sx={{ fontSize: "0.8rem" }}>
                  {name}
                </Box>
                <br />
                <Box
                  component="span"
                  sx={{
                    color:
                      stateColor[getStatusLabel(c.Status, c.State)] ??
                      "grey.500",
                  }}
                >
                  {getStatusLabel(c.Status, c.State)}
                </Box>
              </Box>
            }
            arrow
            placement="top"
          >
            <Box
              onContextMenu={(e) => handleContextMenu(e, c.Id, name, c.State)}
              sx={{
                position: "relative",
                width: 36,
                height: 36,
                cursor: "context-menu",
              }}
            >
              <DockerIcon identifier={c.icon} size={36} alt={name} />
              <Box
                sx={{
                  position: "absolute",
                  bottom: 0,
                  right: 0,
                  width: 8,
                  height: 8,
                  borderRadius: "50%",
                  bgcolor: stateColor[c.State] ?? "grey.500",
                  border: "1.5px solid",
                  borderColor: "background.paper",
                }}
              />
            </Box>
          </Tooltip>
        );
      })}
      <Menu
        anchorEl={menuAnchor}
        open={Boolean(menuAnchor)}
        onClose={handleMenuClose}
        autoFocus={false}
        slotProps={{ paper: { sx: { minWidth: 140 } } }}
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
    </Box>
  );

  return (
    <>
      <GeneralCard
        title="Docker"
        avatarIcon="mdi:docker"
        stats={stats}
        stats2={stats2}
        connectionStatus={isContainersError ? "offline" : "online"}
      />
      {dialogContainer && (
        <>
          <LogsDialog
            open={logsOpen}
            onClose={() => setLogsOpen(false)}
            containerId={dialogContainer.id}
            containerName={dialogContainer.name}
          />
          <TerminalDialog
            open={terminalOpen}
            onClose={() => setTerminalOpen(false)}
            containerId={dialogContainer.id}
            containerName={dialogContainer.name}
          />
        </>
      )}
    </>
  );
};

export default DockerInfo;
