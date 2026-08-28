import { Icon } from "@iconify/react";
import { animate, useReducedMotion } from "motion/react";
import { flushSync } from "react-dom";

import {
  EASING_DECELERATE,
  TRANSITION_DURATION_MEDIUM_MS,
} from "@/theme/constants";

import AppIconButton from "./AppIconButton";
import AppTooltip from "./AppTooltip";

type AlternateViewMode = "list" | "table";

export interface ViewModeToggleProps<T extends AlternateViewMode> {
  alternateMode: T;
  onViewModeChange: (next: "card" | T) => void;
  viewMode: "card" | T;
}

function ViewModeToggle<T extends AlternateViewMode>({
  alternateMode,
  onViewModeChange,
  viewMode,
}: ViewModeToggleProps<T>) {
  const prefersReducedMotion = useReducedMotion();
  const nextViewMode = viewMode === "card" ? alternateMode : "card";
  const actionLabel = `Switch to ${nextViewMode} view`;
  const handleViewModeChange = (trigger: HTMLElement) => {
    const routeContent =
      trigger.closest<HTMLElement>("[data-app-route-content]") ??
      document.querySelector<HTMLElement>("[data-app-route-content]");
    const content =
      routeContent?.querySelector<HTMLElement>(
        "[data-app-view-mode-content]",
      ) ?? routeContent;
    const shouldAnimate = content !== null && !prefersReducedMotion;

    // Hide before the synchronous render so the new view never paints at full
    // opacity for a frame before animate() applies its first keyframe.
    if (shouldAnimate) content.style.opacity = "0";
    flushSync(() => onViewModeChange(nextViewMode));
    if (!shouldAnimate) return;

    // Virtualized grids correct estimated row heights from a ResizeObserver
    // deferred one animation frame; wait it out so the correction happens while
    // the content is still invisible instead of mid-fade.
    requestAnimationFrame(() =>
      requestAnimationFrame(() =>
        animate(
          content,
          { opacity: [0, 1], y: [6, 0] },
          {
            duration: TRANSITION_DURATION_MEDIUM_MS / 1000,
            ease: EASING_DECELERATE,
          },
        ),
      ),
    );
  };

  return (
    <AppTooltip title={actionLabel}>
      <AppIconButton
        aria-label={actionLabel}
        onClick={(event) => handleViewModeChange(event.currentTarget)}
        size="small"
      >
        <Icon
          height={20}
          icon={nextViewMode === "card" ? "mdi:card-multiple" : "mdi:view-list"}
          width={20}
        />
      </AppIconButton>
    </AppTooltip>
  );
}

export default ViewModeToggle;
