import { Icon } from "@iconify/react";
import type { ReactNode } from "react";

import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";
import "./section-header.css";

interface SectionHeaderProps {
  /** Controls floated on the trailing edge just before the chevron, outside the toggle button. */
  actions?: ReactNode;
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
  actions,
  controlsId,
  expanded,
  onToggle,
  title,
}: SectionHeaderProps) => (
  <div
    className="section-header"
    style={{
      alignItems: "center",
      display: "flex",
      marginBottom: 6,
      position: "relative",
    }}
  >
    <AppButton
      aria-controls={controlsId}
      aria-expanded={expanded}
      onClick={onToggle}
      style={{
        alignItems: "center",
        background: "none",
        border: 0,
        color: "inherit",
        cursor: "pointer",
        display: "flex",
        flex: 1,
        font: "inherit",
        justifyContent: "space-between",
        minWidth: 0,
        padding: 0,
        textAlign: "start",
        userSelect: "none",
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
            transition:
              "transform var(--app-transition-duration-fast) var(--app-easing-standard)",
            transform: expanded ? "rotate(0deg)" : "rotate(180deg)",
          }}
          width={24}
        />
      </span>
    </AppButton>
    {actions ? (
      <div style={{ insetInlineEnd: 52, position: "absolute" }}>{actions}</div>
    ) : null}
  </div>
);

export default SectionHeader;
