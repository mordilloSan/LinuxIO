import type { AppTheme } from "@/theme/appTheme";
import { alpha, darken, lighten } from "@/theme/colorUtils";
import { MOTION_CSS_VARS } from "@/theme/constants";
import { getFrostedCardShadow, getSurfaceCssVariables } from "@/theme/surfaces";

function toColorChannel(color: string) {
  const trimmed = color.trim();

  if (trimmed.startsWith("#")) {
    const hex = trimmed.slice(1);
    const normalized =
      hex.length === 3
        ? hex
            .split("")
            .map((value) => value + value)
            .join("")
        : hex;

    if (normalized.length === 6) {
      const red = Number.parseInt(normalized.slice(0, 2), 16);
      const green = Number.parseInt(normalized.slice(2, 4), 16);
      const blue = Number.parseInt(normalized.slice(4, 6), 16);
      return `${red} ${green} ${blue}`;
    }
  }

  const rgbMatch = /^rgba?\(([^)]+)\)$/i.exec(trimmed);

  if (rgbMatch) {
    return rgbMatch[1]
      .split(",")
      .slice(0, 3)
      .map((value) => String(Math.round(Number.parseFloat(value.trim()))))
      .join(" ");
  }

  return "0 0 0";
}

export function applyCssVariables(
  root: HTMLElement,
  vars: Record<string, string>,
): void {
  for (const [key, value] of Object.entries(vars)) {
    root.style.setProperty(key, value);
  }
}

export function getThemeCssVariables(theme: AppTheme): Record<string, string> {
  return {
    "--app-color-scheme": theme.colorScheme,
    "--app-font-family": theme.typography.fontFamily,
    "--app-font-mono": theme.typography.fontFamilyMono,
    "--app-radius-base": `${theme.shape.borderRadius}px`,
    ...MOTION_CSS_VARS,
    "--app-palette-primary-main": theme.palette.primary.main,
    "--app-palette-primary-dark": theme.palette.primary.dark,
    "--app-palette-primary-contrast-text": theme.palette.primary.contrastText,
    "--app-palette-secondary-main": theme.palette.secondary.main,
    "--app-palette-secondary-dark": theme.palette.secondary.dark,
    "--app-palette-secondary-contrast-text":
      theme.palette.secondary.contrastText,
    "--app-palette-error-main": theme.palette.error.main,
    "--app-palette-error-dark": theme.palette.error.dark,
    "--app-palette-error-contrast-text": theme.palette.error.contrastText,
    "--app-palette-warning-main": theme.palette.warning.main,
    "--app-palette-warning-dark": theme.palette.warning.dark,
    "--app-palette-warning-contrast-text": theme.palette.warning.contrastText,
    "--app-palette-success-main": theme.palette.success.main,
    "--app-palette-success-dark": theme.palette.success.dark,
    "--app-palette-success-contrast-text": theme.palette.success.contrastText,
    "--app-palette-info-main": theme.palette.info.main,
    "--app-palette-background-default": theme.palette.background.default,
    "--app-palette-background-paper": theme.palette.background.paper,
    "--app-palette-text-primary": theme.palette.text.primary,
    "--app-palette-text-secondary": theme.palette.text.secondary,
    "--app-palette-text-disabled": theme.palette.text.disabled,
    "--app-palette-divider": theme.palette.divider,
    "--app-palette-action-active": theme.palette.action.active,
    "--app-palette-action-hover": theme.palette.action.hover,
    "--app-palette-action-selected": theme.palette.action.selected,
    "--app-palette-action-disabled": theme.palette.action.disabled,
    "--app-palette-action-disabled-background":
      theme.palette.action.disabledBackground,
    "--app-palette-action-disabled-opacity": `${theme.palette.action.disabledOpacity}`,
    // RGB-channel + grey compat tokens, consumed via rgba(var(--x) / alpha).
    "--app-palette-primary-mainChannel": toColorChannel(
      theme.palette.primary.main,
    ),
    "--app-palette-dividerChannel": toColorChannel(theme.palette.divider),
    "--app-palette-grey-100": "#f5f5f5",
    "--app-palette-grey-900": "#212121",
    "--app-header-background": theme.header.background,
    "--app-header-color": theme.header.color,
    "--app-header-search-bg": alpha(
      theme.palette.common.white,
      theme.palette.mode === "dark" ? 0.04 : 0.6,
    ),
    "--app-header-search-bg-hover":
      theme.palette.mode === "dark"
        ? lighten(theme.header.background, 0.07)
        : darken(theme.header.background, 0.07),
    "--app-sidebar-background": theme.sidebar.background,
    "--app-sidebar-color": theme.sidebar.color,
    "--app-sidebar-header-background": theme.sidebar.header.background,
    "--app-sidebar-item-hover-bg":
      theme.palette.mode === "dark"
        ? lighten(theme.sidebar.background, 0.07)
        : darken(theme.sidebar.background, 0.07),
    "--app-sidebar-item-grad-start": lighten(theme.palette.primary.main, 0.35),
    "--app-sidebar-item-grad-end": theme.palette.primary.main,
    "--app-sidebar-item-active-color": theme.palette.primary.contrastText,
    "--app-panel-background": theme.palette.background.paper,
    ...getSurfaceCssVariables(theme),
    "--app-chart-rx": theme.chart.rx,
    "--app-chart-tx": theme.chart.tx,
    "--app-chart-neutral": theme.chart.neutral,
    "--app-code-block-background": theme.codeBlock.background,
    "--app-code-block-color": theme.codeBlock.color,
    "--app-file-browser-surface": theme.fileBrowser.surface,
    "--app-file-browser-chrome": theme.fileBrowser.chrome,
    "--app-file-browser-breadcrumb-background":
      theme.fileBrowser.breadcrumbBackground,
    "--app-file-browser-breadcrumb-text": theme.fileBrowser.breadcrumbText,
    "--app-footer-background": theme.footer.background,
    "--app-footer-color": theme.footer.color,
    "--update-banner-bg": theme.palette.mode === "dark" ? "#000" : "#e3f2fd",
    "--update-banner-color":
      theme.palette.mode === "dark"
        ? lighten("var(--app-palette-info-main)", 0.7)
        : "var(--app-palette-info-main)",
    "--app-panel-text": theme.palette.text.primary,
    "--app-panel-border": theme.palette.divider,
    "--app-panel-shadow":
      theme.palette.mode === "dark"
        ? getFrostedCardShadow(theme)
        : "rgba(50, 50, 93, 0.025) 0px 2px 5px -1px, rgba(0, 0, 0, 0.05) 0px 1px 3px -1px",
    "--accent": theme.palette.primary.main,
    "--accent-soft": theme.palette.primary.light,
    "--accent-strong": theme.palette.primary.dark,
    "--app-overlay-dark": "rgba(0, 0, 0, 0.35)",
  };
}
