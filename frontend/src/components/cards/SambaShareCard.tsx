import React from "react";

import { type SambaShare } from "@/api";
import FrostedCard from "@/components/cards/RootCard";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";

const displayProps = ["browseable", "read only", "guest ok", "writable"] as const;

export interface SambaShareCardProps {
  share: SambaShare;
  onEdit: () => void;
  onRemove: () => void;
}

const SambaShareCard: React.FC<SambaShareCardProps> = ({ share, onEdit, onRemove }) => (
  <FrostedCard style={{ padding: 8 }}>
    {/* Name */}
    <AppTypography variant="body2" fontWeight={700} style={{ marginBottom: 2 }}>
      {share.name}
    </AppTypography>

    {/* Path */}
    <AppTypography
      variant="body2"
      style={{ marginBottom: 4, fontFamily: "monospace" }}
    >
      {share.properties["path"]}
    </AppTypography>

    {/* Comment */}
    {share.properties["comment"] && (
      <AppTypography
        variant="caption"
        color="text.secondary"
        style={{ marginBottom: 4, display: "block" }}
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
      <AppButton size="small" variant="outlined" onClick={onEdit}>
        Edit
      </AppButton>
      <AppButton size="small" color="error" onClick={onRemove}>
        Remove
      </AppButton>
    </div>
  </FrostedCard>
);

export default SambaShareCard;
