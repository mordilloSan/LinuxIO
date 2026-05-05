import { type AppTheme, buildAppTheme } from "@/theme";

const authThemeBase = buildAppTheme({
  theme: "DARK",
  primaryColor: "#407AD6",
  themeColors: {
    dark: {
      backgroundDefault: "#1B2635",
      backgroundPaper: "#233044",
      footerBackground: "#1B2635",
      headerBackground: "#1B2635",
      sidebarBackground: "#1B2635",
      cardBackground: "#233044",
    },
  },
});

const authTheme: AppTheme = {
  ...authThemeBase,
  shape: {
    borderRadius: 16,
  },
  typography: {
    ...authThemeBase.typography,
    fontFamily: '"Space Grotesk", "Sora", sans-serif',
    h4: {
      ...authThemeBase.typography.h4,
      fontWeight: 600,
      letterSpacing: "-0.02em",
    },
    button: {
      textTransform: "none",
      fontWeight: 600,
    },
  },
};

export default authTheme;
