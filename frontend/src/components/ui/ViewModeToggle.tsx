import { Icon } from "@iconify/react";

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

  return (
    <AppTooltip title={actionLabel}>
      <AppIconButton
        aria-label={actionLabel}
        onClick={() => onViewModeChange(nextViewMode)}
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
