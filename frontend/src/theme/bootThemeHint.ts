import type { AppTheme } from "@/theme/appTheme";

/* The first-paint hint: the four colours the inline script in index.html
   paints before the app has loaded the user's settings. Only that script
   reads it; the app writes it and never reads it back, so the settings the
   bridge serves stay the only configuration. */
export const BOOT_THEME_HINT_KEY = "linuxio.bootTheme";

export function writeBootThemeHint(theme: AppTheme): void {
  try {
    window.localStorage.setItem(
      BOOT_THEME_HINT_KEY,
      JSON.stringify({
        scheme: theme.colorScheme,
        bg: theme.palette.background.default,
        text: theme.palette.text.primary,
        primary: theme.palette.primary.main,
      }),
    );
  } catch {
    // Best-effort: without storage the next load paints the OS preference.
  }
}

export function clearBootThemeHint(): void {
  try {
    window.localStorage.removeItem(BOOT_THEME_HINT_KEY);
  } catch {
    // Best-effort cleanup only.
  }
}
