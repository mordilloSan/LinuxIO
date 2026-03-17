import { Icon } from "@iconify/react";
import {
  Paper,
  ToggleButton,
  ToggleButtonGroup,
  Popover,
  useMediaQuery,
} from "@mui/material";
import AppIconButton from "@/components/ui/AppIconButton";
import { useTheme } from "@mui/material/styles";
import React from "react";

interface TabOption {
  value: string;
  label: string;
}
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
  const isMobile = useMediaQuery(theme.breakpoints.down("sm"));
  const [anchorEl, setAnchorEl] = React.useState<HTMLButtonElement | null>(
    null,
  );

  const primaryHex = theme.palette.primary.main;
  const contrast = theme.palette.getContrastText(primaryHex);

  return (
    <div
      style={{
        display: "flex",
        alignItems: "flex-start",
        justifyContent: "space-between",
        gap: 8,
        marginBottom: 8,
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
        <>
          {isMobile ? (
            <>
              <AppIconButton
                size="small"
                onClick={(e) => setAnchorEl(e.currentTarget)}
                style={{ marginTop: 2, flexShrink: 0 }}
              >
                <Icon icon="mdi:tune" width={20} height={20} />
              </AppIconButton>
              <Popover
                open={Boolean(anchorEl)}
                anchorEl={anchorEl}
                onClose={() => setAnchorEl(null)}
                anchorOrigin={{ vertical: "bottom", horizontal: "right" }}
                transformOrigin={{ vertical: "top", horizontal: "right" }}
              >
                <div
                  style={{
                    padding: 6,
                    display: "flex",
                    flexDirection: "column",
                    gap: 4,
                  }}
                >
                  {rightContent}
                </div>
              </Popover>
            </>
          ) : (
            <div
              style={{
                display: "flex",
                alignItems: "center",
                alignSelf: "flex-start",
                marginTop: 2,
                gap: 4,
                flexShrink: 0,
              }}
            >
              {rightContent}
            </div>
          )}
        </>
      )}
    </div>
  );
};

export default TabSelector;
