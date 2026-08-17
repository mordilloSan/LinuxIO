import type { ReactNode } from "react";

import CardIconHeader from "@/components/cards/CardIconHeader";
import FrostedCard from "@/components/cards/FrostedCard";
import { CARD_PADDING_SM, GAP_SM } from "@/theme/constants";

export interface DockerSectionCardProps {
  children: ReactNode;
  fullHeight?: boolean;
  icon: ReactNode;
  subtitle: string;
  title: string;
}

const DockerSectionCard = ({
  icon,
  title,
  subtitle,
  children,
  fullHeight,
}: DockerSectionCardProps) => (
  <FrostedCard
    style={{ padding: CARD_PADDING_SM, ...(fullHeight ? { height: "100%" } : {}) }}
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
