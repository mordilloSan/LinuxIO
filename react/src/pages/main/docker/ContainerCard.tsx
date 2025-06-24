import {
  Box,
  CircularProgress,
  Grid,
  Tooltip,
  Typography,
  Fade,
} from "@mui/material";
import { useQueryClient } from "@tanstack/react-query";
import React from "react";

import ActionButton from "./ActionButton";

import FrostedCard from "@/components/cards/RootCard";
import { ContainerInfo } from "@/types/container";
import axios from "@/utils/axios";

const getContainerIconUrl = (name: string) => {
  const sanitized = name.replace(/[^a-zA-Z0-9-]/g, "").toLowerCase();
  return `https://raw.githubusercontent.com/homarr-labs/dashboard-icons/main/svg/${sanitized}.svg`;
};

const fallbackDockerIcon =
  "https://raw.githubusercontent.com/homarr-labs/dashboard-icons/main/svg/docker.svg";

const getStatusColor = (container: ContainerInfo) => {
  const status = container.Status.toLowerCase();
  if (status.includes("unhealthy")) return "warning.main";
  if (status.includes("healthy")) return "success.main";
  if (container.State === "running") return "success.main";
  if (container.State === "exited" || container.State === "dead")
    return "error.main";
  return "warning.main";
};

const getStatusTooltip = (container: ContainerInfo) => {
  const status = container.Status.toLowerCase();
  if (status.includes("unhealthy")) return "Unhealthy";
  if (status.includes("healthy")) return "Healthy";
  if (container.State === "running") return "Running";
  if (container.State === "exited") return "Stopped";
  if (container.State === "dead") return "Dead";
  return "Unhealthy / Starting";
};

interface ContainerCardProps {
  container: ContainerInfo;
}

const ContainerCard: React.FC<ContainerCardProps> = ({ container }) => {
  const queryClient = useQueryClient();
  const [loading, setLoading] = React.useState(false);

  const name = container.Names?.[0]?.replace("/", "") || "Unnamed";
  const iconUrl = getContainerIconUrl(name);

  const handleAction = async (
    id: string,
    action: "start" | "stop" | "restart" | "remove",
  ) => {
    setLoading(true);
    try {
      await axios.post(`/docker/containers/${id}/${action}`);
      queryClient.invalidateQueries({ queryKey: ["containers"] });
    } finally {
      setLoading(false);
    }
  };

  return (
    <Grid size={{ xs: 6, sm: 4, md: 4, lg: 3, xl: 2 }}>
      <FrostedCard
        sx={{
          p: 2,
          display: "flex",
          alignItems: "center",
          height: "100%",
          position: "relative",
          transition: "transform 0.2s, box-shadow 0.2s",
          "&:hover": {
            transform: "translateY(-4px)",
            boxShadow: "0 8px 24px rgba(0,0,0,0.35)",
          },
        }}
      >
        <Tooltip
          title={getStatusTooltip(container)}
          placement="top"
          arrow
          slots={{ transition: Fade }}
          slotProps={{ transition: { timeout: 300 } }}
        >
          <Box
            sx={{
              position: "absolute",
              top: 16,
              right: 8,
              width: 10,
              height: 10,
              borderRadius: "50%",
              backgroundColor: getStatusColor(container),
              cursor: "default",
            }}
          />
        </Tooltip>

        <Box
          component="img"
          src={iconUrl}
          alt={name}
          sx={{
            width: 48,
            height: 48,
            minWidth: 48,
            minHeight: 48,
            objectFit: "contain",
            flexShrink: 0,
            mr: 1.5,
            alignSelf: "flex-start",
          }}
          onError={(e) => {
            (e.currentTarget as HTMLImageElement).src = fallbackDockerIcon;
          }}
        />

        <Box
          sx={{
            display: "flex",
            flexDirection: "column",
            justifyContent: "center",
            height: "100%",
            flexGrow: 1,
          }}
        >
          <Typography
            variant="subtitle1"
            fontWeight="600"
            noWrap
            sx={{ mb: 1, ml: 1 }}
          >
            {name}
          </Typography>

          {loading ? (
            <Box sx={{ display: "flex", justifyContent: "flex-start", ml: 1 }}>
              <CircularProgress size={16} />
            </Box>
          ) : (
            <Box
              sx={{
                display: "flex",
                flexDirection: "column",
                alignItems: "flex-start",
                gap: 0,
                ml: 1,
              }}
            >
              <Box sx={{ display: "flex", alignItems: "center", gap: 0 }}>
                {container.State !== "running" && (
                  <ActionButton
                    icon="mdi:play"
                    onClick={() => handleAction(container.Id, "start")}
                  />
                )}
                {container.State === "running" && (
                  <ActionButton
                    icon="mdi:stop"
                    onClick={() => handleAction(container.Id, "stop")}
                  />
                )}
                <ActionButton
                  icon="mdi:restart"
                  onClick={() => handleAction(container.Id, "restart")}
                />
                <ActionButton
                  icon="mdi:delete"
                  onClick={() => handleAction(container.Id, "remove")}
                />
              </Box>
            </Box>
          )}
        </Box>
      </FrostedCard>
    </Grid>
  );
};

export default ContainerCard;
