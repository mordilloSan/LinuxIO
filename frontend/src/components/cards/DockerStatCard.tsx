import type { ReactNode } from "react";

import FrostedCard from "@/components/cards/FrostedCard";
import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";
import { CARD_PADDING_MD, CARD_PADDING_SM } from "@/theme/constants";

export interface DockerStatCardProps {
  detail: ReactNode;
  label: string;
  onClick: () => void;
  value: ReactNode;
}

const DockerStatCard = ({
  label,
  value,
  detail,
  onClick,
}: DockerStatCardProps) => (
  <FrostedCard
    hoverLift
    style={{
      padding: 0,
    }}
  >
    <AppButton
      color="inherit"
      onClick={onClick}
      style={{
        appearance: "none",
        background: "none",
        border: 0,
        color: "inherit",
        cursor: "pointer",
        display: "block",
        font: "inherit",
        padding: `${CARD_PADDING_SM}px ${CARD_PADDING_MD}px`,
        textAlign: "left",
        width: "100%",
      }}
    >
      <AppTypography
        color="text.secondary"
        style={{ lineHeight: 1.6 }}
        variant="overline"
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
        <AppTypography
          fontWeight={700}
          style={{ lineHeight: 1.2 }}
          variant="h6"
        >
          {value}
        </AppTypography>
        <AppTypography
          color="text.secondary"
          noWrap
          style={{ textAlign: "right" }}
          variant="caption"
        >
          {detail}
        </AppTypography>
      </div>
    </AppButton>
  </FrostedCard>
);

export default DockerStatCard;
