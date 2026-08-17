import { Icon } from "@iconify/react";
import { useId, type ReactNode } from "react";

import CardIconHeader from "@/components/cards/CardIconHeader";
import FrostedCard from "@/components/cards/FrostedCard";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import AppCollapse from "@/components/ui/AppCollapse";
import { CARD_PADDING_SM, TRANSITION_SLOW_CSS, GAP_SM } from "@/theme/constants";

export interface LVMSectionCardProps {
  accent: string;
  children: ReactNode;
  count: number;
  expanded: boolean;
  icon: string;
  onToggle: () => void;
  subtitle: string;
  title: string;
}

const LVMSectionCard = ({
  title,
  subtitle,
  count,
  icon,
  accent,
  expanded,
  onToggle,
  children,
}: LVMSectionCardProps) => {
  const panelId = useId();

  return (
    <FrostedCard style={{ padding: CARD_PADDING_SM }}>
      <AppButton
        aria-controls={panelId}
        aria-expanded={expanded}
        color="inherit"
        fullWidth
        onClick={onToggle}
        style={{
          cursor: "pointer",
          display: "block",
          minWidth: 0,
          padding: 0,
          textAlign: "left",
          userSelect: "none",
        }}
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
                  background: "var(--app-palette-action-hover)",
                  color: "var(--app-palette-text-secondary)",
                  flexShrink: 0,
                }}
              >
                <Icon
                  height={22}
                  icon="mdi:chevron-down"
                  style={{
                    transform: expanded ? "rotate(0deg)" : "rotate(-90deg)",
                    transition: `transform ${TRANSITION_SLOW_CSS}`,
                  }}
                  width={22}
                />
              </div>
            </div>
          }
          subtitle={subtitle}
          title={title}
        />
      </AppButton>
      <div id={panelId}>
        <AppCollapse in={expanded} unmountOnExit>
          <div style={{ marginTop: GAP_SM }}>{children}</div>
        </AppCollapse>
      </div>
    </FrostedCard>
  );
};

export default LVMSectionCard;
