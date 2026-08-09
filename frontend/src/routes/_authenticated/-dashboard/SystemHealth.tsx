import { Icon } from "@iconify/react";
import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import {
  Fragment,
  useCallback,
  useState,
  type CSSProperties,
  type MouseEvent,
} from "react";

import { type AccountUserLogin, linuxio, useCallMutation } from "@/api";
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

const HEALTH_REFETCH_MS = 50000;

type HealthRoute = "/accounts" | "/logs" | "/services" | "/updates";

interface HealthItem {
  color: string;
  detail?: string;
  detailColor?: string;
  icon: string;
  iconStyle?: CSSProperties;
  onClick?: () => void;
  serviceSearch?: { service?: string };
  accountSearch?: ReturnType<typeof accountDetailSearch>;
  secondaryAction?: {
    label: string;
    icon?: string;
    ariaLabel?: string;
    onClick: (event: MouseEvent) => void;
    disabled?: boolean;
  };
  spaceBefore?: boolean;
  text: string;
  textColor?: string;
  to?: HealthRoute;
}

function pluralize(count: number, singular: string, plural: string): string {
  return `${count} ${count === 1 ? singular : plural}`;
}

function accountDetailSearch(
  username: string | undefined | null,
  focusLogin?: {
    eventId?: string;
    failedLoginAlertId?: string;
    autoDismissFailedLoginAlert?: boolean;
  },
) {
  return {
    autoDismissFailedLoginAlert: focusLogin?.autoDismissFailedLoginAlert,
    failedLoginAlertId: focusLogin?.failedLoginAlertId,
    focusLoginEventId: focusLogin?.eventId,
    user: username?.trim() || undefined,
  };
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

interface FailedLoginsProps {
  onOpenFailedLogins: () => void;
}

const HealthStats = ({ onOpenFailedLogins }: FailedLoginsProps) => {
  const theme = useAppTheme();
  const { user: currentUser } = useAuth();

  const { data: health } = useSuspenseQuery({
    ...linuxio.system.get_health_summary,
    refetchInterval: HEALTH_REFETCH_MS,
  });

  const { mutate: dismissUncleanShutdown, isPending: dismissingUnclean } =
    useCallMutation(linuxio.system.dismiss_unclean_shutdown);

  const { mutate: dismissFailedLoginAlert, isPending: dismissingFailedLogin } =
    useCallMutation(linuxio.system.dismiss_failed_login_alert);

  const items: HealthItem[] = [];
  const failedLoginAlert = health?.failedLoginAlert;

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
      to: "/services",
      serviceSearch: failed ? { service: failed } : undefined,
      detail: health.failedServices?.slice(0, 2).join(", "),
    });
  }

  if (failedLoginAlert) {
    items.push({
      icon: "mdi:account-alert-outline",
      color: theme.palette.error.main,
      text: `${pluralize(failedLoginAlert.count, "failed login attempt", "failed login attempts")}\nbefore this session`,
      onClick: onOpenFailedLogins,
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

  if (!health?.upToDate) {
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
    const lastLoginUsername = health.lastLogin.username;
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
      to: "/accounts",
      accountSearch: accountDetailSearch(
        lastLoginUsername || currentUser?.name,
      ),
      detail: detailLines.length > 0 ? detailLines.join("\n") : undefined,
      spaceBefore: true,
      iconStyle: { transform: "translateY(-6px)" },
    });
  }

  const renderItem = (item: HealthItem) => {
    const mainContent = (
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
      </div>
    );

    const actionStyle: CSSProperties = {
      display: "flex",
      alignItems: "center",
      minWidth: 0,
      flex: 1,
      color: "inherit",
      textAlign: "left",
      textDecoration: "none",
      cursor: "pointer",
    };
    const navigationContent =
      item.to === "/services" ? (
        <Link search={item.serviceSearch} style={actionStyle} to="/services">
          {mainContent}
        </Link>
      ) : item.to === "/accounts" ? (
        <Link search={item.accountSearch} style={actionStyle} to="/accounts">
          {mainContent}
        </Link>
      ) : item.to ? (
        <Link style={actionStyle} to={item.to}>
          {mainContent}
        </Link>
      ) : null;
    const content =
      navigationContent ??
      (item.onClick ? (
        <AppButton
          color="inherit"
          onClick={item.onClick}
          style={{ ...actionStyle, border: 0, padding: 0 }}
        >
          {mainContent}
        </AppButton>
      ) : (
        mainContent
      ));

    const row = (
      <div style={{ display: "flex", alignItems: "center", minWidth: 0 }}>
        {content}
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
          <AppButton
            color="inherit"
            disabled={item.secondaryAction.disabled}
            onClick={item.secondaryAction.onClick}
            size="small"
            style={{
              marginLeft: theme.spacing(1),
              color: theme.palette.text.secondary,
              fontSize: "0.75rem",
              minWidth: 0,
              opacity: item.secondaryAction.disabled ? 0.5 : 1,
              padding: 0,
              textDecoration: "underline",
              userSelect: "none",
            }}
          >
            {item.secondaryAction.label}
          </AppButton>
        ) : null}
      </div>
    );

    const spacing = item.spaceBefore
      ? { marginTop: theme.spacing(1) }
      : undefined;

    return (
      <div key={item.text} style={spacing}>
        {row}
      </div>
    );
  };

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

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        alignSelf: "flex-start",
        width: "fit-content",
      }}
    >
      {servicesItem ? renderItem(servicesItem) : null}
      {alertItems.map(renderItem)}
      {updatesItem ? renderItem(updatesItem) : null}
      {bottomItem ? renderItem(bottomItem) : null}
    </div>
  );
};

const HealthShield = ({ onOpenFailedLogins }: FailedLoginsProps) => {
  const theme = useAppTheme();
  const navigate = useNavigate();

  const { data: health } = useSuspenseQuery({
    ...linuxio.system.get_health_summary,
    refetchInterval: HEALTH_REFETCH_MS,
  });

  const failedLoginAlert = health?.failedLoginAlert;

  let statusColor = theme.palette.success.dark;
  let iconName = "mdi:shield-check-outline";
  let iconLink: HealthRoute = "/updates";

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
      onOpenFailedLogins();
      return;
    }
    navigate({ to: iconLink });
  };

  return (
    <AppIconButton
      aria-label={
        failedLoginAlert ? "Review failed login alerts" : "View system status"
      }
      onClick={handleStatusIconClick}
      style={{
        backgroundColor: "transparent",
        cursor: "pointer",
      }}
    >
      <Icon color={statusColor} height={100} icon={iconName} width={100} />
    </AppIconButton>
  );
};

interface FailedLoginsDialogProps {
  onClose: () => void;
  open: boolean;
}

const FailedLoginsDialog = ({ onClose, open }: FailedLoginsDialogProps) => {
  const theme = useAppTheme();

  const { data: health } = useSuspenseQuery({
    ...linuxio.system.get_health_summary,
    refetchInterval: HEALTH_REFETCH_MS,
  });

  const {
    data: failedLoginEvents = [],
    isLoading: failedLoginEventsLoading,
    isError: failedLoginEventsError,
    error: failedLoginEventsErrorValue,
  } = useQuery({
    ...linuxio.system.list_failed_login_events({ limit: "24" }),
    enabled: open,
    refetchInterval: open ? 30000 : false,
  });

  const { mutate: dismissFailedLoginAlert, isPending: dismissingFailedLogin } =
    useCallMutation(linuxio.system.dismiss_failed_login_alert, {
      success: () => {
        onClose();
      },
    });

  const failedLoginAlert = health?.failedLoginAlert;

  const skeletonRow = (key: string, width: string) => (
    <div
      key={key}
      style={{
        display: "flex",
        alignItems: "center",
        gap: theme.spacing(1),
      }}
    >
      <AppSkeleton height={18} variant="circular" width={18} />
      <AppSkeleton textVariant="body2" width={width} />
    </div>
  );

  const failedLoginGridColumns =
    "minmax(0, 1.2fr) minmax(0, 1fr) minmax(0, 1fr) auto";

  return (
    <GeneralDialog fullWidth maxWidth="md" onClose={onClose} open={open}>
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
        {failedLoginEventsLoading ? (
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
              <Fragment key={login.id || `${login.username}-${index}`}>
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
                  <AppTypography color="text.secondary" noWrap variant="body2">
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
              </Fragment>
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
        <AppButton color="inherit" onClick={onClose}>
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
  );
};

const SystemHealth = () => {
  const [failedLoginsOpen, setFailedLoginsOpen] = useState(false);
  const openFailedLogins = useCallback(() => setFailedLoginsOpen(true), []);
  const closeFailedLogins = useCallback(() => setFailedLoginsOpen(false), []);

  return (
    <>
      <DashboardCard
        avatarIcon="mdi:heart-pulse"
        contentLayout={[1.5, 1]}
        stats={<HealthStats onOpenFailedLogins={openFailedLogins} />}
        stats2={<HealthShield onOpenFailedLogins={openFailedLogins} />}
        title="System Health"
      />

      <FailedLoginsDialog onClose={closeFailedLogins} open={failedLoginsOpen} />
    </>
  );
};

export default SystemHealth;
