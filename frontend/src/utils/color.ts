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
    /rgba?\(\s*([\d.]+)\s*,\s*([\d.]+)\s*,\s*([\d.]+)/
  );
  if (rgbMatch) {
    return `rgba(${rgbMatch[1]}, ${rgbMatch[2]}, ${rgbMatch[3]}, ${clamped})`;
  }

  // hsl(h, s%, l%) or hsla(h, s%, l%, a)
  const hslMatch = color.match(
    /hsla?\(\s*([\d.]+)\s*,\s*([\d.%]+)\s*,\s*([\d.%]+)/
  );
  if (hslMatch) {
    return `hsla(${hslMatch[1]}, ${hslMatch[2]}, ${hslMatch[3]}, ${clamped})`;
  }

  return color;
}
