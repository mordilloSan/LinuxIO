import { Icon } from "@iconify/react";
import type { ReactNode } from "react";

import CardIconHeader from "@/components/cards/CardIconHeader";
import FrostedCard from "@/components/cards/FrostedCard";
import SelectableCard from "@/components/cards/SelectableCard";
import { useAppTheme } from "@/theme";
import { CARD_PADDING_SM, GAP_MD } from "@/theme/constants";

export interface DockerResourceCardProps {
  children: ReactNode;
  icon: string;
  label: string;
  onSelect: (checked: boolean) => void;
  selected: boolean;
  subtitle: ReactNode;
  title: string;
}

const DockerResourceCard = ({
  children,
  icon,
  label,
  onSelect,
  selected,
  subtitle,
  title,
}: DockerResourceCardProps) => {
  const theme = useAppTheme();

  return (
    <SelectableCard label={label} onSelect={onSelect} selected={selected}>
      <FrostedCard
        accent
        className={selected ? "docker-resource-card--selected" : undefined}
        hoverLift
        style={{
          boxSizing: "border-box",
          display: "flex",
          flexDirection: "column",
          height: "100%",
          minWidth: 0,
          padding: CARD_PADDING_SM,
          width: "100%",
          ...(selected && {
            borderBottomColor: "var(--fc-accent)",
            boxShadow: "var(--fc-lift-shadow)",
          }),
        }}
      >
        <CardIconHeader
          icon={
            <Icon
              color={theme.palette.primary.main}
              height={28}
              icon={icon}
              width={28}
            />
          }
          style={{ marginBottom: GAP_MD }}
          subtitle={subtitle}
          title={title}
        />
        {children}
      </FrostedCard>
    </SelectableCard>
  );
};

export default DockerResourceCard;
