import { Icon } from "@iconify/react";
import { flushSync } from "react-dom";

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
  const nextViewMode = viewMode === "card" ? alternateMode : "card";
  const actionLabel = `Switch to ${nextViewMode} view`;
  const handleViewModeChange = () => {
    const startViewTransition = (
      document as Document & {
        startViewTransition?: (update: () => void) => unknown;
      }
    ).startViewTransition;

    if (!startViewTransition) {
      onViewModeChange(nextViewMode);
      return;
    }

    startViewTransition.call(document, () => {
      flushSync(() => onViewModeChange(nextViewMode));
    });
  };

  return (
    <AppTooltip title={actionLabel}>
      <AppIconButton
        aria-label={actionLabel}
        onClick={handleViewModeChange}
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
