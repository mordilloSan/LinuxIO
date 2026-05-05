import { Icon } from "@iconify/react";
import { useMemo, useRef, useState } from "react";

import FrostedCard from "@/components/cards/FrostedCard";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { useConfig, useConfigValue } from "@/hooks/useConfig";
import { buildAppTheme, useAppTheme } from "@/theme";
import { ThemeColors, ThemeColorsByMode } from "@/types/config";
import { alpha } from "@/utils/color";

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
  const theme = useAppTheme();
  const { config } = useConfig();
  const [themeColors, setThemeColors] = useConfigValue("themeColors");

  const [editMode, setEditMode] = useState<"light" | "dark">(
    config.theme === "DARK" ? "dark" : "light",
  );

  const editTheme = useMemo(
    () =>
      buildAppTheme(
        editMode === "dark" ? "DARK" : "LIGHT",
        config.primaryColor,
        themeColors,
      ),
    [editMode, config.primaryColor, themeColors],
  );

  const entries: ColorEntry[] = [
    {
      key: "backgroundDefault",
      label: "Background",
      description: "Main page background",
      effectiveColor: editTheme.palette.background.default,
    },
    {
      key: "backgroundPaper",
      label: "Surface",
      description: "Dialogs and panels",
      effectiveColor: editTheme.palette.background.paper,
    },
    {
      key: "headerBackground",
      label: "Header",
      description: "Top navigation bar",
      effectiveColor: editTheme.header.background,
    },
    {
      key: "footerBackground",
      label: "Footer",
      description: "Bottom status bar",
      effectiveColor: editTheme.footer.background,
    },
    {
      key: "sidebarBackground",
      label: "Sidebar",
      description: "Navigation sidebar",
      effectiveColor: editTheme.sidebar.background,
    },
    {
      key: "cardBackground",
      label: "Card",
      description: "Frosted glass card tint",
      effectiveColor: editTheme.card.background,
    },
    {
      key: "dialogBorder",
      label: "Dialog border",
      description: "Modal border glow color",
      effectiveColor: editTheme.dialog.border,
    },
    {
      key: "dialogGlow",
      label: "Dialog glow",
      description: "Modal glow highlight color",
      effectiveColor: editTheme.dialog.glow,
    },
    {
      key: "dialogBackdrop",
      label: "Dialog backdrop",
      description: "Modal backdrop tint",
      effectiveColor: editTheme.dialog.backdrop,
    },
    {
      key: "codeBackground",
      label: "Code background",
      description: "Logs, terminal output, and code blocks",
      effectiveColor: editTheme.codeBlock.background,
    },
    {
      key: "codeText",
      label: "Code text",
      description: "Logs, terminal output, and code text",
      effectiveColor: editTheme.codeBlock.color,
    },
    {
      key: "chartRx",
      label: "Chart RX",
      description: "Receive traffic chart color",
      effectiveColor: editTheme.chart.rx,
    },
    {
      key: "chartTx",
      label: "Chart TX",
      description: "Transmit traffic chart color",
      effectiveColor: editTheme.chart.tx,
    },
    {
      key: "chartNeutral",
      label: "Chart neutral",
      description: "Chart gridlines and neutral graph accents",
      effectiveColor: editTheme.chart.neutral,
    },
    {
      key: "fileBrowserSurface",
      label: "File surface",
      description: "File cards and list surfaces",
      effectiveColor: editTheme.fileBrowser.surface,
    },
    {
      key: "fileBrowserChrome",
      label: "File chrome",
      description: "Quick actions and filebrowser chrome",
      effectiveColor: editTheme.fileBrowser.chrome,
    },
    {
      key: "fileBrowserBreadcrumbBackground",
      label: "Breadcrumb bg",
      description: "Filebrowser breadcrumb background",
      effectiveColor: editTheme.fileBrowser.breadcrumbBackground,
    },
    {
      key: "fileBrowserBreadcrumbText",
      label: "Breadcrumb text",
      description: "Filebrowser breadcrumb text",
      effectiveColor: editTheme.fileBrowser.breadcrumbText,
    },
  ];

  const handleChange = (key: keyof ThemeColors, value: string) => {
    setThemeColors((prev) => ({
      ...prev,
      [editMode]: { ...(prev?.[editMode] ?? {}), [key]: value },
    }));
  };

  const handleReset = (key: keyof ThemeColors) => {
    setThemeColors((prev) => {
      const modeColors = prev?.[editMode];
      if (!modeColors) return prev;
      // eslint-disable-next-line @typescript-eslint/no-unused-vars
      const { [key]: _, ...rest } = modeColors;
      const newModeColors = Object.values(rest).some((v) => v != null)
        ? (rest as ThemeColors)
        : undefined;
      const next: ThemeColorsByMode = { ...prev, [editMode]: newModeColors };
      if (!next.light && !next.dark) return undefined;
      return next;
    });
  };

  const hasAnyOverride =
    themeColors != null &&
    (Object.values(themeColors.light ?? {}).some((v) => v != null) ||
      Object.values(themeColors.dark ?? {}).some((v) => v != null));

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: theme.spacing(1),
      }}
    >
      <div
        style={{ display: "flex", alignItems: "center", gap: theme.spacing(1) }}
      >
        <AppTypography variant="body1" fontWeight={600} style={{ flexGrow: 1 }}>
          Colors
        </AppTypography>

        <div style={{ display: "flex", gap: 2 }}>
          {(["light", "dark"] as const).map((m) => (
            <button
              key={m}
              type="button"
              onClick={() => setEditMode(m)}
              style={{
                padding: "2px 10px",
                borderRadius: 999,
                border: "none",
                background:
                  editMode === m ? theme.palette.primary.main : "transparent",
                color:
                  editMode === m
                    ? theme.palette.primary.contrastText
                    : theme.palette.text.secondary,
                cursor: "pointer",
                fontSize: "0.75rem",
                fontWeight: 500,
                fontFamily: "inherit",
                transition: "background 120ms ease, color 120ms ease",
              }}
            >
              {m === "light" ? "Light" : "Dark"}
            </button>
          ))}
        </div>

        <AppTooltip
          title={
            hasAnyOverride
              ? "Reset all colors to default"
              : "No color overrides active"
          }
        >
          <span>
            <AppIconButton
              size="small"
              onClick={() => setThemeColors(undefined)}
              disabled={!hasAnyOverride}
              aria-label="Reset all colors to default"
            >
              <Icon icon="mdi:refresh" width={14} height={14} />
            </AppIconButton>
          </span>
        </AppTooltip>
      </div>

      {entries.map(({ key, label, description, effectiveColor }) => {
        const isOverridden = themeColors?.[editMode]?.[key] != null;
        return (
          <FrostedCard
            key={key}
            hoverLift
            onClick={(e) => {
              const target = e.target as HTMLElement;
              if (target.closest("button, input")) return;
              (e.currentTarget as HTMLElement)
                .querySelector<HTMLInputElement>('input[type="color"]')
                ?.click();
            }}
            style={{
              cursor: "pointer",
              display: "flex",
              alignItems: "center",
              justifyContent: "space-between",
              padding: theme.spacing(1.5),
              ...(isOverridden && {
                border: `1px solid ${theme.palette.primary.main}`,
              }),
            }}
          >
            <div>
              <AppTypography variant="body2" fontWeight={600}>
                {label}
              </AppTypography>
              <AppTypography variant="caption" color="text.secondary">
                {description}
              </AppTypography>
            </div>

            <div
              style={{
                display: "flex",
                alignItems: "center",
                gap: theme.spacing(0.5),
              }}
            >
              {isOverridden && (
                <AppTooltip title="Reset to default">
                  <AppIconButton
                    size="small"
                    onClick={() => handleReset(key)}
                    aria-label={`Reset ${label} to default`}
                  >
                    <Icon icon="mdi:refresh" width={14} height={14} />
                  </AppIconButton>
                </AppTooltip>
              )}
              <ColorSwatch
                color={effectiveColor}
                onChange={(val) => handleChange(key, val)}
                label={label}
              />
            </div>
          </FrostedCard>
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
  const theme = useAppTheme();
  const inputRef = useRef<HTMLInputElement>(null);
  const normalized = toInputColor(color);

  return (
    <div style={{ position: "relative", flexShrink: 0 }}>
      <div
        aria-label={`Current color for ${label}`}
        style={{
          width: 28,
          height: 28,
          borderRadius: theme.shape.borderRadius,
          backgroundColor: normalized,
          border: `1px solid ${alpha(theme.palette.text.secondary, 0.3)}`,
          boxSizing: "border-box",
        }}
      />
      <input
        ref={inputRef}
        type="color"
        value={normalized}
        onChange={(e) => onChange(e.target.value)}
        style={{
          position: "fixed",
          left: "50%",
          top: "50%",
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
