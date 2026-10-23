/* The public surface of the theme. Each concern lives in its own module:
   appTheme (the model and its builder), cssVariables (the --app-* writer),
   AppThemeProvider (context and installation), constants/breakpoints/colors/
   variants (tokens). */
export { BASE_SPACING_UNIT } from "@/theme/constants";
export { applyCssVariables } from "@/theme/cssVariables";
export { AppThemeProvider, useAppTheme } from "@/theme/AppThemeProvider";
export { buildAppTheme, default } from "@/theme/appTheme";
export type { AppTheme } from "@/theme/appTheme";
