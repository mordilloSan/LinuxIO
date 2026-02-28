import { Box, IconButton, Tooltip, Typography, useTheme } from "@mui/material";
import RotateCcw from "lucide-react/dist/esm/icons/rotate-ccw";
import { useRef } from "react";

import { useConfigValue } from "@/hooks/useConfig";
import { ThemeColors } from "@/types/config";

// Expand shorthand hex so native <input type="color"> always gets #rrggbb
function toInputColor(color: string): string {
  const short = /^#([0-9a-f])([0-9a-f])([0-9a-f])$/i.exec(color);
  if (short) {
    return `#${short[1]}${short[1]}${short[2]}${short[2]}${short[3]}${short[3]}`;
  }
  return color;
}

interface ColorEntry {
  key: keyof ThemeColors;
  label: string;
  description: string;
  effectiveColor: string;
}

function ThemeColorsSection() {
  const theme = useTheme();
  const [themeColors, setThemeColors] = useConfigValue("themeColors");

  const entries: ColorEntry[] = [
    {
      key: "backgroundDefault",
      label: "Background",
      description: "Main page background",
      effectiveColor: theme.palette.background.default,
    },
    {
      key: "backgroundPaper",
      label: "Surface",
      description: "Dialogs and panels",
      effectiveColor: theme.palette.background.paper,
    },
    {
      key: "headerBackground",
      label: "Header",
      description: "Top navigation bar",
      effectiveColor: theme.header.background,
    },
    {
      key: "footerBackground",
      label: "Footer",
      description: "Bottom status bar",
      effectiveColor: theme.footer.background,
    },
    {
      key: "sidebarBackground",
      label: "Sidebar",
      description: "Navigation sidebar",
      effectiveColor: theme.sidebar.background,
    },
    {
      key: "cardBackground",
      label: "Card",
      description: "Frosted glass card tint",
      effectiveColor: theme.card.background,
    },
  ];

  const handleChange = (key: keyof ThemeColors, value: string) => {
    setThemeColors((prev) => ({ ...(prev ?? {}), [key]: value }));
  };

  const handleReset = (key: keyof ThemeColors) => {
    setThemeColors((prev) => {
      if (!prev) return prev;
      const { [key]: _, ...rest } = prev;
      return Object.values(rest).some((v) => v != null)
        ? (rest as ThemeColors)
        : undefined;
    });
  };

  const hasAnyOverride =
    themeColors != null && Object.values(themeColors).some((v) => v != null);

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
      <Box sx={{ display: "flex", alignItems: "center" }}>
        <Typography variant="body1" fontWeight={600} sx={{ flexGrow: 1 }}>
          Colors
        </Typography>
        <Tooltip title={hasAnyOverride ? "Reset all colors to default" : "No color overrides active"}>
          <span>
            <IconButton
              size="small"
              onClick={() => setThemeColors(undefined)}
              disabled={!hasAnyOverride}
              aria-label="Reset all colors to default"
            >
              <RotateCcw size={14} />
            </IconButton>
          </span>
        </Tooltip>
      </Box>

      {entries.map(({ key, label, description, effectiveColor }) => {
        const isOverridden = themeColors?.[key] != null;
        return (
          <Box
            key={key}
            sx={{
              display: "flex",
              alignItems: "center",
              justifyContent: "space-between",
              p: 1.5,
              borderRadius: 1.5,
              border: `1px solid ${isOverridden ? theme.palette.primary.main : theme.palette.divider}`,
            }}
          >
            <Box>
              <Typography variant="body2" fontWeight={600}>
                {label}
              </Typography>
              <Typography variant="caption" color="text.secondary">
                {description}
              </Typography>
            </Box>

            <Box sx={{ display: "flex", alignItems: "center", gap: 0.5 }}>
              {isOverridden && (
                <Tooltip title="Reset to default">
                  <IconButton
                    size="small"
                    onClick={() => handleReset(key)}
                    aria-label={`Reset ${label} to default`}
                  >
                    <RotateCcw size={14} />
                  </IconButton>
                </Tooltip>
              )}
              <ColorSwatch
                color={effectiveColor}
                onChange={(val) => handleChange(key, val)}
                label={label}
              />
            </Box>
          </Box>
        );
      })}
    </Box>
  );
}

interface ColorSwatchProps {
  color: string;
  onChange: (value: string) => void;
  label: string;
}

function ColorSwatch({ color, onChange, label }: ColorSwatchProps) {
  const inputRef = useRef<HTMLInputElement>(null);
  const normalized = toInputColor(color);

  return (
    <Box sx={{ position: "relative", flexShrink: 0 }}>
      <Box
        onClick={() => inputRef.current?.click()}
        role="button"
        tabIndex={0}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") inputRef.current?.click();
        }}
        aria-label={`Pick color for ${label}`}
        sx={{
          width: 28,
          height: 28,
          borderRadius: 1,
          bgcolor: normalized,
          cursor: "pointer",
          border: "1px solid rgba(128,128,128,0.3)",
        }}
      />
      <input
        ref={inputRef}
        type="color"
        value={normalized}
        onChange={(e) => onChange(e.target.value)}
        style={{
          position: "absolute",
          opacity: 0,
          width: 0,
          height: 0,
          pointerEvents: "none",
        }}
        aria-hidden="true"
      />
    </Box>
  );
}

export default ThemeColorsSection;
