import React from "react";

import FrostedCard from "@/components/cards/FrostedCard";
import AppTypography from "@/components/ui/AppTypography";

export interface DockerStatCardProps {
  detail: React.ReactNode;
  label: string;
  onClick: () => void;
  value: React.ReactNode;
}

const DockerStatCard: React.FC<DockerStatCardProps> = ({
  label,
  value,
  detail,
  onClick,
}) => (
  <FrostedCard
    className="fc-opacity-hover"
    onClick={onClick}
    style={{
      paddingInline: 10,
      paddingBlock: 8,
      cursor: "pointer",
      transition: "opacity 0.15s",
    }}
  >
    <AppTypography
      color="text.secondary"
      style={{ lineHeight: 1.6 }}
      variant="overline"
    >
      {label}
    </AppTypography>
    <div
      style={{
        display: "flex",
        alignItems: "baseline",
        justifyContent: "space-between",
        marginTop: 1,
      }}
    >
      <AppTypography fontWeight={700} style={{ lineHeight: 1.2 }} variant="h6">
        {value}
      </AppTypography>
      <AppTypography
        color="text.secondary"
        noWrap
        style={{ textAlign: "right" }}
        variant="caption"
      >
        {detail}
      </AppTypography>
    </div>
  </FrostedCard>
);

export default DockerStatCard;
