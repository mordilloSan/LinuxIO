import React from "react";

import { type SambaShare } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";

const displayProps = [
  "browseable",
  "read only",
  "guest ok",
  "writable",
] as const;

export interface SambaShareCardProps {
  onEdit: () => void;
  onRemove: () => void;
  share: SambaShare;
}

const SambaShareCard: React.FC<SambaShareCardProps> = ({
  share,
  onEdit,
  onRemove,
}) => (
  <FrostedCard style={{ padding: 8 }}>
    {/* Name */}
    <AppTypography fontWeight={700} style={{ marginBottom: 2 }} variant="body2">
      {share.name}
    </AppTypography>

    {/* Path */}
    <AppTypography
      style={{ marginBottom: 4, fontFamily: "monospace" }}
      variant="body2"
    >
      {share.properties["path"]}
    </AppTypography>

    {/* Comment */}
    {share.properties["comment"] && (
      <AppTypography
        color="text.secondary"
        style={{ marginBottom: 4, display: "block" }}
        variant="caption"
      >
        {share.properties["comment"]}
      </AppTypography>
    )}

    {/* Property chips */}
    <div style={{ display: "flex", flexWrap: "wrap", gap: 3, marginBottom: 8 }}>
      {displayProps.map((prop) =>
        share.properties[prop] ? (
          <Chip
            key={prop}
            label={`${prop}: ${share.properties[prop]}`}
            size="small"
            variant="soft"
          />
        ) : null,
      )}
    </div>

    {/* Actions */}
    <div style={{ display: "flex", gap: 4 }}>
      <AppButton onClick={onEdit} size="small" variant="outlined">
        Edit
      </AppButton>
      <AppButton color="error" onClick={onRemove} size="small">
        Remove
      </AppButton>
    </div>
  </FrostedCard>
);

export default SambaShareCard;
