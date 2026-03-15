import { Icon } from "@iconify/react";
import { IconButton } from "@mui/material";
import React from "react";

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
      <IconButton
        size="small"
        onClick={() => setViewMode(viewMode === "table" ? "card" : "table")}
      >
        {viewMode === "table" ? (
          <Icon icon="mdi:view-grid" width={20} height={20} />
        ) : (
          <Icon icon="mdi:table-row" width={20} height={20} />
        )}
      </IconButton>
    </AppTooltip>
  );
};

export default UnitViewToggle;
