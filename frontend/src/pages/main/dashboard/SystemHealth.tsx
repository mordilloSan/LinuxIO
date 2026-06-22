import { Icon } from "@iconify/react";
import { useQueryClient } from "@tanstack/react-query";
import React from "react";
import { useNavigate } from "react-router-dom";

import { type AccountUserLogin, linuxio } from "@/api";
import DashboardCard from "@/components/cards/DashboardCard";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppDivider from "@/components/ui/AppDivider";
import AppIconButton from "@/components/ui/AppIconButton";
import AppSkeleton from "@/components/ui/AppSkeleton";
import AppTypography from "@/components/ui/AppTypography";
import useAuth from "@/hooks/useAuth";
import { useAppTheme } from "@/theme";

interface HealthItem {
  color: string;
  detail?: string;
  detailColor?: string;
  icon: string;
  iconStyle?: React.CSSProperties;
  onClick?: () => void;
  secondaryAction?: {
    label: string;
    icon?: string;
    ariaLabel?: string;
    onClick: (event: React.MouseEvent) => void;
    disabled?: boolean;
  };
  spaceBefore?: boolean;
  text: string;
  textColor?: string;
  to?: string;
}

function pluralize(count: number, singular: string, plural: string): string {
  return `${count} ${count === 1 ? singular : plural}`;
}

function userDetailsPath(
  username: string | undefined | null,
  focusLogin?: {
    eventId?: string;
    failedLoginAlertId?: string;
    autoDismissFailedLoginAlert?: boolean;
  },
): string {
  const user = username?.trim();
  const params = new URLSearchParams({ accountsTab: "users" });
  if (user) {
    params.set("user", user);
  }
  if (focusLogin) {
    if (focusLogin.eventId) {
      params.set("focusLoginEventId", focusLogin.eventId);
    }
    if (focusLogin.failedLoginAlertId) {
      params.set("failedLoginAlertId", focusLogin.failedLoginAlertId);
    }
    if (focusLogin.autoDismissFailedLoginAlert) {
      params.set("autoDismissFailedLoginAlert", "1");
    }
  }
  return `/accounts?${params.toString()}`;
}

function loginAttemptLocation(login: AccountUserLogin): string {
  if (login.source) {
    return login.source;
  }
  if (login.terminal?.startsWith("tty")) {
    return "Local console";
  }
  return "Local";
}

function failedLoginDetail(
  login: AccountUserLogin | undefined,
): string | undefined {
  if (!login) {
    return undefined;
  }

  const username = login.username?.trim() || "unknown user";
  const location = loginAttemptLocation(login);
  return [`Latest: ${username}`, location ? `from ${location}` : ""]
    .filter(Boolean)
    .join("\n");
}

const SystemHealth = () => {
  const theme = useAppTheme();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { user: currentUser } = useAuth();
  const [failedLoginsOpen, setFailedLoginsOpen] = React.useState(false);

  const {
    data: health,
    isPending: loadingHealth,
    isFetching: fetchingHealth,
  } = linuxio.system.get_health_summary.useQuery({ refetchInterval: 50000 });

  const {
    data: failedLoginEvents = [],
    isPending: failedLoginEventsPending,
    isError: failedLoginEventsError,
    error: failedLoginEventsErrorValue,
  } = linuxio.system.list_failed_login_events.useQuery(
    { limit: "24" },
    {
      enabled: failedLoginsOpen,
      refetchInterval: failedLoginsOpen ? 30000 : false,
    },
  );

  const { mutate: dismissUncleanShutdown, isPending: dismissingUnclean } =
    linuxio.system.dismiss_unclean_shutdown.useMutation({
      onSuccess: () => {
        queryClient.invalidateQueries({
          queryKey: linuxio.system.get_health_summary.queryKey(),
        });
      },
    });

  const { mutate: dismissFailedLoginAlert, isPending: dismissingFailedLogin } =
    linuxio.system.dismiss_failed_login_alert.useMutation({
      onSuccess: () => {
        setFailedLoginsOpen(false);
        queryClient.invalidateQueries({
          queryKey: linuxio.system.get_health_summary.queryKey(),
        });
      },
    });

  const items: HealthItem[] = [];
  const failedLoginAlert = health?.failedLoginAlert;
  const openFailedLogins = () => setFailedLoginsOpen(true);

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

  if (failedLoginAlert) {
    items.push({
      icon: "mdi:account-alert-outline",
      color: theme.palette.error.main,
      text: `${pluralize(failedLoginAlert.count, "failed login attempt", "failed login attempts")}\nbefore this session`,
      onClick: openFailedLogins,
      detail: failedLoginDetail(failedLoginAlert.latestEvent),
      textColor: "#fff",
      detailColor: "rgba(255, 255, 255, 0.72)",
      spaceBefore: true,
      iconStyle: { transform: "translateY(-6px)" },
      secondaryAction: {
        label: "Dismiss",
        icon: "mdi:close",
        ariaLabel: "Dismiss failed login alert",
        disabled: dismissingFailedLogin,
        onClick: (event) => {
          event.stopPropagation();
          dismissFailedLoginAlert({ alertId: failedLoginAlert.id });
        },
      },
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
    const bootId = health.uncleanShutdownBootId;
    items.push({
      icon: "mdi:alert-outline",
      color: theme.palette.warning.main,
      text: "Unclean shutdown",
      to: "/logs",
      secondaryAction: bootId
        ? {
            label: "Dismiss",
            icon: "mdi:close",
            ariaLabel: "Dismiss unclean shutdown alert",
            disabled: dismissingUnclean,
            onClick: (event) => {
              event.stopPropagation();
              dismissUncleanShutdown({ bootId });
            },
          }
        : undefined,
    });
  }

  if (health?.lastLogin?.time && !failedLoginAlert) {
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
      ? `${timeMatch[1]} ${timeMatch[2]}, ${timeMatch[3]}:${timeMatch[4]}`
      : timeStr;
    const terminal =
      health.lastLogin.terminal === "web"
        ? "web console"
        : health.lastLogin.terminal;
    const detailLines = [
      displaySource ? `from ${displaySource}` : "",
      terminal ? `on ${terminal}` : "",
    ].filter(Boolean);
    items.push({
      icon: "mdi:account-clock-outline",
      color: theme.palette.text.primary,
      text: `Last login: ${displayTime}`,
      to: userDetailsPath(health.lastLogin.username || currentUser?.name),
      detail: detailLines.length > 0 ? detailLines.join("\n") : undefined,
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
  } else if (failedLoginAlert) {
    statusColor = theme.palette.warning.main;
    iconName = "mdi:shield-alert-outline";
  } else if ((health?.updatesAvailable ?? 0) > 0 || health?.uncleanShutdown) {
    statusColor = theme.palette.warning.main;
    iconName = "mdi:shield-alert-outline";
    iconLink = health?.uncleanShutdown ? "/logs" : "/updates";
  }

  const handleStatusIconClick = () => {
    if (failedLoginAlert) {
      openFailedLogins();
      return;
    }
    navigate(iconLink);
  };

  const stats2 = (
    <div>
      {!health && (loadingHealth || fetchingHealth) ? (
        <AppSkeleton height={100} variant="circular" width={100} />
      ) : (
        <div onClick={handleStatusIconClick} style={{ cursor: "pointer" }}>
          <Icon color={statusColor} height={100} icon={iconName} width={100} />
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
          color={item.color}
          height={18}
          icon={item.icon}
          style={{ flexShrink: 0, ...item.iconStyle }}
          width={18}
        />
        <div style={{ minWidth: 0 }}>
          <AppTypography
            fontWeight={500}
            style={{
              color:
                item.textColor ??
                (item.color === theme.palette.text.secondary
                  ? undefined
                  : item.color),
              whiteSpace: "pre-line",
            }}
            variant="body2"
          >
            {item.text}
          </AppTypography>
          {item.detail ? (
            <AppTypography
              style={{
                color: item.detailColor ?? theme.palette.text.secondary,
                display: "block",
                marginTop: -2,
                lineHeight: 1.2,
                whiteSpace: "pre-line",
              }}
              variant="caption"
            >
              {item.detail}
            </AppTypography>
          ) : null}
        </div>
        {item.secondaryAction?.icon ? (
          <AppIconButton
            aria-label={
              item.secondaryAction.ariaLabel ?? item.secondaryAction.label
            }
            color="inherit"
            disabled={item.secondaryAction.disabled}
            onClick={item.secondaryAction.onClick}
            size="small"
            style={{
              marginLeft: theme.spacing(0.5),
              color: "#fff",
              opacity: item.secondaryAction.disabled ? 0.5 : 0.85,
              flexShrink: 0,
            }}
          >
            <Icon height={18} icon={item.secondaryAction.icon} width={18} />
          </AppIconButton>
        ) : item.secondaryAction ? (
          <span
            aria-disabled={item.secondaryAction.disabled || undefined}
            onClick={
              item.secondaryAction.disabled
                ? undefined
                : item.secondaryAction.onClick
            }
            role="button"
            style={{
              marginLeft: theme.spacing(1),
              cursor: item.secondaryAction.disabled ? "default" : "pointer",
              opacity: item.secondaryAction.disabled ? 0.5 : 1,
              color: theme.palette.text.secondary,
              fontSize: "0.75rem",
              textDecoration: "underline",
              userSelect: "none",
            }}
          >
            {item.secondaryAction.label}
          </span>
        ) : null}
      </div>
    );

    const spacing = item.spaceBefore
      ? { marginTop: theme.spacing(1) }
      : undefined;
    const clickHandler =
      item.onClick ?? (item.to ? () => navigate(item.to!) : undefined);

    return clickHandler ? (
      <div
        key={item.text}
        onClick={clickHandler}
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
      <AppSkeleton height={18} variant="circular" width={18} />
      <AppSkeleton textVariant="body2" width={width} />
    </div>
  );

  const servicesItem = items.find((i) => i.icon === "mdi:cog-sync-outline");
  const updatesItem = items.find(
    (i) => i.icon === "mdi:package-up" || i.icon === "mdi:check-circle",
  );
  const bottomItem = items.find(
    (i) =>
      i.icon === "mdi:account-clock-outline" ||
      i.icon === "mdi:account-alert-outline",
  );
  const alertItems = items.filter(
    (i) => i !== servicesItem && i !== updatesItem && i !== bottomItem,
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
      {bottomItem
        ? renderItem(bottomItem)
        : health?.lastLogin?.time === undefined &&
            (loadingHealth || fetchingHealth)
          ? skeletonRow("s-lastlogin", "18ch", true)
          : null}
    </div>
  );
  const failedLoginGridColumns =
    "minmax(0, 1.2fr) minmax(0, 1fr) minmax(0, 1fr) auto";

  return (
    <>
      <DashboardCard
        avatarIcon="mdi:heart-pulse"
        contentLayout={[1.5, 1]}
        stats={stats}
        stats2={stats2}
        title="System Health"
      />

      <GeneralDialog
        fullWidth
        maxWidth="md"
        onClose={() => setFailedLoginsOpen(false)}
        open={failedLoginsOpen}
      >
        <AppDialogTitle
          style={{
            display: "flex",
            alignItems: "center",
            gap: 8,
            borderBottom: `1px solid ${theme.palette.divider}`,
          }}
        >
          <Icon
            color={theme.palette.warning.main}
            height={22}
            icon="mdi:account-alert-outline"
            width={22}
          />
          <AppTypography variant="h6">Failed logins</AppTypography>
        </AppDialogTitle>
        <AppDialogContent style={{ paddingTop: 12 }}>
          {failedLoginEventsPending ? (
            <div style={{ display: "grid", gap: 8 }}>
              {skeletonRow("failed-login-1", "28ch")}
              {skeletonRow("failed-login-2", "24ch")}
              {skeletonRow("failed-login-3", "22ch")}
            </div>
          ) : failedLoginEventsError ? (
            <AppAlert severity="error">
              {failedLoginEventsErrorValue instanceof Error
                ? failedLoginEventsErrorValue.message
                : "Failed login history unavailable"}
            </AppAlert>
          ) : failedLoginEvents.length === 0 ? (
            <AppTypography color="text.secondary" variant="body2">
              No failed login attempts found before this session.
            </AppTypography>
          ) : (
            <div style={{ display: "grid", gap: 0 }}>
              <div
                style={{
                  display: "grid",
                  gridTemplateColumns: failedLoginGridColumns,
                  gap: 12,
                  padding: "0 0 8px",
                }}
              >
                {["Time", "Username", "Source", "Result"].map((label) => (
                  <AppTypography
                    color="text.secondary"
                    key={label}
                    style={{ fontSize: "0.65rem" }}
                    variant="overline"
                  >
                    {label}
                  </AppTypography>
                ))}
              </div>
              {failedLoginEvents.map((login, index) => (
                <React.Fragment key={login.id || `${login.username}-${index}`}>
                  <div
                    style={{
                      display: "grid",
                      gridTemplateColumns: failedLoginGridColumns,
                      gap: 12,
                      alignItems: "center",
                      padding: "8px 0",
                    }}
                  >
                    <AppTypography fontWeight={500} noWrap variant="body2">
                      {login.time || "-"}
                    </AppTypography>
                    <AppTypography fontWeight={500} noWrap variant="body2">
                      {login.username || "unknown"}
                    </AppTypography>
                    <AppTypography
                      color="text.secondary"
                      noWrap
                      variant="body2"
                    >
                      {loginAttemptLocation(login)}
                    </AppTypography>
                    <Chip
                      color="error"
                      label="Failed"
                      size="small"
                      style={{ fontSize: "0.7rem" }}
                      variant="soft"
                    />
                  </div>
                  {index < failedLoginEvents.length - 1 ? <AppDivider /> : null}
                </React.Fragment>
              ))}
            </div>
          )}
        </AppDialogContent>
        <AppDialogActions
          style={{
            padding: 8,
            borderTop: `1px solid ${theme.palette.divider}`,
          }}
        >
          <AppButton color="inherit" onClick={() => setFailedLoginsOpen(false)}>
            Close
          </AppButton>
          {failedLoginAlert?.id ? (
            <AppButton
              color="warning"
              disabled={dismissingFailedLogin}
              onClick={() =>
                dismissFailedLoginAlert({ alertId: failedLoginAlert.id })
              }
              startIcon={<Icon height={18} icon="mdi:check" width={18} />}
              variant="contained"
            >
              {dismissingFailedLogin ? "Dismissing..." : "Dismiss alert"}
            </AppButton>
          ) : null}
        </AppDialogActions>
      </GeneralDialog>
    </>
  );
};

export default SystemHealth;
