import type { AppTheme } from "@/theme";
import { alpha } from "@/utils/color";

const mixWithTransparency = (color: string, opacity: number) => {
  const transparentShare = `${Math.round((1 - opacity) * 100)}%`;
  return `color-mix(in srgb, ${color}, transparent ${transparentShare})`;
};

export const getFrostedCardShadow = (theme: AppTheme) =>
  `0 16px 40px -28px ${alpha(theme.palette.common.black, 0.6)}`;

export const getFrostedCardLiftShadow = (theme: AppTheme) =>
  `0 8px 24px ${alpha(theme.palette.common.black, 0.35)}`;

export const getFrostedCardLiftStyles = (theme: AppTheme) => ({
  transform: "translateY(-4px)",
  boxShadow: getFrostedCardLiftShadow(theme),
});

export const getFrostedCardStyles = (theme: AppTheme) => ({
  backgroundColor: alpha(
    theme.card.background,
    theme.palette.mode === "dark" ? 0.6 : 0.82,
  ),
  backgroundImage:
    theme.palette.mode === "dark"
      ? `linear-gradient(180deg, ${alpha(theme.palette.common.white, 0.18)} 0%, ${alpha(theme.palette.common.white, 0.14)} 18%, ${alpha(theme.palette.common.white, 0.11)} 38%, ${alpha(theme.palette.common.white, 0.09)} 62%, ${alpha(theme.palette.common.white, 0.08)} 100%)`
      : `linear-gradient(180deg, ${alpha(theme.palette.background.paper, 0.72)} 0%, ${alpha(theme.palette.common.white, 0.82)} 18%, ${alpha(theme.palette.common.white, 0.9)} 40%, ${alpha(theme.palette.common.white, 0.95)} 70%, ${alpha(theme.palette.common.white, 0.98)} 100%)`,
  border: "1px solid transparent",
  backdropFilter: theme.palette.mode === "dark" ? "blur(20px)" : "blur(16px)",
  boxShadow: getFrostedCardShadow(theme),
});

export const getAccentCardStyles = (accentColor: string) => ({
  borderBottomWidth: "2px",
  borderBottomStyle: "solid" as const,
  borderBottomColor: mixWithTransparency(accentColor, 0.3),
});

export const getAccentCardHoverStyles = (
  theme: AppTheme,
  accentColor: string,
) => ({
  boxShadow: getFrostedCardLiftShadow(theme),
  borderBottomWidth: "3px",
  borderBottomColor: accentColor,
  marginBlockEnd: "-1px",
});

export const getChromeSurfaceColor = (
  theme: AppTheme,
  chromeColor: string,
  emphasis: "default" | "hover" = "default",
) => {
  const opacity =
    theme.palette.mode === "dark"
      ? emphasis === "hover"
        ? 0.28
        : 0.18
      : emphasis === "hover"
        ? 0.14
        : 0.08;

  return mixWithTransparency(chromeColor, opacity);
};

export const getSubtleDividerColor = (theme: AppTheme) =>
  alpha(theme.palette.divider, theme.palette.mode === "dark" ? 0.15 : 0.1);
