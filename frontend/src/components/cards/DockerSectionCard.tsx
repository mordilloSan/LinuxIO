import React from "react";

import FrostedCard from "@/components/cards/FrostedCard";
import AppTypography from "@/components/ui/AppTypography";

export interface DockerSectionCardProps {
  icon: React.ReactNode;
  title: string;
  subtitle: string;
  children: React.ReactNode;
  fullHeight?: boolean;
}

const DockerSectionCard: React.FC<DockerSectionCardProps> = ({
  icon,
  title,
  subtitle,
  children,
  fullHeight,
}) => (
  <FrostedCard
    style={{ padding: 8, ...(fullHeight ? { height: "100%" } : {}) }}
  >
    <div
      style={{
        display: "flex",
        alignItems: "center",
        gap: 6,
        marginBottom: 6,
      }}
    >
      <div
        style={{
          width: 40,
          height: 40,
          borderRadius: 8,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          flexShrink: 0,
        }}
      >
        {icon}
      </div>
      <div>
        <AppTypography
          variant="subtitle1"
          fontWeight={700}
          style={{ lineHeight: 1.2 }}
        >
          {title}
        </AppTypography>
        <AppTypography variant="caption" color="text.secondary">
          {subtitle}
        </AppTypography>
      </div>
    </div>
    {children}
  </FrostedCard>
);

export default DockerSectionCard;
