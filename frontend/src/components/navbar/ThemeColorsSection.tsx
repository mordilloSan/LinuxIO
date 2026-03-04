import { IconButton, Tooltip, Typography, useTheme } from "@mui/material";
import { alpha } from "@mui/material/styles";
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
  const baseBorderRadius = parseFloat(String(theme.shape.borderRadius)) || 0;
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
    {
      key: "dialogBorder",
      label: "Dialog border",
      description: "Modal border glow color",
      effectiveColor: theme.dialog.border,
    },
    {
      key: "dialogGlow",
      label: "Dialog glow",
      description: "Modal glow highlight color",
      effectiveColor: theme.dialog.glow,
    },
    {
      key: "dialogBackdrop",
      label: "Dialog backdrop",
      description: "Modal backdrop tint",
      effectiveColor: theme.dialog.backdrop,
    },
    {
      key: "codeBackground",
      label: "Code background",
      description: "Logs, terminal output, and code blocks",
      effectiveColor: theme.codeBlock.background,
    },
    {
      key: "codeText",
      label: "Code text",
      description: "Logs, terminal output, and code text",
      effectiveColor: theme.codeBlock.color,
    },
    {
      key: "chartRx",
      label: "Chart RX",
      description: "Receive traffic chart color",
      effectiveColor: theme.chart.rx,
    },
    {
      key: "chartTx",
      label: "Chart TX",
      description: "Transmit traffic chart color",
      effectiveColor: theme.chart.tx,
    },
    {
      key: "chartNeutral",
      label: "Chart neutral",
      description: "Chart gridlines and neutral graph accents",
      effectiveColor: theme.chart.neutral,
    },
    {
      key: "fileBrowserSurface",
      label: "File surface",
      description: "File cards and list surfaces",
      effectiveColor: theme.fileBrowser.surface,
    },
    {
      key: "fileBrowserChrome",
      label: "File chrome",
      description: "Quick actions and filebrowser chrome",
      effectiveColor: theme.fileBrowser.chrome,
    },
    {
      key: "fileBrowserBreadcrumbBackground",
      label: "Breadcrumb bg",
      description: "Filebrowser breadcrumb background",
      effectiveColor: theme.fileBrowser.breadcrumbBackground,
    },
    {
      key: "fileBrowserBreadcrumbText",
      label: "Breadcrumb text",
      description: "Filebrowser breadcrumb text",
      effectiveColor: theme.fileBrowser.breadcrumbText,
    },
  ];

  const handleChange = (key: keyof ThemeColors, value: string) => {
    setThemeColors((prev) => ({ ...(prev ?? {}), [key]: value }));
  };

  const handleReset = (key: keyof ThemeColors) => {
    setThemeColors((prev) => {
      if (!prev) return prev;
      // eslint-disable-next-line @typescript-eslint/no-unused-vars
      const { [key]: _, ...rest } = prev;
      return Object.values(rest).some((v) => v != null)
        ? (rest as ThemeColors)
        : undefined;
    });
  };

  const hasAnyOverride =
    themeColors != null && Object.values(themeColors).some((v) => v != null);

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: theme.spacing(1),
      }}
    >
      <div style={{ display: "flex", alignItems: "center" }}>
        <Typography variant="body1" fontWeight={600} sx={{ flexGrow: 1 }}>
          Colors
        </Typography>
        <Tooltip
          title={
            hasAnyOverride
              ? "Reset all colors to default"
              : "No color overrides active"
          }
        >
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
      </div>

      {entries.map(({ key, label, description, effectiveColor }) => {
        const isOverridden = themeColors?.[key] != null;
        return (
          <div
            key={key}
            style={{
              display: "flex",
              alignItems: "center",
              justifyContent: "space-between",
              padding: theme.spacing(1.5),
              borderRadius: `${baseBorderRadius * 1.5}px`,
              border: `1px solid ${isOverridden ? theme.palette.primary.main : theme.palette.divider}`,
            }}
          >
            <div>
              <Typography variant="body2" fontWeight={600}>
                {label}
              </Typography>
              <Typography variant="caption" color="text.secondary">
                {description}
              </Typography>
            </div>

            <div
              style={{
                display: "flex",
                alignItems: "center",
                gap: theme.spacing(0.5),
              }}
            >
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
            </div>
          </div>
        );
      })}
    </div>
  );
}

interface ColorSwatchProps {
  color: string;
  onChange: (value: string) => void;
  label: string;
}

function ColorSwatch({ color, onChange, label }: ColorSwatchProps) {
  const theme = useTheme();
  const inputRef = useRef<HTMLInputElement>(null);
  const normalized = toInputColor(color);

  return (
    <div
      style={{
        position: "relative",
        flexShrink: 0,
        display: "flex",
        alignItems: "center",
      }}
    >
      <button
        type="button"
        onClick={() => inputRef.current?.click()}
        aria-label={`Pick color for ${label}`}
        style={{
          display: "block",
          width: 28,
          height: 28,
          borderRadius: theme.shape.borderRadius,
          backgroundColor: normalized,
          cursor: "pointer",
          padding: 0,
          margin: 0,
          lineHeight: 0,
          border: `1px solid ${alpha(theme.palette.text.secondary, 0.3)}`,
          appearance: "none",
          boxSizing: "border-box",
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
    </div>
  );
}

export default ThemeColorsSection;
