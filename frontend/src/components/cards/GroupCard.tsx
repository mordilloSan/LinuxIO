import { Icon } from "@iconify/react";
import React from "react";

import { type AccountGroup } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import AppCheckbox from "@/components/ui/AppCheckbox";
import Chip from "@/components/ui/AppChip";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";

export interface GroupCardProps {
  group: AccountGroup;
  selected: boolean;
  onSelect: (checked: boolean) => void;
  onEditMembers: () => void;
}

const GroupCard: React.FC<GroupCardProps> = ({
  group,
  selected,
  onSelect,
  onEditMembers,
}) => (
  <FrostedCard style={{ padding: 8 }}>
    {/* Header: checkbox + name + edit action */}
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
          disabled={group.name === "root"}
        />
        <AppTypography variant="body2" fontWeight={700} noWrap>
          {group.name}
        </AppTypography>
      </div>
      <AppTooltip title="Edit Members">
        <AppIconButton
          size="small"
          onClick={onEditMembers}
          disabled={group.name === "root"}
        >
          <Icon icon="mdi:pencil" width={20} height={20} />
        </AppIconButton>
      </AppTooltip>
    </div>

    {/* Badges */}
    <div style={{ display: "flex", flexWrap: "wrap", gap: 3, marginBottom: 4 }}>
      {group.isSystem && <Chip label="System" size="small" variant="soft" />}
      <Chip label={`GID: ${group.gid}`} size="small" variant="soft" />
    </div>

    {/* Members */}
    <AppTypography variant="caption" color="text.secondary">
      Members ({group.members.length})
    </AppTypography>
    <div style={{ marginTop: 2, display: "flex", flexWrap: "wrap", gap: 2 }}>
      {group.members.length > 0 ? (
        group.members.map((member) => (
          <Chip
            key={`${group.name}-${member}`}
            label={member}
            size="small"
            variant="soft"
            style={{ fontSize: "0.7rem" }}
          />
        ))
      ) : (
        <AppTypography variant="body2" color="text.secondary">
          (no members)
        </AppTypography>
      )}
    </div>
  </FrostedCard>
);

export default GroupCard;
