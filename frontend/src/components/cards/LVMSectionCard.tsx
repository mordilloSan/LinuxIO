import { Icon } from "@iconify/react";
import React from "react";

import FrostedCard from "@/components/cards/FrostedCard";
import Chip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";

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
  <FrostedCard style={{ padding: 12 }}>
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
      style={{
        display: "flex",
        alignItems: "center",
        justifyContent: "space-between",
        gap: 12,
        cursor: "pointer",
        userSelect: "none",
      }}
    >
      <div
        style={{ display: "flex", alignItems: "center", gap: 12, minWidth: 0 }}
      >
        <div
          style={{
            width: 46,
            height: 46,
            borderRadius: 14,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            color: accent,
            background: `color-mix(in srgb, ${accent} 16%, transparent)`,
            border: `1px solid color-mix(in srgb, ${accent} 30%, transparent)`,
            flexShrink: 0,
          }}
        >
          <Icon icon={icon} width={24} height={24} />
        </div>
        <div style={{ minWidth: 0 }}>
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: 8,
              flexWrap: "wrap",
              marginBottom: 2,
            }}
          >
            <AppTypography variant="subtitle1" fontWeight={700}>
              {title}
            </AppTypography>
            <Chip label={`${count}`} size="small" variant="soft" />
          </div>
          <AppTypography variant="body2" color="text.secondary">
            {subtitle}
          </AppTypography>
        </div>
      </div>
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
    {expanded ? <div style={{ marginTop: 14 }}>{children}</div> : null}
  </FrostedCard>
);

export default LVMSectionCard;
