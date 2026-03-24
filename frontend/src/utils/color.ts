/**
 * Applies alpha transparency to a CSS color string.
 * Supports hex (#rgb, #rrggbb, #rrggbbaa), rgb(), rgba(), and hsl() formats.
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
  const rgbMatch = color.match(
    /rgba?\(\s*([\d.]+)\s*,\s*([\d.]+)\s*,\s*([\d.]+)/,
  );
  if (rgbMatch) {
    return `rgba(${rgbMatch[1]}, ${rgbMatch[2]}, ${rgbMatch[3]}, ${clamped})`;
  }

  // hsl(h, s%, l%) or hsla(h, s%, l%, a)
  const hslMatch = color.match(
    /hsla?\(\s*([\d.]+)\s*,\s*([\d.%]+)\s*,\s*([\d.%]+)/,
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
  const match = color.match(/rgba?\(\s*([\d.]+)\s*,\s*([\d.]+)\s*,\s*([\d.]+)/);

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
