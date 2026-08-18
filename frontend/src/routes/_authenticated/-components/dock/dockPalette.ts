import type { ConfigDockAccentGradient } from "@/api";
import { fromHsl, interpolateHsl, toHsl } from "@/utils/color";

export type TileGradient = readonly [string, string];

export const DEFAULT_DOCK_ACCENT_GRADIENT = {
  startColor: "",
  endColor: "",
  rangeStart: 0,
  rangeEnd: 100,
} satisfies ConfigDockAccentGradient;

/* The old accent-family palette spanned this same arc around the theme accent.
   Keeping it as the derived endpoint default preserves that familiar family,
   while the settings editor can replace either endpoint explicitly. */
const DEFAULT_ACCENT_HUE_SPREAD = 60;

export interface ResolvedDockAccentGradient {
  endColor: string;
  rangeEnd: number;
  rangeStart: number;
  startColor: string;
}

const clampPercent = (value: number) =>
  Math.min(100, Math.max(0, Number.isFinite(value) ? value : 0));

export function defaultDockAccentColors(accent: string): TileGradient {
  const hsl = toHsl(accent);
  if (!hsl) return [accent, accent];

  const halfSpread = DEFAULT_ACCENT_HUE_SPREAD / 2;
  return [
    fromHsl(hsl.h - halfSpread, hsl.s, hsl.l),
    fromHsl(hsl.h + halfSpread, hsl.s, hsl.l),
  ];
}

export function resolveDockAccentGradient(
  accent: string,
  value?: ConfigDockAccentGradient,
): ResolvedDockAccentGradient {
  const [defaultStart, defaultEnd] = defaultDockAccentColors(accent);
  const rangeStart = clampPercent(
    value?.rangeStart ?? DEFAULT_DOCK_ACCENT_GRADIENT.rangeStart,
  );
  const rangeEnd = Math.max(
    rangeStart,
    clampPercent(value?.rangeEnd ?? DEFAULT_DOCK_ACCENT_GRADIENT.rangeEnd),
  );

  return {
    startColor: value?.startColor || defaultStart,
    endColor: value?.endColor || defaultEnd,
    rangeStart,
    rangeEnd,
  };
}

function sampleFullDockAccentGradient(
  accent: string,
  value: ConfigDockAccentGradient | undefined,
  position: number,
): string {
  const clampedPosition = Math.min(1, Math.max(0, position));

  // With no custom endpoints, sample directly from the accent instead of
  // round-tripping the two rendered endpoint colors. That preserves the old
  // accent-family palette exactly, including the accent at its midpoint.
  if (!value?.startColor && !value?.endColor) {
    const hsl = toHsl(accent);
    if (hsl) {
      return fromHsl(
        hsl.h + (clampedPosition - 0.5) * DEFAULT_ACCENT_HUE_SPREAD,
        hsl.s,
        hsl.l,
      );
    }
  }

  const resolved = resolveDockAccentGradient(accent, value);
  return interpolateHsl(
    resolved.startColor,
    resolved.endColor,
    clampedPosition,
  );
}

/** Samples a position across the selected slice of the full endpoint blend. */
export function sampleDockAccentColor(
  accent: string,
  value: ConfigDockAccentGradient | undefined,
  position: number,
): string {
  const resolved = resolveDockAccentGradient(accent, value);
  const clampedPosition = Math.min(1, Math.max(0, position));
  const fullGradientPosition =
    (resolved.rangeStart +
      (resolved.rangeEnd - resolved.rangeStart) * clampedPosition) /
    100;

  return sampleFullDockAccentGradient(accent, value, fullGradientPosition);
}

/** A multi-stop rail that matches the same HSL sampling used by dock tiles. */
export function dockAccentGradientCss(
  accent: string,
  value?: ConfigDockAccentGradient,
): string {
  const stops = Array.from({ length: 13 }, (_, index) => {
    const position = index / 12;
    return `${sampleFullDockAccentGradient(accent, value, position)} ${Math.round(position * 100)}%`;
  });
  return `linear-gradient(90deg, ${stops.join(", ")})`;
}
