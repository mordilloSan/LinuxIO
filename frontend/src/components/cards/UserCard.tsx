import { Icon } from "@iconify/react";
import React from "react";

import { type AccountUser } from "@/api";
import FrostedCard from "@/components/cards/RootCard";
import AppCheckbox from "@/components/ui/AppCheckbox";
import Chip from "@/components/ui/AppChip";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { responsiveTextStyles } from "@/theme/tableStyles";

function formatLastLogin(
  lastLogin: string,
  username: string,
  currentUsername: string | undefined,
): string {
  if (!lastLogin || lastLogin === "Never") return "Never logged in";
  if (username === currentUsername) return "Logged in";
  return lastLogin;
}

function getAllGroups(user: AccountUser): string[] {
  const groups: string[] = [user.primaryGroup];
  user.groups?.forEach((g) => {
    if (!groups.includes(g)) groups.push(g);
  });
  return groups;
}

export interface UserCardProps {
  user: AccountUser;
  currentUsername: string | undefined;
  selected: boolean;
  isLocking: boolean;
  isUnlocking: boolean;
  onSelect: (checked: boolean) => void;
  onEdit: () => void;
  onChangePassword: () => void;
  onToggleLock: () => void;
}

const UserCard: React.FC<UserCardProps> = ({
  user,
  currentUsername,
  selected,
  isLocking,
  isUnlocking,
  onSelect,
  onEdit,
  onChangePassword,
  onToggleLock,
}) => {
  const isCurrentUser = user.username === currentUsername;
  const isProtected = user.username === "root" || isCurrentUser;

  return (
    <FrostedCard style={{ padding: 8 }}>
      {/* Header: checkbox + username + actions */}
      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
          gap: 4,
          marginBottom: 4,
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: 4 }}>
          <AppCheckbox
            size="small"
            checked={selected}
            onChange={(e) => onSelect(e.target.checked)}
            disabled={isProtected}
          />
          <AppTypography variant="body2" fontWeight={700} noWrap>
            {user.username}
          </AppTypography>
        </div>

        <div style={{ display: "flex", gap: 2 }}>
          <AppTooltip title="Edit">
            <AppIconButton
              size="small"
              onClick={onEdit}
              disabled={user.username === "root"}
            >
              <Icon icon="mdi:pencil" width={20} height={20} />
            </AppIconButton>
          </AppTooltip>
          <AppTooltip title="Change Password">
            <AppIconButton size="small" onClick={onChangePassword}>
              <Icon icon="mdi:form-textbox-password" width={20} height={20} />
            </AppIconButton>
          </AppTooltip>
          <AppTooltip title={user.isLocked ? "Unlock" : "Lock"}>
            <AppIconButton
              size="small"
              onClick={onToggleLock}
              disabled={isProtected || isLocking || isUnlocking}
            >
              {user.isLocked ? (
                <Icon icon="mdi:lock-open" width={20} height={20} />
              ) : (
                <Icon icon="mdi:lock" width={20} height={20} />
              )}
            </AppIconButton>
          </AppTooltip>
        </div>
      </div>

      {/* Status chips */}
      <div
        style={{ display: "flex", flexWrap: "wrap", gap: 3, marginBottom: 4 }}
      >
        {isCurrentUser && (
          <Chip label="Your account" size="small" color="primary" variant="soft" />
        )}
        {user.isLocked && (
          <Chip label="Locked" size="small" color="warning" variant="soft" />
        )}
      </div>

      {/* Details */}
      <AppTypography variant="body2" style={responsiveTextStyles}>
        Full name: {user.gecos || "-"}
      </AppTypography>
      <AppTypography variant="body2" style={responsiveTextStyles}>
        UID: {user.uid}
      </AppTypography>
      <AppTypography variant="body2" style={responsiveTextStyles}>
        Last active: {formatLastLogin(user.lastLogin, user.username, currentUsername)}
      </AppTypography>
      <AppTypography variant="body2" style={responsiveTextStyles}>
        Shell: {user.shell}
      </AppTypography>
      <AppTypography
        variant="body2"
        style={{ fontFamily: "monospace", ...responsiveTextStyles }}
      >
        Home: {user.homeDir}
      </AppTypography>

      {/* Groups */}
      <div
        style={{ marginTop: 4, display: "flex", flexWrap: "wrap", gap: 2 }}
      >
        {getAllGroups(user).map((group, idx) => (
          <Chip
            key={`${user.username}-${group}`}
            label={idx === 0 ? `${group} (primary)` : group}
            size="small"
            variant="soft"
            style={{ fontSize: "0.7rem" }}
          />
        ))}
      </div>
    </FrostedCard>
  );
};

export default UserCard;
