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
        width: "100%",
        minWidth: 0,
      }}
    >
      <Paper
        elevation={0}
        className="custom-scrollbar"
        sx={{
          display: "flex",
          p: 0.5,
          flex: "1 1 auto",
          minWidth: 0,
          borderRadius: "999px",
          backgroundColor: "transparent",
          backdropFilter: "none",
          overflowX: "auto",
          overflowY: "hidden",
        }}
      >
        <ToggleButtonGroup
          value={value}
          exclusive
          onChange={(_, newValue) => newValue && onChange(newValue)}
          size="small"
          sx={{
            flexWrap: "nowrap",
            "& .MuiToggleButton-root": {
              color: "text.secondary",
              border: "none",
              borderRadius: "999px",
              px: 2,
              minHeight: 28,
              py: 0,
              fontWeight: 500,
              transition: "background 0.1s",
              whiteSpace: "nowrap",
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
        <Box sx={{ display: "flex", alignItems: "center", gap: 1, flexShrink: 0 }}>
          {rightContent}
        </Box>
      )}
    </Box>
  );
};

export default TabSelector;
