/**
 * Applies alpha transparency to a CSS color string.
 * Supports hex (#rgb, #rrggbb, #rrggbbaa), rgb(), rgba(), and hsl() formats.
 *
 * Anything it doesn't recognize — a `var(...)` reference included — falls
 * through the last branch and comes back unchanged, opacity silently
 * dropped. For a CSS variable input, reach for mixWithTransparency in
 * theme/surfaces.ts instead, which mixes through color-mix and so still
 * works on values it can't parse.
 */
export function alpha(color: string, opacity: number): string {
  const clamped = Math.min(1, Math.max(0, opacity));

  // hex
  if (color.startsWith("#")) {
    const hex = color.slice(1);
    let r: number, g: number, b: number;
    if (hex.length === 3) {
      r = parseInt(hex[0] + hex[0], 16);
      g = parseInt(hex[1] + hex[1], 16);
      b = parseInt(hex[2] + hex[2], 16);
    } else {
      r = parseInt(hex.slice(0, 2), 16);
      g = parseInt(hex.slice(2, 4), 16);
      b = parseInt(hex.slice(4, 6), 16);
    }
    return `rgba(${r}, ${g}, ${b}, ${clamped})`;
  }

  // rgb(r, g, b) or rgba(r, g, b, a)
  const rgbMatch = /rgba?\(\s*([\d.]+)\s*,\s*([\d.]+)\s*,\s*([\d.]+)/.exec(
    color,
  );
  if (rgbMatch) {
    return `rgba(${rgbMatch[1]}, ${rgbMatch[2]}, ${rgbMatch[3]}, ${clamped})`;
  }

  // hsl(h, s%, l%) or hsla(h, s%, l%, a)
  const hslMatch = /hsla?\(\s*([\d.]+)\s*,\s*([\d.%]+)\s*,\s*([\d.%]+)/.exec(
    color,
  );
  if (hslMatch) {
    return `hsla(${hslMatch[1]}, ${hslMatch[2]}, ${hslMatch[3]}, ${clamped})`;
  }

  return color;
}

type RgbColor = {
  r: number;
  g: number;
  b: number;
};

function clampChannel(value: number) {
  return Math.min(255, Math.max(0, Math.round(value)));
}

function parseHexColor(color: string): RgbColor | null {
  if (!color.startsWith("#")) {
    return null;
  }

  const hex = color.slice(1);

  if (hex.length === 3) {
    return {
      r: parseInt(hex[0] + hex[0], 16),
      g: parseInt(hex[1] + hex[1], 16),
      b: parseInt(hex[2] + hex[2], 16),
    };
  }

  if (hex.length === 6 || hex.length === 8) {
    return {
      r: parseInt(hex.slice(0, 2), 16),
      g: parseInt(hex.slice(2, 4), 16),
      b: parseInt(hex.slice(4, 6), 16),
    };
  }

  return null;
}

function parseRgbColor(color: string): RgbColor | null {
  const match = /rgba?\(\s*([\d.]+)\s*,\s*([\d.]+)\s*,\s*([\d.]+)/.exec(color);

  if (!match) {
    return null;
  }

  return {
    r: Number.parseFloat(match[1]),
    g: Number.parseFloat(match[2]),
    b: Number.parseFloat(match[3]),
  };
}

function parseColor(color: string): RgbColor | null {
  return parseHexColor(color) ?? parseRgbColor(color);
}

function formatRgb({ r, g, b }: RgbColor) {
  return `rgb(${clampChannel(r)}, ${clampChannel(g)}, ${clampChannel(b)})`;
}

/** Converts a parsed RGB/hex color to the six-digit format native pickers use. */
export function toHexColor(color: string): string | null {
  const parsed = parseColor(color);
  if (!parsed) return null;

  const channel = (value: number) =>
    clampChannel(value).toString(16).padStart(2, "0");
  return `#${channel(parsed.r)}${channel(parsed.g)}${channel(parsed.b)}`;
}

function mix(color: string, target: RgbColor, amount: number) {
  const parsed = parseColor(color);

  if (!parsed) {
    const ratio = Math.round(Math.min(1, Math.max(0, amount)) * 100);
    const targetColor =
      target.r === 255 && target.g === 255 && target.b === 255
        ? "white"
        : "black";

    return `color-mix(in srgb, ${color} ${100 - ratio}%, ${targetColor} ${ratio}%)`;
  }

  const clamped = Math.min(1, Math.max(0, amount));

  return formatRgb({
    r: parsed.r + (target.r - parsed.r) * clamped,
    g: parsed.g + (target.g - parsed.g) * clamped,
    b: parsed.b + (target.b - parsed.b) * clamped,
  });
}

export function lighten(color: string, amount: number): string {
  return mix(color, { r: 255, g: 255, b: 255 }, amount);
}

export function darken(color: string, amount: number): string {
  return mix(color, { r: 0, g: 0, b: 0 }, amount);
}

export type HslColor = {
  /** Hue in degrees, 0–360. */
  h: number;
  /** Saturation, 0–1. */
  s: number;
  /** Lightness, 0–1. */
  l: number;
};

/**
 * Reads a color as HSL. Returns null for values this module cannot parse
 * (named colors, `color-mix(...)`, CSS variables), so callers can fall back.
 *
 * Paired with `fromHsl`, this is what lets a palette be derived from a single
 * color by varying one component at a time — see the dock's tile palettes,
 * which fan hue while holding saturation and lightness fixed.
 */
export function toHsl(color: string): HslColor | null {
  const parsed = parseColor(color);

  if (!parsed) {
    return null;
  }

  const r = parsed.r / 255;
  const g = parsed.g / 255;
  const b = parsed.b / 255;
  const max = Math.max(r, g, b);
  const min = Math.min(r, g, b);
  const l = (max + min) / 2;
  const delta = max - min;

  if (delta === 0) {
    return { h: 0, s: 0, l };
  }

  const s = delta / (1 - Math.abs(2 * l - 1));
  const h =
    max === r
      ? 60 * (((g - b) / delta + 6) % 6)
      : max === g
        ? 60 * ((b - r) / delta + 2)
        : 60 * ((r - g) / delta + 4);

  return { h, s, l };
}

/** Builds an rgb() color from HSL components. Inverse of `toHsl`. */
export function fromHsl(h: number, s: number, l: number): string {
  const hue = ((h % 360) + 360) % 360;
  const saturation = Math.min(1, Math.max(0, s));
  const lightness = Math.min(1, Math.max(0, l));
  const c = (1 - Math.abs(2 * lightness - 1)) * saturation;
  const x = c * (1 - Math.abs(((hue / 60) % 2) - 1));
  const m = lightness - c / 2;
  const sector = Math.floor(hue / 60) % 6;
  const [r, g, b] = [
    [c, x, 0],
    [x, c, 0],
    [0, c, x],
    [0, x, c],
    [x, 0, c],
    [c, 0, x],
  ][sector];

  return formatRgb({
    r: (r + m) * 255,
    g: (g + m) * 255,
    b: (b + m) * 255,
  });
}

/**
 * Samples the shortest HSL path between two colors. This keeps colorful
 * palettes vivid where straight RGB interpolation can pass through gray.
 */
export function interpolateHsl(
  start: string,
  end: string,
  amount: number,
): string {
  const from = toHsl(start);
  const to = toHsl(end);
  const position = Math.min(1, Math.max(0, amount));

  if (!from || !to) {
    const endShare = Math.round(position * 100);
    return `color-mix(in srgb, ${start} ${100 - endShare}%, ${end} ${endShare}%)`;
  }

  // Hue has no visual meaning at zero saturation. Borrow the colorful end's
  // hue so a neutral endpoint fades cleanly instead of taking a detour via 0°.
  const startHue = from.s === 0 && to.s > 0 ? to.h : from.h;
  const endHue = to.s === 0 && from.s > 0 ? from.h : to.h;
  const hueDelta = ((endHue - startHue + 540) % 360) - 180;

  return fromHsl(
    startHue + hueDelta * position,
    from.s + (to.s - from.s) * position,
    from.l + (to.l - from.l) * position,
  );
}
