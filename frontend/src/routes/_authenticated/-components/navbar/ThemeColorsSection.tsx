import { Icon } from "@iconify/react";
import { useEffect, useMemo, useRef, useState } from "react";

import type {
  ConfigThemeColorsByModePayload as ThemeColorsByMode,
  ConfigThemeColorsPayload as ThemeColors,
} from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import AppButton from "@/components/ui/AppButton";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { useConfigValue } from "@/hooks/useConfig";
import { buildAppTheme, useAppTheme } from "@/theme";
import { alpha } from "@/utils/color";

// Trailing debounce for saves driven by the native colour picker. Long enough
// to coalesce a drag into a few RPCs, short enough that the app previews the
// colour (via the optimistic config cache) while the picker is still open.
const PICKER_SAVE_DEBOUNCE_MS = 250;

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
  const [themeMode] = useConfigValue("theme");
  const [primaryColor] = useConfigValue("primaryColor");
  const [themeColors, setThemeColors] = useConfigValue("themeColors");

  const [editMode, setEditMode] = useState<"light" | "dark">(
    themeMode === "DARK" ? "dark" : "light",
  );

  const editTheme = useMemo(
    () =>
      buildAppTheme(
        editMode === "dark" ? "DARK" : "LIGHT",
        primaryColor,
        themeColors,
      ),
    [editMode, primaryColor, themeColors],
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
      const rest = { ...modeColors };
      delete rest[key];
      const newModeColors = Object.values(rest).some((v) => v != null)
        ? rest
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
        gap: "var(--app-space-6)",
      }}
    >
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: "var(--app-space-4)",
        }}
      >
        <div style={{ flexGrow: 1 }}>
          <AppTypography fontWeight={600} variant="body1">
            Colors
          </AppTypography>
          <AppTypography color="text.secondary" variant="caption">
            Each mode carries its own palette. These swatches edit the mode
            selected here, which is not necessarily the one on screen — switch
            that under General.
          </AppTypography>
        </div>

        <AppTypography color="text.secondary" variant="caption">
          Editing
        </AppTypography>

        <div style={{ display: "flex", gap: 2 }}>
          {(["light", "dark"] as const).map((m) => (
            <AppButton
              aria-label={`Edit ${m} theme colors`}
              color={editMode === m ? "primary" : "inherit"}
              key={m}
              onClick={() => setEditMode(m)}
              size="small"
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
                lineHeight: "normal",
                minWidth: 0,
                transition: "background-color 120ms ease, color 120ms ease",
              }}
            >
              {m === "light" ? "Light" : "Dark"}
            </AppButton>
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
          gap: "var(--app-space-4)",
        }}
      >
        {entries.map(({ key, label, description, effectiveColor }) => {
          const isOverridden = themeColors?.[editMode]?.[key] != null;
          return (
            <FrostedCard
              hoverLift
              key={key}
              style={{
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
                padding: "var(--app-space-6)",
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
                  gap: "var(--app-space-2)",
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
  // The picker's live value while a save is pending, so the swatch and the
  // controlled colour input track the pointer between debounced saves.
  const [previewOverride, setPreviewOverride] = useState<string | null>(null);
  const previewColor = previewOverride ?? normalized;

  // The native colour picker fires per pointer movement (Chromium fires
  // "change" per tick as well, so there is no reliable commit event). Every
  // tick reschedules one trailing save; the last tick is always followed by
  // silence, so the final pick lands within PICKER_SAVE_DEBOUNCE_MS. The
  // pending save carries its own onChange so an unmount can flush it.
  const pendingSaveRef = useRef<{
    timer: number;
    value: string;
    save: (value: string) => void;
  } | null>(null);
  const schedulePickerSave = (value: string) => {
    if (pendingSaveRef.current) {
      window.clearTimeout(pendingSaveRef.current.timer);
    }
    const timer = window.setTimeout(() => {
      pendingSaveRef.current = null;
      setPreviewOverride(null);
      onChange(value);
    }, PICKER_SAVE_DEBOUNCE_MS);
    pendingSaveRef.current = { timer, value, save: onChange };
  };
  useEffect(
    () => () => {
      const pending = pendingSaveRef.current;
      if (!pending) return;
      pendingSaveRef.current = null;
      window.clearTimeout(pending.timer);
      pending.save(pending.value);
    },
    [],
  );

  const focused = draft !== null;
  const displayValue = focused ? draft : previewColor;
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
        gap: "var(--app-space-4)",
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
          fontFamily: "var(--app-font-mono)",
          fontSize: "0.7rem",
          letterSpacing: "0.01em",
          color: focused
            ? theme.palette.text.primary
            : theme.palette.text.secondary,
          background: focused
            ? alpha(theme.palette.text.primary, 0.04)
            : "transparent",
          border: `1px solid ${borderColor}`,
          borderRadius: "var(--app-radius-base)",
          outline: "none",
          textTransform: "lowercase",
          textAlign: "center",
          boxSizing: "border-box",
          transition:
            "border-color 120ms ease, background-color 120ms ease, color 120ms ease",
        }}
        type="text"
        value={displayValue}
      />
      <div style={{ position: "relative" }}>
        <AppButton
          aria-label={`Pick color for ${label}`}
          color="inherit"
          onClick={(e) => {
            e.stopPropagation();
            colorInputRef.current?.click();
          }}
          style={{
            width: 28,
            height: 28,
            minWidth: 28,
            padding: 0,
            borderRadius: "var(--app-radius-base)",
            backgroundColor: previewColor,
            border: `1px solid ${alpha(theme.palette.text.secondary, 0.3)}`,
            boxSizing: "border-box",
            cursor: "pointer",
          }}
        />
        <input
          aria-hidden="true"
          onChange={(e) => {
            setPreviewOverride(e.target.value);
            schedulePickerSave(e.target.value);
          }}
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
          tabIndex={-1}
          type="color"
          value={previewColor}
        />
      </div>
    </div>
  );
}

export default ThemeColorsSection;
