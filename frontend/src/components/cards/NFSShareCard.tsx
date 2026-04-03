import React from "react";

import { type NFSExport } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";

export interface NFSShareCardProps {
  share: NFSExport;
  onEdit: () => void;
  onRemove: () => void;
}

const NFSShareCard: React.FC<NFSShareCardProps> = ({
  share,
  onEdit,
  onRemove,
}) => (
  <FrostedCard style={{ padding: 8 }}>
    {/* Path + active indicator */}
    <div
      style={{ display: "flex", alignItems: "center", gap: 6, marginBottom: 4 }}
    >
      <span
        style={{
          width: 8,
          height: 8,
          borderRadius: "50%",
          backgroundColor: share.active ? "#00E676" : "#9e9e9e",
          flexShrink: 0,
        }}
      />
      <AppTypography
        variant="body2"
        fontWeight={700}
        style={{ fontFamily: "monospace" }}
      >
        {share.path}
      </AppTypography>
    </div>

    {/* Client chips */}
    <div style={{ display: "flex", flexWrap: "wrap", gap: 3, marginBottom: 8 }}>
      {share.clients.map((client, i) => (
        <Chip
          key={i}
          label={
            client.options?.length > 0
              ? `${client.host}(${client.options.slice(0, 2).join(",")}${client.options.length > 2 ? "..." : ""})`
              : client.host
          }
          size="small"
          variant="soft"
        />
      ))}
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

export default NFSShareCard;
