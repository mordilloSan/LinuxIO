// src/theme/colors.ts

export const SEMANTIC_STATUS_COLORS = {
  success: "#00E676",
  warning: "#FFC107",
  error: "#f54b3f",
  danger: "#FF5252",
  info: "#2196F3",
  neutral: "#BDBDBD",
  muted: "#9E9E9E",
  caution: "#FF9800",
} as const;

export const COLOR_TOKENS = {
  blue: "#1d99f3",
  red: "#da4453",
  green: "#2ecc71",
  yellow: "#fdbc4b",
  orange: "#f47750",
  violet: "#9b59b6",
} as const;

export const GREY_TOKENS = {
  50: "#fafafa",
  100: "#f5f5f5",
  200: "#eeeeee",
  300: "#e0e0e0",
  400: "#bdbdbd",
  500: "#9e9e9e",
  600: "#757575",
  700: "#616161",
  800: "#424242",
  900: "#212121",
} as const;

/**
 * Brand colours of the OS logos the VM pages show, keyed by Iconify id (the
 * Simple Icons hex, Microsoft's blue for Windows). A brand whose colour is
 * black or a dark navy (AlmaLinux, Alpine, CentOS, Slackware, SUSE) is left
 * out, since it would vanish on a dark card; its logo keeps the text colour.
 */
export const OS_BRAND_COLORS: Readonly<Record<string, string>> = {
  "mdi:microsoft-windows": "#0078D4",
  "simple-icons:archlinux": "#1793D1",
  "simple-icons:debian": "#A81D33",
  "simple-icons:elementary": "#64BAFF",
  "simple-icons:fedora": "#51A2DA",
  "simple-icons:freebsd": "#AB2B28",
  "simple-icons:gentoo": "#54487A",
  "simple-icons:homeassistant": "#18BCF2",
  "simple-icons:kalilinux": "#557C94",
  "simple-icons:linux": "#FCC624",
  "simple-icons:linuxmint": "#86BE43",
  "simple-icons:manjaro": "#35BFA4",
  "simple-icons:nixos": "#5277C3",
  "simple-icons:opensuse": "#73BA25",
  "simple-icons:popos": "#48B9C7",
  "simple-icons:raspberrypi": "#A22846",
  "simple-icons:redhat": "#EE0000",
  "simple-icons:rockylinux": "#10B981",
  "simple-icons:ubuntu": "#E95420",
  "simple-icons:voidlinux": "#478061",
  "simple-icons:zorin": "#15A6F0",
};

type ColorName = keyof typeof COLOR_TOKENS;

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
