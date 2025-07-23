import { IColor } from "react-color-palette";
import { colord } from "colord";

/**
 * Converts a hex string to a fully valid IColor object.
 */
export function hexToIColor(hex: string): IColor {
  const c = colord(hex);
  const { r, g, b, a } = c.toRgb();
  const { h, s, v } = c.toHsv();

  return {
    hex: c.toHex(),
    rgb: { r, g, b, a },
    hsv: { h, s, v, a },
  };
}
