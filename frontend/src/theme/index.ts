import {
  Theme,
  Components,
  createTheme as createMuiTheme,
} from "@mui/material/styles";
import variants from "@/theme/variants";
import typography from "@/theme/typography";
import breakpoints from "@/theme/breakpoints";
import components from "@/theme/components";
import { resolvePrimaryColor, getContrastText } from "@/theme/colors";
import { ThemeColors } from "@/types/config";

const createTheme = (
  variantName: string,
  primaryColorToken?: string,
  themeColors?: ThemeColors,
): Theme => {
  let themeConfig = variants.find((v) => v.name === variantName);
  if (!themeConfig) {
    console.warn(new Error(`The theme ${variantName} is not valid`));
    themeConfig = variants[0];
  }

  const defaultPrimaryMain =
    (themeConfig.palette?.primary?.main as string) ||
    resolvePrimaryColor("blue");

  const primaryMain = resolvePrimaryColor(
    primaryColorToken,
    defaultPrimaryMain,
  );

  const palette = {
    ...themeConfig.palette,
    primary: {
      ...themeConfig.palette.primary,
      main: primaryMain,
      contrastText: getContrastText(primaryMain),
    },
    background: {
      ...themeConfig.palette.background,
      ...(themeColors?.backgroundDefault && {
        default: themeColors.backgroundDefault,
      }),
      ...(themeColors?.backgroundPaper && {
        paper: themeColors.backgroundPaper,
      }),
    },
  };

  const header = {
    ...themeConfig.header,
    ...(themeColors?.headerBackground && {
      background: themeColors.headerBackground,
    }),
  };

  const footer = {
    ...themeConfig.footer,
    ...(themeColors?.footerBackground && {
      background: themeColors.footerBackground,
    }),
  };

  const card = {
    ...themeConfig.card,
    ...(themeColors?.cardBackground && {
      background: themeColors.cardBackground,
    }),
  };

  const dialog = {
    ...themeConfig.dialog,
    ...(themeColors?.dialogBorder && { border: themeColors.dialogBorder }),
    ...(themeColors?.dialogGlow && { glow: themeColors.dialogGlow }),
    ...(themeColors?.dialogBackdrop && {
      backdrop: themeColors.dialogBackdrop,
    }),
  };

  const codeBlock = {
    ...themeConfig.codeBlock,
    ...(themeColors?.codeBackground && {
      background: themeColors.codeBackground,
    }),
    ...(themeColors?.codeText && {
      color: themeColors.codeText,
    }),
  };

  const chart = {
    ...themeConfig.chart,
    ...(themeColors?.chartRx && { rx: themeColors.chartRx }),
    ...(themeColors?.chartTx && { tx: themeColors.chartTx }),
    ...(themeColors?.chartNeutral && {
      neutral: themeColors.chartNeutral,
    }),
  };

  const fileBrowser = {
    ...themeConfig.fileBrowser,
    ...(themeColors?.fileBrowserSurface && {
      surface: themeColors.fileBrowserSurface,
    }),
    ...(themeColors?.fileBrowserChrome && {
      chrome: themeColors.fileBrowserChrome,
    }),
    ...(themeColors?.fileBrowserBreadcrumbBackground && {
      breadcrumbBackground: themeColors.fileBrowserBreadcrumbBackground,
    }),
    ...(themeColors?.fileBrowserBreadcrumbText && {
      breadcrumbText: themeColors.fileBrowserBreadcrumbText,
    }),
  };

  const sidebar = {
    ...themeConfig.sidebar,
    ...(themeColors?.sidebarBackground && {
      background: themeColors.sidebarBackground,
    }),
    header: {
      ...themeConfig.sidebar.header,
      ...(themeColors?.sidebarBackground && {
        background: themeColors.sidebarBackground,
      }),
    },
  };

  return createMuiTheme(
    {
      spacing: 4,
      breakpoints,
      components: components as Components<Theme>,
      typography,
      palette,
      cssVariables: { nativeColor: true },
    },
    {
      name: themeConfig.name,
      card,
      codeBlock,
      chart,
      dialog,
      fileBrowser,
      header,
      footer,
      sidebar,
    },
  );
};

export default createTheme;
