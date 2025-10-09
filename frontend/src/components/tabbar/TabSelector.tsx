import { Paper, ToggleButton, ToggleButtonGroup, Box } from "@mui/material";
import { useTheme } from "@mui/material/styles";
import React from "react";

type TabOption = { value: string; label: string };
interface TabSelectorProps {
  value: string;
  onChange: (value: string) => void;
  options: TabOption[];
  rightContent?: React.ReactNode;
}

const TabSelector: React.FC<TabSelectorProps> = ({
  value,
  onChange,
  options,
  rightContent,
}) => {
  const theme = useTheme();

  const primaryHex = theme.palette.primary.main;
  const contrast = theme.palette.getContrastText(primaryHex);

  return (
    <Box
      sx={{
        display: "flex",
        alignItems: "center",
        justifyContent: "space-between",
        gap: 2,
        mb: 2,
      }}
    >
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
            "& .MuiToggleButton-root.Mui-selected": {
              backgroundColor: primaryHex,
              color: contrast,
              "&:hover": { backgroundColor: primaryHex },
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

      {rightContent && (
        <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
          {rightContent}
        </Box>
      )}
    </Box>
  );
};

export default TabSelector;
