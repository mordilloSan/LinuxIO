import GridViewIcon from "@mui/icons-material/GridView";
import TableRowsIcon from "@mui/icons-material/TableRows";
import { IconButton, Tooltip } from "@mui/material";
import React from "react";

import { useViewMode } from "@/hooks/useViewMode";

interface UnitViewToggleProps {
  viewModeKey: string;
}

const UnitViewToggle: React.FC<UnitViewToggleProps> = ({ viewModeKey }) => {
  const [viewMode, setViewMode] = useViewMode(viewModeKey, "table");

  return (
    <Tooltip
      title={
        viewMode === "table" ? "Switch to card view" : "Switch to table view"
      }
    >
      <IconButton
        size="small"
        onClick={() => setViewMode(viewMode === "table" ? "card" : "table")}
      >
        {viewMode === "table" ? (
          <GridViewIcon fontSize="small" />
        ) : (
          <TableRowsIcon fontSize="small" />
        )}
      </IconButton>
    </Tooltip>
  );
};

export default UnitViewToggle;
