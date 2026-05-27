import React from "react";

import CardIconHeader from "@/components/cards/CardIconHeader";
import FrostedCard from "@/components/cards/FrostedCard";
import { GAP_SM } from "@/theme/constants";

export interface DockerSectionCardProps {
  children: React.ReactNode;
  fullHeight?: boolean;
  icon: React.ReactNode;
  subtitle: string;
  title: string;
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
      style={{ marginBottom: GAP_SM }}
      subtitle={subtitle}
      title={title}
    />
    {children}
  </FrostedCard>
);

export default DockerSectionCard;
