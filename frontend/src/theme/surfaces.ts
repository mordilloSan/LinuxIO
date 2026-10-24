import type { AppTheme } from "@/theme/appTheme";
import { alpha } from "@/theme/colorUtils";

export const getFrostedCardShadow = (theme: AppTheme) =>
  `0 16px 40px -28px ${alpha(theme.palette.common.black, 0.6)}`;

/* Surface chrome that depends on the theme, resolved once per theme into CSS
   variables by AppThemeProvider. FrostedCard.css, the dialog paper below and
   the file browser's surface helpers consume the variables, so no component
   reads the theme for its surface. */
export const getSurfaceCssVariables = (
  theme: AppTheme,
): Record<string, string> => {
  const dark = theme.palette.mode === "dark";
  const white = theme.palette.common.white;
  const paper = theme.palette.background.paper;
  return {
    "--app-card-background": alpha(theme.card.background, dark ? 0.6 : 0.82),
    "--app-card-gradient": dark
      ? `linear-gradient(180deg, ${alpha(white, 0.18)} 0%, ${alpha(white, 0.14)} 18%, ${alpha(white, 0.11)} 38%, ${alpha(white, 0.09)} 62%, ${alpha(white, 0.08)} 100%)`
      : `linear-gradient(180deg, ${alpha(paper, 0.72)} 0%, ${alpha(paper, 0.82)} 18%, ${alpha(paper, 0.9)} 40%, ${alpha(paper, 0.95)} 70%, ${alpha(paper, 0.98)} 100%)`,
    "--app-card-blur": dark ? "blur(20px)" : "blur(16px)",
    "--app-card-shadow": getFrostedCardShadow(theme),
    "--app-card-lift-shadow": `0 8px 24px ${alpha(theme.palette.common.black, 0.35)}`,
    "--app-dialog-surface-border": alpha(theme.dialog.border, 0.2),
    "--app-dialog-surface-shadow": `0 0 10px ${alpha(theme.dialog.glow, 0.5)}, 0 0 20px ${alpha(theme.dialog.glow, 0.3)}, inset 0 0 20px ${alpha(theme.dialog.glow, 0.1)}`,
    "--app-dialog-backdrop": theme.dialog.backdrop,
    // Read by the file browser's getSubtleDividerColor.
    "--app-divider-subtle": alpha(theme.palette.divider, dark ? 0.15 : 0.1),
    /* Read by the file browser's getChromeSurfaceColor. Dark's default matches
       the gallery-size chip's hand-tuned color-mix (transparent 67%); these
       are the transparent shares a chrome surface mixes toward, at rest and
       hovered. */
    "--app-chrome-surface-transparent": dark ? "67%" : "92%",
    "--app-chrome-surface-transparent-hover": dark ? "72%" : "86%",
  };
};

/* The dialog paper: a paper surface ringed by the theme's dialog glow. Shared
   with the settings sheet, which is a route rather than an overlay but wears
   the same chrome, so the two cannot drift apart. */
export const getDialogSurfaceStyles = () => ({
  backgroundColor: "var(--app-palette-background-paper)",
  borderRadius: "var(--app-radius-card)",
  border: "1px solid var(--app-dialog-surface-border)",
  boxShadow: "var(--app-dialog-surface-shadow)",
});
