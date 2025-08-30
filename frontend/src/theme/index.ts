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

const createTheme = (
  variantName: string,
  primaryColorToken?: string,
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
  };

  return createMuiTheme(
    {
      spacing: 4,
      breakpoints,
      components: components as Components<Theme>,
      typography,
      shadows,
      palette,
    },
    {
      name: themeConfig.name,
      header: themeConfig.header,
      footer: themeConfig.footer,
      sidebar: themeConfig.sidebar,
    },
  );
};

export default createTheme;
