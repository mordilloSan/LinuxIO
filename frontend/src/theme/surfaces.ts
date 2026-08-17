import type { AppTheme } from "@/theme";
import { alpha } from "@/utils/color";

// Unlike alpha() in utils/color.ts, this goes through color-mix so var(...)
// and other unparseable inputs still get a real mix instead of passing
// through opaque. Exported so other call sites that need var-safe
// transparency mixing aren't forced back onto alpha()'s footgun.
export const mixWithTransparency = (color: string, opacity: number) => {
  const transparentShare = `${Math.round((1 - opacity) * 100)}%`;
  return `color-mix(in srgb, ${color}, transparent ${transparentShare})`;
};

export const getFrostedCardShadow = (theme: AppTheme) =>
  `0 16px 40px -28px ${alpha(theme.palette.common.black, 0.6)}`;

export const getFrostedCardLiftShadow = (theme: AppTheme) =>
  `0 8px 24px ${alpha(theme.palette.common.black, 0.35)}`;

export const getFrostedCardStyles = (theme: AppTheme) => ({
  backgroundColor: alpha(
    theme.card.background,
    theme.palette.mode === "dark" ? 0.6 : 0.82,
  ),
  backgroundImage:
    theme.palette.mode === "dark"
      ? `linear-gradient(180deg, ${alpha(theme.palette.common.white, 0.18)} 0%, ${alpha(theme.palette.common.white, 0.14)} 18%, ${alpha(theme.palette.common.white, 0.11)} 38%, ${alpha(theme.palette.common.white, 0.09)} 62%, ${alpha(theme.palette.common.white, 0.08)} 100%)`
      : `linear-gradient(180deg, ${alpha(theme.palette.background.paper, 0.72)} 0%, ${alpha(theme.palette.background.paper, 0.82)} 18%, ${alpha(theme.palette.background.paper, 0.9)} 40%, ${alpha(theme.palette.background.paper, 0.95)} 70%, ${alpha(theme.palette.background.paper, 0.98)} 100%)`,
  border: "1px solid transparent",
  backdropFilter: theme.palette.mode === "dark" ? "blur(20px)" : "blur(16px)",
  boxShadow: getFrostedCardShadow(theme),
});

/* The dialog paper: a paper surface ringed by the theme's dialog glow. Shared
   with the settings sheet, which is a route rather than an overlay but wears
   the same chrome, so the two cannot drift apart. */
export const getDialogSurfaceStyles = (theme: AppTheme) => ({
  backgroundColor: theme.palette.background.paper,
  borderRadius: 16,
  border: `1px solid ${alpha(theme.dialog.border, 0.2)}`,
  boxShadow: `0 0 10px ${alpha(theme.dialog.glow, 0.5)}, 0 0 20px ${alpha(theme.dialog.glow, 0.3)}, inset 0 0 20px ${alpha(theme.dialog.glow, 0.1)}`,
});

/* The accent line along the bottom of a card. Its width is fixed: the line is
   the hold-to-reorder indicator, and that gesture is expressed purely as the
   line brightening (see .sc-hold in FrostedCard.css). Animating the width too
   moves the card's own box mid-gesture and needs margin compensation to hide
   it, which was tried and dropped. */
export const getAccentCardStyles = (accentColor: string) => ({
  borderBottomWidth: "3px",
  borderBottomStyle: "solid" as const,
  borderBottomColor: mixWithTransparency(accentColor, 0.3),
});

/* Dark's default matches the filebrowser gallery-size chip's hand-tuned
   color-mix (transparent 67%) — the last other caller of this helper was
   deleted, so this call site is what the table is now calibrated against. */
const CHROME_SURFACE_OPACITY = {
  dark: { default: 0.33, hover: 0.28 },
  light: { default: 0.08, hover: 0.14 },
} as const;

export const getChromeSurfaceColor = (
  theme: AppTheme,
  chromeColor: string,
  emphasis: "default" | "hover" = "default",
) => {
  const mode = theme.palette.mode === "dark" ? "dark" : "light";
  const opacity = CHROME_SURFACE_OPACITY[mode][emphasis];

  return mixWithTransparency(chromeColor, opacity);
};

export const getSubtleDividerColor = (theme: AppTheme) =>
  alpha(theme.palette.divider, theme.palette.mode === "dark" ? 0.15 : 0.1);

/* Shared by FileCard and FileListRow: a listing entry's resting background,
   keyed off selection and hidden-file dimming. Selected wins over hidden so a
   selected dotfile still reads as selected. */
export const getFileEntryBackground = (
  theme: AppTheme,
  { hidden, selected }: { hidden?: boolean; selected?: boolean },
) => {
  if (selected) {
    return mixWithTransparency("var(--app-palette-primary-main)", 0.4);
  }
  if (hidden) {
    return mixWithTransparency(theme.fileBrowser.surface, 0.5);
  }
  return theme.fileBrowser.surface;
};

/* Brightening the entry's own resting background toward the theme's text
   color — rather than painting a fixed hover color over it — keeps the
   selected and hidden variants distinguishable while hovered. */
export const getFileEntryHoverBackground = (baseBackground: string) =>
  `color-mix(in srgb, ${baseBackground}, var(--app-palette-text-primary) 7%)`;
