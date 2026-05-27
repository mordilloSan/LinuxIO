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

function parseHexInput(raw: string): string | null {
  const trimmed = raw.trim().replace(/^#/, "");
  if (/^[0-9a-f]{3}$/i.test(trimmed)) {
    const [r, g, b] = trimmed;
    return `#${r}${r}${g}${g}${b}${b}`.toLowerCase();
  }
  if (/^[0-9a-f]{6}$/i.test(trimmed)) {
    return `#${trimmed.toLowerCase()}`;
  }
  return null;
}

interface ColorEntry {
  description: string;
  effectiveColor: string;
  key: keyof ThemeColors;
  label: string;
}

function ThemeColorsSection() {
  const theme = useAppTheme();
  const { config } = useConfig();
  const [themeColors, setThemeColors] = useConfigValue("themeColors");

  const [editMode, setEditMode] = useState<"light" | "dark">(
    config.appSettings.theme === "DARK" ? "dark" : "light",
  );

  const editTheme = useMemo(
    () =>
      buildAppTheme(
        editMode === "dark" ? "DARK" : "LIGHT",
        config.appSettings.primaryColor,
        themeColors,
      ),
    [editMode, config.appSettings.primaryColor, themeColors],
  );

  const entries: ColorEntry[] = useMemo(
    () => [
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
    ],
    [editTheme],
  );

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
        gap: theme.spacing(1.5),
      }}
    >
      <div
        style={{ display: "flex", alignItems: "center", gap: theme.spacing(1) }}
      >
        <div style={{ flexGrow: 1 }}>
          <AppTypography fontWeight={600} variant="body1">
            Colors
          </AppTypography>
          <AppTypography color="text.secondary" variant="caption">
            Customize light and dark theme colors.
          </AppTypography>
        </div>

        <div style={{ display: "flex", gap: 2 }}>
          {(["light", "dark"] as const).map((m) => (
            <button
              key={m}
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
              type="button"
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
              aria-label="Reset all colors to default"
              disabled={!hasAnyOverride}
              onClick={() => setThemeColors(undefined)}
              size="small"
            >
              <Icon height={14} icon="mdi:refresh" width={14} />
            </AppIconButton>
          </span>
        </AppTooltip>
      </div>

      <div
        style={{
          display: "flex",
          flexDirection: "column",
          gap: theme.spacing(1),
        }}
      >
        {entries.map(({ key, label, description, effectiveColor }) => {
          const isOverridden = themeColors?.[editMode]?.[key] != null;
          return (
            <FrostedCard
              hoverLift
              key={key}
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
                <AppTypography fontWeight={600} variant="body2">
                  {label}
                </AppTypography>
                <AppTypography color="text.secondary" variant="caption">
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
                      aria-label={`Reset ${label} to default`}
                      onClick={() => handleReset(key)}
                      size="small"
                    >
                      <Icon height={14} icon="mdi:refresh" width={14} />
                    </AppIconButton>
                  </AppTooltip>
                )}
                <ColorSwatch
                  color={effectiveColor}
                  label={label}
                  onChange={(val) => handleChange(key, val)}
                />
              </div>
            </FrostedCard>
          );
        })}
      </div>
    </div>
  );
}

interface ColorSwatchProps {
  color: string;
  label: string;
  onChange: (value: string) => void;
}

function ColorSwatch({ color, onChange, label }: ColorSwatchProps) {
  const theme = useAppTheme();
  const colorInputRef = useRef<HTMLInputElement>(null);
  const normalized = toInputColor(color);

  const [draft, setDraft] = useState<string | null>(null);
  const [hovered, setHovered] = useState(false);

  const focused = draft !== null;
  const displayValue = focused ? draft : normalized;
  const draftValid = focused ? parseHexInput(draft) != null : true;
  const showAffordance = hovered || focused;

  const commitDraft = () => {
    if (draft == null) return;
    const parsed = parseHexInput(draft);
    if (parsed && parsed !== normalized) {
      onChange(parsed);
    }
    setDraft(null);
  };

  const borderColor = !draftValid
    ? alpha(theme.palette.error.main, 0.6)
    : focused
      ? alpha(theme.palette.primary.main, 0.6)
      : showAffordance
        ? alpha(theme.palette.text.secondary, 0.25)
        : "transparent";

  return (
    <div
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      style={{
        display: "flex",
        alignItems: "center",
        gap: theme.spacing(0.75),
        flexShrink: 0,
      }}
    >
      <input
        aria-label={`Hex color for ${label}`}
        autoComplete="off"
        onBlur={commitDraft}
        onChange={(e) => setDraft(e.target.value)}
        onFocus={(e) => {
          setDraft(normalized);
          e.target.select();
        }}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            (e.target as HTMLInputElement).blur();
          } else if (e.key === "Escape") {
            e.preventDefault();
            setDraft(null);
            (e.target as HTMLInputElement).blur();
          }
        }}
        spellCheck={false}
        style={{
          width: 78,
          padding: "3px 6px",
          fontFamily:
            'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace',
          fontSize: "0.7rem",
          letterSpacing: "0.01em",
          color: focused
            ? theme.palette.text.primary
            : theme.palette.text.secondary,
          background: focused
            ? alpha(theme.palette.text.primary, 0.04)
            : "transparent",
          border: `1px solid ${borderColor}`,
          borderRadius: theme.shape.borderRadius,
          outline: "none",
          textTransform: "lowercase",
          textAlign: "center",
          boxSizing: "border-box",
          transition:
            "border-color 120ms ease, background 120ms ease, color 120ms ease",
        }}
        type="text"
        value={displayValue}
      />
      <div style={{ position: "relative" }}>
        <div
          aria-label={`Pick color for ${label}`}
          onClick={(e) => {
            e.stopPropagation();
            colorInputRef.current?.click();
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter" || e.key === " ") {
              e.preventDefault();
              colorInputRef.current?.click();
            }
          }}
          role="button"
          style={{
            width: 28,
            height: 28,
            borderRadius: theme.shape.borderRadius,
            backgroundColor: normalized,
            border: `1px solid ${alpha(theme.palette.text.secondary, 0.3)}`,
            boxSizing: "border-box",
            cursor: "pointer",
          }}
          tabIndex={0}
        />
        <input
          aria-hidden="true"
          onChange={(e) => onChange(e.target.value)}
          ref={colorInputRef}
          style={{
            position: "fixed",
            left: "50%",
            top: "50%",
            opacity: 0,
            width: 0,
            height: 0,
            pointerEvents: "none",
          }}
          type="color"
          value={normalized}
        />
      </div>
    </div>
  );
}

export default ThemeColorsSection;
