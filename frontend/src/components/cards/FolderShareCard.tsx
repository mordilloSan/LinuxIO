import React from "react";

import FrostedCard from "@/components/cards/RootCard";
import AppTypography from "@/components/ui/AppTypography";

export interface FolderShareCardProps {
  name: string;
  path: string;
  comment?: string;
  actions: React.ReactNode;
  protocolSummary: React.ReactNode;
}

const FolderShareCard: React.FC<FolderShareCardProps> = ({
  name,
  path,
  comment,
  actions,
  protocolSummary,
}) => (
  <FrostedCard style={{ padding: 10 }}>
    {/* Header: name + actions */}
    <div
      style={{
        display: "flex",
        justifyContent: "space-between",
        alignItems: "flex-start",
        gap: 8,
        marginBottom: 6,
      }}
    >
      <AppTypography variant="body2" fontWeight={700}>
        {name}
      </AppTypography>
      {actions}
    </div>

    {/* Path */}
    <AppTypography
      variant="body2"
      color="text.secondary"
      style={{ marginBottom: 6, fontFamily: "monospace" }}
    >
      {path}
    </AppTypography>

    {/* Comment */}
    {comment && (
      <AppTypography
        variant="caption"
        color="text.secondary"
        style={{ display: "block", marginBottom: 8 }}
      >
        {comment}
      </AppTypography>
    )}

    {protocolSummary}
  </FrostedCard>
);

export default FolderShareCard;
