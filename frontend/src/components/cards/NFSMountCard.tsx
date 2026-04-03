import React from "react";

import { type NFSMount } from "@/api";
import FrostedCard from "@/components/cards/RootCard";
import Chip from "@/components/ui/AppChip";
import AppLinearProgress from "@/components/ui/AppLinearProgress";
import AppTypography from "@/components/ui/AppTypography";
import { formatFileSize } from "@/utils/formaters";

export interface NFSMountCardProps {
  mount: NFSMount;
  statusLabel: string;
  persistenceLabel: string;
  actions: React.ReactNode;
}

const NFSMountCard: React.FC<NFSMountCardProps> = ({
  mount,
  statusLabel,
  persistenceLabel,
  actions,
}) => (
  <FrostedCard style={{ padding: 8 }}>
    {/* Header: mountpoint + actions */}
    <div
      style={{
        display: "flex",
        justifyContent: "space-between",
        alignItems: "center",
        gap: 8,
      }}
    >
      <AppTypography
        variant="body1"
        fontWeight={700}
        style={{
          fontFamily: "monospace",
          flex: 1,
          minWidth: 0,
          whiteSpace: "nowrap",
          overflow: "hidden",
          textOverflow: "ellipsis",
          lineHeight: 1.25,
        }}
      >
        {mount.mountpoint}
      </AppTypography>
      {actions}
    </div>

    {/* Source */}
    <AppTypography
      variant="body2"
      color="text.secondary"
      style={{
        marginBottom: 4,
        fontFamily: "monospace",
        fontSize: "0.8rem",
        lineHeight: 1.3,
        whiteSpace: "nowrap",
        overflow: "hidden",
        textOverflow: "ellipsis",
      }}
    >
      {mount.source}
    </AppTypography>

    {/* Usage bar or "not mounted" */}
    {mount.mounted ? (
      <div style={{ width: "100%", marginBottom: 4 }}>
        <AppLinearProgress
          variant="determinate"
          value={mount.usedPct}
          style={{ height: 6, borderRadius: 3, marginBottom: 2 }}
          color={
            mount.usedPct > 90 ? "error" : mount.usedPct > 70 ? "warning" : "primary"
          }
        />
        <AppTypography variant="caption" color="text.secondary">
          {formatFileSize(mount.used)} / {formatFileSize(mount.size)}
        </AppTypography>
      </div>
    ) : (
      <AppTypography
        variant="caption"
        color="text.secondary"
        style={{ display: "block", marginBottom: 4 }}
      >
        Not currently mounted
      </AppTypography>
    )}

    {/* Chips */}
    <div style={{ display: "flex", flexWrap: "wrap", gap: 3, marginBottom: 4 }}>
      <Chip label={mount.fsType} size="small" variant="soft" />
      <Chip label={statusLabel} size="small" variant="soft" />
      <Chip label={persistenceLabel} size="small" variant="soft" />
      {mount.options?.slice(0, 2).map((opt, i) => (
        <Chip key={`${mount.mountpoint}-${i}`} label={opt} size="small" variant="soft" />
      ))}
    </div>
  </FrostedCard>
);

export default NFSMountCard;
