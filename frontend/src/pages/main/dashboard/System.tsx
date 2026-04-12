import { Icon } from "@iconify/react";
import React from "react";
import { useNavigate } from "react-router-dom";

import { linuxio } from "@/api";
import DashboardCard from "@/components/cards/DashboardCard";
import AppSkeleton from "@/components/ui/AppSkeleton";
import AppTypography from "@/components/ui/AppTypography";
import SkeletonText from "@/components/ui/SkeletonText";
import { useAppTheme } from "@/theme";

interface HealthItem {
  icon: string;
  color: string;
  text: string;
  to?: string;
  detail?: string;
  spaceBefore?: boolean;
  iconStyle?: React.CSSProperties;
}

function pluralize(count: number, singular: string, plural: string): string {
  return `${count} ${count === 1 ? singular : plural}`;
}

const SystemHealth = () => {
  const theme = useAppTheme();
  const navigate = useNavigate();

  const {
    data: health,
    isPending: loadingHealth,
    isFetching: fetchingHealth,
  } = linuxio.system.get_health_summary.useQuery({ refetchInterval: 50000 });

  const { data: hostInfo } = linuxio.system.get_host_info.useQuery({
    refetchInterval: 50000,
  });

  const items: HealthItem[] = [];

  if (health !== undefined) {
    items.push({
      icon: "mdi:cog-sync-outline",
      color: health.failedServicesCount
        ? theme.palette.text.secondary
        : theme.palette.success.main,
      text: `${pluralize(health.runningServicesCount, "service", "services")} running`,
      to: "/services",
    });
  }

  if (health?.failedServicesCount) {
    const failed = health.failedServices?.[0];
    items.push({
      icon: "mdi:alert-circle",
      color: theme.palette.error.main,
      text: `${pluralize(health.failedServicesCount, "service has", "services have")} failed`,
      to: failed
        ? `/services?section=services&service=${encodeURIComponent(failed)}`
        : "/services",
      detail: health.failedServices?.slice(0, 2).join(", "),
    });
  }

  if (health?.failedLoginAttempts) {
    items.push({
      icon: "mdi:account-alert-outline",
      color: theme.palette.warning.main,
      text: `${pluralize(health.failedLoginAttempts, "failed login attempt", "failed login attempts")} before this session`,
      to: "/logs",
    });
  }

  if (health?.upToDate === false) {
    items.push({
      icon: "mdi:package-up",
      color: theme.palette.warning.main,
      text: `${pluralize(health.updatesAvailable, "update", "updates")} available`,
      to: "/updates",
      spaceBefore: true,
      iconStyle: { transform: "translateY(2px)" },
    });
  } else if (health) {
    items.push({
      icon: "mdi:check-circle",
      color: theme.palette.success.main,
      text: "System is up to date",
      to: "/updates",
      spaceBefore: true,
      iconStyle: { transform: "translateY(2px)" },
    });
  }

  if (health?.uncleanShutdown) {
    items.push({
      icon: "mdi:alert-outline",
      color: theme.palette.warning.main,
      text: "Unclean shutdown",
      to: "/logs",
    });
  }

  if (health?.lastLogin?.time) {
    const daysOfWeek = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];
    const timeParts = health.lastLogin.time.split(" ");
    let displaySource = health.lastLogin.source;
    let timeStr = health.lastLogin.time;
    if (timeParts.length > 0 && !daysOfWeek.includes(timeParts[0])) {
      displaySource = timeParts[0];
      timeStr = timeParts.slice(1).join(" ");
    }
    const timeMatch = timeStr.match(
      /^(?:Mon|Tue|Wed|Thu|Fri|Sat|Sun)\s+(\w+)\s+(\d+)\s+(\d{2}):(\d{2}):\d{2}/,
    );
    const displayTime = timeMatch
      ? `${timeMatch[1]} ${timeMatch[2]}, ${timeMatch[3]}:${timeMatch[4]}}`
      : timeStr;
    const terminal =
      health.lastLogin.terminal === "web"
        ? "web console"
        : health.lastLogin.terminal;
    const detailParts = [displaySource, terminal].filter(Boolean);
    items.push({
      icon: "mdi:account-clock-outline",
      color: health.failedLoginAttempts
        ? theme.palette.warning.main
        : theme.palette.text.primary,
      text: `Last login: ${displayTime}`,
      detail:
        detailParts.length > 0 ? `from ${detailParts.join(" on ")}` : undefined,
      spaceBefore: true,
      iconStyle: { transform: "translateY(-6px)" },
    });
  }

  let statusColor = theme.palette.success.dark;
  let iconName = "mdi:shield-check-outline";
  let iconLink = "/updates";

  if (health?.failedServicesCount) {
    statusColor = theme.palette.error.main;
    iconName = "mdi:shield-alert-outline";
    iconLink = "/services";
  } else if (health?.failedLoginAttempts) {
    statusColor = theme.palette.warning.main;
    iconName = "mdi:shield-alert-outline";
    iconLink = "/logs";
  } else if ((health?.updatesAvailable ?? 0) > 0 || health?.uncleanShutdown) {
    statusColor = theme.palette.warning.main;
    iconName = "mdi:shield-alert-outline";
    iconLink = health?.uncleanShutdown ? "/logs" : "/updates";
  }

  const stats2 = (
    <div>
      {!health && (loadingHealth || fetchingHealth) ? (
        <AppSkeleton variant="circular" width={100} height={100} />
      ) : (
        <div onClick={() => navigate(iconLink)} style={{ cursor: "pointer" }}>
          <Icon icon={iconName} width={100} height={100} color={statusColor} />
        </div>
      )}
    </div>
  );

  const renderItem = (item: HealthItem) => {
    const content = (
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: theme.spacing(1),
        }}
      >
        <Icon
          icon={item.icon}
          width={18}
          height={18}
          color={item.color}
          style={{ flexShrink: 0, ...item.iconStyle }}
        />
        <div style={{ minWidth: 0 }}>
          <AppTypography
            variant="body2"
            fontWeight={500}
            style={{
              color:
                item.color === theme.palette.text.secondary
                  ? undefined
                  : item.color,
            }}
          >
            {item.text}
          </AppTypography>
          {item.detail ? (
            <AppTypography
              variant="caption"
              color="text.secondary"
              style={{ display: "block", marginTop: -2 }}
            >
              {item.detail}
            </AppTypography>
          ) : null}
        </div>
      </div>
    );

    const spacing = item.spaceBefore
      ? { marginTop: theme.spacing(1) }
      : undefined;

    return item.to ? (
      <div
        key={item.text}
        onClick={() => navigate(item.to!)}
        style={{ cursor: "pointer", ...spacing }}
      >
        {content}
      </div>
    ) : (
      <div key={item.text} style={spacing}>
        {content}
      </div>
    );
  };

  const skeletonRow = (key: string, width: string, spaceBefore = false) => (
    <div
      key={key}
      style={{
        display: "flex",
        alignItems: "center",
        gap: theme.spacing(1),
        ...(spaceBefore ? { marginTop: theme.spacing(1) } : undefined),
      }}
    >
      <AppSkeleton variant="circular" width={18} height={18} />
      <SkeletonText variant="body2" width={width} />
    </div>
  );

  const servicesItem = items.find((i) => i.icon === "mdi:cog-sync-outline");
  const updatesItem = items.find(
    (i) => i.icon === "mdi:package-up" || i.icon === "mdi:check-circle",
  );
  const lastLoginItem = items.find(
    (i) => i.icon === "mdi:account-clock-outline",
  );
  const alertItems = items.filter(
    (i) => i !== servicesItem && i !== updatesItem && i !== lastLoginItem,
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
      {servicesItem
        ? renderItem(servicesItem)
        : skeletonRow("s-services", "12ch")}
      {alertItems.map(renderItem)}
      {updatesItem
        ? renderItem(updatesItem)
        : skeletonRow("s-updates", "16ch", true)}
      {lastLoginItem
        ? renderItem(lastLoginItem)
        : health?.lastLogin?.time === undefined &&
            (loadingHealth || fetchingHealth)
          ? skeletonRow("s-lastlogin", "18ch", true)
          : null}
    </div>
  );

  return (
    <DashboardCard
      title="System Health"
      stats={stats}
      stats2={stats2}
      avatarIcon={`simple-icons:${hostInfo?.platform || "linux"}`}
      contentLayout={[1.5, 1]}
    />
  );
};

export default SystemHealth;
