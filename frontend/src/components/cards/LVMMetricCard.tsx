import { Icon } from "@iconify/react";

import CardIconHeader from "@/components/cards/CardIconHeader";
import FrostedCard from "@/components/cards/FrostedCard";
import AppTypography from "@/components/ui/AppTypography";
import { CARD_PADDING_SM, GAP_SM } from "@/theme/constants";

export interface LVMMetricCardProps {
  color: string;
  icon: string;
  subtitle: string;
  title: string;
  value: string;
}

const LVMMetricCard = ({
  title,
  value,
  subtitle,
  icon,
  color,
}: LVMMetricCardProps) => (
  <FrostedCard style={{ padding: CARD_PADDING_SM, minWidth: 0 }}>
    <CardIconHeader
      icon={<Icon color={color} height={22} icon={icon} width={22} />}
      style={{ marginBottom: GAP_SM }}
      title={title}
    />
    <AppTypography
      fontWeight={800}
      style={{ marginBottom: 4 }}
      variant="subtitle1"
    >
      {value}
    </AppTypography>
    <AppTypography color="text.secondary" variant="body2">
      {subtitle}
    </AppTypography>
  </FrostedCard>
);

export default LVMMetricCard;
