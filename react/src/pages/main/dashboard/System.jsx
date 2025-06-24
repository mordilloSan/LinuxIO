import GppGoodOutlinedIcon from "@mui/icons-material/GppGoodOutlined";
import HighlightOffIcon from "@mui/icons-material/HighlightOff";
import SecurityUpdateWarningIcon from "@mui/icons-material/SecurityUpdateWarning";
import { Typography, Box, useTheme } from "@mui/material";
import { Link } from "@mui/material";
import { useQuery } from "@tanstack/react-query";
import React from "react";
import { Link as RouterLink } from "react-router-dom";

import GeneralCard from "@/components/cards/GeneralCard";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import axios from "@/utils/axios";

const SystemHealth = () => {
  const theme = useTheme();

  const { data: systemHealth, isLoading: loadingHealth } = useQuery({
    queryKey: ["SystemHealth"],
    queryFn: () => axios.get("system/updates").then((res) => res.data),
    refetchInterval: 50000,
  });

  const { data: servicesRaw, isLoading: loadingStatus } = useQuery({
    queryKey: ["SystemStatus"],
    queryFn: () => axios.get("/system/services/status").then((res) => res.data),
    refetchInterval: 50000,
  });

  const { data: distroInfo, isLoading: loadingDistro } = useQuery({
    queryKey: ["DistroInfo"],
    queryFn: () => axios.get("/system/info").then((res) => res.data),
    refetchInterval: 50000,
  });

  const isLoading = loadingHealth || loadingStatus || loadingDistro;

  // === NEW: service stats extraction ===
  const services = Array.isArray(servicesRaw) ? servicesRaw : [];
  const units = services.length;
  const failed = services.filter((svc) => svc.active_state === "failed").length;
  const running = services.filter(
    (svc) => svc.active_state === "active",
  ).length;

  const updates = systemHealth?.updates || [];
  const distro = distroInfo?.platform || "Unknown";

  // Determine icon + color
  let statusColor = theme.palette.success.dark;
  let IconComponent = GppGoodOutlinedIcon;
  let iconLink = "/updates";

  if (failed > 0) {
    statusColor = theme.palette.error.main;
    IconComponent = HighlightOffIcon;
    iconLink = "/services";
  } else if (updates.length > 0) {
    statusColor = theme.palette.warning.main;
    IconComponent = SecurityUpdateWarningIcon;
  }

  const stats2 = (
    <Box
      sx={{
        position: "relative",
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        width: 120,
        height: 120,
        borderRadius: "50%",
      }}
    >
      {isLoading ? (
        <ComponentLoader />
      ) : (
        <Link
          component={RouterLink}
          to={iconLink}
          underline="hover"
          color="inherit"
        >
          <IconComponent sx={{ fontSize: 80, color: statusColor }} />
        </Link>
      )}
    </Box>
  );

  const totalPackages = updates.reduce(
    (sum, u) => sum + (u.packages?.length || 1),
    0,
  );

  const stats = (
    <Box sx={{ display: "flex", gap: 1, flexDirection: "column" }}>
      <Typography variant="body1">
        <strong>Distro:</strong> {distro}
      </Typography>
      <Typography variant="body1">
        <strong>Updates:</strong>{" "}
        <Link
          component={RouterLink}
          to="/updates"
          underline="hover"
          color="inherit"
        >
          {totalPackages > 0 ? `${totalPackages} available` : "None available"}
        </Link>
      </Typography>

      <Typography variant="body1">
        <strong>Services:</strong>{" "}
        <Link
          component={RouterLink}
          to="/services"
          underline="hover"
          color="inherit"
        >
          {failed > 0 ? `${failed} failed` : `${running}/${units} running`}
        </Link>
      </Typography>
    </Box>
  );

  return (
    <GeneralCard
      title="System Health"
      stats={stats}
      stats2={stats2}
      avatarIcon={`simple-icons:${distroInfo?.platform || "linux"}`}
    />
  );
};

export default SystemHealth;
