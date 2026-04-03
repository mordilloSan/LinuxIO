import { Icon } from "@iconify/react";
import React from "react";

import CardIconHeader from "@/components/cards/CardIconHeader";
import FrostedCard from "@/components/cards/FrostedCard";
import Chip from "@/components/ui/AppChip";
import { GAP_SM } from "@/theme/constants";

export interface LVMSectionCardProps {
  title: string;
  subtitle: string;
  count: number;
  icon: string;
  accent: string;
  expanded: boolean;
  onToggle: () => void;
  children: React.ReactNode;
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
      role="button"
      tabIndex={0}
      onClick={onToggle}
      onKeyDown={(event) => {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          onToggle();
        }
      }}
      style={{ cursor: "pointer", userSelect: "none" }}
    >
      <CardIconHeader
        icon={<Icon icon={icon} width={24} height={24} color={accent} />}
        title={title}
        subtitle={subtitle}
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
                icon="mdi:chevron-down"
                width={22}
                height={22}
                style={{
                  transform: expanded ? "rotate(0deg)" : "rotate(-90deg)",
                  transition: "transform 0.2s ease",
                }}
              />
            </div>
          </div>
        }
      />
    </div>
    {expanded ? <div style={{ marginTop: GAP_SM }}>{children}</div> : null}
  </FrostedCard>
);

export default LVMSectionCard;
