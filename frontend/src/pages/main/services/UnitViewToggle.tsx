import { Icon } from "@iconify/react";
import React from "react";

import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import { useViewMode } from "@/hooks/useViewMode";

interface UnitViewToggleProps {
  viewModeKey: string;
}

const UnitViewToggle: React.FC<UnitViewToggleProps> = ({ viewModeKey }) => {
  const [viewMode, setViewMode] = useViewMode(viewModeKey, "table");

  return (
    <AppTooltip
      title={
        viewMode === "table" ? "Switch to card view" : "Switch to table view"
      }
    >
      <AppIconButton
        onClick={() => setViewMode(viewMode === "table" ? "card" : "table")}
        size="small"
      >
        {viewMode === "table" ? (
          <Icon height={20} icon="mdi:card-multiple" width={20} />
        ) : (
          <Icon height={20} icon="mdi:table" width={20} />
        )}
      </AppIconButton>
    </AppTooltip>
  );
};

export default UnitViewToggle;
