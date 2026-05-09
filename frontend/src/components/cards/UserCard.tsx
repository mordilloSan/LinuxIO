import { Icon } from "@iconify/react";
import React, { useLayoutEffect, useRef, useState } from "react";

import { type AccountUser } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import {
  SummaryRowsList,
  type SummaryRow,
} from "@/components/cards/HardwareCard";
import Chip from "@/components/ui/AppChip";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import StatusDot from "@/components/ui/StatusDot";
import { useAppTheme } from "@/theme";
import { GAP_SM } from "@/theme/constants";

function formatLastLogin(
  lastLogin: string,
  username: string,
  currentUsername: string | undefined,
): string {
  if (!lastLogin || lastLogin === "Never") return "Never";
  if (username === currentUsername) return "Now";
  return lastLogin;
}

function getAllGroups(user: AccountUser): string[] {
  const groups: string[] = [user.primaryGroup];
  user.groups?.forEach((g) => {
    if (!groups.includes(g)) groups.push(g);
  });
  return groups;
}

function getUserIcon(user: AccountUser): string {
  if (user.username === "root") return "mdi:shield-crown";
  if (user.isLocked) return "mdi:account-lock";
  return "mdi:account-circle";
}

const selectedRowLabelStyle: React.CSSProperties = {
  textTransform: "uppercase",
  letterSpacing: "0.06em",
  fontSize: "0.6rem",
  color: "var(--app-palette-text-secondary)",
  flexShrink: 0,
  width: 90,
};

const CompactGroupChips: React.FC<{
  username: string;
  groups: string[];
}> = ({ username, groups }) => {
  const measureRef = useRef<HTMLDivElement | null>(null);
  const [firstRowCount, setFirstRowCount] = useState(groups.length);

  useLayoutEffect(() => {
    const node = measureRef.current;
    if (!node) return;

    const measure = () => {
      const children = Array.from(node.children) as HTMLElement[];
      if (children.length === 0) {
        setFirstRowCount(0);
        return;
      }
      const firstTop = children[0].offsetTop;
      let count = 0;
      for (const child of children) {
        if (child.offsetTop !== firstTop) break;
        count++;
      }
      setFirstRowCount(count);
    };

    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(node);
    return () => observer.disconnect();
  }, [groups]);

  const overflowing = firstRowCount < groups.length;
  const visible = overflowing
    ? groups.slice(0, Math.max(1, firstRowCount - 1))
    : groups;
  const hiddenCount = groups.length - visible.length;

  return (
    <div style={{ position: "relative", minHeight: 20 }}>
      <div
        ref={measureRef}
        aria-hidden
        style={{
          display: "flex",
          flexWrap: "wrap",
          gap: 3,
          position: "absolute",
          top: 0,
          left: 0,
          right: 0,
          opacity: 0,
          pointerEvents: "none",
        }}
      >
        {groups.map((group) => (
          <Chip
            key={`measure-${username}-${group}`}
            label={group}
            size="small"
            variant="soft"
            style={{ fontSize: "0.65rem", height: 20 }}
          />
        ))}
      </div>
      <div
        style={{
          display: "flex",
          flexWrap: "nowrap",
          gap: 3,
          overflow: "hidden",
        }}
      >
        {visible.map((group, idx) => (
          <Chip
            key={`${username}-${group}`}
            label={group}
            size="small"
            variant="soft"
            color={idx === 0 ? "primary" : "default"}
            style={{ fontSize: "0.65rem", height: 20, flexShrink: 0 }}
          />
        ))}
        {hiddenCount > 0 && (
          <Chip
            label={`+${hiddenCount}`}
            size="small"
            variant="soft"
            style={{ fontSize: "0.65rem", height: 20, flexShrink: 0 }}
          />
        )}
      </div>
    </div>
  );
};

const SelectedSummaryRows: React.FC<{ rows: SummaryRow[] }> = ({ rows }) => (
  <div
    style={{
      display: "flex",
      flexDirection: "column",
      alignSelf: "stretch",
      width: "100%",
    }}
  >
    {rows.map(({ label, value }, index) => (
      <div
        key={label}
        style={{
          display: "flex",
          padding: "3px 0",
          borderTop:
            index === 0 ? undefined : "1px solid var(--app-palette-divider)",
          alignItems: "baseline",
        }}
      >
        <span style={selectedRowLabelStyle}>{label}</span>
        <div style={{ flex: 1, minWidth: 0, textAlign: "right" }}>
          <span style={{ fontSize: "0.75rem", fontWeight: 500 }}>{value}</span>
        </div>
      </div>
    ))}
  </div>
);

export interface UserCardProps {
  user: AccountUser;
  currentUsername: string | undefined;
  isLocking: boolean;
  isUnlocking: boolean;
  isSelected?: boolean;
  onOpen?: () => void;
  onEdit: () => void;
  onChangePassword: () => void;
  onToggleLock: () => void;
}

const UserCard: React.FC<UserCardProps> = ({
  user,
  currentUsername,
  isLocking,
  isUnlocking,
  isSelected = false,
  onOpen,
  onEdit,
  onChangePassword,
  onToggleLock,
}) => {
  const theme = useAppTheme();
  const isCurrentUser = user.username === currentUsername;
  const isProtected = user.username === "root" || isCurrentUser;

  const accentColor = user.isLocked
    ? theme.palette.warning.main
    : theme.palette.primary.main;

  const statusColor = user.isLocked
    ? theme.palette.warning.main
    : isCurrentUser
      ? theme.palette.success.main
      : theme.palette.text.disabled;

  const statusTooltip = user.isLocked
    ? "Locked"
    : isCurrentUser
      ? "Active session"
      : "Active";

  const rows: SummaryRow[] = [
    { label: "UID", value: String(user.uid) },
    {
      label: "Last Active",
      value: formatLastLogin(user.lastLogin, user.username, currentUsername),
    },
    { label: "Shell", value: user.shell || "—" },
    { label: "Home", value: user.homeDir || "—" },
  ];

  const groups = getAllGroups(user);

  return (
    <FrostedCard
      hoverLift={Boolean(onOpen) && !isSelected}
      onClick={onOpen}
      onKeyDown={(event) => {
        if (
          !onOpen ||
          event.target !== event.currentTarget ||
          (event.key !== "Enter" && event.key !== " ")
        ) {
          return;
        }
        event.preventDefault();
        onOpen();
      }}
      role={onOpen ? "button" : undefined}
      tabIndex={onOpen ? 0 : undefined}
      style={{
        padding: isSelected ? 12 : 10,
        display: "flex",
        flexDirection: "column",
        height: "100%",
        position: "relative",
        width: isSelected ? "100%" : undefined,
        cursor: onOpen ? "pointer" : undefined,
        transition:
          "transform 0.2s, box-shadow 0.2s, border 0.3s ease-in-out, margin 0.3s ease-in-out",
        borderBottomWidth: 2,
        borderBottomStyle: "solid",
        borderBottomColor: isSelected
          ? "transparent"
          : `color-mix(in srgb, ${accentColor}, transparent 70%)`,
      }}
    >
      {/* Header */}
      <div
        style={{
          display: "flex",
          alignItems: "flex-start",
          justifyContent: "space-between",
          gap: GAP_SM,
          minHeight: isSelected ? 46 : undefined,
        }}
      >
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: GAP_SM,
            minWidth: 0,
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
            <Icon
              icon={getUserIcon(user)}
              width={32}
              height={32}
              color={accentColor}
            />
          </div>
          <div style={{ minWidth: 0 }}>
            <AppTypography
              variant="subtitle1"
              fontWeight={700}
              noWrap
              style={{ lineHeight: 1.2 }}
            >
              {user.username}
            </AppTypography>
            <AppTypography
              variant="caption"
              color="text.secondary"
              noWrap
              style={{ display: "block" }}
            >
              {user.gecos || "No full name"}
            </AppTypography>
          </div>
          {(isCurrentUser || user.isLocked || user.isSystem) && (
            <div
              style={{
                display: "flex",
                flexWrap: "wrap",
                gap: 4,
                flexShrink: 0,
              }}
            >
              {isCurrentUser && (
                <Chip
                  label="Your account"
                  size="small"
                  color="primary"
                  variant="soft"
                  style={{ fontSize: "0.65rem", height: 20 }}
                />
              )}
              {user.isLocked && (
                <Chip
                  label="Locked"
                  size="small"
                  color="warning"
                  variant="soft"
                  style={{ fontSize: "0.65rem", height: 20 }}
                />
              )}
              {user.isSystem && !isCurrentUser && (
                <Chip
                  label="System"
                  size="small"
                  variant="soft"
                  style={{ fontSize: "0.65rem", height: 20 }}
                />
              )}
            </div>
          )}
        </div>

        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: 2,
            flexShrink: 0,
          }}
          onClick={(event) => event.stopPropagation()}
        >
          <AppTooltip title="Edit">
            <span>
              <AppIconButton
                size="small"
                style={{ padding: 4 }}
                onClick={(event) => {
                  event.stopPropagation();
                  onEdit();
                }}
                disabled={user.username === "root"}
              >
                <Icon icon="mdi:pencil" width={14} height={14} />
              </AppIconButton>
            </span>
          </AppTooltip>
          <AppTooltip title="Change Password">
            <AppIconButton
              size="small"
              style={{ padding: 4 }}
              onClick={(event) => {
                event.stopPropagation();
                onChangePassword();
              }}
            >
              <Icon icon="mdi:form-textbox-password" width={14} height={14} />
            </AppIconButton>
          </AppTooltip>
          <AppTooltip title={user.isLocked ? "Unlock" : "Lock"}>
            <span>
              <AppIconButton
                size="small"
                style={{ padding: 4 }}
                onClick={(event) => {
                  event.stopPropagation();
                  onToggleLock();
                }}
                disabled={isProtected || isLocking || isUnlocking}
              >
                {user.isLocked ? (
                  <Icon icon="mdi:lock-open" width={14} height={14} />
                ) : (
                  <Icon icon="mdi:lock" width={14} height={14} />
                )}
              </AppIconButton>
            </span>
          </AppTooltip>
          <StatusDot
            color={statusColor}
            tooltip={statusTooltip}
            size={8}
            style={{ marginLeft: 4 }}
          />
        </div>
      </div>

      {/* Summary rows */}
      <div style={{ marginTop: 8 }}>
        {isSelected ? (
          <SelectedSummaryRows rows={rows} />
        ) : (
          <SummaryRowsList rows={rows} />
        )}
      </div>

      {/* Groups footer */}
      <div style={{ marginTop: "auto", paddingTop: 8 }}>
        <AppTypography
          variant="caption"
          color="text.secondary"
          style={{
            letterSpacing: "0.06em",
            fontSize: "0.62rem",
            display: "block",
            marginBottom: 4,
          }}
        >
          Groups ({groups.length})
        </AppTypography>
        {isSelected ? (
          <div
            className="custom-scrollbar"
            style={{
              display: "flex",
              flexWrap: "wrap",
              gap: 3,
              maxHeight: 43,
              overflowY: "auto",
              scrollbarGutter: "stable",
            }}
          >
            {groups.map((group, idx) => (
              <Chip
                key={`${user.username}-${group}`}
                label={group}
                size="small"
                variant="soft"
                color={idx === 0 ? "primary" : "default"}
                style={{ fontSize: "0.65rem", height: 20 }}
              />
            ))}
          </div>
        ) : (
          <CompactGroupChips username={user.username} groups={groups} />
        )}
      </div>
    </FrostedCard>
  );
};

export default UserCard;
