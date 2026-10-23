import { Icon } from "@iconify/react";
import type { ReactNode } from "react";

import CardIconHeader from "@/components/cards/CardIconHeader";
import FrostedCard from "@/components/cards/FrostedCard";
import InfoRow from "@/components/ui/InfoRow";
import { CARD_PADDING_SM, GAP_MD } from "@/theme/constants";

export type SummaryRow = {
  label: string;
  value: ReactNode;
  noWrap?: boolean;
};

export const SummaryRowsList = ({ rows }: { rows: SummaryRow[] }) => (
  <div
    style={{
      display: "flex",
      flexDirection: "column",
      alignSelf: "stretch",
      width: "100%",
    }}
  >
    {rows.map(({ label, value, noWrap }) => (
      <InfoRow
        key={label}
        label={label}
        raw={typeof value !== "string"}
        variant="list"
        wrap={noWrap === false}
      >
        {value}
      </InfoRow>
    ))}
  </div>
);

const HardwareCard = ({
  title,
  subtitle,
  avatarIcon,
  accentColor,
  rows,
  actions,
}: {
  title: string;
  subtitle: string;
  avatarIcon: string;
  accentColor: string;
  rows: SummaryRow[];
  actions?: ReactNode;
}) => (
  <FrostedCard
    accent
    hoverLift
    style={{
      display: "flex",
      flexDirection: "column",
      boxSizing: "border-box",
      height: "100%",
      padding: CARD_PADDING_SM,
    }}
  >
    <CardIconHeader
      icon={
        <Icon color={accentColor} height={28} icon={avatarIcon} width={28} />
      }
      right={
        actions ? (
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: GAP_MD,
              flexShrink: 0,
            }}
          >
            {actions}
          </div>
        ) : undefined
      }
      style={{ marginBottom: 6 }}
      subtitle={subtitle}
      title={title}
    />

    <div style={{ display: "flex", flex: 1, minWidth: 0 }}>
      <div
        style={{
          flex: 1,
          minWidth: 0,
          display: "flex",
          alignItems: "stretch",
        }}
      >
        <SummaryRowsList rows={rows} />
      </div>
    </div>
  </FrostedCard>
);

export default HardwareCard;
