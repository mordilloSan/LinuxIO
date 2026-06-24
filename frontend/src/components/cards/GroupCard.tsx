import { Icon } from "@iconify/react";
import React from "react";

import { type AccountGroup } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import {
  type SummaryRow,
  SummaryRowsList,
} from "@/components/cards/HardwareCard";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import Chip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";
import { GAP_SM } from "@/theme/constants";

export interface GroupCardProps {
  group: AccountGroup;
  onDelete: () => void;
  onEditMembers: () => void;
}

const GroupCard: React.FC<GroupCardProps> = ({
  group,
  onEditMembers,
  onDelete,
}) => {
  const theme = useAppTheme();
  const isRoot = group.name === "root";
  const isProtected = isRoot || group.isSystem;

  const accentColor = group.isSystem
    ? theme.palette.text.secondary
    : theme.palette.primary.main;

  const icon = group.isSystem ? "mdi:account-cog" : "mdi:account-group";

  const visibleMembers = group.members.slice(0, 6);
  const overflow = group.members.length - visibleMembers.length;

  const rows: SummaryRow[] = [
    { label: "GID", value: String(group.gid) },
    {
      label: `Members (${group.members.length})`,
      value:
        group.members.length > 0 ? (
          <div
            style={{
              display: "flex",
              flexWrap: "wrap",
              gap: 3,
              justifyContent: "flex-end",
            }}
          >
            {visibleMembers.map((member) => (
              <Chip
                key={`${group.name}-${member}`}
                label={member}
                size="small"
                style={{ fontSize: "0.65rem", height: 20 }}
                variant="soft"
              />
            ))}
            {overflow > 0 && (
              <Chip
                label={`+${overflow}`}
                size="small"
                style={{ fontSize: "0.65rem", height: 20 }}
                variant="soft"
              />
            )}
          </div>
        ) : (
          <AppTypography color="text.disabled" variant="caption">
            None
          </AppTypography>
        ),
    },
  ];

  return (
    <FrostedCard
      hoverLift
      style={{
        padding: 10,
        display: "flex",
        flexDirection: "column",
        height: "100%",
      }}
    >
      {/* Header */}
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          gap: GAP_SM,
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
            <Icon color={accentColor} height={32} icon={icon} width={32} />
          </div>
          <div style={{ minWidth: 0 }}>
            <AppTypography
              fontWeight={700}
              noWrap
              style={{ lineHeight: 1.2 }}
              variant="subtitle1"
            >
              {group.name}
            </AppTypography>
            <AppTypography
              color="text.secondary"
              noWrap
              style={{ display: "block" }}
              variant="caption"
            >
              {group.isSystem ? "System group" : "User group"}
            </AppTypography>
          </div>
        </div>

        <div style={{ display: "flex", gap: 2, flexShrink: 0 }}>
          <AppActionIconButton
            disabled={isRoot}
            icon="mdi:pencil"
            iconSize={18}
            label="Edit Members"
            onClick={onEditMembers}
          />
          <AppActionIconButton
            color={isProtected ? undefined : theme.palette.error.main}
            disabled={isProtected}
            icon="mdi:delete"
            iconSize={18}
            label={isProtected ? "Cannot delete" : "Delete Group"}
            onClick={onDelete}
          />
        </div>
      </div>

      {/* Summary rows */}
      <div style={{ marginTop: 8 }}>
        <SummaryRowsList rows={rows} />
      </div>
    </FrostedCard>
  );
};

export default GroupCard;
