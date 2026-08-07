import { Icon } from "@iconify/react";

import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";
import { TRANSITION_SLOW_CSS } from "@/theme/constants";
import "./section-header.css";

interface SectionHeaderProps {
  /** id of the collapsible panel this header controls */
  controlsId: string;
  expanded: boolean;
  onToggle: () => void;
  title: string;
}

/**
 * Collapsible section title with a chevron that fades in on hover/focus.
 * Used for the section stacks on dashboard-style pages.
 */
const SectionHeader = ({
  controlsId,
  expanded,
  onToggle,
  title,
}: SectionHeaderProps) => (
  <AppButton
    aria-controls={controlsId}
    aria-expanded={expanded}
    className="section-header"
    onClick={onToggle}
    style={{
      alignItems: "center",
      background: "none",
      border: 0,
      color: "inherit",
      cursor: "pointer",
      display: "flex",
      font: "inherit",
      justifyContent: "space-between",
      marginBottom: 6,
      padding: 0,
      textAlign: "left",
      userSelect: "none",
      width: "100%",
    }}
    type="button"
  >
    <AppTypography fontWeight={700} variant="subtitle1">
      {title}
    </AppTypography>
    <span
      aria-hidden="true"
      className="section-toggle"
      style={{
        alignItems: "center",
        display: "flex",
        height: 36,
        justifyContent: "center",
        opacity: 0,
        pointerEvents: "none",
        transition: "opacity 0.15s",
        width: 36,
      }}
    >
      <Icon
        height={24}
        icon="mdi:chevron-up"
        style={{
          transition: `transform ${TRANSITION_SLOW_CSS}`,
          transform: expanded ? "rotate(0deg)" : "rotate(180deg)",
        }}
        width={24}
      />
    </span>
  </AppButton>
);

export default SectionHeader;
