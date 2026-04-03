import React from "react";

import CardIconHeader from "@/components/cards/CardIconHeader";
import FrostedCard from "@/components/cards/FrostedCard";

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
    <CardIconHeader
      icon={icon}
      title={title}
      subtitle={subtitle}
      style={{ marginBottom: 6 }}
    />
    {children}
  </FrostedCard>
);

export default DockerSectionCard;
