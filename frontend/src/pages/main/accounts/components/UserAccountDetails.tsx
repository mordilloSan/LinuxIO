import { Icon } from "@iconify/react";
import React from "react";

import { linuxio, type AccountUser, type AccountUserLogin } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import { DetailRow } from "@/components/cards/UnitInfoPanelCard";
import AppAlert from "@/components/ui/AppAlert";
import Chip from "@/components/ui/AppChip";
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
import StatusDot from "@/components/ui/StatusDot";
import { useAppTheme } from "@/theme";

function getAllGroups(user: AccountUser): string[] {
  const groups: string[] = [user.primaryGroup];
  user.groups?.forEach((group) => {
    if (!groups.includes(group)) {
      groups.push(group);
    }
  });
  return groups.filter(Boolean);
}

function getAccessLabel(user: AccountUser): string {
  if (user.isLocked) {
    return "Locked";
  }
  if (
    user.shell === "/usr/sbin/nologin" ||
    user.shell === "/sbin/nologin" ||
    user.shell === "/bin/false" ||
    user.shell === "/usr/bin/false"
  ) {
    return "Interactive login disabled";
  }
  return "Interactive login allowed";
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

interface UserDetailsPanelProps {
  user: AccountUser;
  currentUsername?: string;
  onClose: () => void;
}

export const UserDetailsPanel: React.FC<UserDetailsPanelProps> = ({
  user,
  currentUsername,
  onClose,
}) => {
  const theme = useAppTheme();
  const groups = getAllGroups(user);
  const isCurrentUser = user.username === currentUsername;
  const statusColor = user.isLocked
    ? theme.palette.warning.main
    : isCurrentUser
      ? theme.palette.success.main
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
          <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
            <StatusDot color={statusColor} />
            <AppTypography variant="subtitle2" fontWeight={700} noWrap>
              Account details
            </AppTypography>
          </div>
          <AppTypography
            variant="caption"
            color="text.secondary"
            noWrap
            style={{ display: "block", marginTop: 2 }}
          >
            {user.username}
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

      <div style={{ flex: 1, overflowX: "auto" }} className="custom-scrollbar">
        <div style={{ minWidth: "max-content" }}>
          <DetailRow label="Access" noBorder>
            <span
              style={{
                fontSize: "0.75rem",
                fontWeight: 600,
                color: statusColor,
                whiteSpace: "nowrap",
              }}
            >
              {getAccessLabel(user)}
            </span>
          </DetailRow>
          <DetailRow label="UID / GID">
            <span style={{ fontSize: "0.75rem", fontWeight: 500 }}>
              {user.uid} / {user.gid}
            </span>
          </DetailRow>
          <DetailRow label="Type">
            <span style={{ fontSize: "0.75rem", fontWeight: 500 }}>
              {user.isSystem ? "System account" : "Regular account"}
            </span>
          </DetailRow>
          <DetailRow label="Primary">
            <span style={{ fontSize: "0.75rem", fontWeight: 500 }}>
              {user.primaryGroup || "-"}
            </span>
          </DetailRow>
          <DetailRow label="Home">
            <span
              style={{
                fontSize: "0.75rem",
                fontWeight: 500,
                whiteSpace: "nowrap",
              }}
            >
              {user.homeDir || "-"}
            </span>
          </DetailRow>
          <DetailRow label="Shell">
            <span
              style={{
                fontSize: "0.75rem",
                fontWeight: 500,
                whiteSpace: "nowrap",
              }}
            >
              {user.shell || "-"}
            </span>
          </DetailRow>
          <DetailRow label="Last active">
            <span style={{ fontSize: "0.75rem", fontWeight: 500 }}>
              {isCurrentUser ? "Logged in now" : user.lastLogin || "Never"}
            </span>
          </DetailRow>
        </div>
      </div>

      <div style={{ marginTop: 12 }}>
        <AppTypography
          variant="caption"
          color="text.secondary"
          style={{ display: "block", marginBottom: 6 }}
        >
          Groups ({groups.length})
        </AppTypography>
        <div style={{ display: "flex", flexWrap: "wrap", gap: 4 }}>
          {groups.map((group, index) => (
            <Chip
              key={`${user.username}-${group}`}
              label={index === 0 ? `${group} primary` : group}
              size="small"
              variant="soft"
              color={index === 0 ? "primary" : "default"}
              style={{ fontSize: "0.65rem", height: 20 }}
            />
          ))}
        </div>
      </div>
    </FrostedCard>
  );
};

export const UserLoginHistoryCard: React.FC<{ username: string }> = ({
  username,
}) => {
  const {
    data: logins = [],
    isPending,
    isError,
    error,
  } = linuxio.accounts.list_user_logins.useQuery(username, {
    enabled: Boolean(username),
    refetchInterval: 30000,
  });

  return (
    <FrostedCard style={{ padding: 12 }}>
      <div style={{ marginBottom: 12 }}>
        <AppTypography variant="subtitle2" fontWeight={700}>
          Login history
        </AppTypography>
        <AppTypography
          variant="caption"
          color="text.secondary"
          style={{ display: "block", marginTop: 2 }}
        >
          Recent successful sessions
        </AppTypography>
      </div>

      {isPending ? (
        <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
          {Array.from({ length: 3 }).map((_, index) => (
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
      ) : isError ? (
        <AppAlert severity="warning">
          {error instanceof Error
            ? error.message
            : "Login history unavailable"}
        </AppAlert>
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
              {logins.map((login) => (
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

export const UserDetailsStack: React.FC<UserDetailsPanelProps> = (props) => (
  <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
    <UserDetailsPanel {...props} />
    <UserLoginHistoryCard username={props.user.username} />
  </div>
);
