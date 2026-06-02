import { Icon } from "@iconify/react";
import React from "react";

import CardIconHeader from "@/components/cards/CardIconHeader";
import FrostedCard from "@/components/cards/FrostedCard";
import Chip from "@/components/ui/AppChip";
import { GAP_SM } from "@/theme/constants";

export interface LVMSectionCardProps {
  accent: string;
  children: React.ReactNode;
  count: number;
  expanded: boolean;
  icon: string;
  onToggle: () => void;
  subtitle: string;
  title: string;
}

const LVMSectionCard: React.FC<LVMSectionCardProps> = ({
  title,
  subtitle,
  count,
  icon,
  accent,
  expanded,
  onToggle,
  children,
}) => (
  <FrostedCard style={{ padding: GAP_SM }}>
    <div
      onClick={onToggle}
      onKeyDown={(event) => {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          onToggle();
        }
      }}
      role="button"
      style={{ cursor: "pointer", userSelect: "none" }}
      tabIndex={0}
    >
      <CardIconHeader
        icon={<Icon color={accent} height={24} icon={icon} width={24} />}
        right={
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: GAP_SM,
              flexShrink: 0,
            }}
          >
            <Chip label={`${count}`} size="small" variant="soft" />
            <div
              style={{
                width: 36,
                height: 36,
                borderRadius: 12,
                display: "flex",
                alignItems: "center",
                justifyContent: "center",
                background: "var(--mui-palette-action-hover)",
                color: "var(--mui-palette-text-secondary)",
                flexShrink: 0,
              }}
            >
              <Icon
                height={22}
                icon="mdi:chevron-down"
                style={{
                  transform: expanded ? "rotate(0deg)" : "rotate(-90deg)",
                  transition: "transform 0.2s ease",
                }}
                width={22}
              />
            </div>
          </div>
        }
        subtitle={subtitle}
        title={title}
      />
    </div>
    {expanded ? <div style={{ marginTop: GAP_SM }}>{children}</div> : null}
  </FrostedCard>
);

export default LVMSectionCard;
