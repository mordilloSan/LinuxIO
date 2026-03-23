import type { AppTheme } from "@/theme";

export const getHoverBackground = (theme: AppTheme) =>
  theme.palette.mode === "light"
    ? theme.darken(theme.sidebar.background, 0.07)
    : theme.lighten(theme.sidebar.background, 0.07);

const legacyMuiOverrides = {};

export default legacyMuiOverrides;
