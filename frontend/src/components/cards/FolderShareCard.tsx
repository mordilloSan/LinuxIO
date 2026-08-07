import type { ReactNode } from "react";

import FrostedCard from "@/components/cards/FrostedCard";
import AppTypography from "@/components/ui/AppTypography";

export interface FolderShareCardProps {
  actions: ReactNode;
  comment?: string;
  name: string;
  path: string;
  protocolSummary: ReactNode;
}

const FolderShareCard = ({
  name,
  path,
  comment,
  actions,
  protocolSummary,
}: FolderShareCardProps) => (
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
      <AppTypography fontWeight={700} variant="body2">
        {name}
      </AppTypography>
      {actions}
    </div>

    {/* Path */}
    <AppTypography
      color="text.secondary"
      style={{ marginBottom: 6, fontFamily: "monospace" }}
      variant="body2"
    >
      {path}
    </AppTypography>

    {/* Comment */}
    {comment && (
      <AppTypography
        color="text.secondary"
        style={{ display: "block", marginBottom: 8 }}
        variant="caption"
      >
        {comment}
      </AppTypography>
    )}

    {protocolSummary}
  </FrostedCard>
);

export default FolderShareCard;
