import GppGoodOutlinedIcon from "@mui/icons-material/GppGoodOutlined";
import SecurityUpdateWarningIcon from "@mui/icons-material/SecurityUpdateWarning";
import { Link, Stack, Typography, useTheme } from "@mui/material";
import React from "react";
import { Link as RouterLink } from "react-router-dom";

import { linuxio } from "@/api";
import DashboardCard from "@/components/cards/DashboardCard";
import ComponentLoader from "@/components/loaders/ComponentLoader";

// --- Types ---
interface Update {
  package_id: string;
  summary: string;
  version: string;
  issued: string;
  changelog: string;
  cve: string[];
  restart: number;
  state: number;
}

interface SystemUpdatesResponse {
  updates: Update[];
}

// --- Component ---
const SystemHealth = () => {
  const theme = useTheme();

  // Updates
  const {
    data: updatesRaw,
    isPending: loadingHealth,
    isFetching: fetchingHealth,
  } = linuxio.system.get_updates_fast.useQuery({ refetchInterval: 50000 });

  // Normalize updates response
  const systemHealth: SystemUpdatesResponse | undefined = updatesRaw
    ? Array.isArray(updatesRaw)
      ? { updates: updatesRaw }
      : updatesRaw
    : undefined;

  // Services
  const { data: servicesRaw } = linuxio.system.get_processes.useQuery({
    refetchInterval: 50000,
  });

  // Distro Info
  const { data: distroInfo } = linuxio.system.get_host_info.useQuery({
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
    <Stack>
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
    </Stack>
  );

  const stats = (
    <Stack
      sx={{
        display: "flex",
        flexDirection: "column",
        alignSelf: "flex-start",
        width: "fit-content",
      }}
    >
      {[
        { label: "Distro", value: <>{distro}</> },
        {
          label: "Updates",
          value: (
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
          ),
        },
        {
          label: "Services",
          value: (
            <Link
              component={RouterLink}
              to="/services"
              underline="hover"
              color="inherit"
            >
              {`${running}/${units} running`}
            </Link>
          ),
        },
      ].map(({ label, value }) => (
        <Stack
          key={label}
          direction="row"
          alignItems="baseline"
          sx={{
            justifyContent: "flex-start",
            py: 0.5,
            borderBottom: "1px solid",
            borderColor: "divider",
            "&:last-child": { borderBottom: "none" },
            gap: 1,
          }}
        >
          <Typography
            variant="caption"
            color="text.secondary"
            sx={{
              textTransform: "uppercase",
              letterSpacing: "0.06em",
              fontSize: "0.62rem",
              flexShrink: 0,
            }}
          >
            {label}
          </Typography>
          <Typography variant="body2" fontWeight={500} noWrap>
            {value}
          </Typography>
        </Stack>
      ))}
    </Stack>
  );

  return (
    <DashboardCard
      title="System Health"
      stats={stats}
      stats2={stats2}
      avatarIcon={`simple-icons:${distroInfo?.platform || "linux"}`}
    />
  );
};

export default SystemHealth;
