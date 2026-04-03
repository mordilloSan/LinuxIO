import { Icon } from "@iconify/react";
import React from "react";

import CardIconHeader from "@/components/cards/CardIconHeader";
import FrostedCard from "@/components/cards/FrostedCard";
import AppTypography from "@/components/ui/AppTypography";
import { GAP_SM } from "@/theme/constants";

export interface LVMMetricCardProps {
  title: string;
  value: string;
  subtitle: string;
  icon: string;
  color: string;
}

const LVMMetricCard: React.FC<LVMMetricCardProps> = ({
  title,
  value,
  subtitle,
  icon,
  color,
}) => (
  <FrostedCard style={{ padding: GAP_SM, minWidth: 0 }}>
    <CardIconHeader
      icon={<Icon icon={icon} width={22} height={22} color={color} />}
      title={title}
      style={{ marginBottom: GAP_SM }}
    />
    <AppTypography
      variant="subtitle1"
      fontWeight={800}
      style={{ marginBottom: 4 }}
    >
      {value}
    </AppTypography>
    <AppTypography variant="body2" color="text.secondary">
      {subtitle}
    </AppTypography>
  </FrostedCard>
);

export default LVMMetricCard;
