import "@mui/lab/themeAugmentation";
import { Theme } from "@mui/material/styles";
import { createTheme as createMuiTheme } from "@mui/material/styles";
import variants from "@/theme/variants";
import typography from "@/theme/typography";
import breakpoints from "@/theme/breakpoints";
import shadows from "@/theme/shadows";
import { Components } from "@mui/material/styles";
import components from "@/theme/components";

const createTheme = (name: string, primaryColor?: string): Theme => {
  let themeConfig = variants.find((variant) => variant.name === name);

  if (!themeConfig) {
    console.warn(new Error(`The theme ${name} is not valid`));
    themeConfig = variants[0];
  }

  const palette = {
    ...themeConfig.palette,
    primary: {
      ...themeConfig.palette.primary,
      ...(primaryColor && { main: primaryColor }),
    },
  };

  return createMuiTheme(
    {
      spacing: 4,
      breakpoints: breakpoints,
      components: components as Components,
      typography: typography,
      shadows: shadows,
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
