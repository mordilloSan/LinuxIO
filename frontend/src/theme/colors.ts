// src/theme/colors.ts

export const COLOR_TOKENS = {
  blue: "#1d99f3",
  red: "#da4453",
  green: "#2ecc71",
  yellow: "#fdbc4b",
  orange: "#f47750",
  violet: "#9b59b6",
} as const;

export type ColorName = keyof typeof COLOR_TOKENS;

/**
 * Resolve a token name to its hex value.
 * Falls back to the provided hex or default "blue".
 */
export function resolvePrimaryColor(name?: string, fallback?: string): string {
  if (!name) return fallback || COLOR_TOKENS.blue;
  const key = name.toLowerCase() as ColorName;
  return COLOR_TOKENS[key] || fallback || COLOR_TOKENS.blue;
}

/** Quick WCAG-ish contrast chooser (black/white) */
export function getContrastText(hex: string): "#000" | "#fff" {
  const c = hex.replace("#", "");
  const [r, g, b] =
    c.length === 3
      ? c.split("").map((x) => parseInt(x + x, 16))
      : [
          parseInt(c.slice(0, 2), 16),
          parseInt(c.slice(2, 4), 16),
          parseInt(c.slice(4, 6), 16),
        ];

  const [R, G, B] = [r, g, b].map((v) => {
    const s = v / 255;
    return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
  });

  const luminance = 0.2126 * R + 0.7152 * G + 0.0722 * B;
  return luminance > 0.5 ? "#000" : "#fff";
}
