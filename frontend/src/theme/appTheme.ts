import {
  COLOR_TOKENS,
  getContrastText,
  resolvePrimaryColor,
} from "@/theme/colors";
import { darken, lighten } from "@/theme/colorUtils";
import variants from "@/theme/variants";

/* User overrides for the theme's resolved colours, per colour scheme. The
   config payload from the bridge (api ConfigThemeColorsByModePayload) is
   structurally identical, so it is passed in as-is. */
export interface ThemeColors {
  backgroundDefault?: string;
  backgroundPaper?: string;
  headerBackground?: string;
  footerBackground?: string;
  sidebarBackground?: string;
  cardBackground?: string;
  dialogBorder?: string;
  dialogGlow?: string;
  dialogBackdrop?: string;
  codeBackground?: string;
  codeText?: string;
  chartRx?: string;
  chartTx?: string;
  chartNeutral?: string;
  fileBrowserSurface?: string;
  fileBrowserChrome?: string;
  fileBrowserBreadcrumbBackground?: string;
  fileBrowserBreadcrumbText?: string;
}

export interface ThemeColorsByMode {
  light?: ThemeColors;
  dark?: ThemeColors;
}

interface AppPaletteColor {
  contrastText: string;
  dark: string;
  light: string;
  main: string;
}

/* What the theme still is in JS: the generator input for the --app-*
   variables AppThemeProvider writes, plus the resolved colours the few
   canvas/xterm/ace/colour-editor consumers must read as values. Spacing,
   radii, typography variants, transitions and breakpoints live in CSS
   (theme/variables.css, components/ui/*.css, MOTION_CSS_VARS) and in
   theme/breakpoints.ts. */
export interface AppTheme {
  card: {
    background: string;
  };
  chart: {
    rx: string;
    tx: string;
    neutral: string;
  };
  codeBlock: {
    background: string;
    color: string;
  };
  colorScheme: "light" | "dark";
  dialog: {
    border: string;
    glow: string;
    backdrop: string;
  };
  fileBrowser: {
    surface: string;
    chrome: string;
    breadcrumbBackground: string;
    breadcrumbText: string;
  };
  footer: {
    color: string;
    background: string;
  };
  header: {
    color: string;
    background: string;
    indicator: {
      background: string;
    };
  };
  name: string;
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
  typography: {
    fontFamily: string;
    fontFamilyMono: string;
  };
}

const FONT_FAMILY = [
  '"Inter Variable"',
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
// JetBrains Mono is bundled (index.tsx imports it) so paths and addresses never
// fall back to whatever each OS picked decades ago (DejaVu Sans Mono on most
// Linux desktops), which sits badly next to Inter.
const FONT_FAMILY_MONO = [
  '"JetBrains Mono"',
  "ui-monospace",
  "SFMono-Regular",
  '"SF Mono"',
  "Menlo",
  "Consolas",
  "monospace",
].join(",");

const DEFAULT_STATUS_PALETTE = {
  error: "#da4453",
  warning: "#fdbc4b",
  success: "#2ecc71",
  info: COLOR_TOKENS.blue,
};

function createPaletteColor(main: string): AppPaletteColor {
  return {
    main,
    light: lighten(main, 0.16),
    dark: darken(main, 0.16),
    contrastText: getContrastText(main),
  };
}

function resolveVariantTheme(
  variantName: string,
  primaryColorToken?: string,
  themeColorsByMode?: ThemeColorsByMode,
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
  const themeColors =
    mode === "dark" ? themeColorsByMode?.dark : themeColorsByMode?.light;
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
  themeName: string,
  primaryColorToken?: string,
  themeColors?: ThemeColorsByMode,
): AppTheme {
  const resolved = resolveVariantTheme(
    themeName,
    primaryColorToken,
    themeColors,
  );

  return {
    ...resolved,
    colorScheme: resolved.palette.mode,
    shape: {
      borderRadius: 4,
    },
    typography: { fontFamily: FONT_FAMILY, fontFamilyMono: FONT_FAMILY_MONO },
  };
}

export default buildAppTheme;
