import { Icon } from "@iconify/react";
import React from "react";

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
import AppGrid from "@/components/ui/AppGrid";
import AppIconButton from "@/components/ui/AppIconButton";
import {
  AppTable,
  AppTableBody,
  AppTableCell,
  AppTableContainer,
  AppTableHead,
  AppTableRow,
} from "@/components/ui/AppTable";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";

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

export const UserDetailsPanel: React.FC<UserDetailsPanelProps> = ({
  user,
  onClose,
}) => {
  const theme = useAppTheme();
  const { data: details, isPending, isError, error } = useAccountDetails(
    user.username,
  );

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
          marginBottom: 12,
          gap: 8,
        }}
      >
        <div style={{ flex: 1, minWidth: 0 }}>
          <AppTypography variant="subtitle2" fontWeight={700} noWrap>
            Access & security
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

const UserActivityCard: React.FC<{ username: string }> = ({ username }) => {
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

  return (
    <FrostedCard style={{ padding: 12, height: "100%" }}>
      <div style={{ marginBottom: 12 }}>
        <AppTypography variant="subtitle2" fontWeight={700}>
          Sessions & login history
        </AppTypography>
        <AppTypography
          variant="caption"
          color="text.secondary"
          style={{ display: "block", marginTop: 2 }}
        >
          Current sessions and recent successful logins
        </AppTypography>
      </div>

      {detailsPending ? (
        <LoadingRows rows={2} />
      ) : detailsError ? (
        <InlineError
          message={
            detailsErrorValue instanceof Error
              ? detailsErrorValue.message
              : "Session details unavailable"
          }
        />
      ) : !details ? (
        <LoadingRows rows={2} />
      ) : details.activeSessions.length > 0 ? (
        <div style={{ marginBottom: 12 }}>
          <AppTypography
            variant="caption"
            color="text.secondary"
            style={{ display: "block", marginBottom: 6 }}
          >
            Active sessions ({details.activeSessions.length})
          </AppTypography>
          <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
            {details.activeSessions.map((session) => (
              <div
                key={`${session.terminal}-${session.startedAt}`}
                style={{
                  display: "flex",
                  justifyContent: "space-between",
                  gap: 8,
                  borderBottom: "1px solid var(--app-palette-divider)",
                  paddingBottom: 6,
                }}
              >
                <div style={{ minWidth: 0 }}>
                  <AppTypography variant="body2" fontWeight={600} noWrap>
                    {session.terminal}
                  </AppTypography>
                  <AppTypography
                    variant="caption"
                    color="text.secondary"
                    noWrap
                    style={{ display: "block" }}
                  >
                    {sessionLocation(session)}
                  </AppTypography>
                </div>
                <AppTypography variant="caption" color="text.secondary" noWrap>
                  {session.startedAt}
                </AppTypography>
              </div>
            ))}
          </div>
        </div>
      ) : (
        <AppTypography
          variant="body2"
          color="text.secondary"
          style={{ display: "block", marginBottom: 12 }}
        >
          No active sessions.
        </AppTypography>
      )}

      {loginsPending ? (
        <LoadingRows rows={3} />
      ) : loginsError ? (
        <InlineError
          message={
            loginsErrorValue instanceof Error
              ? loginsErrorValue.message
              : "Login history unavailable"
          }
        />
      ) : logins.length === 0 ? (
        <AppTypography variant="body2" color="text.secondary">
          No login history found.
        </AppTypography>
      ) : (
        <AppTableContainer>
          <AppTable>
            <AppTableHead>
              <AppTableRow>
                <AppTableCell component="th">Time</AppTableCell>
                <AppTableCell component="th">Terminal</AppTableCell>
                <AppTableCell component="th">Source</AppTableCell>
              </AppTableRow>
            </AppTableHead>
            <AppTableBody>
              {logins.slice(0, 6).map((login) => (
                <AppTableRow
                  key={`${login.time}-${login.terminal}-${login.source}`}
                >
                  <AppTableCell>
                    <AppTypography variant="body2" fontWeight={500} noWrap>
                      {login.time || "-"}
                    </AppTypography>
                  </AppTableCell>
                  <AppTableCell>
                    <AppTypography variant="body2" color="text.secondary">
                      {login.terminal || "-"}
                    </AppTypography>
                  </AppTableCell>
                  <AppTableCell>
                    <AppTypography variant="body2" color="text.secondary">
                      {getLoginLocation(login)}
                    </AppTypography>
                  </AppTableCell>
                </AppTableRow>
              ))}
            </AppTableBody>
          </AppTable>
        </AppTableContainer>
      )}
    </FrostedCard>
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
  <FrostedCard style={{ padding: 12, height: "100%" }}>
    <div style={{ marginBottom: 12 }}>
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

const ProcessCard: React.FC<{ details: AccountUserDetails }> = ({
  details,
}) => (
  <FrostedCard style={{ padding: 12, height: "100%" }}>
    <div style={{ marginBottom: 12 }}>
      <AppTypography variant="subtitle2" fontWeight={700}>
        Owned processes
      </AppTypography>
      <AppTypography
        variant="caption"
        color="text.secondary"
        style={{ display: "block", marginTop: 2 }}
      >
        Current process count and busiest commands
      </AppTypography>
    </div>

    {details.processes.error ? (
      <InlineError message={details.processes.error} />
    ) : (
      <>
        <DetailRow label="Count" noBorder>
          <DetailText>{details.processes.count}</DetailText>
        </DetailRow>
        {details.processes.top.length === 0 ? (
          <AppTypography
            variant="body2"
            color="text.secondary"
            style={{ display: "block", marginTop: 10 }}
          >
            No running processes.
          </AppTypography>
        ) : (
          <AppTableContainer style={{ marginTop: 10 }}>
            <AppTable>
              <AppTableHead>
                <AppTableRow>
                  <AppTableCell component="th">PID</AppTableCell>
                  <AppTableCell component="th">Command</AppTableCell>
                  <AppTableCell component="th" align="right">
                    CPU
                  </AppTableCell>
                  <AppTableCell component="th" align="right">
                    MEM
                  </AppTableCell>
                </AppTableRow>
              </AppTableHead>
              <AppTableBody>
                {details.processes.top.map((process) => (
                  <AppTableRow key={process.pid}>
                    <AppTableCell>{process.pid}</AppTableCell>
                    <AppTableCell>
                      <AppTypography variant="body2" noWrap>
                        {process.command}
                      </AppTypography>
                    </AppTableCell>
                    <AppTableCell align="right">
                      {process.cpu.toFixed(1)}%
                    </AppTableCell>
                    <AppTableCell align="right">
                      {process.memory.toFixed(1)}%
                    </AppTableCell>
                  </AppTableRow>
                ))}
              </AppTableBody>
            </AppTable>
          </AppTableContainer>
        )}
      </>
    )}
  </FrostedCard>
);

export const UserSupplementalCards: React.FC<{ username: string }> = ({
  username,
}) => {
  const { data: details, isPending, isError, error } =
    useAccountDetails(username);

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
