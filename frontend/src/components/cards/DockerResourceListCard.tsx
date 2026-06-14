import { Icon } from "@iconify/react";
import React from "react";

import CardIconHeader from "@/components/cards/CardIconHeader";
import FrostedCard from "@/components/cards/FrostedCard";
import AppButton from "@/components/ui/AppButton";
import AppDivider from "@/components/ui/AppDivider";
import AppTypography from "@/components/ui/AppTypography";

export interface DockerResourceListCardProps {
  children: React.ReactNode;
  footerText: string;
  icon: React.ReactNode;
  onViewAll: () => void;
  subtitle: React.ReactNode;
  title: string;
}

const DockerResourceListCard: React.FC<DockerResourceListCardProps> = ({
  icon,
  title,
  subtitle,
  onViewAll,
  children,
  footerText,
}) => (
  <FrostedCard>
    <CardIconHeader
      icon={icon}
      right={
        <AppButton onClick={onViewAll} size="small" style={{ flexShrink: 0 }}>
          <span
            style={{ display: "inline-flex", alignItems: "center", gap: 6 }}
          >
            View All
            <Icon height={20} icon="mdi:chevron-right" width={20} />
          </span>
        </AppButton>
      }
      style={{ padding: 8, paddingBottom: 6 }}
      subtitle={subtitle}
      title={title}
    />

    {children}

    <AppDivider />
    <div style={{ paddingInline: 8, paddingBlock: 4 }}>
      <AppTypography color="text.secondary" variant="caption">
        {footerText}
      </AppTypography>
    </div>
  </FrostedCard>
);

export default DockerResourceListCard;
