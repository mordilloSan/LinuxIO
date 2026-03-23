import { Icon } from "@iconify/react";
import { Link } from "@mui/material";
import { useAppTheme } from "@/theme";
import React from "react";
import { Link as RouterLink } from "react-router-dom";

import { linuxio } from "@/api";
import DashboardCard from "@/components/cards/DashboardCard";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import AppTypography from "@/components/ui/AppTypography";

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

const SystemHealth = () => {
  const theme = useAppTheme();

  const {
    data: updatesRaw,
    isPending: loadingHealth,
    isFetching: fetchingHealth,
  } = linuxio.system.get_updates_fast.useQuery({ refetchInterval: 50000 });

  const systemHealth: SystemUpdatesResponse | undefined = updatesRaw
    ? Array.isArray(updatesRaw)
      ? { updates: updatesRaw }
      : updatesRaw
    : undefined;

  const { data: servicesRaw } = linuxio.system.get_processes.useQuery({
    refetchInterval: 50000,
  });

  const { data: distroInfo } = linuxio.system.get_host_info.useQuery({
    refetchInterval: 50000,
  });

  const services = Array.isArray(servicesRaw) ? servicesRaw : [];
  const units = services.length;
  const running = services.filter((svc) => svc.running === true).length;

  const updates = systemHealth?.updates ?? [];
  const totalPackages = updates.length;
  const distro = distroInfo?.platform || "Unknown";

  let statusColor = theme.palette.success.dark;
  let iconName = "mdi:shield-check-outline";
  let iconLink = "/updates";
  if (totalPackages > 0) {
    statusColor = theme.palette.warning.main;
    iconName = "mdi:shield-alert-outline";
  }

  const stats2 = (
    <div>
      {!systemHealth && (loadingHealth || fetchingHealth) ? (
        <ComponentLoader />
      ) : (
        <Link
          component={RouterLink}
          to={iconLink}
          underline="hover"
          color="inherit"
        >
          <Icon icon={iconName} width={100} height={100} color={statusColor} />
        </Link>
      )}
    </div>
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
                : "1px solid var(--app-palette-divider)",
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
