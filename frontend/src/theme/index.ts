import React, {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";

import breakpoints from "@/theme/breakpoints";
import {
  COLOR_TOKENS,
  getContrastText,
  resolvePrimaryColor,
} from "@/theme/colors";
import variants from "@/theme/variants";
import { useConfig } from "@/hooks/useConfig";
import type { AppConfig, ThemeColors } from "@/types/config";
import { alpha, darken, lighten } from "@/utils/color";

type BreakpointKey = keyof typeof breakpoints.values;
type SpacingInput = number | string;

interface AppPaletteColor {
  main: string;
  light: string;
  dark: string;
  contrastText: string;
}

interface AppTypographyVariant {
  fontSize: string;
  fontWeight: number;
  lineHeight: number;
  letterSpacing?: string;
}

export interface AppTypography {
  fontFamily: string;
  fontSize: number;
  fontWeightLight: number;
  fontWeightRegular: number;
  fontWeightMedium: number;
  fontWeightBold: number;
  h1: AppTypographyVariant;
  h2: AppTypographyVariant;
  h3: AppTypographyVariant;
  h4: AppTypographyVariant;
  h5: AppTypographyVariant;
  h6: AppTypographyVariant;
  body1: AppTypographyVariant;
  body2: AppTypographyVariant;
  caption: AppTypographyVariant;
  subtitle1: AppTypographyVariant;
  subtitle2: AppTypographyVariant;
  overline: AppTypographyVariant & { textTransform: "uppercase" };
  button: {
    textTransform: "none";
    fontWeight: number;
  };
  pxToRem: (value: number) => string;
}

export interface AppTheme {
  name: string;
  colorScheme: "light" | "dark";
  palette: {
    mode: "light" | "dark";
    common: {
      white: string;
      black: string;
    };
    primary: AppPaletteColor;
    secondary: AppPaletteColor;
    error: AppPaletteColor;
    warning: AppPaletteColor;
    success: AppPaletteColor;
    info: AppPaletteColor;
    background: {
      default: string;
      paper: string;
    };
    text: {
      primary: string;
      secondary: string;
      disabled: string;
    };
    divider: string;
    action: {
      active: string;
      hover: string;
      selected: string;
      disabled: string;
      disabledBackground: string;
      disabledOpacity: number;
    };
  };
  shape: {
    borderRadius: number;
  };
  spacing: (...values: SpacingInput[]) => string;
  breakpoints: {
    values: Record<BreakpointKey, number>;
    up: (key: BreakpointKey) => string;
    down: (key: BreakpointKey) => string;
    between: (start: BreakpointKey, end: BreakpointKey) => string;
  };
  transitions: {
    easing: {
      easeInOut: string;
      sharp: string;
    };
    duration: {
      shortest: number;
      standard: number;
      leavingScreen: number;
    };
    create: (
      properties: string | string[],
      options?: { duration?: number; easing?: string },
    ) => string;
  };
  typography: AppTypography;
  card: {
    background: string;
  };
  dialog: {
    border: string;
    glow: string;
    backdrop: string;
  };
  codeBlock: {
    background: string;
    color: string;
  };
  chart: {
    rx: string;
    tx: string;
    neutral: string;
  };
  fileBrowser: {
    surface: string;
    chrome: string;
    breadcrumbBackground: string;
    breadcrumbText: string;
  };
  header: {
    color: string;
    background: string;
    search: {
      color: string;
    };
    indicator: {
      background: string;
    };
  };
  footer: {
    color: string;
    background: string;
  };
  sidebar: {
    color: string;
    background: string;
    header: {
      color: string;
      background: string;
      brand: {
        color: string;
      };
    };
    badge: {
      color: string;
      background: string;
    };
  };
  alpha: typeof alpha;
  lighten: typeof lighten;
  darken: typeof darken;
}

interface AppThemeProviderProps {
  children: React.ReactNode;
  value?: AppTheme;
}

const APP_THEME_CONTEXT = createContext<AppTheme | undefined>(undefined);
const BASE_SPACING_UNIT = 4;
const FONT_FAMILY = [
  "Inter",
  "-apple-system",
  "BlinkMacSystemFont",
  '"Segoe UI"',
  "Roboto",
  '"Helvetica Neue"',
  "Arial",
  "sans-serif",
  '"Apple Color Emoji"',
  '"Segoe UI Emoji"',
  '"Segoe UI Symbol"',
].join(",");

const DEFAULT_STATUS_PALETTE = {
  error: "#da4453",
  warning: "#fdbc4b",
  success: "#2ecc71",
  info: COLOR_TOKENS.blue,
};

function createSpacing(...values: SpacingInput[]) {
  if (values.length === 0) {
    return "0px";
  }

  return values
    .map((value) =>
      typeof value === "number"
        ? `${Math.round(value * BASE_SPACING_UNIT * 1000) / 1000}px`
        : value,
    )
    .join(" ");
}

function createBreakpoints() {
  const values = breakpoints.values;

  return {
    values,
    up: (key: BreakpointKey) => `@media (min-width:${values[key]}px)`,
    down: (key: BreakpointKey) =>
      `@media (max-width:${Math.max(values[key] - 0.05, 0)}px)`,
    between: (start: BreakpointKey, end: BreakpointKey) =>
      `@media (min-width:${values[start]}px) and (max-width:${Math.max(values[end] - 0.05, 0)}px)`,
  };
}

function createTransitions() {
  const easing = {
    easeInOut: "cubic-bezier(0.4, 0, 0.2, 1)",
    sharp: "cubic-bezier(0.4, 0, 0.6, 1)",
  } as const;

  const duration = {
    shortest: 150,
    standard: 250,
    leavingScreen: 195,
  } as const;

  return {
    easing,
    duration,
    create: (
      properties: string | string[],
      options?: { duration?: number; easing?: string },
    ) => {
      const props = Array.isArray(properties) ? properties : [properties];
      const resolvedDuration = options?.duration ?? duration.standard;
      const resolvedEasing = options?.easing ?? easing.easeInOut;

      return props
        .map(
          (property) => `${property} ${resolvedDuration}ms ${resolvedEasing}`,
        )
        .join(", ");
    },
  };
}

function pxToRem(value: number, baseFontSize: number) {
  return `${value / baseFontSize}rem`;
}

function createTypography(
  fontFamily = FONT_FAMILY,
  fontSize = 13,
): AppTypography {
  const makeVariant = (
    px: number,
    fontWeight: number,
    lineHeight: number,
    letterSpacing?: string,
  ): AppTypographyVariant => ({
    fontSize: pxToRem(px, fontSize),
    fontWeight,
    lineHeight,
    ...(letterSpacing ? { letterSpacing } : {}),
  });

  return {
    fontFamily,
    fontSize,
    fontWeightLight: 300,
    fontWeightRegular: 400,
    fontWeightMedium: 500,
    fontWeightBold: 600,
    h1: makeVariant(26, 600, 1.25),
    h2: makeVariant(22.75, 600, 1.25),
    h3: makeVariant(19.5, 600, 1.25),
    h4: makeVariant(14.625, 500, 1.25),
    h5: makeVariant(13.8125, 500, 1.25),
    h6: makeVariant(13, 500, 1.25),
    body1: makeVariant(13, 400, 1.5),
    body2: makeVariant(11.375, 400, 1.43),
    caption: makeVariant(9.75, 400, 1.66),
    subtitle1: makeVariant(13, 400, 1.75),
    subtitle2: makeVariant(11.375, 500, 1.57),
    overline: {
      ...makeVariant(9.75, 400, 2.66),
      textTransform: "uppercase",
    },
    button: {
      textTransform: "none",
      fontWeight: 500,
    },
    pxToRem: (value: number) => pxToRem(value, fontSize),
  };
}

function createPaletteColor(main: string): AppPaletteColor {
  return {
    main,
    light: lighten(main, 0.16),
    dark: darken(main, 0.16),
    contrastText: getContrastText(main),
  };
}

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

  const rgbMatch = trimmed.match(/^rgba?\(([^)]+)\)$/i);

  if (rgbMatch) {
    return rgbMatch[1]
      .split(",")
      .slice(0, 3)
      .map((value) => String(Math.round(Number.parseFloat(value.trim()))))
      .join(" ");
  }

  return "0 0 0";
}

function resolveVariantTheme(
  variantName: string,
  primaryColorToken?: string,
  themeColors?: ThemeColors,
) {
  let themeConfig = variants.find((variant) => variant.name === variantName);

  if (!themeConfig) {
    console.warn(new Error(`The theme ${variantName} is not valid`));
    themeConfig = variants[0];
  }

  const defaultPrimaryMain =
    themeConfig.palette?.primary?.main || resolvePrimaryColor("blue");
  const primaryMain = resolvePrimaryColor(
    primaryColorToken,
    defaultPrimaryMain,
  );
  const primary = createPaletteColor(primaryMain);
  const secondary = createPaletteColor(
    themeConfig.palette.secondary.main || DEFAULT_STATUS_PALETTE.info,
  );

  const mode = themeConfig.palette.mode;
  const palette = {
    mode,
    common: {
      black: "#000000",
      white: "#FFFFFF",
    },
    primary,
    secondary,
    error: createPaletteColor(DEFAULT_STATUS_PALETTE.error),
    warning: createPaletteColor(DEFAULT_STATUS_PALETTE.warning),
    success: createPaletteColor(DEFAULT_STATUS_PALETTE.success),
    info: createPaletteColor(DEFAULT_STATUS_PALETTE.info),
    background: {
      default:
        themeColors?.backgroundDefault ??
        themeConfig.palette.background.default,
      paper:
        themeColors?.backgroundPaper ?? themeConfig.palette.background.paper,
    },
    text: {
      primary: themeConfig.palette.text.primary,
      secondary: themeConfig.palette.text.secondary,
      disabled:
        mode === "dark" ? "rgba(255, 255, 255, 0.4)" : "rgba(0, 0, 0, 0.38)",
    },
    divider:
      mode === "dark" ? "rgba(255, 255, 255, 0.12)" : "rgba(0, 0, 0, 0.12)",
    action: {
      active:
        mode === "dark" ? "rgba(255, 255, 255, 0.7)" : "rgba(0, 0, 0, 0.54)",
      hover:
        mode === "dark" ? "rgba(255, 255, 255, 0.08)" : "rgba(0, 0, 0, 0.04)",
      selected:
        mode === "dark" ? "rgba(255, 255, 255, 0.16)" : "rgba(0, 0, 0, 0.08)",
      disabled:
        mode === "dark" ? "rgba(255, 255, 255, 0.3)" : "rgba(0, 0, 0, 0.26)",
      disabledBackground:
        mode === "dark" ? "rgba(255, 255, 255, 0.12)" : "rgba(0, 0, 0, 0.12)",
      disabledOpacity: 0.38,
    },
  } as const;

  return {
    name: themeConfig.name,
    palette,
    card: {
      background: themeColors?.cardBackground ?? themeConfig.card.background,
    },
    dialog: {
      border: themeColors?.dialogBorder ?? themeConfig.dialog.border,
      glow: themeColors?.dialogGlow ?? themeConfig.dialog.glow,
      backdrop: themeColors?.dialogBackdrop ?? themeConfig.dialog.backdrop,
    },
    codeBlock: {
      background:
        themeColors?.codeBackground ?? themeConfig.codeBlock.background,
      color: themeColors?.codeText ?? themeConfig.codeBlock.color,
    },
    chart: {
      rx: themeColors?.chartRx ?? themeConfig.chart.rx,
      tx: themeColors?.chartTx ?? themeConfig.chart.tx,
      neutral: themeColors?.chartNeutral ?? themeConfig.chart.neutral,
    },
    fileBrowser: {
      surface:
        themeColors?.fileBrowserSurface ?? themeConfig.fileBrowser.surface,
      chrome: themeColors?.fileBrowserChrome ?? themeConfig.fileBrowser.chrome,
      breadcrumbBackground:
        themeColors?.fileBrowserBreadcrumbBackground ??
        themeConfig.fileBrowser.breadcrumbBackground,
      breadcrumbText:
        themeColors?.fileBrowserBreadcrumbText ??
        themeConfig.fileBrowser.breadcrumbText,
    },
    header: {
      color: themeConfig.header.color,
      background:
        themeColors?.headerBackground ?? themeConfig.header.background,
      search: {
        color: themeConfig.header.search.color,
      },
      indicator: {
        background: themeConfig.header.indicator.background,
      },
    },
    footer: {
      color: themeConfig.footer.color,
      background:
        themeColors?.footerBackground ?? themeConfig.footer.background,
    },
    sidebar: {
      color: themeConfig.sidebar.color,
      background:
        themeColors?.sidebarBackground ?? themeConfig.sidebar.background,
      header: {
        color: themeConfig.sidebar.header.color,
        background:
          themeColors?.sidebarBackground ??
          themeConfig.sidebar.header.background,
        brand: {
          color: themeConfig.sidebar.header.brand.color,
        },
      },
      badge: {
        color: themeConfig.sidebar.badge.color,
        background: themeConfig.sidebar.badge.background,
      },
    },
  };
}

export function buildAppTheme(
  configOrTheme:
    | Pick<AppConfig, "theme" | "primaryColor" | "themeColors">
    | string,
  primaryColorToken?: string,
  themeColors?: ThemeColors,
): AppTheme {
  const config =
    typeof configOrTheme === "string"
      ? {
          theme: configOrTheme,
          primaryColor: primaryColorToken ?? resolvePrimaryColor("blue"),
          themeColors,
        }
      : configOrTheme;

  const resolved = resolveVariantTheme(
    config.theme,
    config.primaryColor,
    config.themeColors,
  );

  const transitions = createTransitions();
  const typography = createTypography();

  return {
    ...resolved,
    colorScheme: resolved.palette.mode,
    shape: {
      borderRadius: 4,
    },
    spacing: (...values: SpacingInput[]) => createSpacing(...values),
    breakpoints: createBreakpoints(),
    transitions,
    typography,
    alpha,
    lighten,
    darken,
  };
}

function getThemeCssVariables(theme: AppTheme): Record<string, string> {
  return {
    "--app-color-scheme": theme.colorScheme,
    "--app-font-family": theme.typography.fontFamily,
    "--app-radius-base": `${theme.shape.borderRadius}px`,
    "--app-palette-primary-main": theme.palette.primary.main,
    "--app-palette-primary-light": theme.palette.primary.light,
    "--app-palette-primary-dark": theme.palette.primary.dark,
    "--app-palette-primary-contrast-text": theme.palette.primary.contrastText,
    "--app-palette-secondary-main": theme.palette.secondary.main,
    "--app-palette-secondary-light": theme.palette.secondary.light,
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
    "--app-header-background": theme.header.background,
    "--app-header-color": theme.header.color,
    "--app-header-search-color": theme.header.search.color,
    "--app-header-search-bg": alpha(
      theme.palette.common.white,
      theme.palette.mode === "dark" ? 0.04 : 0.6,
    ),
    "--app-header-search-bg-hover":
      theme.palette.mode === "dark"
        ? lighten(theme.header.background, 0.07)
        : darken(theme.header.background, 0.07),
    "--app-footer-background": theme.footer.background,
    "--app-footer-color": theme.footer.color,
    "--app-sidebar-background": theme.sidebar.background,
    "--app-sidebar-color": theme.sidebar.color,
    "--app-sidebar-header-background": theme.sidebar.header.background,
    "--app-sidebar-header-color": theme.sidebar.header.color,
    "--app-sidebar-item-hover-bg":
      theme.palette.mode === "dark"
        ? lighten(theme.sidebar.background, 0.07)
        : darken(theme.sidebar.background, 0.07),
    "--app-sidebar-item-grad-start": lighten(theme.palette.primary.main, 0.35),
    "--app-sidebar-item-grad-end": theme.palette.primary.main,
    "--app-sidebar-item-active-color": theme.palette.primary.contrastText,
    "--app-card-background": theme.card.background,
    "--app-dialog-border": theme.dialog.border,
    "--app-dialog-glow": theme.dialog.glow,
    "--app-dialog-backdrop": theme.dialog.backdrop,
    "--app-tooltip-bg":
      theme.palette.mode === "dark"
        ? "rgba(66, 66, 66, 0.95)"
        : "rgba(110, 110, 110, 0.92)",
    "--app-tooltip-color": "#ffffff",
    "--app-panel-background": theme.palette.background.paper,
    "--app-panel-text": theme.palette.text.primary,
    "--app-panel-border": alpha(
      theme.palette.divider,
      theme.palette.mode === "dark" ? 0.75 : 1,
    ),
    "--app-panel-shadow":
      theme.palette.mode === "dark"
        ? "0 16px 40px -28px rgba(0, 0, 0, 0.6)"
        : "rgba(50, 50, 93, 0.025) 0px 2px 5px -1px, rgba(0, 0, 0, 0.05) 0px 1px 3px -1px",
    "--accent": theme.palette.primary.main,
    "--accent-soft": theme.palette.primary.light,
    "--accent-strong": theme.palette.primary.dark,
    "--color-primary": "var(--app-palette-primary-main)",
    "--color-primary-contrast": "var(--app-palette-primary-contrast-text)",
    "--color-bg": "var(--app-palette-background-default)",
    "--color-surface": "var(--app-palette-background-paper)",
    "--color-text": "var(--app-palette-text-primary)",
    "--color-text-secondary": "var(--app-palette-text-secondary)",
    "--color-text-disabled": "var(--app-palette-text-disabled)",
    "--color-action-active": "var(--app-palette-action-active)",
    "--color-action-hover": "var(--app-palette-action-hover)",
    "--color-action-selected": "var(--app-palette-action-selected)",
    "--color-action-disabled": "var(--app-palette-action-disabled)",
    "--color-action-disabled-bg":
      "var(--app-palette-action-disabled-background)",
    "--color-action-disabled-opacity":
      "var(--app-palette-action-disabled-opacity)",
    "--color-error": "var(--app-palette-error-main)",
    "--color-warning": "var(--app-palette-warning-main)",
    "--color-success": "var(--app-palette-success-main)",
    "--color-info": "var(--app-palette-info-main)",
    "--color-divider": "var(--app-palette-divider)",
    "--mui-palette-primary-main": theme.palette.primary.main,
    "--mui-palette-primary-mainChannel": toColorChannel(
      theme.palette.primary.main,
    ),
    "--mui-palette-warning-main": theme.palette.warning.main,
    "--mui-palette-success-main": theme.palette.success.main,
    "--mui-palette-error-main": theme.palette.error.main,
    "--mui-palette-text-secondary": theme.palette.text.secondary,
    "--mui-palette-divider": theme.palette.divider,
    "--mui-palette-dividerChannel": toColorChannel(theme.palette.divider),
    "--mui-palette-background-default": theme.palette.background.default,
    "--mui-palette-action-hover": theme.palette.action.hover,
    "--mui-palette-common-blackChannel": "0 0 0",
    "--mui-palette-grey-100": "#f5f5f5",
    "--mui-palette-grey-900": "#212121",
  };
}

export function AppThemeProvider({ children, value }: AppThemeProviderProps) {
  const resolvedTheme = value;
  const cssVariables = useMemo(
    () => (resolvedTheme ? getThemeCssVariables(resolvedTheme) : {}),
    [resolvedTheme],
  );

  useEffect(() => {
    if (!resolvedTheme) return;

    const root = document.documentElement;
    root.dataset.appTheme = resolvedTheme.name.toLowerCase();
    root.dataset.muiColorScheme = resolvedTheme.colorScheme;

    for (const [key, value] of Object.entries(cssVariables)) {
      root.style.setProperty(key, value);
    }
  }, [cssVariables, resolvedTheme]);

  if (!resolvedTheme) {
    return null;
  }

  return React.createElement(
    APP_THEME_CONTEXT.Provider,
    { value: resolvedTheme },
    children,
  );
}

export function ConfiguredAppThemeProvider({
  children,
}: {
  children: React.ReactNode;
}) {
  const { config } = useConfig();
  const theme = useMemo(() => buildAppTheme(config), [config]);

  return React.createElement(AppThemeProvider, { value: theme, children });
}

export function useAppTheme() {
  const theme = useContext(APP_THEME_CONTEXT);

  if (!theme) {
    throw new Error("useAppTheme must be used within an AppThemeProvider");
  }

  return theme;
}

export function useAppMediaQuery(query: string) {
  const normalizedQuery = query.trim().replace(/^@media\s*/i, "");

  const [matches, setMatches] = useState(() =>
    typeof window !== "undefined"
      ? window.matchMedia(normalizedQuery).matches
      : false,
  );

  useEffect(() => {
    if (typeof window === "undefined") return undefined;

    const mediaQueryList = window.matchMedia(normalizedQuery);
    const update = (event: MediaQueryListEvent) => {
      setMatches(event.matches);
    };

    setMatches(mediaQueryList.matches);
    mediaQueryList.addEventListener("change", update);
    return () => mediaQueryList.removeEventListener("change", update);
  }, [normalizedQuery]);

  return matches;
}

export default buildAppTheme;
