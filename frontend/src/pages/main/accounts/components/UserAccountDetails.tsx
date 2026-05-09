import { Icon } from "@iconify/react";
import { useQueryClient } from "@tanstack/react-query";
import React, { useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";

import {
  linuxio,
  type AccountActiveSession,
  type AccountHomeHealth,
  type AccountPasswordState,
  type AccountSSHAccess,
  type AccountUser,
  type AccountUserDetails,
  type AccountUserLogin,
} from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import { DetailRow } from "@/components/cards/UnitInfoPanelCard";
import AppAlert from "@/components/ui/AppAlert";
import Chip from "@/components/ui/AppChip";
import AppDivider from "@/components/ui/AppDivider";
import AppGrid from "@/components/ui/AppGrid";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";
import { SEMANTIC_STATUS_COLORS } from "@/theme/colors";

import "./user-account-details.css";

interface UserDetailsPanelProps {
  user: AccountUser;
  currentUsername?: string;
  onClose: () => void;
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

const loginStatusChipStyle: React.CSSProperties = {
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
  label: string;
  hiddenXs?: boolean;
  onClick?: () => void;
  active?: boolean;
  direction?: "asc" | "desc";
}

const ActivitySection: React.FC<{
  title: string;
  subtitle: string;
  headers: ActivityHeader[];
  gridClassName: string;
  metaText: string;
  className?: string;
  children: React.ReactNode;
}> = ({
  title,
  subtitle,
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
      <div style={{ minWidth: 0 }}>
        <AppTypography variant="subtitle2" fontWeight={700} noWrap>
          {title}
        </AppTypography>
        {subtitle && (
          <AppTypography
            variant="caption"
            color="text.secondary"
            style={{ display: "block", marginTop: 2 }}
          >
            {subtitle}
          </AppTypography>
        )}
      </div>
      <AppTypography variant="caption" color="text.secondary" noWrap>
        {metaText}
      </AppTypography>
    </div>

    <div className={`${gridClassName} account-activity-column-header`}>
      {headers.map((header) => (
        <AppTypography
          key={header.label}
          variant="overline"
          color="text.secondary"
          className={header.hiddenXs ? "account-hidden-xs" : undefined}
          style={{
            fontSize: "0.65rem",
            cursor: header.onClick ? "pointer" : undefined,
            userSelect: header.onClick ? "none" : undefined,
          }}
          onClick={header.onClick}
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
    <AppTypography variant="body2" color="text.secondary">
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
      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "flex-start",
          gap: 8,
          ...topCardHeaderStyle,
        }}
      >
        <div style={{ flex: 1, minWidth: 0 }}>
          <AppTypography variant="subtitle2" fontWeight={700} noWrap>
            Access & security
          </AppTypography>
          <AppTypography
            variant="caption"
            color="text.secondary"
            style={{ display: "block", marginTop: 2 }}
          >
            Admin privileges, password status, and elevated groups
          </AppTypography>
        </div>

        <AppTooltip title="Close details">
          <AppIconButton
            size="small"
            onClick={onClose}
            style={{ flexShrink: 0 }}
          >
            <Icon icon="mdi:close" width={20} height={20} />
          </AppIconButton>
        </AppTooltip>
      </div>

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
            style={{ flex: 1, overflowX: "auto" }}
            className="custom-scrollbar"
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
              variant="caption"
              color="text.secondary"
              style={{ display: "block", marginBottom: 6 }}
            >
              Elevated groups
            </AppTypography>
            <div style={{ display: "flex", flexWrap: "wrap", gap: 4 }}>
              {details.admin.groups.length ? (
                details.admin.groups.map((group) => (
                  <Chip
                    key={`${user.username}-${group}`}
                    label={group}
                    size="small"
                    variant="soft"
                    color="warning"
                    style={{ fontSize: "0.65rem", height: 20 }}
                  />
                ))
              ) : (
                <Chip
                  label="none"
                  size="small"
                  variant="soft"
                  style={{ fontSize: "0.65rem", height: 20 }}
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
      dismissFailedLoginAlert([failedLoginAlertId]);
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
        title="Active sessions"
        subtitle="Current authenticated sessions"
        headers={[
          { label: "Terminal" },
          { label: "Source", hiddenXs: true },
          { label: "Started" },
        ]}
        gridClassName="account-sessions-grid"
        metaText={
          detailsPending || !details
            ? "Checking sessions"
            : `${sessions.length} active ${sessions.length === 1 ? "session" : "sessions"}`
        }
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
              <div className="account-sessions-grid account-activity-row">
                <AppTypography variant="body2" fontWeight={600} noWrap>
                  {session.terminal || "-"}
                </AppTypography>
                <AppTypography
                  variant="caption"
                  color="text.secondary"
                  noWrap
                  className="account-hidden-xs"
                >
                  {sessionLocation(session)}
                </AppTypography>
                <AppTypography variant="caption" color="text.secondary" noWrap>
                  {session.startedAt || "-"}
                </AppTypography>
              </div>
              {index < sessions.length - 1 && <AppDivider />}
            </React.Fragment>
          ))
        )}
      </ActivitySection>

      <ActivitySection
        className="account-activity-card--logins"
        title="Login history"
        subtitle="Recent login events"
        headers={[
          { label: "Time" },
          { label: "Terminal", hiddenXs: true },
          { label: "Source", hiddenXs: true },
          { label: "Result" },
        ]}
        gridClassName="account-logins-grid"
        metaText={
          loginsPending
            ? "Loading login history"
            : `${logins.length} recent ${logins.length === 1 ? "event" : "events"}`
        }
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
                ref={(node) => {
                  loginRowRefs.current[loginEventKey(login)] = node;
                }}
                className={[
                  "account-logins-grid",
                  "account-activity-row",
                  flashingLoginKey === loginEventKey(login)
                    ? "account-activity-row--flash"
                    : "",
                ]
                  .filter(Boolean)
                  .join(" ")}
              >
                <AppTypography variant="body2" fontWeight={500} noWrap>
                  {login.time || "-"}
                </AppTypography>
                <AppTypography
                  variant="caption"
                  color="text.secondary"
                  noWrap
                  className="account-hidden-xs"
                >
                  {login.terminal || "-"}
                </AppTypography>
                <AppTypography
                  variant="caption"
                  color="text.secondary"
                  noWrap
                  className="account-hidden-xs"
                >
                  {getLoginLocation(login)}
                </AppTypography>
                <div>
                  <Chip
                    label={loginStatusLabel(login)}
                    size="small"
                    variant="soft"
                    color={loginStatusColor(login)}
                    style={loginStatusChipStyle}
                  />
                </div>
              </div>
              {index < logins.length - 1 && <AppDivider />}
            </React.Fragment>
          ))
        )}
      </ActivitySection>
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
}) => (
  <FrostedCard style={{ padding: 12, height: "100%", width: "100%" }}>
    <div style={topCardHeaderStyle}>
      <AppTypography variant="subtitle2" fontWeight={700}>
        Home & SSH access
      </AppTypography>
      <AppTypography
        variant="caption"
        color="text.secondary"
        style={{ display: "block", marginTop: 2 }}
      >
        Directory ownership, permissions, and authorized keys
      </AppTypography>
    </div>

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

type ProcessSortField = "pid" | "command" | "cpu" | "memory";
type SortDirection = "asc" | "desc";

const PROCESS_DEFAULT_DIRECTION: Record<ProcessSortField, SortDirection> = {
  pid: "asc",
  command: "asc",
  cpu: "desc",
  memory: "desc",
};

const ProcessCard: React.FC<{ details: AccountUserDetails }> = ({
  details,
}) => {
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

  const processes = React.useMemo(() => {
    const factor = sortDirection === "asc" ? 1 : -1;
    return [...details.processes.top].sort((a, b) => {
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
  }, [details.processes.top, sortField, sortDirection]);

  const metaText = details.processes.error
    ? "Unavailable"
    : `${details.processes.count} ${details.processes.count === 1 ? "process" : "processes"}`;

  const headerFor = (label: string, field: ProcessSortField) => ({
    label,
    onClick: () => handleSort(field),
    active: sortField === field,
    direction: sortField === field ? sortDirection : undefined,
  });

  return (
    <ActivitySection
      className="account-activity-card--processes"
      title="Owned processes"
      subtitle="Current process's resource usage"
      headers={[
        headerFor("PID", "pid"),
        headerFor("Command", "command"),
        headerFor("CPU", "cpu"),
        headerFor("MEM", "memory"),
      ]}
      gridClassName="account-processes-grid"
      metaText={metaText}
    >
      {details.processes.error ? (
        <div style={{ padding: 12 }}>
          <InlineError message={details.processes.error} />
        </div>
      ) : processes.length === 0 ? (
        <ActivityEmpty>No running processes.</ActivityEmpty>
      ) : (
        processes.map((process, index) => (
          <React.Fragment key={process.pid}>
            <div className="account-processes-grid account-activity-row">
              <AppTypography variant="body2" fontWeight={500} noWrap>
                {process.pid}
              </AppTypography>
              <AppTypography variant="body2" noWrap>
                {process.command}
              </AppTypography>
              <AppTypography variant="caption" color="text.secondary" noWrap>
                {process.cpu.toFixed(1)}%
              </AppTypography>
              <AppTypography variant="caption" color="text.secondary" noWrap>
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
