import { Icon } from "@iconify/react";
import {
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
} from "react";

import { type AccountUser } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import {
  type SummaryRow,
  SummaryRowsList,
} from "@/components/cards/HardwareCard";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";
import StatusDot from "@/components/ui/StatusDot";
import { CARD_PADDING_LG, CARD_PADDING_MD, GAP_SM } from "@/theme/constants";

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

const selectedRowLabelStyle: CSSProperties = {
  textTransform: "uppercase",
  letterSpacing: "0.06em",
  flexShrink: 0,
  width: 90,
};

const CompactGroupChips = ({
  username,
  groups,
}: {
  username: string;
  groups: string[];
}) => {
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
    // `groups` intentionally retriggers measurement after the chip DOM changes.
    // oxlint-disable-next-line react/exhaustive-effect-dependencies
  }, [groups]);

  const overflowing = firstRowCount < groups.length;
  const visible = overflowing
    ? groups.slice(0, Math.max(1, firstRowCount - 1))
    : groups;
  const hiddenCount = groups.length - visible.length;

  return (
    <div style={{ position: "relative", minHeight: 20 }}>
      <div
        aria-hidden
        ref={measureRef}
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
            size="xsmall"
            variant="soft"
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
            color={idx === 0 ? "primary" : "default"}
            key={`${username}-${group}`}
            label={group}
            size="xsmall"
            style={{ flexShrink: 0 }}
            variant="soft"
          />
        ))}
        {hiddenCount > 0 && (
          <Chip
            label={`+${hiddenCount}`}
            size="xsmall"
            style={{ flexShrink: 0 }}
            variant="soft"
          />
        )}
      </div>
    </div>
  );
};

const SelectedSummaryRows = ({ rows }: { rows: SummaryRow[] }) => (
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
        <AppTypography
          color="text.secondary"
          component="span"
          style={selectedRowLabelStyle}
          variant="caption"
        >
          {label}
        </AppTypography>
        <div style={{ flex: 1, minWidth: 0, textAlign: "right" }}>
          <AppTypography component="span" fontWeight={500} variant="caption">
            {value}
          </AppTypography>
        </div>
      </div>
    ))}
  </div>
);

export type UserLockAction = "lock" | "unlock";

export interface UserCardProps {
  currentUsername: string | undefined;
  isSelected?: boolean;
  onChangePassword: () => void;
  onEdit: () => void;
  onOpen: () => void;
  onToggleLock: () => void;
  pendingLockAction?: UserLockAction;
  user: AccountUser;
}

const UserCard = ({
  user,
  currentUsername,
  isSelected = false,
  onOpen,
  onEdit,
  onChangePassword,
  onToggleLock,
  pendingLockAction,
}: UserCardProps) => {
  const isCurrentUser = user.username === currentUsername;
  const isProtected = user.username === "root" || isCurrentUser;
  const lockLabel = user.isLocked ? "Unlock" : "Lock";

  const accentColor = user.isLocked
    ? "var(--app-palette-warning-main)"
    : "var(--app-palette-primary-main)";

  const statusColor = user.isLocked
    ? "var(--app-palette-warning-main)"
    : isCurrentUser
      ? "var(--app-palette-success-main)"
      : "var(--app-palette-text-disabled)";

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
  const detailsId = useId();

  return (
    <FrostedCard
      accent
      hoverLift={!isSelected}
      style={{
        padding: isSelected ? CARD_PADDING_LG : CARD_PADDING_MD,
        display: "flex",
        flexDirection: "column",
        height: "100%",
        position: "relative",
        width: isSelected ? "100%" : undefined,
        // This card, UnitCard, and WireguardInterfaceCard settle their accent
        // line at the same speed so the expand-in-place cards read as one set.
        transition:
          "transform var(--hover-lift-duration) var(--hover-lift-ease), box-shadow var(--hover-lift-duration) var(--hover-lift-ease), border-color var(--app-transition-duration-fast) var(--app-easing-standard)",
        // The line itself comes from `accent` above. Opening a user isolates the
        // card outside the grid, where it can no longer be held to reorder — so
        // the line stands down with the lift.
        ...(isSelected && { borderBottomColor: "transparent" }),
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
        <AppButton
          aria-controls={detailsId}
          aria-expanded={isSelected}
          aria-label={`Toggle details for ${user.username}`}
          color="inherit"
          onClick={onOpen}
          style={{
            alignItems: "center",
            background: "transparent",
            border: 0,
            display: "flex",
            flex: 1,
            gap: GAP_SM,
            justifyContent: "flex-start",
            minWidth: 0,
            padding: 0,
            textAlign: "left",
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
              color={accentColor}
              height={32}
              icon={getUserIcon(user)}
              width={32}
            />
          </div>
          <div style={{ minWidth: 0 }}>
            <AppTypography
              fontWeight={700}
              noWrap
              style={{ lineHeight: 1.2 }}
              variant="subtitle1"
            >
              {user.username}
            </AppTypography>
            <AppTypography
              color="text.secondary"
              noWrap
              style={{ display: "block" }}
              variant="caption"
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
                  color="primary"
                  label="Your account"
                  size="xsmall"
                  variant="soft"
                />
              )}
              {user.isLocked && (
                <Chip
                  color="warning"
                  label="Locked"
                  size="xsmall"
                  variant="soft"
                />
              )}
              {user.isSystem && !isCurrentUser && (
                <Chip label="System" size="xsmall" variant="soft" />
              )}
            </div>
          )}
        </AppButton>

        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: 2,
            flexShrink: 0,
          }}
        >
          <AppActionIconButton
            disabled={user.username === "root"}
            icon="mdi:pencil"
            iconSize={14}
            label="Edit"
            onClick={onEdit}
          />
          <AppActionIconButton
            icon="mdi:form-textbox-password"
            iconSize={14}
            label="Change Password"
            onClick={onChangePassword}
          />
          <AppActionIconButton
            ariaLabel={
              pendingLockAction
                ? `${pendingLockAction === "lock" ? "Locking" : "Unlocking"} ${user.username}`
                : `${lockLabel} ${user.username}`
            }
            disabled={isProtected || Boolean(pendingLockAction)}
            icon={user.isLocked ? "mdi:lock-open" : "mdi:lock"}
            iconSize={14}
            label={lockLabel}
            loading={Boolean(pendingLockAction)}
            onClick={onToggleLock}
          />
          <StatusDot
            color={statusColor}
            size={8}
            style={{ marginLeft: 4 }}
            tooltip={statusTooltip}
          />
        </div>
      </div>

      <div id={detailsId}>
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
            color="text.secondary"
            style={{
              letterSpacing: "0.06em",
              display: "block",
              marginBottom: 4,
            }}
            variant="caption"
          >
            Groups ({groups.length})
          </AppTypography>
          {isSelected ? (
            <div
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
                  color={idx === 0 ? "primary" : "default"}
                  key={`${user.username}-${group}`}
                  label={group}
                  size="xsmall"
                  variant="soft"
                />
              ))}
            </div>
          ) : (
            <CompactGroupChips groups={groups} username={user.username} />
          )}
        </div>
      </div>
    </FrostedCard>
  );
};

export default UserCard;
