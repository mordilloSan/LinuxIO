import type { CSSProperties } from "react";

import type { DockTileColors } from "@/api";
import { useConfigValue } from "@/hooks/useConfig";
import { useUpdateCanNavigate } from "@/hooks/useLinuxIOUpdater";
import { useAppMediaQuery, useAppTheme } from "@/theme";
import { fromHsl, lighten, toHsl } from "@/utils/color";

import DockItem from "./DockItem";
import {
  DockMagnificationProvider,
  useDockPointerLiveness,
} from "./useDockMagnification";
import { useSidebarItems } from "../sidebar/useSidebarItems";
import "./dock.css";

type TileGradient = readonly [string, string];

/* Fixed per-route gradients (top → bottom), macOS-app-icon style. Routes
   without an entry fall back to cycling through this palette. Owes nothing to
   the theme, which is why it is no longer the default — see the accent
   palettes below. */
const VIBRANT_GRADIENTS: Record<string, TileGradient> = {
  "/": ["#4fa8f8", "#1670e0"],
  "/accounts": ["#ff86a3", "#e83e64"],
  "/docker": ["#57a8ff", "#1d63ed"],
  "/filebrowser/$": ["#ffd968", "#f2a93b"],
  "/hardware": ["#6f8fb8", "#3c5a80"],
  "/logs": ["#aab4c2", "#6b7686"],
  "/network": ["#3fd4e0", "#0aa3b5"],
  "/services": ["#b57af2", "#7c3fd6"],
  "/settings": ["#c3ccd8", "#5f6a78"],
  "/shares": ["#7f96f7", "#4356d6"],
  "/storage": ["#ff9f5a", "#ee6c1d"],
  "/terminal": ["#43494f", "#17191c"],
  "/updates": ["#63de74", "#22a83a"],
};

const VIBRANT_FALLBACK = Object.values(VIBRANT_GRADIENTS);

/* Total hue arc the accent palette spans, centered on the accent. Wide enough
   to tell one tile from the next, narrow enough that every tile still reads as
   the same color family. */
const ACCENT_HUE_SPREAD = 60;

/* Neutral tiles keep a trace of the accent hue rather than being flat gray, and
   are clamped to a lightness band that keeps a white icon legible whatever
   accent the user picked. */
const NEUTRAL_SATURATION = 0.08;
const NEUTRAL_MIN_LIGHTNESS = 0.38;
const NEUTRAL_MAX_LIGHTNESS = 0.55;

/* A lit face over the base color: the same 26% white top that the notification
   icons use (.app-navbar-notifications__icon), so a status icon and a dock tile
   read as one object at two sizes. */
const tileGradient = (base: string): TileGradient => [
  lighten(base, 0.26),
  base,
];

const neutralBase = (accent: string) => {
  const hsl = toHsl(accent);
  if (!hsl) return "#6b7686";
  return fromHsl(
    hsl.h,
    NEUTRAL_SATURATION,
    Math.min(NEUTRAL_MAX_LIGHTNESS, Math.max(NEUTRAL_MIN_LIGHTNESS, hsl.l)),
  );
};

/* Fanned left to right across the arc so the row reads as a ramp through one
   family. Hue is the only component that moves — saturation and lightness are
   what make a palette look noisy, and those come from the accent unchanged. */
const accentFamilyGradient = (
  accent: string,
  index: number,
  count: number,
): TileGradient => {
  const hsl = toHsl(accent);
  if (!hsl || count < 2) return tileGradient(accent);
  const offset = (index / (count - 1) - 0.5) * ACCENT_HUE_SPREAD;
  return tileGradient(fromHsl(hsl.h + offset, hsl.s, hsl.l));
};

const gradientFor = (
  palette: DockTileColors,
  accent: string,
  to: string,
  index: number,
  count: number,
): TileGradient => {
  switch (palette) {
    case "vibrant":
      return (
        VIBRANT_GRADIENTS[to] ??
        VIBRANT_FALLBACK[index % VIBRANT_FALLBACK.length]
      );
    case "mono":
      return tileGradient(accent);
    case "neutral":
      return tileGradient(neutralBase(accent));
    default:
      return accentFamilyGradient(accent, index, count);
  }
};

const Dock = () => {
  const items = useSidebarItems();
  const canNavigate = useUpdateCanNavigate();
  const theme = useAppTheme();
  const [dockTileColors] = useConfigValue("dockTileColors");
  const magnificationEnabled = useAppMediaQuery(theme.breakpoints.up("sm"));
  const { navRef, onPointerLeave, onPointerMove } =
    useDockPointerLiveness(magnificationEnabled);

  const palette = dockTileColors ?? "accent";
  const accent = theme.palette.primary.main;

  /* Only the neutral palette lights the current route differently, and which
     tile that is already lives in CSS as .app-dock-link--active. Publishing the
     accent pair here lets that rule do the swap without the dock having to
     track the active route itself. */
  const activeGradient = palette === "neutral" ? tileGradient(accent) : null;

  return (
    <nav
      aria-label="Primary navigation"
      className="app-dock"
      onPointerLeave={onPointerLeave}
      onPointerMove={onPointerMove}
      ref={navRef}
      style={
        activeGradient
          ? ({
              "--dock-tile-active-top": activeGradient[0],
              "--dock-tile-active-bottom": activeGradient[1],
            } as CSSProperties)
          : undefined
      }
    >
      <ul className="app-dock__list">
        {items.map((page, index) => (
          <DockItem
            {...page}
            disabled={!canNavigate}
            gradient={gradientFor(
              palette,
              accent,
              page.to,
              index,
              items.length,
            )}
            key={page.title}
          />
        ))}
      </ul>
    </nav>
  );
};

const DockWithMagnification = () => (
  <DockMagnificationProvider>
    <Dock />
  </DockMagnificationProvider>
);

export default DockWithMagnification;
