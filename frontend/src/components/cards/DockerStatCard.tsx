import React from "react";

import FrostedCard from "@/components/cards/FrostedCard";
import AppTypography from "@/components/ui/AppTypography";

export interface DockerStatCardProps {
  label: string;
  value: React.ReactNode;
  detail: React.ReactNode;
  onClick: () => void;
}

const DockerStatCard: React.FC<DockerStatCardProps> = ({
  label,
  value,
  detail,
  onClick,
}) => (
  <FrostedCard
    onClick={onClick}
    className="fc-opacity-hover"
    style={{
      paddingInline: 10,
      paddingBlock: 8,
      cursor: "pointer",
      transition: "opacity 0.15s",
    }}
  >
    <AppTypography
      variant="overline"
      color="text.secondary"
      style={{ lineHeight: 1.6 }}
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
      <AppTypography variant="h6" fontWeight={700} style={{ lineHeight: 1.2 }}>
        {value}
      </AppTypography>
      <AppTypography
        variant="caption"
        color="text.secondary"
        noWrap
        style={{ textAlign: "right" }}
      >
        {detail}
      </AppTypography>
    </div>
  </FrostedCard>
);

export default DockerStatCard;
