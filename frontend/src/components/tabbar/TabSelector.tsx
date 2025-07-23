import { Paper, ToggleButton, ToggleButtonGroup } from "@mui/material";
import React, { useContext } from "react";

import { ThemeContext } from "@/contexts/ThemeContext";

type TabOption = {
  value: string;
  label: string;
};

interface TabSelectorProps {
  value: string;
  onChange: (value: string) => void;
  options: TabOption[];
}

const TabSelector: React.FC<TabSelectorProps> = ({
  value,
  onChange,
  options,
}) => {
  const { primaryColor } = useContext(ThemeContext);

  return (
    <Paper
      elevation={0}
      sx={{
        display: "flex",
        justifyContent: "center",
        p: 0.5,
        width: "fit-content",
        borderRadius: "999px",
        backgroundColor: "transparent",
        backdropFilter: "none",
        mb: 2,
      }}
    >
      <ToggleButtonGroup
        value={value}
        exclusive
        onChange={(_, newValue) => newValue && onChange(newValue)}
        size="small"
        sx={{
          "& .MuiToggleButton-root": {
            color: "text.secondary",
            border: "none",
            borderRadius: "999px",
            px: 2,
            minHeight: 28,
            py: 0,
            fontWeight: 500,
            transition: "background 0.1s",
          },
          "& .Mui-selected": {
            backgroundColor: `${primaryColor} !important`,
            color: "#fff",
            "&:hover": {
              backgroundColor: primaryColor,
            },
          },
        }}
      >
        {options.map((opt) => (
          <ToggleButton key={opt.value} value={opt.value}>
            {opt.label}
          </ToggleButton>
        ))}
      </ToggleButtonGroup>
    </Paper>
  );
};

export default TabSelector;
