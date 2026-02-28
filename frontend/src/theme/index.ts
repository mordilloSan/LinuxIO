import {
  Theme,
  Components,
  createTheme as createMuiTheme,
} from "@mui/material/styles";
import variants from "@/theme/variants";
import typography from "@/theme/typography";
import breakpoints from "@/theme/breakpoints";
import shadows from "@/theme/shadows";
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
    ...(themeColors?.cardBackground && { background: themeColors.cardBackground }),
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
      shadows,
      palette,
      cssVariables: { nativeColor: true },
    },
    {
      name: themeConfig.name,
      card,
      header,
      footer,
      sidebar,
    },
  );
};

export default createTheme;
