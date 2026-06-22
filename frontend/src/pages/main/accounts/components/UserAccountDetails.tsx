import { Icon } from "@iconify/react";
import { useQueryClient } from "@tanstack/react-query";
import React, { useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";

import {
  type AccountActiveSession,
  type AccountHomeHealth,
  type AccountPasswordState,
  type AccountSSHAccess,
  type AccountUser,
  type AccountUserDetails,
  type AccountUserLogin,
  type AccountUserProcess,
  linuxio,
} from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import { DetailRow } from "@/components/cards/UnitInfoPanelCard";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppDivider from "@/components/ui/AppDivider";
import AppGrid from "@/components/ui/AppGrid";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";
import { SEMANTIC_STATUS_COLORS } from "@/theme/colors";

import "./user-account-details.css";

interface UserDetailsPanelProps {
  currentUsername?: string;
  onClose: () => void;
  user: AccountUser;
}

function useAccountDetails(username: string) {
  return linuxio.accounts.get_user_details.useQuery(username, {
    enabled: Boolean(username),
    refetchInterval: 10000,
  });
}

function getLoginLocation(login: AccountUserLogin): string {
  if (login.source) {
    return login.source;
  }
  if (login.terminal.startsWith("tty")) {
    return "Local console";
  }
  return "Local";
}

function passwordLabel(password: AccountPasswordState | undefined): string {
  if (!password) {
    return "-";
  }
  if (password.error) {
    return "Unavailable";
  }
  if (password.locked) {
    return "Locked";
  }
  if (!password.hasPassword) {
    return "No password set";
  }
  return "Password enabled";
}

function passwordColor(password: AccountPasswordState | undefined): string {
  if (!password || password.error) {
    return "var(--app-palette-warning-main)";
  }
  if (password.locked || !password.hasPassword) {
    return "var(--app-palette-warning-main)";
  }
  return "var(--app-palette-success-main)";
}

function expiryLabel(password: AccountPasswordState | undefined): string {
  if (!password || password.error) {
    return "-";
  }
  if (!password.expires) {
    return "Never";
  }
  if (password.expiresInDays === undefined) {
    return password.expires;
  }
  if (password.expiresInDays < 0) {
    return `${password.expires} (expired)`;
  }
  return `${password.expires} (${password.expiresInDays} days)`;
}

function sessionLocation(session: AccountActiveSession): string {
  if (session.source) {
    return session.source;
  }
  if (session.terminal.startsWith("tty")) {
    return "Local console";
  }
  return "Local";
}

function loginStatusLabel(login: AccountUserLogin): string {
  return login.status === "failed" ? "Failed" : "Success";
}

const failedLoginStatusColor = SEMANTIC_STATUS_COLORS.error;

function loginStatusColor(login: AccountUserLogin) {
  return login.status === "failed" ? failedLoginStatusColor : "success";
}

function isSessionActive(idle: string | undefined): boolean {
  const value = idle?.trim();
  return !value || value === ".";
}

function sessionStatusLabel(): string {
  return "Active";
}

function sessionStatusColor(): "success" {
  return "success";
}

function formatSessionIdle(idle: string): string {
  const match = idle.match(/^(\d+):([0-5]\d)$/);
  if (!match) {
    return idle;
  }

  const hours = Number(match[1]);
  const minutes = Number(match[2]);
  if (hours === 0) {
    return `${minutes}m`;
  }
  if (minutes === 0) {
    return `${hours}h`;
  }
  return `${hours}h ${minutes}m`;
}

function sessionStatusTooltip(idle: string | undefined): string {
  if (isSessionActive(idle)) return "Active session";
  const value = idle?.trim();
  if (value === "?") return "Active session. Terminal idle is unavailable.";
  if (value === "old")
    return "Active session. Terminal idle for over 24 hours.";
  return `Active session. Terminal idle for ${formatSessionIdle(value || "")}.`;
}

const activityChipStyle: React.CSSProperties = {
  fontSize: "0.65rem",
  height: 20,
  width: 65,
  minWidth: 65,
};

function loginEventKey(login: AccountUserLogin): string {
  return (
    login.id ||
    [
      login.status,
      login.startedAt || login.time,
      login.terminal,
      login.source,
    ].join("|")
  );
}

const LoadingRows: React.FC<{ rows?: number }> = ({ rows = 4 }) => (
  <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
    {Array.from({ length: rows }).map((_, index) => (
      <div
        key={index}
        style={{
          height: 22,
          borderRadius: 4,
          backgroundColor: "var(--app-palette-action-hover)",
        }}
      />
    ))}
  </div>
);

const InlineError: React.FC<{ message: string }> = ({ message }) => (
  <AppAlert severity="warning">{message}</AppAlert>
);

const topCardHeaderStyle: React.CSSProperties = {
  minHeight: 40,
  marginBottom: 12,
};

const TopCardHeader: React.FC<{
  icon: string;
  iconColor: string;
  title: string;
  subtitle: string;
  right?: React.ReactNode;
}> = ({ icon, iconColor, title, subtitle, right }) => (
  <div
    style={{
      display: "flex",
      justifyContent: "space-between",
      alignItems: "flex-start",
      gap: 8,
      ...topCardHeaderStyle,
    }}
  >
    <div
      style={{
        display: "flex",
        alignItems: "center",
        gap: 8,
        minWidth: 0,
        flex: 1,
      }}
    >
      <div
        style={{
          width: 36,
          height: 36,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          flexShrink: 0,
        }}
      >
        <Icon color={iconColor} height={30} icon={icon} width={30} />
      </div>
      <div style={{ minWidth: 0 }}>
        <AppTypography fontWeight={700} noWrap variant="subtitle2">
          {title}
        </AppTypography>
        <AppTypography
          color="text.secondary"
          style={{ display: "block", marginTop: 2 }}
          variant="caption"
        >
          {subtitle}
        </AppTypography>
      </div>
    </div>
    {right && <div style={{ flexShrink: 0 }}>{right}</div>}
  </div>
);

const DetailText: React.FC<{
  children: React.ReactNode;
  color?: string;
  nowrap?: boolean;
}> = ({ children, color, nowrap }) => (
  <span
    style={{
      fontSize: "0.75rem",
      fontWeight: 500,
      color,
      whiteSpace: nowrap ? "nowrap" : undefined,
    }}
  >
    {children}
  </span>
);

interface ActivityHeader {
  active?: boolean;
  direction?: "asc" | "desc";
  hiddenXs?: boolean;
  label: string;
  onClick?: () => void;
}

const ActivitySection: React.FC<{
  title: string;
  subtitle: string;
  icon?: string;
  iconColor?: string;
  headers: ActivityHeader[];
  gridClassName: string;
  metaText: string;
  className?: string;
  children: React.ReactNode;
}> = ({
  title,
  subtitle,
  icon,
  iconColor,
  headers,
  gridClassName,
  metaText,
  className,
  children,
}) => (
  <FrostedCard
    className={`account-activity-card ${className || ""}`.trim()}
    style={{ padding: 12 }}
  >
    <div className="account-activity-section-header">
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 8,
          minWidth: 0,
        }}
      >
        {icon && (
          <div
            style={{
              width: 32,
              height: 32,
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              flexShrink: 0,
            }}
          >
            <Icon color={iconColor} height={26} icon={icon} width={26} />
          </div>
        )}
        <div style={{ minWidth: 0 }}>
          <AppTypography fontWeight={700} noWrap variant="subtitle2">
            {title}
          </AppTypography>
          {subtitle && (
            <AppTypography
              color="text.secondary"
              style={{ display: "block", marginTop: 2 }}
              variant="caption"
            >
              {subtitle}
            </AppTypography>
          )}
        </div>
      </div>
      <AppTypography color="text.secondary" noWrap variant="caption">
        {metaText}
      </AppTypography>
    </div>

    <div className={`${gridClassName} account-activity-column-header`}>
      {headers.map((header) => (
        <AppTypography
          className={header.hiddenXs ? "account-hidden-xs" : undefined}
          color="text.secondary"
          key={header.label}
          onClick={header.onClick}
          style={{
            fontSize: "0.65rem",
            cursor: header.onClick ? "pointer" : undefined,
            userSelect: header.onClick ? "none" : undefined,
          }}
          variant="overline"
        >
          {header.label}
          {header.active ? (header.direction === "asc" ? " ↑" : " ↓") : ""}
        </AppTypography>
      ))}
    </div>
    <AppDivider />

    <div className="account-activity-section-body custom-scrollbar">
      {children}
    </div>
  </FrostedCard>
);

const ActivityEmpty: React.FC<{ children: React.ReactNode }> = ({
  children,
}) => (
  <div style={{ paddingBlock: 14, textAlign: "center" }}>
    <AppTypography color="text.secondary" variant="body2">
      {children}
    </AppTypography>
  </div>
);

const ActivityLoading: React.FC<{ rows?: number }> = ({ rows }) => (
  <div style={{ paddingBlock: 12 }}>
    <LoadingRows rows={rows} />
  </div>
);

export const UserDetailsPanel: React.FC<UserDetailsPanelProps> = ({
  user,
  onClose,
}) => {
  const theme = useAppTheme();
  const {
    data: details,
    isPending,
    isError,
    error,
  } = useAccountDetails(user.username);

  const adminColor = details?.admin.isAdmin
    ? theme.palette.warning.main
    : theme.palette.text.secondary;
  const securityIconColor = details?.admin.isAdmin
    ? theme.palette.warning.main
    : theme.palette.primary.main;

  return (
    <FrostedCard
      style={{
        padding: 12,
        height: "100%",
        flex: 1,
        display: "flex",
        flexDirection: "column",
      }}
    >
      <TopCardHeader
        icon="mdi:shield-account"
        iconColor={securityIconColor}
        right={
          <AppActionIconButton
            icon="mdi:close"
            iconSize={20}
            label="Close details"
            onClick={onClose}
          />
        }
        subtitle="Admin privileges, password status, and elevated groups"
        title="Access & security"
      />

      {isPending ? (
        <LoadingRows />
      ) : isError ? (
        <InlineError
          message={
            error instanceof Error
              ? error.message
              : "Account detail is unavailable"
          }
        />
      ) : !details ? (
        <LoadingRows />
      ) : (
        <>
          <div
            className="custom-scrollbar"
            style={{ flex: 1, overflowX: "auto" }}
          >
            <div style={{ minWidth: "max-content" }}>
              <DetailRow label="Admin" noBorder>
                <DetailText color={adminColor}>
                  {details.admin.isAdmin
                    ? "Elevated account"
                    : "Standard account"}
                </DetailText>
              </DetailRow>
              <DetailRow label="Password">
                <DetailText color={passwordColor(details.password)}>
                  {passwordLabel(details.password)}
                </DetailText>
              </DetailRow>
              <DetailRow label="Changed">
                <DetailText>{details.password.lastChanged || "-"}</DetailText>
              </DetailRow>
              <DetailRow label="Expires">
                <DetailText>{expiryLabel(details.password)}</DetailText>
              </DetailRow>
              <DetailRow label="Sessions">
                <DetailText>
                  {details.activeSessions.length
                    ? `${details.activeSessions.length} active`
                    : "No active sessions"}
                </DetailText>
              </DetailRow>
              <DetailRow label="Failed">
                <DetailText
                  color={
                    details.failedLoginAttempts > 0
                      ? theme.palette.warning.main
                      : undefined
                  }
                >
                  {details.failedLoginAttemptsAvailable
                    ? `${details.failedLoginAttempts} attempts`
                    : "Unavailable"}
                </DetailText>
              </DetailRow>
            </div>
          </div>

          <div style={{ marginTop: 12 }}>
            <AppTypography
              color="text.secondary"
              style={{ display: "block", marginBottom: 6 }}
              variant="caption"
            >
              Elevated groups
            </AppTypography>
            <div style={{ display: "flex", flexWrap: "wrap", gap: 4 }}>
              {details.admin.groups.length ? (
                details.admin.groups.map((group) => (
                  <Chip
                    color="warning"
                    key={`${user.username}-${group}`}
                    label={group}
                    size="small"
                    style={{ fontSize: "0.65rem", height: 20 }}
                    variant="soft"
                  />
                ))
              ) : (
                <Chip
                  label="none"
                  size="small"
                  style={{ fontSize: "0.65rem", height: 20 }}
                  variant="soft"
                />
              )}
            </div>
          </div>
        </>
      )}
    </FrostedCard>
  );
};

export const UserActivityCard: React.FC<{ username: string }> = ({
  username,
}) => {
  const theme = useAppTheme();
  const queryClient = useQueryClient();
  const [searchParams] = useSearchParams();
  const {
    data: details,
    isPending: detailsPending,
    isError: detailsError,
    error: detailsErrorValue,
  } = useAccountDetails(username);
  const {
    data: logins = [],
    isPending: loginsPending,
    isError: loginsError,
    error: loginsErrorValue,
  } = linuxio.accounts.list_user_logins.useQuery(username, {
    enabled: Boolean(username),
    refetchInterval: 30000,
  });
  const sessions = details?.activeSessions ?? [];
  const loginRowRefs = useRef<Record<string, HTMLDivElement | null>>({});
  const dismissedAlertRef = useRef("");
  const [flashingLoginKey, setFlashingLoginKey] = useState("");
  const focusLoginEventId = searchParams.get("focusLoginEventId");
  const failedLoginAlertId = searchParams.get("failedLoginAlertId");
  const autoDismissFailedLoginAlert =
    searchParams.get("autoDismissFailedLoginAlert") === "1";
  const { mutate: dismissFailedLoginAlert } =
    linuxio.system.dismiss_failed_login_alert.useMutation({
      onSuccess: () => {
        queryClient.invalidateQueries({
          queryKey: linuxio.system.get_health_summary.queryKey(),
        });
      },
    });
  const [pendingKillSession, setPendingKillSession] =
    useState<AccountActiveSession | null>(null);
  const [killError, setKillError] = useState<string>("");
  const { mutate: terminateSession, isPending: terminateIsPending } =
    linuxio.accounts.terminate_session.useMutation({
      onSuccess: () => {
        setPendingKillSession(null);
        setKillError("");
        queryClient.invalidateQueries({
          queryKey: linuxio.accounts.get_user_details.queryKey(username),
        });
      },
      onError: (error) => {
        setKillError(
          error instanceof Error
            ? error.message
            : "Failed to terminate session",
        );
      },
    });
  const cancelKill = () => {
    if (terminateIsPending) return;
    setPendingKillSession(null);
    setKillError("");
  };
  const confirmKill = () => {
    if (!pendingKillSession) return;
    terminateSession({
      sessionId: pendingKillSession.sessionId ?? "",
      pid: String(pendingKillSession.pid ?? 0),
    });
  };
  const focusedLoginKey = useMemo(() => {
    if (!focusLoginEventId) {
      return "";
    }

    const focusedLogin = logins.find((login) => login.id === focusLoginEventId);
    return focusedLogin ? loginEventKey(focusedLogin) : "";
  }, [focusLoginEventId, logins]);

  useEffect(() => {
    if (!focusedLoginKey) {
      return;
    }

    const row = loginRowRefs.current[focusedLoginKey];
    if (!row) {
      return;
    }

    row.scrollIntoView({
      behavior: "smooth",
      block: "center",
      inline: "nearest",
    });
    setFlashingLoginKey(focusedLoginKey);
    if (
      autoDismissFailedLoginAlert &&
      failedLoginAlertId &&
      dismissedAlertRef.current !== failedLoginAlertId
    ) {
      dismissedAlertRef.current = failedLoginAlertId;
      dismissFailedLoginAlert({ alertId: failedLoginAlertId });
    }

    const timeout = window.setTimeout(() => {
      setFlashingLoginKey((current) =>
        current === focusedLoginKey ? "" : current,
      );
    }, 2400);

    return () => window.clearTimeout(timeout);
  }, [
    autoDismissFailedLoginAlert,
    dismissFailedLoginAlert,
    failedLoginAlertId,
    focusedLoginKey,
  ]);

  return (
    <div className="account-activity-stack">
      <ActivitySection
        className="account-activity-card--sessions"
        gridClassName="account-events-grid"
        headers={[
          { label: "Started", hiddenXs: true },
          { label: "Terminal" },
          { label: "Source", hiddenXs: true },
          { label: "Status" },
        ]}
        icon="mdi:account-clock"
        iconColor={theme.palette.primary.main}
        metaText={
          detailsPending || !details
            ? "Checking sessions"
            : `${sessions.length} active ${sessions.length === 1 ? "session" : "sessions"}`
        }
        subtitle="Current authenticated sessions"
        title="Active sessions"
      >
        {detailsPending ? (
          <ActivityLoading rows={2} />
        ) : detailsError ? (
          <div style={{ padding: 12 }}>
            <InlineError
              message={
                detailsErrorValue instanceof Error
                  ? detailsErrorValue.message
                  : "Session details unavailable"
              }
            />
          </div>
        ) : !details ? (
          <ActivityLoading rows={2} />
        ) : sessions.length === 0 ? (
          <ActivityEmpty>No active sessions.</ActivityEmpty>
        ) : (
          sessions.map((session, index) => (
            <React.Fragment key={`${session.terminal}-${session.startedAt}`}>
              <div className="account-events-grid account-activity-row">
                <AppTypography
                  className="account-hidden-xs"
                  fontWeight={500}
                  noWrap
                  variant="body2"
                >
                  {session.startedAt || "-"}
                </AppTypography>
                <div className="account-session-terminal">
                  <div className="account-session-terminal-info">
                    <AppTypography fontWeight={500} noWrap variant="body2">
                      {session.terminal || "-"}
                    </AppTypography>
                    {session.pid ? (
                      <AppTypography fontWeight={500} noWrap variant="body2">
                        (PID {session.pid})
                      </AppTypography>
                    ) : null}
                  </div>
                  {session.pid || session.sessionId ? (
                    <AppActionIconButton
                      ariaLabel={`Terminate session ${session.terminal}`}
                      className="account-session-kill"
                      icon="mdi:trash-can-outline"
                      iconSize={16}
                      label="Terminate session"
                      onClick={() => {
                        setKillError("");
                        setPendingKillSession(session);
                      }}
                    />
                  ) : null}
                </div>
                <AppTypography
                  className="account-hidden-xs"
                  fontWeight={500}
                  noWrap
                  variant="body2"
                >
                  {sessionLocation(session)}
                </AppTypography>
                <AppTooltip title={sessionStatusTooltip(session.idle)}>
                  <div>
                    <Chip
                      color={sessionStatusColor()}
                      label={sessionStatusLabel()}
                      size="small"
                      style={activityChipStyle}
                      variant="soft"
                    />
                  </div>
                </AppTooltip>
              </div>
              {index < sessions.length - 1 && <AppDivider />}
            </React.Fragment>
          ))
        )}
      </ActivitySection>

      <ActivitySection
        className="account-activity-card--logins"
        gridClassName="account-events-grid"
        headers={[
          { label: "Time", hiddenXs: true },
          { label: "Terminal", hiddenXs: true },
          { label: "Source" },
          { label: "Result" },
        ]}
        icon="mdi:history"
        iconColor={theme.palette.primary.main}
        metaText={
          loginsPending
            ? "Loading login history"
            : `${logins.length} recent ${logins.length === 1 ? "event" : "events"}`
        }
        subtitle="Recent login events"
        title="Login history"
      >
        {loginsPending ? (
          <ActivityLoading rows={3} />
        ) : loginsError ? (
          <div style={{ padding: 12 }}>
            <InlineError
              message={
                loginsErrorValue instanceof Error
                  ? loginsErrorValue.message
                  : "Login history unavailable"
              }
            />
          </div>
        ) : logins.length === 0 ? (
          <ActivityEmpty>No login history found.</ActivityEmpty>
        ) : (
          logins.map((login, index) => (
            <React.Fragment key={loginEventKey(login)}>
              <div
                className={[
                  "account-events-grid",
                  "account-activity-row",
                  flashingLoginKey === loginEventKey(login)
                    ? "account-activity-row--flash"
                    : "",
                ]
                  .filter(Boolean)
                  .join(" ")}
                ref={(node) => {
                  loginRowRefs.current[loginEventKey(login)] = node;
                }}
              >
                <AppTypography
                  className="account-hidden-xs"
                  fontWeight={500}
                  noWrap
                  variant="body2"
                >
                  {login.time || "-"}
                </AppTypography>
                <AppTypography
                  className="account-hidden-xs"
                  fontWeight={500}
                  noWrap
                  variant="body2"
                >
                  {login.terminal || "-"}
                </AppTypography>
                <AppTypography fontWeight={500} noWrap variant="body2">
                  {getLoginLocation(login)}
                </AppTypography>
                <div>
                  <Chip
                    color={loginStatusColor(login)}
                    label={loginStatusLabel(login)}
                    size="small"
                    style={activityChipStyle}
                    variant="soft"
                  />
                </div>
              </div>
              {index < logins.length - 1 && <AppDivider />}
            </React.Fragment>
          ))
        )}
      </ActivitySection>

      <GeneralDialog
        fullWidth
        maxWidth="xs"
        onClose={cancelKill}
        open={pendingKillSession !== null}
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
            color={theme.palette.error.main}
            height={22}
            icon="mdi:close-octagon"
            width={22}
          />
          <AppTypography variant="h6">Terminate session</AppTypography>
        </AppDialogTitle>
        <AppDialogContent style={{ paddingTop: 12 }}>
          <AppTypography color="text.secondary" variant="body2">
            End the active session for{" "}
            <strong>{pendingKillSession?.terminal || "this session"}</strong>
            {pendingKillSession?.source
              ? ` from ${pendingKillSession.source}`
              : ""}
            ? Any unsaved work in that session will be lost.
          </AppTypography>
          {pendingKillSession?.sessionId ? (
            <AppTypography
              color="text.disabled"
              style={{ display: "block", marginTop: 8 }}
              variant="caption"
            >
              loginctl session {pendingKillSession.sessionId}
              {pendingKillSession.pid ? ` · PID ${pendingKillSession.pid}` : ""}
            </AppTypography>
          ) : pendingKillSession?.pid ? (
            <AppTypography
              color="text.disabled"
              style={{ display: "block", marginTop: 8 }}
              variant="caption"
            >
              PID {pendingKillSession.pid} (no logind session — will SIGHUP the
              leader)
            </AppTypography>
          ) : null}
          {killError ? (
            <AppAlert severity="error" style={{ marginTop: 12 }}>
              {killError}
            </AppAlert>
          ) : null}
        </AppDialogContent>
        <AppDialogActions
          style={{
            padding: 8,
            borderTop: `1px solid ${theme.palette.divider}`,
          }}
        >
          <AppButton
            color="inherit"
            disabled={terminateIsPending}
            onClick={cancelKill}
          >
            Cancel
          </AppButton>
          <AppButton
            color="error"
            disabled={terminateIsPending}
            onClick={confirmKill}
            startIcon={<Icon height={18} icon="mdi:close" width={18} />}
            variant="contained"
          >
            {terminateIsPending ? "Terminating..." : "Terminate"}
          </AppButton>
        </AppDialogActions>
      </GeneralDialog>
    </div>
  );
};

function homeStatus(home: AccountHomeHealth | undefined): string {
  if (!home) return "-";
  if (home.error) return "Unavailable";
  if (!home.exists) return "Missing";
  if (!home.isDirectory) return "Not a directory";
  if (!home.ownerMatches) return "Owner mismatch";
  return "Healthy";
}

function sshStatus(ssh: AccountSSHAccess | undefined): string {
  if (!ssh) return "-";
  if (ssh.error) return "Unavailable";
  if (!ssh.sshDirExists) return "No .ssh directory";
  if (!ssh.authorizedKeysExists) return "No authorized_keys";
  return `${ssh.authorizedKeysCount} authorized keys`;
}

const HomeAndSSHCard: React.FC<{ details: AccountUserDetails }> = ({
  details,
}) => {
  const theme = useAppTheme();

  return (
    <FrostedCard style={{ padding: 12, height: "100%", width: "100%" }}>
      <TopCardHeader
        icon="mdi:home-lock"
        iconColor={theme.palette.primary.main}
        subtitle="Directory ownership, permissions, and authorized keys"
        title="Home & SSH access"
      />

      <DetailRow label="Home" noBorder>
        <DetailText>{homeStatus(details.home)}</DetailText>
      </DetailRow>
      <DetailRow label="Owner">
        <DetailText>
          {details.home.exists
            ? details.home.ownerMatches
              ? `UID ${details.home.ownerUid}`
              : `UID ${details.home.ownerUid} mismatch`
            : "-"}
        </DetailText>
      </DetailRow>
      <DetailRow label="Group">
        <DetailText>
          {details.home.groupName ||
            (details.home.groupGid !== undefined ? details.home.groupGid : "-")}
        </DetailText>
      </DetailRow>
      <DetailRow label="Mode">
        <DetailText>{details.home.mode || "-"}</DetailText>
      </DetailRow>
      <DetailRow label="SSH">
        <DetailText>{sshStatus(details.ssh)}</DetailText>
      </DetailRow>
      <DetailRow label=".ssh mode">
        <DetailText>{details.ssh.sshDirMode || "-"}</DetailText>
      </DetailRow>
      <DetailRow label="Keys mode">
        <DetailText>{details.ssh.authorizedKeysMode || "-"}</DetailText>
      </DetailRow>
    </FrostedCard>
  );
};

type ProcessSortField = "pid" | "command" | "cpu" | "memory";
type SortDirection = "asc" | "desc";

const PROCESS_DEFAULT_DIRECTION: Record<ProcessSortField, SortDirection> = {
  pid: "asc",
  command: "asc",
  cpu: "desc",
  memory: "desc",
};

const EMPTY_PROCESS_TOP: AccountUserProcess[] = [];

const ProcessCard: React.FC<{ details: AccountUserDetails }> = ({
  details,
}) => {
  const theme = useAppTheme();
  const [sortField, setSortField] = React.useState<ProcessSortField>("cpu");
  const [sortDirection, setSortDirection] = React.useState<SortDirection>(
    PROCESS_DEFAULT_DIRECTION.cpu,
  );

  const handleSort = (field: ProcessSortField) => {
    if (field === sortField) {
      setSortDirection((dir) => (dir === "asc" ? "desc" : "asc"));
    } else {
      setSortField(field);
      setSortDirection(PROCESS_DEFAULT_DIRECTION[field]);
    }
  };

  const processTop = Array.isArray(details.processes.top)
    ? details.processes.top
    : EMPTY_PROCESS_TOP;
  const processCount = Number.isFinite(details.processes.count)
    ? details.processes.count
    : processTop.length;

  const processes = React.useMemo(() => {
    const factor = sortDirection === "asc" ? 1 : -1;
    return [...processTop].sort((a, b) => {
      switch (sortField) {
        case "pid":
          return (a.pid - b.pid) * factor;
        case "command":
          return a.command.localeCompare(b.command) * factor;
        case "cpu":
          return (a.cpu - b.cpu) * factor;
        case "memory":
          return (a.memory - b.memory) * factor;
      }
    });
  }, [processTop, sortField, sortDirection]);

  const metaText = details.processes.error
    ? "Unavailable"
    : `${processCount} ${processCount === 1 ? "process" : "processes"}`;

  const headerFor = (label: string, field: ProcessSortField) => ({
    label,
    onClick: () => handleSort(field),
    active: sortField === field,
    direction: sortField === field ? sortDirection : undefined,
  });

  return (
    <ActivitySection
      className="account-activity-card--processes"
      gridClassName="account-processes-grid"
      headers={[
        headerFor("PID", "pid"),
        headerFor("Command", "command"),
        headerFor("CPU", "cpu"),
        headerFor("MEM", "memory"),
      ]}
      icon="mdi:application-cog"
      iconColor={theme.palette.primary.main}
      metaText={metaText}
      subtitle="Current process's resource usage"
      title="Owned processes"
    >
      {details.processes.error ? (
        <div style={{ padding: 12 }}>
          <InlineError message={details.processes.error} />
        </div>
      ) : processes.length === 0 ? (
        <ActivityEmpty>No processes running.</ActivityEmpty>
      ) : (
        processes.map((process, index) => (
          <React.Fragment key={process.pid}>
            <div className="account-processes-grid account-activity-row">
              <AppTypography fontWeight={500} noWrap variant="body2">
                {process.pid}
              </AppTypography>
              <AppTypography noWrap variant="body2">
                {process.command}
              </AppTypography>
              <AppTypography noWrap variant="body2">
                {process.cpu.toFixed(1)}%
              </AppTypography>
              <AppTypography noWrap variant="body2">
                {process.memory.toFixed(1)}%
              </AppTypography>
            </div>
            {index < processes.length - 1 && <AppDivider />}
          </React.Fragment>
        ))
      )}
    </ActivitySection>
  );
};

export const UserHomeSSHPanel: React.FC<{ username: string }> = ({
  username,
}) => {
  const {
    data: details,
    isPending,
    isError,
    error,
  } = useAccountDetails(username);

  if (isError) {
    return (
      <FrostedCard style={{ padding: 12, height: "100%" }}>
        <InlineError
          message={
            error instanceof Error
              ? error.message
              : "Account detail is unavailable"
          }
        />
      </FrostedCard>
    );
  }

  if (isPending || !details) {
    return (
      <FrostedCard style={{ padding: 12, height: "100%" }}>
        <LoadingRows />
      </FrostedCard>
    );
  }

  return <HomeAndSSHCard details={details} />;
};

export const UserProcessPanel: React.FC<{ username: string }> = ({
  username,
}) => {
  const {
    data: details,
    isPending,
    isError,
    error,
  } = useAccountDetails(username);

  if (isError) {
    return (
      <FrostedCard style={{ padding: 12, height: "100%" }}>
        <InlineError
          message={
            error instanceof Error
              ? error.message
              : "Account detail is unavailable"
          }
        />
      </FrostedCard>
    );
  }

  if (isPending || !details) {
    return (
      <FrostedCard style={{ padding: 12, height: "100%" }}>
        <LoadingRows />
      </FrostedCard>
    );
  }

  return <ProcessCard details={details} />;
};

export const UserSupplementalCards: React.FC<{ username: string }> = ({
  username,
}) => {
  const {
    data: details,
    isPending,
    isError,
    error,
  } = useAccountDetails(username);

  return (
    <AppGrid container spacing={2.5}>
      <AppGrid size={{ xs: 12, lg: 5 }}>
        <UserActivityCard username={username} />
      </AppGrid>
      <AppGrid size={{ xs: 12, md: 6, lg: 3 }}>
        {isPending ? (
          <FrostedCard style={{ padding: 12, height: "100%" }}>
            <LoadingRows />
          </FrostedCard>
        ) : isError ? (
          <FrostedCard style={{ padding: 12, height: "100%" }}>
            <InlineError
              message={
                error instanceof Error
                  ? error.message
                  : "Account detail is unavailable"
              }
            />
          </FrostedCard>
        ) : !details ? (
          <FrostedCard style={{ padding: 12, height: "100%" }}>
            <LoadingRows />
          </FrostedCard>
        ) : (
          <HomeAndSSHCard details={details} />
        )}
      </AppGrid>
      <AppGrid size={{ xs: 12, md: 6, lg: 4 }}>
        {isPending ? (
          <FrostedCard style={{ padding: 12, height: "100%" }}>
            <LoadingRows />
          </FrostedCard>
        ) : isError ? (
          <FrostedCard style={{ padding: 12, height: "100%" }}>
            <InlineError
              message={
                error instanceof Error
                  ? error.message
                  : "Account detail is unavailable"
              }
            />
          </FrostedCard>
        ) : !details ? (
          <FrostedCard style={{ padding: 12, height: "100%" }}>
            <LoadingRows />
          </FrostedCard>
        ) : (
          <ProcessCard details={details} />
        )}
      </AppGrid>
    </AppGrid>
  );
};

export const UserDetailsStack: React.FC<UserDetailsPanelProps> = (props) => (
  <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
    <UserDetailsPanel {...props} />
    <UserSupplementalCards username={props.user.username} />
  </div>
);
