import React from "react";

import CardIconHeader from "@/components/cards/CardIconHeader";
import FrostedCard from "@/components/cards/FrostedCard";
import { GAP_SM } from "@/theme/constants";

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
    style={{ padding: GAP_SM, ...(fullHeight ? { height: "100%" } : {}) }}
  >
    <CardIconHeader
      icon={icon}
      title={title}
      subtitle={subtitle}
      style={{ marginBottom: GAP_SM }}
    />
    {children}
  </FrostedCard>
);

export default DockerSectionCard;
