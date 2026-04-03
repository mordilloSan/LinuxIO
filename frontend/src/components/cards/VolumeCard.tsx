import React from "react";

import { type DockerVolume } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import AppCheckbox from "@/components/ui/AppCheckbox";
import Chip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";
import { longTextStyles } from "@/theme/tableStyles";

export interface VolumeCardProps {
  volume: DockerVolume;
  selected: boolean;
  onSelect: (checked: boolean) => void;
}

const VolumeCard: React.FC<VolumeCardProps> = ({
  volume,
  selected,
  onSelect,
}) => (
  <FrostedCard style={{ padding: 8 }}>
    {/* Header: checkbox + name + driver chip */}
    <div
      style={{
        display: "flex",
        alignItems: "center",
        justifyContent: "space-between",
        gap: 8,
        marginBottom: 8,
      }}
    >
      <div
        style={{ display: "flex", alignItems: "center", gap: 8, minWidth: 0 }}
      >
        <AppCheckbox
          size="small"
          checked={selected}
          onChange={(e) => onSelect(e.target.checked)}
        />
        <AppTypography variant="body2" fontWeight={700} noWrap>
          {volume.Name}
        </AppTypography>
      </div>
      <Chip
        label={volume.Driver}
        size="small"
        variant="soft"
        style={{ fontSize: "0.75rem" }}
      />
    </div>

    {/* Mountpoint */}
    <AppTypography
      variant="body2"
      style={{
        marginBottom: 4,
        fontFamily: "monospace",
        fontSize: "0.8rem",
        ...longTextStyles,
      }}
    >
      {volume.Mountpoint || "-"}
    </AppTypography>

    {/* Meta chips */}
    <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
      <Chip
        label={`Scope: ${volume.Scope || "local"}`}
        size="small"
        variant="soft"
      />
      {volume.CreatedAt && (
        <Chip
          label={new Date(volume.CreatedAt).toLocaleDateString()}
          size="small"
          variant="soft"
        />
      )}
    </div>
  </FrostedCard>
);

export default VolumeCard;
