import React from "react";

import { type DockerVolume } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import AppCheckbox from "@/components/ui/AppCheckbox";
import Chip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";
import { longTextStyles } from "@/theme/tableStyles";

export interface VolumeCardProps {
  onSelect: (checked: boolean) => void;
  selected: boolean;
  volume: DockerVolume;
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
          checked={selected}
          onChange={(e) => onSelect(e.target.checked)}
          size="small"
        />
        <AppTypography fontWeight={700} noWrap variant="body2">
          {volume.Name}
        </AppTypography>
      </div>
      <Chip
        label={volume.Driver}
        size="small"
        style={{ fontSize: "0.75rem" }}
        variant="soft"
      />
    </div>

    {/* Mountpoint */}
    <AppTypography
      style={{
        marginBottom: 4,
        fontFamily: "monospace",
        fontSize: "0.8rem",
        ...longTextStyles,
      }}
      variant="body2"
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
