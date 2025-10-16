import GppGoodOutlinedIcon from "@mui/icons-material/GppGoodOutlined";
import SecurityUpdateWarningIcon from "@mui/icons-material/SecurityUpdateWarning";
import { Typography, Box, useTheme } from "@mui/material";
import { Link } from "@mui/material";
import { useQuery } from "@tanstack/react-query";
import React from "react";
import { Link as RouterLink } from "react-router-dom";

import GeneralCard from "@/components/cards/GeneralCard";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import axios from "@/utils/axios";

// --- Types ---
type Update = {
  package_id: string;
  summary: string;
  version: string;
  issued: string;
  changelog: string;
  cve: string[];
  restart: number;
  state: number;
};

type SystemUpdatesResponse = {
  updates: Update[];
};

type ServiceStatus = {
  running: boolean;
  // ...any other fields
};

type DistroInfo = {
  platform: string;
};

// --- Component ---
const SystemHealth = () => {
  const theme = useTheme();

  // Updates
  const {
    data: systemHealth,
    isLoading: loadingHealth,
    isFetching: fetchingHealth,
  } = useQuery<SystemUpdatesResponse>({
    queryKey: ["updateInfo"],
    queryFn: async () => {
      const res = await axios.get("/system/updates");
      if (Array.isArray(res.data)) return { updates: res.data };
      if (res.data && Array.isArray(res.data.updates)) return res.data;
      return { updates: [] };
    },
    refetchInterval: 50000,
  });

  // Services
  const { data: servicesRaw } = useQuery<ServiceStatus[]>({
    queryKey: ["SystemStatus"],
    queryFn: () => axios.get("/system/processes").then((res) => res.data),
    refetchInterval: 50000,
  });

  // Distro Info
  const { data: distroInfo } = useQuery<DistroInfo>({
    queryKey: ["DistroInfo"],
    queryFn: () => axios.get("/system/info").then((res) => res.data),
    refetchInterval: 50000,
  });

  // --- Data extraction ---
  const services = Array.isArray(servicesRaw) ? servicesRaw : [];
  const units = services.length;
  const running = services.filter((svc) => svc.running === true).length;

  const updates = systemHealth?.updates ?? [];
  const totalPackages = updates.length;
  const distro = distroInfo?.platform || "Unknown";

  // --- Icon and link selection ---
  let statusColor = theme.palette.success.dark;
  let IconComponent = GppGoodOutlinedIcon;
  let iconLink = "/updates";
  if (totalPackages > 0) {
    statusColor = theme.palette.warning.main;
    IconComponent = SecurityUpdateWarningIcon;
  }

  // --- Stats UI ---
  const stats2 = (
    <Box>
      {!systemHealth && (loadingHealth || fetchingHealth) ? (
        <ComponentLoader />
      ) : (
        <Link
          component={RouterLink}
          to={iconLink}
          underline="hover"
          color="inherit"
        >
          <IconComponent sx={{ fontSize: 100, color: statusColor }} />
        </Link>
      )}
    </Box>
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
          {!systemHealth && (loadingHealth || fetchingHealth)
            ? "Loading..."
            : totalPackages > 0
              ? `${totalPackages} available`
              : "None available"}
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
          {`${running}/${units} running`}
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
