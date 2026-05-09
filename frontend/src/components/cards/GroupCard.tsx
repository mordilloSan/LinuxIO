import { Icon } from "@iconify/react";
import React from "react";

import { type AccountGroup } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import {
  SummaryRowsList,
  type SummaryRow,
} from "@/components/cards/HardwareCard";
import Chip from "@/components/ui/AppChip";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";
import { GAP_SM } from "@/theme/constants";

export interface GroupCardProps {
  group: AccountGroup;
  onEditMembers: () => void;
  onDelete: () => void;
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
                variant="soft"
                style={{ fontSize: "0.65rem", height: 20 }}
              />
            ))}
            {overflow > 0 && (
              <Chip
                label={`+${overflow}`}
                size="small"
                variant="soft"
                style={{ fontSize: "0.65rem", height: 20 }}
              />
            )}
          </div>
        ) : (
          <AppTypography variant="caption" color="text.disabled">
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
            <Icon icon={icon} width={32} height={32} color={accentColor} />
          </div>
          <div style={{ minWidth: 0 }}>
            <AppTypography
              variant="subtitle1"
              fontWeight={700}
              noWrap
              style={{ lineHeight: 1.2 }}
            >
              {group.name}
            </AppTypography>
            <AppTypography
              variant="caption"
              color="text.secondary"
              noWrap
              style={{ display: "block" }}
            >
              {group.isSystem ? "System group" : "User group"}
            </AppTypography>
          </div>
        </div>

        <div style={{ display: "flex", gap: 2, flexShrink: 0 }}>
          <AppTooltip title="Edit Members">
            <span>
              <AppIconButton
                size="small"
                onClick={onEditMembers}
                disabled={isRoot}
              >
                <Icon icon="mdi:pencil" width={18} height={18} />
              </AppIconButton>
            </span>
          </AppTooltip>
          <AppTooltip title={isProtected ? "Cannot delete" : "Delete Group"}>
            <span>
              <AppIconButton
                size="small"
                onClick={onDelete}
                disabled={isProtected}
                style={{
                  color: isProtected
                    ? undefined
                    : theme.palette.error.main,
                }}
              >
                <Icon icon="mdi:delete" width={18} height={18} />
              </AppIconButton>
            </span>
          </AppTooltip>
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
